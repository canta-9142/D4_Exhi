"""Finished目視確認ツールが、コマンド・MCU要求の両方でpreviewを解除することを検証。

連続運転の結果（rank=null、round=normal・window=30）を既定とし、ranked・window・practice・resetの
コマンドと、もう一度遊ぶ（ready）への応答も確かめる。実機は使わない。
"""

from contextlib import redirect_stdout
import io
from unittest.mock import patch
from finished_cdc import NO_BESTS, visual


class FakeCDC:
    def __init__(self, request_resync, hold=False):
        self.messages = []
        self.request_resync = request_resync
        self.hold = hold  # trueなら、最初のreadで両端の長押し（ready）を1回返す。
        self.preview = False
        self.generation = 0

    def send(self, kind, **data):
        self.messages.append((kind, data))
        if kind == "config":
            self.preview = data["preview"]

    def expect(self, kind):
        assert kind == "resync"
        return {}

    def reset(self, packets, phase, with_status=True):
        self.generation += 1
        sync = f"{self.generation:032X}"
        self.send("reset", sync=sync, packets=packets)
        self.send("phase", phase=phase)
        if with_status:
            self.status()
        return sync

    def status(self, score=0, remaining=120, ready=False, countdown=0, rank=None, gains=(), combo=0,
               round=None, window=0, bots=None):
        self.send("status", score=score, remaining=remaining, ready=ready, countdown=countdown, round=round,
                  window=window, rank=rank, gains=list(gains), combo=combo)

    def sync(self):
        return [data for kind, data in self.messages if kind == "reset"][-1]["sync"]

    def read(self, timeout):
        if self.request_resync and self.preview:
            self.request_resync = False
            return {"type": "resync", "data": {}}
        if self.hold:
            self.hold = False
            return {"type": "ready", "data": {"sync": self.sync()}}
        return None


def main():
    for request_resync in (False, True):
        cdc = FakeCDC(request_resync)
        commands = "preview\nstop\n" if request_resync else "preview\nresync\nstop\n"
        stream = io.StringIO(commands)
        with patch("finished_cdc.sys.stdin", stream), \
             patch("finished_cdc.select.select", return_value=([stream], [], [])), \
             redirect_stdout(io.StringIO()):
            visual(cdc)
        configs = [data for kind, data in cdc.messages if kind == "config"]
        assert configs == [
            {"node": 1, "mode": "manual", "preview": False},
            {"node": 1, "mode": "auto", "preview": True},
            {"node": 1, "mode": "auto", "preview": False},
        ], configs
        restored = cdc.messages[-5:]
        assert [kind for kind, _ in restored] == ["config", "reset", "phase", "status", "summary"]
        assert restored[1][1]["sync"] == restored[4][1]["sync"]
        assert restored[4][1]["score"] == 2147483647 and restored[4][1]["rank"] is None
        assert restored[3][1]["round"] == "normal" and restored[3][1]["window"] == 30
        assert restored[4][1]["topPercent"] is None and restored[4][1]["bottomHalf"] is False

    # top Nは上位%付きの結果を出し、animate・duplicate・resyncでも同じ値を送る。不正な値は無視する。
    # bests KINDは③の値を変え、以後の結果にも付ける。対象外では*Mineをfalseにする。
    cdc = FakeCDC(False)
    stream = io.StringIO("top 12\ntop 0\ntop 51\nanimate\nduplicate\nresync\nbests mine\nbests bogus\n"
                         "unranked\nstop\n")
    with patch("finished_cdc.sys.stdin", stream), \
         patch("finished_cdc.select.select", return_value=([stream], [], [])), \
         redirect_stdout(io.StringIO()):
        visual(cdc)
    summaries = [data for kind, data in cdc.messages if kind == "summary"]
    assert all(set(NO_BESTS) <= set(data) for data in summaries)
    tops = [data["topPercent"] for data in summaries]
    assert tops == [None, 12, 12, 12, 12, 12, None], tops
    assert summaries[0]["gameBest"] == 410 and summaries[0]["gameBestNodes"] == [3]
    mine = summaries[5]
    assert mine["hourBestMine"] and mine["allTimeBestMine"] and mine["allTimeBest"] == mine["score"] == 345
    assert mine["gameBestNodes"] == [1, 3] and mine["hourBestStart"] == 23
    assert not summaries[6]["eligible"] and not summaries[6]["hourBestMine"] and not summaries[6]["allTimeBestMine"]

    # bottomは値を伏せた下位50%の結果を出し、animateでも保つ。top Nと他のコマンドで解除する。
    cdc = FakeCDC(False)
    stream = io.StringIO("bottom\nanimate\ntop 12\nbottom\nresult\nstop\n")
    with patch("finished_cdc.sys.stdin", stream), \
         patch("finished_cdc.select.select", return_value=([stream], [], [])), \
         redirect_stdout(io.StringIO()):
        visual(cdc)
    halves = [(data["topPercent"], data["bottomHalf"]) for kind, data in cdc.messages if kind == "summary"]
    assert halves == [(None, False), (None, True), (None, True), (12, False), (None, True), (None, False)], halves

    # ranked・unrankedは一斉ゲーム方式の結果、resultは連続運転の結果へ戻す。window Nはstatusだけを送る。
    # practiceはround=tutorialのstatusだけでsummaryを送らず、resetは待機（Idle）へ移す。
    cdc = FakeCDC(False)
    stream = io.StringIO("ranked\nunranked\nresult\nwindow 10\nwindow 0\nwindow 31\npractice\nreset\nstop\n")
    with patch("finished_cdc.sys.stdin", stream), \
         patch("finished_cdc.select.select", return_value=([stream], [], [])), \
         redirect_stdout(io.StringIO()):
        visual(cdc)
    summaries = [(data["rank"], data["eligible"]) for kind, data in cdc.messages if kind == "summary"]
    assert summaries == [(None, True), (2, True), (None, False), (None, True)], summaries
    windows = [(data["round"], data["window"]) for kind, data in cdc.messages if kind == "status"]
    assert windows == [("normal", 30), ("normal", 30), ("normal", 30), ("normal", 30), ("normal", 10),
                       ("normal", 0), ("tutorial", 0), (None, 0)], windows
    tail = [kind for kind, _ in cdc.messages[-8:]]
    assert tail == ["config", "reset", "phase", "status"] * 2, tail
    assert [data["phase"] for kind, data in cdc.messages if kind == "phase"][-2:] == ["Finished", "Idle"]

    # 結果での両端の長押し（ready）には、段階を変えずにstatusだけを返す。
    cdc = FakeCDC(False, hold=True)
    stream = io.StringIO("stop\n")
    # 1周目は標準入力なしでreadyを受け取り、2周目でstopを読む。
    with patch("finished_cdc.sys.stdin", stream), \
         patch("finished_cdc.select.select", side_effect=[([], [], []), ([stream], [], [])]), \
         redirect_stdout(io.StringIO()):
        visual(cdc)
    kinds = [kind for kind, _ in cdc.messages]
    assert kinds == ["config", "reset", "phase", "status", "summary", "status"], kinds
    print("PASS: preview ends on command/MCU resync; auto mode, fresh generation and final result restored; "
          "top N; bottom; bests KIND; ranked/unranked/result; window N; practice; reset to Idle; ready from the result")


if __name__ == "__main__":
    main()
