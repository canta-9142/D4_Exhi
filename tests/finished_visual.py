"""Finished目視確認ツールが、コマンド・MCU要求の両方でpreviewを解除することを検証。"""

from contextlib import redirect_stdout
import io
from unittest.mock import patch
from finished_cdc import visual


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
        self.send("status", score=0, remaining=120, ready=False, countdown=0)
        return sync

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
    print("PASS: preview ends on command/MCU resync; auto mode, fresh generation and final result restored")


if __name__ == "__main__":
    main()
