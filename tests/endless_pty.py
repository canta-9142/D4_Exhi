"""endless_cdc.pyを実機なしで確かめる。疑似TTYの片側でStationを動かし、反対側の疑似MCUが
ファームウェアと同じ規則（player.md）で通知を検証しながら、長押し・選択・送信を送る。

待機→選択→通常ラウンド→結果→待機、結果からの再挑戦、練習の出入り、各段階の時間切れ、
打ち切り（ボットに戻す・自動への切替・運転停止）、応答のないreadyからの再同期、PIDの周回を通す。
時間は20倍で進める（ゲームの1秒が実時間の50ms）。確かめるのはこのツールの送信順序と値であり、
サーバーやファームウェアそのものではない。
"""

import io
import json
import os
import queue
import re
import select
import threading
import time
from endless_cdc import Station, connect, run
import player_cdc
from player_cdc import CDC

# CDC.readが受信のたびに出す「MCU: ...」の行を出さない（このテストの出力だけを残す）。
player_cdc.print = lambda *args, **kwargs: None

SPEED = 20
ROUND_SECONDS = 10
FIRST_PID = 65533
STATUS_KEYS = {"score", "remaining", "ready", "countdown", "round", "window", "rank", "gains", "combo"}
SUMMARY_KEYS = {"sync", "score", "rank", "eligible", "topPercent", "bottomHalf", "gameBest", "gameBestNodes",
                "hourBest", "hourBestStart", "hourBestMine", "allTimeBest", "allTimeBestMine"}
PHASES = {"Preparing", "Idle", "Select", "Playing", "Finished"}


def integer(value, low, high):
    return type(value) is int and low <= value <= high


