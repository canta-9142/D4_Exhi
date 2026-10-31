"""連続運転の1筐体を、サーバーの代わりに通常ファームウェアへ通す実機確認。

ノードA（P1）を受け付け中（manual）で識別し、停止中（Preparing）から始める。
筐体の段階はサーバー仕様3.1のとおりに進める。待機（Idle）→両端の2秒長押し（ready）→
モード選択（Select、20秒）→左から3番目で通常ラウンド（Playing、3秒＋90秒）→結果（Finished、30秒）、
または左から2番目で練習ラウンド（無操作60秒、長押しでモード選択へ）。
長押し・選択・送信は実機のボタンで行い、受け取った要求を仕様の受理条件で確かめて応答する。

待機・モード選択・結果・練習中は、サーバーと同じく実際のFIFOを自動転送する（1秒に1件生成し、
0.5秒ごとに先頭を転送して`reset`を送る。結果の段階ではphase・status・summaryも続ける）。
練習中は練習用キュー（常に3件）を送り、実際のFIFOは送らない。
得点は受理した`send`ごとに発信者の15点とする（転送先での得点の判定はしない）。

標準入力のコマンド:
  start / stop            運転の開始・停止（打ち切り）
  bot                     ボットに戻す（打ち切り。モードは変えない）
  auto / manual           操作モードの切替（autoはラウンドを打ち切る）
  ack accept|reject|ignore  ready・chooseへの応答（拒否はstatusだけ、ignoreは何も返さない）
  top N|none / bottom     次の結果の上位%（1〜50）、なし、下位50%
  bots A B C              練習中のB1〜B3の使用量（0〜4）
  quit                    終了（接続待ちへ戻す）
"""

import argparse
import json
import math
import os
from pathlib import Path
import queue
import random
import select
import sys
import threading
import time
import uuid
from player_cdc import CDC

NODE = 1
NEIGHBORS = (2, 9, 11, 8)  # ノードAの下部の4ボタン（player.md）。
SELECT_SECONDS, COUNTDOWN_SECONDS, RESULT_SECONDS, PRACTICE_IDLE_SECONDS = 20, 3, 30, 60
PRACTICE_LENGTH = 3


def ceil_seconds(value):
    return max(0, math.ceil(round(value, 6)))


