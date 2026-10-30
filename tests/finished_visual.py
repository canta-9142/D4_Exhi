"""Finished目視確認ツールが、コマンド・MCU要求の両方でpreviewを解除することを検証。"""

from contextlib import redirect_stdout
import io
from unittest.mock import patch
from finished_cdc import NO_BESTS, visual


class FakeCDC:
    def __init__(self, request_resync):
        self.messages = []
        self.request_resync = request_resync
        self.preview = False
        self.generation = 0

    def send(self, kind, **data):
        self.messages.append((kind, data))
        if kind == "config":
            self.preview = data["preview"]

    def expect(self, kind):
        assert kind == "resync"
        return {}

    def reset(self, packets, phase):
        self.generation += 1
        sync = f"{self.generation:032X}"
        self.send("reset", sync=sync, packets=packets)
        self.send("phase", phase=phase)
        self.status()
        return sync

    def status(self, score=0, remaining=120, ready=False, countdown=0, rank=None, gains=()):
        self.send("status", score=score, remaining=remaining, ready=ready, countdown=countdown,
                  rank=rank, gains=list(gains), combo=0)

    def read(self, timeout):
        if self.request_resync and self.preview:
            self.request_resync = False
            return {"type": "resync", "data": {}}
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
        assert restored[4][1]["score"] == 2147483647 and restored[4][1]["rank"] == 2
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
    print("PASS: preview ends on command/MCU resync; auto mode, fresh generation and final result restored; "
          "top N; bottom; bests KIND")


if __name__ == "__main__":
    main()