class FakeMCU:
    """ファームウェアの受信側の検証だけを真似る。不正な通知はAssertionErrorにする。"""

    def __init__(self, fd):
        self.fd, self.buffer = fd, b""
        self.events = []  # 受け取った(type, data)を順に。
        self.mode, self.phase, self.sync, self.status = None, None, None, None
        self.queue, self.syncs, self.pending, self.rid = [], set(), None, 0

    def write(self, kind, **data):
        os.write(self.fd, (json.dumps({"type": kind, "data": data}, separators=(",", ":")) + "\n").encode())

    def pump(self, timeout):
        if not select.select([self.fd], [], [], timeout)[0]:
            return
        self.buffer += os.read(self.fd, 65536)
        while b"\n" in self.buffer:
            line, self.buffer = self.buffer.split(b"\n", 1)
            if line.strip():
                message = json.loads(line)
                self.apply(message["type"], message["data"])

    def apply(self, kind, data):
        self.events.append((kind, data))
        if kind == "identify":
            self.mode, self.phase, self.status = None, None, None
            self.write("hello", uid="0011223344AA")
        elif kind == "config":
            assert integer(data["node"], 1, 8) and data["mode"] in ("manual", "auto") and data["preview"] is False, data
            self.mode, self.phase, self.status, self.pending = data["mode"], None, None, None
            self.write("resync")
        elif kind == "reset":
            assert re.fullmatch(r"[0-9A-F]{32}", data["sync"]) and data["sync"] not in self.syncs, data
            pids = [p["pid"] for p in data["packets"]]
            assert len(pids) == len(set(pids)) <= 1024 and all(integer(p, 0, 65535) for p in pids), data
            assert all(integer(p["dst"], 1, 11) for p in data["packets"]), data
            self.syncs.add(data["sync"])
            self.sync, self.queue, self.pending = data["sync"], list(data["packets"]), None
        elif kind == "push":
            assert integer(data["pid"], 0, 65535) and integer(data["dst"], 1, 11), data
            # ファームウェアはキャッシュ内のPID重複を再同期にする。周回でも重複させない。
            assert data["pid"] not in [p["pid"] for p in self.queue], (data, self.queue)
            self.queue.append(data)
        elif kind == "phase":
            assert data["phase"] in PHASES, data
            self.phase = data["phase"]
        elif kind == "status":
            keys = set(data)
            assert keys == STATUS_KEYS or keys == STATUS_KEYS | {"bots"}, data
            assert integer(data["score"], 0, 2147483647) and integer(data["remaining"], 0, 120), data
            assert data["ready"] is False and integer(data["countdown"], 0, 3) and data["rank"] is None, data
            assert data["round"] in (None, "normal", "tutorial") and integer(data["window"], 0, 60), data
            assert type(data["gains"]) is list and len(data["gains"]) <= 16, data
            assert all(integer(g, 1, 15) for g in data["gains"]) and integer(data["combo"], 0, 999), data
            if "bots" in data:
                assert len(data["bots"]) == 3 and all(integer(b, 0, 4) for b in data["bots"]), data
            # 段階ごとの値（player.mdの表）。botsは練習ラウンドの間だけ。
            phase = self.phase
            practice = phase == "Playing" and data["round"] == "tutorial"
            assert ("bots" in data) == practice, (phase, data)
            if phase in ("Preparing", "Idle"):
                assert data == {**data, "score": 0, "remaining": 0, "countdown": 0, "round": None, "window": 0,
                                "combo": 0}, (phase, data)
            elif phase == "Select":
                assert data["round"] is None and data["window"] <= 20 and data["score"] == 0, data
            elif phase == "Finished":
                assert data["round"] == "normal" and data["window"] <= 30 and data["gains"] == [], data
            elif practice:
                assert data["score"] == data["combo"] == data["remaining"] == 0 and data["gains"] == [], data
            elif phase == "Playing":
                assert data["round"] == "normal" and data["window"] == 0 and data["remaining"] <= ROUND_SECONDS, data
                if data["countdown"]:
                    assert data["remaining"] == ROUND_SECONDS and data["score"] == 0, data
            self.status = data
        elif kind == "summary":
            assert set(data) == SUMMARY_KEYS, data
            assert self.phase == "Finished" and data["sync"] == self.sync, (self.phase, data, self.sync)
            assert data["rank"] is None and data["eligible"] is True, data
            assert data["gameBest"] == data["score"] and data["gameBestNodes"] == [1], data
            assert not data["bottomHalf"] or data["topPercent"] is None, data
            for best, mine in (("hourBest", "hourBestMine"), ("allTimeBest", "allTimeBestMine")):
                assert not data[mine] or data[best] == data["score"], data
        elif kind == "result":
            pending, self.pending = self.pending, None
            assert pending and data["sync"] == pending["sync"] and data["rid"] == pending["rid"], (data, pending)
            if data["ok"]:
                assert self.queue[0]["pid"] == pending["pid"], (self.queue, pending)
                self.queue.pop(0)
        else:
            raise AssertionError((kind, data))

    # --- 操作 -------------------------------------------------------------
    def mark(self):
        return len(self.events)

    def since(self, mark):
        return self.events[mark:]

    def wait(self, predicate, timeout=5, what=""):
        deadline = time.monotonic() + timeout
        while not predicate():
            assert time.monotonic() < deadline, f"timed out waiting for {what}: {self.events[-8:]}"
            self.pump(.02)

    def wait_types(self, mark, types, what="", phase=None):
        """mark以降に、typesがこの順で続けて届くまで待ち、その通知を返す。
        phaseを指定すると、その中のphaseの通知がその値のものだけを数える。"""
        def match(events, i):
            window = events[i:i + len(types)]
            return [kind for kind, _ in window] == types and (
                phase is None or all(data == {"phase": phase} for kind, data in window if kind == "phase"))

        def found():
            events = self.since(mark)
            return next((i for i in range(len(events)) if match(events, i)), None)
        self.wait(lambda: found() is not None, what=what or str(types))
        i = found()
        return [data for _, data in self.since(mark)[i:i + len(types)]]

    def ready(self):
        self.write("ready", sync=self.sync)

    def choose(self, round_kind):
        self.write("choose", sync=self.sync, round=round_kind)

    def send_head(self, to=2):
        head = self.queue[0]
        self.pending = {"sync": self.sync, "rid": self.rid, "pid": head["pid"]}
        self.write("send", sync=self.sync, rid=self.rid, pid=head["pid"], to=to)
        self.rid += 1