class Station:
    """サーバーのapp（連続運転）のうち、1筐体分の段階・FIFO・通知を真似る。"""

    def __init__(self, cdc, clock, round_seconds=90, first_pid=0, ack="accept", out=print, seed=None):
        self.cdc, self.clock, self.round_seconds, self.out = cdc, clock, round_seconds, out
        self.first_pid, self.ack = first_pid, ack
        self.random = random.Random(seed)
        self.mode, self.running, self.station, self.round = "manual", False, "stopped", None
        self.fifo, self.practice = [], []
        self.next_pid, self.practice_pid = first_pid, 0
        self.sync, self.last_rid = "", -1
        self.score, self.combo, self.gains = 0, 0, []
        self.round_start = self.round_end = self.deadline = 0.0
        self.next_generation = self.next_transfer = None
        self.top, self.bottom, self.bots = None, False, [0, 0, 0]
        self.scores = []  # この接続で確定した通常ラウンドの得点（ベストスコアの代わり）。
        self.summary = None
        self.sent_key, self.sent_countdown = None, 0
        self.passed = []

    # --- 表示値 -----------------------------------------------------------
    def practicing(self):
        return self.station == "playing" and self.round == "tutorial"

    def view(self, now):
        """player.mdの「段階ごとの値」の表に従うstatus（gainsを除く）。"""
        v = {"score": 0, "remaining": 0, "ready": False, "countdown": 0, "round": None, "window": 0,
             "rank": None, "combo": 0}
        if self.station == "select":
            v["window"] = ceil_seconds(self.deadline - now)
        elif self.station == "playing":
            v["round"] = self.round
            v["countdown"] = ceil_seconds(self.round_start - now)
            if self.round == "normal":
                v["remaining"] = ceil_seconds(self.round_end - max(now, self.round_start))
                v["score"], v["combo"] = self.score, self.combo
            else:
                v["window"] = min(ceil_seconds(self.deadline - max(now, self.round_start)), PRACTICE_IDLE_SECONDS)
                v["bots"] = list(self.bots)
        elif self.station == "finished":
            v.update(score=self.score, combo=self.combo, round="normal", window=ceil_seconds(self.deadline - now))
        return v

    def phase_name(self):
        return {"stopped": "Preparing", "idle": "Idle", "select": "Select", "playing": "Playing",
                "finished": "Finished"}[self.station]

    # --- 送信 -------------------------------------------------------------
    def send_status(self, force=False):
        v = self.view(self.clock())
        key = json.dumps(v, sort_keys=True)
        if not force and key == self.sent_key and not self.gains:
            return
        self.sent_key, self.sent_countdown = key, v["countdown"]
        bots = v.pop("bots", None)
        self.cdc.status(gains=self.gains[:16], bots=bots, **v)
        self.gains = []

    def send_reset(self):
        self.sync = uuid.uuid4().hex.upper()
        packets = self.practice if self.practicing() else self.fifo
        self.cdc.send("reset", sync=self.sync, packets=[dict(p) for p in packets])

    def send_summary(self):
        self.cdc.send("summary", sync=self.sync, **self.summary)

    def synchronize(self):
        """初期同期・再同期: reset、phase、status、結果ならsummary。"""
        self.send_reset()
        self.cdc.send("phase", phase=self.phase_name())
        self.gains = []
        self.send_status(force=True)
        if self.station == "finished":
            self.send_summary()

    # --- 実際のFIFO -------------------------------------------------------
    def allocate_pid(self):
        used = {p["pid"] for p in self.fifo}
        while True:
            pid, self.next_pid = self.next_pid, (self.next_pid + 1) % 65536
            if pid not in used:
                return pid

    def automatic(self):
        return not (self.mode == "manual" and self.station == "playing" and self.round == "normal")

    def advance_network(self, now):
        if not self.running:
            return
        while self.next_generation is not None and self.next_generation <= now:
            self.next_generation += 1
            p = {"pid": self.allocate_pid(), "dst": self.random.randint(2, 8)}
            self.fifo.append(p)
            if not self.practicing():
                self.cdc.send("push", **p)
            if self.next_transfer is None and self.automatic():
                self.next_transfer = self.next_generation - 1 + 0.5
        while self.next_transfer is not None and self.next_transfer <= now:
            if not self.automatic() or not self.fifo:
                self.next_transfer = None
                break
            self.fifo.pop(0)
            self.next_transfer = self.next_transfer + 0.5 if self.fifo else None
            if not self.practicing():
                self.fifo_changed()

    def fifo_changed(self):
        """自動転送で先頭が抜けたとき。サーバーと同じく新しい同期世代のresetを送る。
        結果の段階では、その後にphase(Finished)・status・新しい世代のsummaryを続ける（server.md 5.2）。"""
        self.send_reset()
        if self.station == "finished":
            self.cdc.send("phase", phase="Finished")
            self.send_status(force=True)
            self.send_summary()

    def reschedule(self, now):
        self.next_transfer = now + 0.5 if self.automatic() and self.fifo else None

    # --- 段階の移り変わり ---------------------------------------------------
    def enter(self, station, notify=True):
        """待機かモード選択へ移り、ラウンドと結果を捨てる（打ち切りは記録しない）。"""
        now = self.clock()
        was_practice = self.practicing()
        self.station, self.round, self.summary = station, None, None
        self.score, self.combo, self.gains, self.practice = 0, 0, [], []
        self.deadline = now + SELECT_SECONDS if station == "select" else 0.0
        self.reschedule(now)
        if notify:
            self.cdc.send("phase", phase=self.phase_name())
            if was_practice:
                self.send_reset()
            self.send_status(force=True)
        self.report(f"station {self.phase_name()}")

    def refill_practice(self):
        added = []
        while len(self.practice) < PRACTICE_LENGTH:
            pid, self.practice_pid = self.practice_pid, self.practice_pid + 1
            if any(p["pid"] == pid for p in self.practice):
                continue
            p = {"pid": pid, "dst": self.random.randint(2, 8)}
            self.practice.append(p)
            added.append(p)
        return added

    def choose(self, round_kind):
        now = self.clock()
        self.station, self.round = "playing", round_kind
        self.round_start = now + COUNTDOWN_SECONDS
        self.score, self.combo, self.gains = 0, 0, []
        if round_kind == "normal":
            self.round_end = self.deadline = self.round_start + self.round_seconds
        else:
            self.deadline = self.round_start + PRACTICE_IDLE_SECONDS
            self.practice, self.practice_pid = [], 0
            self.refill_practice()
        self.reschedule(now)
        self.cdc.send("phase", phase="Playing")
        if round_kind == "tutorial":
            self.send_reset()
        self.send_status(force=True)
        self.report(f"{round_kind} round, countdown {COUNTDOWN_SECONDS}")

    def finish(self):
        """通常ラウンドの結果を確定する: phase(Finished)、status、summary。"""
        now = self.clock()
        self.scores.append(self.score)
        best = max(self.scores)
        mine = best == self.score
        jst = time.gmtime(time.time() + 9 * 3600)
        self.summary = {"score": self.score, "rank": None, "eligible": True, "topPercent": self.top,
                        "bottomHalf": self.bottom, "gameBest": self.score, "gameBestNodes": [NODE],
                        "hourBest": best, "hourBestStart": jst.tm_hour, "hourBestMine": mine,
                        "allTimeBest": best, "allTimeBestMine": mine}
        self.station, self.deadline, self.gains = "finished", now + RESULT_SECONDS, []
        self.reschedule(now)
        self.cdc.send("phase", phase="Finished")
        self.send_status(force=True)
        self.send_summary()
        self.report(f"result score={self.score} topPercent={self.top} bottomHalf={self.bottom}")

    def expire(self):
        if self.station == "playing" and self.round == "normal":
            self.finish()
        else:
            self.enter("idle")

    def tick(self):
        now = self.clock()
        if self.running and self.station in ("select", "playing", "finished") and self.deadline <= now:
            self.expire()
        self.advance_network(now)
        if self.running:
            self.send_status()

    # --- MCUからの要求 ------------------------------------------------------
    def handle(self, message):
        kind, data = message["type"], message["data"]
        now = self.clock()
        if kind == "resync":
            self.synchronize()
            self.report("resync answered")
        elif kind == "ready":
            assert set(data) == {"sync"}, message
            accepted = (data["sync"] == self.sync and self.mode == "manual" and self.running and
                        (self.station in ("idle", "finished") or self.practicing()))
            self.report(f"READY in {self.phase_name()} accepted={accepted} ack={self.ack}")
            if accepted and self.ack == "accept":
                self.enter("select")
            elif self.ack != "ignore":
                self.send_status(force=True)
        elif kind == "choose":
            assert set(data) == {"sync", "round"} and data["round"] in ("normal", "tutorial"), message
            assert self.mode == "manual", message
            # 時間切れと行き違ったchooseは、サーバーと同じく受理せずstatusを返す。
            accepted = data["sync"] == self.sync and self.station == "select"
            self.report(f"CHOOSE {data['round']} accepted={accepted} ack={self.ack}")
            if accepted and self.ack == "accept":
                self.choose(data["round"])
            elif self.ack != "ignore":
                self.send_status(force=True)
        elif kind == "send":
            assert set(data) == {"sync", "rid", "pid", "to"} and data["to"] in NEIGHBORS, message
            assert data["rid"] > self.last_rid, message
            self.last_rid = data["rid"]
            # MCUはautoでは送らず、送ったcountdownが1以上の間も送らない（player.md）。
            assert self.mode == "manual" and not self.sent_countdown, message
            head = (self.practice if self.practicing() else self.fifo)[:1]
            if data["sync"] != self.sync:
                self.cdc.result(data, False)
                self.report("stale SEND rejected")
            elif self.station != "playing" or not head or head[0]["pid"] != data["pid"] or now < self.round_start:
                # ラウンドの終了・打ち切りと行き違ったsendも、現在の世代なら拒否してキューを送り直す。
                self.cdc.result(data, False)
                self.fifo_changed()
                self.report("SEND rejected; queue resynchronized")
            elif self.practicing():
                self.practice.pop(0)
                self.deadline = now + PRACTICE_IDLE_SECONDS
                self.cdc.result(data)
                self.cdc.send("push", **self.refill_practice()[0])
                self.send_status()
                self.report("practice SEND/RESULT/PUSH; idle timer restarted")
            else:
                self.fifo.pop(0)
                self.score += 15
                self.combo = min(self.combo + 1, 999)
                self.gains.append(15)
                self.cdc.result(data)
                self.send_status()
                self.report(f"SEND/RESULT score={self.score}")
        else:
            raise AssertionError(message)

    # --- 管理操作 -----------------------------------------------------------
    def command(self, text):
        words = text.split()
        if not words:
            return True
        now = self.clock()
        if text == "quit":
            return False
        if text == "start" and not self.running:
            self.running, self.fifo, self.next_pid = True, [], self.first_pid
            self.station, self.round = "idle", None
            self.next_generation, self.next_transfer = now + 1, None
            self.cdc.send("phase", phase="Idle")
            self.send_reset()
            self.send_status(force=True)
            self.report("operation started")
        elif text == "stop" and self.running:
            self.running, self.fifo, self.practice = False, [], []
            self.next_generation = self.next_transfer = None
            self.station, self.round, self.summary = "stopped", None, None
            self.score, self.combo, self.gains = 0, 0, []
            self.cdc.send("phase", phase="Preparing")
            self.send_reset()
            self.send_status(force=True)
            self.report("operation stopped")
        elif text == "bot" and self.running and self.station != "idle":
            self.enter("idle")
        elif text in ("auto", "manual") and text != self.mode:
            self.mode = text
            if text == "auto" and self.running and self.station != "idle":
                self.enter("idle", notify=False)
            self.reschedule(now)
            self.cdc.send("config", node=NODE, mode=self.mode, preview=False)
            self.synchronize()
            self.report(f"mode {self.mode}")
        elif len(words) == 2 and words[0] == "ack" and words[1] in ("accept", "reject", "ignore"):
            self.ack = words[1]
        elif len(words) == 2 and words[0] == "top" and (words[1] == "none" or words[1].isdigit() and 1 <= int(words[1]) <= 50):
            self.top, self.bottom = (None if words[1] == "none" else int(words[1])), False
        elif text == "bottom":
            self.top, self.bottom = None, True
        elif len(words) == 4 and words[0] == "bots" and all(w in "01234" and len(w) == 1 for w in words[1:]):
            self.bots = [int(w) for w in words[1:]]
            if self.practicing():
                self.send_status()
        else:
            self.out(f"IGNORED: {text}", flush=True)
            return True
        self.out(f"STATE: {self.phase_name()} mode={self.mode} round={self.round} score={self.score}", flush=True)
        return True

    def report(self, event):
        self.passed.append(event)
        self.out(f"PASS: {event}", flush=True)