def scenario(mcu, commands):
    def command(text):
        commands.put(text)

    def enter_select():
        mark = mcu.mark()
        mcu.ready()
        if mcu.phase == "Finished":
            phase, reset, status = mcu.wait_types(mark, ["phase", "reset", "status"], "Select", "Select")
        else:
            phase, status = mcu.wait_types(mark, ["phase", "status"], "Select", "Select")
        assert phase == {"phase": "Select"} and status["window"] == 20, (phase, status)

    def start_round(kind):
        enter_select()
        mark = mcu.mark()
        mcu.choose(kind)
        if kind == "normal":
            phase, status = mcu.wait_types(mark, ["phase", "status"], "normal round", "Playing")
            assert status["countdown"] == 3 and status["round"] == "normal" and status["remaining"] == ROUND_SECONDS
        else:
            phase, reset, status = mcu.wait_types(mark, ["phase", "reset", "status"], "practice round", "Playing")
            assert len(reset["packets"]) == 3 and status["countdown"] == 3 and status["window"] == 60
            assert len(status["bots"]) == 3
        assert phase == {"phase": "Playing"}
        mcu.wait(lambda: mcu.status["countdown"] == 0, what="the end of the countdown")
        assert [s["countdown"] for k, s in mcu.since(mark) if k == "status"][:4] == [3, 2, 1, 0]

    # 停止中から運転開始。
    mcu.wait(lambda: mcu.phase == "Preparing" and mcu.status is not None, what="the stopped station")
    mark = mcu.mark()
    command("start")
    phase, reset, status = mcu.wait_types(mark, ["phase", "reset", "status"], "start", "Idle")
    assert phase == {"phase": "Idle"} and reset["packets"] == [] and status["window"] == 0
    # 待機ではボットとしてFIFOが流れる（push、自動転送のreset）。
    mcu.wait(lambda: any(k == "push" for k, _ in mcu.since(mark)) and
             sum(k == "reset" for k, _ in mcu.since(mark)) >= 2, what="the idle FIFO")
    print("PASS: Preparing → start → Idle (phase, empty reset, status); the FIFO flows as a bot")

    # 待機→選択→通常ラウンド→結果。
    start_round("normal")
    mcu.wait(lambda: mcu.queue, what="a packet in the normal round")
    mark = mcu.mark()
    mcu.send_head()
    mcu.wait(lambda: mcu.status["score"] == 15, what="the score")
    assert [k for k, _ in mcu.since(mark)][:2] == ["result", "status"]
    assert mcu.status["gains"] == [15] and mcu.status["combo"] == 1
    mark = mcu.mark()
    mcu.wait(lambda: mcu.phase == "Finished", timeout=ROUND_SECONDS / SPEED + 3, what="the result")
    phase, status, summary = mcu.wait_types(mark, ["phase", "status", "summary"], "the result", "Finished")
    assert status["score"] == summary["score"] == 15 and status["window"] == 30 and status["combo"] == 1
    remaining = [s["remaining"] for k, s in mcu.events if k == "status" and s["round"] == "normal" and not s["window"]]
    # 90秒（ここでは10秒）の終わりで直ちに結果へ移るので、Playingのremainingは1で終わる（サーバーと同じ）。
    assert remaining[-1] == 1 and remaining == sorted(remaining, reverse=True), remaining
    print("PASS: Idle → ready → Select (window 20) → choose normal → countdown 3-2-1-0 → SEND/RESULT/STATUS → "
          "Finished (phase, status, summary with rank null)")

    # 結果中はFIFOが動いても同期世代と結果を保持する。明示的なresyncでは取り直せる。
    mark = mcu.mark()
    result_sync = mcu.sync
    mcu.wait(lambda: mcu.status["window"] <= 27, what="the result without FIFO updates")
    assert mcu.sync == result_sync and all(k == "status" for k, _ in mcu.since(mark)), mcu.since(mark)
    mark = mcu.mark()
    mcu.write("resync")
    reset, phase, status, summary = mcu.wait_types(mark, ["reset", "phase", "status", "summary"], "a result reset", "Finished")
    assert summary["sync"] == reset["sync"] != result_sync and phase == {"phase": "Finished"}
    print("PASS: result keeps its synchronization during FIFO traffic; explicit resync restores FIFO and summary")

    # 結果からの再挑戦（長押し）→練習。
    start_round("tutorial")
    mark = mcu.mark()
    command("bots 1 4 0")
    mcu.wait(lambda: mcu.status.get("bots") == [1, 4, 0], what="bots")
    mark = mcu.mark()
    mcu.send_head(to=9)
    result, push = mcu.wait_types(mark, ["result", "push"], "the practice refill")
    assert result["ok"] and len(mcu.queue) == 3
    print("PASS: Finished → ready → Select → choose tutorial (phase, practice reset of 3, status with bots) → "
          "bots update → practice SEND/RESULT/PUSH")

    # 練習中の長押し→選択（実際のFIFOのreset）。
    mark = selected = mcu.mark()
    mcu.ready()
    phase, reset, status = mcu.wait_types(mark, ["phase", "reset", "status"], "leaving practice", "Select")
    assert phase == {"phase": "Select"} and status["window"] == 20 and "bots" not in status
    print("PASS: practice → ready → Select (phase, real FIFO reset, status)")

    # 選択の20秒の時間切れ→待機。
    mark = mcu.mark()
    phase, status = mcu.wait_types(mark, ["phase", "status"], "the selection timeout", "Idle")
    assert phase == {"phase": "Idle"}
    events = mcu.since(selected)
    idle = events.index(("phase", {"phase": "Idle"}))
    windows = [s["window"] for k, s in events[:idle] if k == "status" and s["round"] is None]
    assert windows[0] == 20 and windows[-1] == 1 and windows == sorted(windows, reverse=True), windows
    print("PASS: Select times out after 20 s → Idle")

    # 練習の無操作60秒→待機。
    start_round("tutorial")
    mark = mcu.mark()
    phase, reset, status = mcu.wait_types(mark, ["phase", "reset", "status"], "the practice timeout", "Idle")
    assert phase == {"phase": "Idle"} and status["round"] is None
    print("PASS: practice without SEND for 60 s → Idle (phase, real FIFO reset, status)")

    # 結果の30秒の時間切れ→待機。
    start_round("normal")
    mcu.wait(lambda: mcu.phase == "Finished", timeout=ROUND_SECONDS / SPEED + 3, what="the result")
    mark = mcu.mark()
    mcu.wait(lambda: mcu.phase == "Idle", timeout=30 / SPEED + 3, what="the result timeout")
    events = mcu.since(mark)
    idle = len(events) - 1 - events[::-1].index(("phase", {"phase": "Idle"}))
    phase, reset, status = mcu.wait_types(mark + idle, ["phase", "reset", "status"], "the status after the result", "Idle")
    assert status["round"] is None and status["window"] == 0 and status["score"] == 0, status
    print("PASS: Finished times out after 30 s → Idle")

    # 打ち切り: ボットに戻す（通常・練習）、自動への切替、運転停止。結果は出ない。
    start_round("normal")
    mark = mcu.mark()
    command("bot")
    phase, status = mcu.wait_types(mark, ["phase", "status"], "return to bot", "Idle")
    assert phase == {"phase": "Idle"} and status["score"] == 0
    start_round("tutorial")
    mark = mcu.mark()
    command("bot")
    phase, reset, status = mcu.wait_types(mark, ["phase", "reset", "status"], "return to bot from practice", "Idle")
    assert phase == {"phase": "Idle"}
    start_round("normal")
    mark = mcu.mark()
    command("auto")
    config, reset, phase, status = mcu.wait_types(mark, ["config", "reset", "phase", "status"], "auto", "Idle")
    assert config["mode"] == "auto" and phase == {"phase": "Idle"}
    mcu.wait(lambda: sum(k == "status" for k, _ in mcu.since(mark)) >= 2, what="the resync after config")
    mark = mcu.mark()
    mcu.ready()  # 実機はautoでは送らない。サーバーは拒否してstatusだけを返す。
    mcu.wait_types(mark, ["status"], "the refused ready")
    mark = mcu.mark()
    command("manual")
    # configへの応答のresyncにも同期が返る。2回目の同期まで待ってから長押しする。
    mcu.wait(lambda: mcu.mode == "manual" and sum(k == "status" for k, _ in mcu.since(mark)) >= 2, what="manual")
    start_round("normal")
    mark = mcu.mark()
    command("stop")
    phase, reset, status = mcu.wait_types(mark, ["phase", "reset", "status"], "stop", "Preparing")
    assert phase == {"phase": "Preparing"} and reset["packets"] == [] and status["score"] == 0
    mark = mcu.mark()
    mcu.ready()
    mcu.wait_types(mark, ["status"], "ready while stopped")
    assert not any(k == "summary" for k, _ in mcu.events[-40:])
    print("PASS: cut short by bot (normal, practice), auto (config, reset, phase, status) and stop "
          "(phase Preparing, empty reset, status); no result; ready refused while auto or stopped")

    # 応答のないready: ファームウェアは3秒後にresyncし、要求を再送しない。
    mark = mcu.mark()
    command("start")
    mcu.wait_types(mark, ["phase", "reset", "status"], "restart", "Idle")
    command("ack ignore")
    time.sleep(.1)
    mark = mcu.mark()
    mcu.ready()
    deadline = time.monotonic() + 3
    while time.monotonic() < deadline:
        mcu.pump(.05)
    assert not any(k in ("phase", "status") for k, _ in mcu.since(mark))
    mcu.write("resync")
    reset, phase, status = mcu.wait_types(mark, ["reset", "phase", "status"], "resync", "Idle")
    assert phase == {"phase": "Idle"}
    command("ack accept")
    print("PASS: an unanswered ready leaves the station; resync restores reset, phase and status")

    pids = [d["pid"] for k, d in mcu.events if k == "push"]
    assert pids[:4] == [65533, 65534, 65535, 0], pids[:4]
    print("PASS: PIDs wrap from 65533 through 65535 to 0 without a duplicate in the MCU cache")
    commands.put(None)


def main():
    master, slave = os.openpty()
    cdc = CDC(os.ttyname(slave))
    os.close(slave)
    mcu = FakeMCU(master)
    t0 = time.monotonic()
    log = io.StringIO()
    station = Station(cdc, lambda: (time.monotonic() - t0) * SPEED, ROUND_SECONDS, FIRST_PID,
                      out=lambda *args, **kwargs: print(*args, file=log), seed=1)
    commands = queue.Queue()
    failure = []

    def serve():
        try:
            connect(cdc, station)
            run(cdc, station, commands, 120)
        except BaseException as error:  # 疑似MCU側で失敗として報告する。
            failure.append(error)

    thread = threading.Thread(target=serve, daemon=True)
    thread.start()
    try:
        scenario(mcu, commands)
        thread.join(5)
        assert not failure, failure
    except BaseException:
        print(log.getvalue()[-4000:])
        raise
    finally:
        cdc.close()
        os.close(master)
    print("PASS: endless_cdc.py station over a pseudo-TTY")


if __name__ == "__main__":
    main()