def connect(cdc, station):
    cdc.write(b"\n")
    cdc.discard_startup()
    cdc.send("identify")
    hello = cdc.expect("hello")
    cdc.send("config", node=NODE, mode=station.mode, preview=False)
    cdc.expect("resync")
    station.synchronize()
    return hello


def run(cdc, station, commands, seconds, log=None):
    """commandsは管理操作の行を受け取るqueue.Queue。Noneを受け取ると終える。"""
    started = time.monotonic()
    while time.monotonic() - started < seconds:
        try:
            while True:
                line = commands.get_nowait()
                if line is None or not station.command(line.strip()):
                    return
        except queue.Empty:
            pass
        station.tick()
        message = cdc.read(.02)
        if message:
            if log:
                with log.open("a") as output:
                    output.write(json.dumps(message, separators=(",", ":")) + "\n")
            station.handle(message)


def read_stdin(commands):
    buffer = ""
    while True:
        select.select([sys.stdin], [], [])
        raw = os.read(sys.stdin.fileno(), 4096)
        if not raw:
            commands.put(None)
            return
        buffer += raw.decode()
        while "\n" in buffer:
            line, buffer = buffer.split("\n", 1)
            commands.put(line)


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--port", default="/dev/ttyACM0")
    parser.add_argument("--seconds", type=float, default=1800)
    parser.add_argument("--round-seconds", type=int, default=90, help="通常ラウンドのゲーム時間（短くして確かめる）")
    parser.add_argument("--first-pid", type=int, default=0, help="運転開始時のPID（65530などで周回を確かめる）")
    parser.add_argument("--ack", choices=("accept", "reject", "ignore"), default="accept")
    parser.add_argument("--log", type=Path)
    args = parser.parse_args()
    assert 1 <= args.round_seconds <= 120 and 0 <= args.first_pid <= 65535
    cdc = CDC(args.port)
    station = Station(cdc, time.monotonic, args.round_seconds, args.first_pid, args.ack)
    commands = queue.Queue()
    threading.Thread(target=read_stdin, args=(commands,), daemon=True).start()
    try:
        connect(cdc, station)
        print("READY: stopped (Preparing)/manual. Type start, then hold both ends for 2 seconds.", flush=True)
        print("Commands: start/stop/bot/auto/manual/ack accept|reject|ignore/top N|none/bottom/bots A B C/quit",
              flush=True)
        run(cdc, station, commands, args.seconds, args.log)
    except KeyboardInterrupt:
        print("Stopping endless operation test", flush=True)
    finally:
        try:
            cdc.discard_startup()
            cdc.send("identify")
            cdc.expect("hello")
        finally:
            cdc.close()
        print("CLEANUP: CONNECTING", flush=True)


if __name__ == "__main__":
    main()
