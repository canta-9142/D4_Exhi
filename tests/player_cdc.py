"""LinuxのUSB CDC実機テスト。--buttons時は案内に従ってEnterと左端スイッチを操作する。"""

import argparse
import fcntl
import json
import os
import re
import select
import termios
import time
import tty
import uuid


class CDC:
    def __init__(self, path):
        self.fd = os.open(path, os.O_RDWR | os.O_NOCTTY | os.O_NONBLOCK)
        fcntl.flock(self.fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        fcntl.ioctl(self.fd, termios.TIOCEXCL)
        tty.setraw(self.fd)
        settings = termios.tcgetattr(self.fd)
        settings[2] |= termios.CLOCAL | termios.CREAD
        settings[2] &= ~termios.HUPCL
        settings[4] = settings[5] = termios.B115200
        termios.tcsetattr(self.fd, termios.TCSANOW, settings)
        self.buffer = b""

    def write(self, raw):
        while raw:
            _, writable, _ = select.select([], [self.fd], [], 5)
            if not writable:
                raise TimeoutError("USB CDC write timed out")
            raw = raw[os.write(self.fd, raw):]

    def send(self, kind, **data):
        self.write((json.dumps({"type": kind, "data": data}, separators=(",", ":")) + "\n").encode())

    def read(self, timeout):
        deadline = time.monotonic() + timeout
        while True:
            if b"\n" in self.buffer:
                line, self.buffer = self.buffer.split(b"\n", 1)
                if not line.strip():
                    continue
                # USB CDCはプロトコル専用。デバッグログ混入も失敗として扱う。
                message = json.loads(line)
                print("MCU:", message, flush=True)
                return message
            remaining = deadline - time.monotonic()
            if remaining <= 0 or not select.select([self.fd], [], [], remaining)[0]:
                return None
            raw = os.read(self.fd, 4096)
            if not raw:
                raise ConnectionError("USB CDC disconnected")
            self.buffer += raw

    def expect(self, kind, timeout=5):
        message = self.read(timeout)
        assert message and message.get("type") == kind, (kind, message)
        return message["data"]

    def quiet(self, timeout=0.25):
        assert self.read(timeout) is None, "Unexpected MCU response"

    def discard_startup(self):
        # 書込直後のROM起動ログや、接続前に蓄積したHELLOを読み捨てる。
        self.buffer = b""
        while select.select([self.fd], [], [], 0.1)[0]:
            if not os.read(self.fd, 4096):
                raise ConnectionError("USB CDC disconnected")

    def status(self, score=0, remaining=120, ready=False, countdown=0):
        self.send("status", score=score, remaining=remaining, ready=ready, countdown=countdown)

    def reset(self, packets, phase="Playing", with_status=True):
        sync = uuid.uuid4().hex.upper()
        self.send("reset", sync=sync, packets=packets)
        if phase is not None:
            self.send("phase", phase=phase)
        if with_status:
            self.status()
        self.quiet()
        return sync

    def result(self, request, ok=True):
        self.send("result", sync=request["sync"], rid=request["rid"], ok=ok)

    def close(self):
        fcntl.ioctl(self.fd, termios.TIOCNXCL)
        os.close(self.fd)


def packet(pid, dst=2):
    return {"pid": pid, "dst": dst}


def exercise_buttons(cdc, packets, sync):
    last_rid = -1

    cdc.send("phase", phase="Preparing")
    input("READY: AUTO mode; press Enter, then wait for AUTO and press the leftmost switch repeatedly for 10 seconds: ")
    cdc.send("config", node=1, mode="auto", preview=False)
    cdc.expect("resync")
    cdc.reset(packets)
    cdc.quiet(10)
    print("PASS: no SEND in AUTO/Playing", flush=True)
    cdc.send("config", node=1, mode="manual", preview=False)
    cdc.expect("resync")
    sync = cdc.reset(packets, "Preparing")

    def press(pid, expected_sync):
        nonlocal last_rid
        # 案内待ち中の先行押下が未応答SENDにならないよう、開始までは操作を止める。
        cdc.send("phase", phase="Preparing")
        input(f"READY: expected pid={pid}; press Enter, then wait for PLAYING and press the leftmost switch once: ")
        cdc.send("phase", phase="Playing")
        data = cdc.expect("send", timeout=120)
        assert set(data) == {"sync", "rid", "pid", "to"}, data
        assert data["sync"] == expected_sync and data["pid"] == pid and data["to"] == 2, data
        assert type(data["rid"]) is int and last_rid < data["rid"] <= 0xFFFFFFFF, data
        last_rid = data["rid"]
        return data

    for phase in ("Preparing", "Finished"):
        cdc.send("phase", phase=phase)
        if phase == "Finished":
            cdc.send("summary", sync=sync, score=0, rank=1, eligible=True, topPercent=None)
        input(f"READY: {phase}; press Enter, then press the leftmost switch repeatedly for 10 seconds: ")
        cdc.quiet(10)
        print(f"PASS: no SEND in {phase}", flush=True)

    failed = press(1001, sync)
    cdc.result(failed, False)
    cdc.quiet()
    accepted = press(1001, sync)
    cdc.result(failed)  # 同世代の旧RIDを無視し、新要求の応答待ちを維持する。
    cdc.quiet()
    cdc.send("phase", phase="Finished")
    cdc.result(accepted)  # 終了後でも、処理中だった要求への応答は適用する。
    cdc.quiet()
    old = press(1002, sync)  # 1001が1件だけ削除されたことを確認。
    new_sync = cdc.reset(packets)
    current = press(1001, new_sync)
    cdc.result(old)  # 旧世代応答が現要求を完了させていないことを成功応答で確認。
    cdc.quiet()
    cdc.result(current)
    cdc.quiet()
    timed_out = press(1002, new_sync)
    cdc.expect("resync", timeout=5)
    cdc.result(timed_out)
    sync = cdc.reset(packets)
    final = press(1001, sync)
    cdc.result(final)
    cdc.quiet()
    print("PASS: physical switches, mode/phase gating, failed/successful RESULT, delayed RESULT, send timeout", flush=True)


def exercise(cdc, buttons, idle_seconds):
    cdc.write(b"\n")
    cdc.discard_startup()
    cdc.send("identify")
    uid = cdc.expect("hello")["uid"]
    assert re.fullmatch(r"[0-9A-F]{12}", uid), uid
    cdc.send("identify")
    assert cdc.expect("hello")["uid"] == uid
    print("PASS: identify/repeated identify, hello uid=" + uid, flush=True)

    def configure(mode="manual"):
        cdc.send("config", node=1, mode=mode, preview=False)
        cdc.expect("resync")

    cdc.write(b'{"type":"config","data":')
    time.sleep(0.05)
    cdc.write(b'{"node":1,"mode":"manual","preview":false}}\r\n')
    cdc.expect("resync")
    sync = cdc.reset([packet(10)], phase=None, with_status=False)
    cdc.expect("resync", timeout=2)
    cdc.reset([packet(10)], "Preparing")
    cdc.send("push", **packet(11, 3))
    cdc.quiet()
    cdc.send("push", **packet(11, 3))
    cdc.expect("resync")
    print("PASS: split CONFIG, missing PHASE retry, FIFO/PUSH validation", flush=True)

    configure()
    cdc.status(23, 87, True)  # RESET前のSTATUSでは取得完了にしない。
    cdc.reset([packet(10)], with_status=False)
    cdc.quiet(2)
    cdc.send("phase", phase="Playing")
    cdc.send("reset", sync=uuid.uuid4().hex.upper(), packets=[packet(10)])
    cdc.expect("resync", timeout=2)  # PHASE再通知・通常RESETでも期限を延長しない。
    cdc.reset([packet(10)])
    cdc.quiet(3.2)
    cdc.status(2147483647, 120, False)
    cdc.status(0, 0, True)
    cdc.status()
    cdc.quiet()
    print("PASS: fresh STATUS acquisition, 3-second deadline, full-range absolute values", flush=True)

    for phase in ("Playing", "Finished", "Preparing"):
        cdc.reset([packet(10)], phase)
        cdc.quiet(1.2)
    cdc.send("phase", phase="Preparing")
    cdc.reset([], phase=None)
    cdc.quiet(1.2)
    configure()
    cdc.send("phase", phase="Finished")
    cdc.reset([packet(10)], phase=None)
    cdc.quiet(1.2)
    cdc.send("identify")
    assert cdc.expect("hello")["uid"] == uid
    cdc.send("reset", sync=sync, packets=[])
    cdc.send("phase", phase="Playing")
    cdc.status()
    cdc.expect("hello", timeout=2)
    configure()
    cdc.reset([packet(10)], "Finished")
    cdc.quiet(1.2)
    print("PASS: phase changes, phase/reset ordering, re-identification", flush=True)

    sync = cdc.reset([packet(10)])
    invalid = [
        ("push", {"pid": -1, "dst": 2}),
        ("reset", {"sync": uuid.uuid4().hex.upper(), "packets": [packet(20), {}]}),
        ("reset", {"sync": uuid.uuid4().hex.upper(), "packets": [packet(20), packet(20)]}),
        ("reset", {"sync": "a" * 32, "packets": []}),
        ("reset", {"packets": []}),
        ("phase", {"phase": "playing"}),
        ("phase", {"phase": "Playing\x00x"}),
        ("result", {"sync": sync, "rid": 0, "ok": 1}),
        ("result", {"sync": sync, "rid": 0x100000000, "ok": True}),
        ("result", {"ok": True}),
        ("status", {"score": -1, "remaining": 120, "ready": False, "countdown": 0}),
        ("status", {"score": 2147483648, "remaining": 120, "ready": False, "countdown": 0}),
        ("status", {"score": 1.0, "remaining": 120, "ready": False, "countdown": 0}),
        ("status", {"score": 0, "remaining": 121, "ready": False, "countdown": 0}),
        ("status", {"score": 0, "remaining": "120", "ready": False, "countdown": 0}),
        ("status", {"score": 0, "remaining": 120, "ready": 1, "countdown": 0}),
        ("status", {"score": 0, "remaining": 120, "countdown": 0}),
        ("status", {"score": 0, "remaining": 120, "ready": False, "countdown": -1}),
        ("status", {"score": 0, "remaining": 120, "ready": False, "countdown": 4}),
        ("status", {"score": 0, "remaining": 120, "ready": False, "countdown": 1.0}),
        ("status", {"score": 0, "remaining": 120, "ready": False, "countdown": "1"}),
        ("status", {"score": 0, "remaining": 120, "ready": False}),
    ]
    for kind, data in invalid:
        cdc.send(kind, **data)
        cdc.expect("resync")
        sync = cdc.reset([packet(10)])
    cdc.send("result", sync=sync, rid=0xFFFFFFFF, ok=True)
    cdc.quiet()
    cdc.send("push", **packet(10))
    cdc.expect("resync")
    cdc.reset([])
    print("PASS: malformed fields, sync/rid/STATUS validation, retained FIFO", flush=True)

    for fields in ({"node": 1}, {"node": 2, "mode": "MANUAL"},
                   {"node": True, "mode": "manual"}, {"node": 1, "mode": "manual\x00x"}):
        cdc.send("config", **fields)
        assert cdc.expect("hello")["uid"] == uid
        configure()
        cdc.reset([])
    for raw in (
        b'{"type":"config","data":{"node":2,"mode":"manual","preview":false}}garbage\n',
        b"{'type':'config','data':{'node':1,'mode':'manual'}}\n",
        b'{type:"config",data:{node:1,mode:"manual"}}\n',
        b'{"type":"config","data":{"node":+1,"mode":"manual","preview":false}}\n',
        b'{"type":"config","data":{"node":1,"mode":"manual","preview":false,"extra":"\xff"}}\n',
        b'{"type":"config","data":{"node":1,"mode":"manual","preview":false,"extra":"\xed\xa0\x80"}}\n',
        b'{"type":"config","data":{"node":1,"mode":"manual\\uD800"}}\n',
        b'{"type":"config","data":{"node":1,"mode":"manual","preview":false,"extra":"\\uDC00"}}\n',
        b'{"type":"config","data":{"node":1,"mode":"manual","preview":false,"extra":{"a":{"b":{}}}}}\n',
    ):
        cdc.write(raw)
        assert cdc.expect("hello")["uid"] == uid
        configure()
        cdc.reset([])
    print("PASS: invalid CONFIG and unreadable JSON/UTF-8 restart identification", flush=True)

    for extra in ("🐟", {"a": {}}, [[]]):
        # ensure_ascii=Trueで絵文字を正しいサロゲートペアとして送る。
        cdc.send("config", node=1, mode="manual", preview=False, extra=extra)
        cdc.expect("resync")
        cdc.reset([])
    print("PASS: valid surrogate pair and nesting depth 4", flush=True)

    cdc.reset([packet(i, 8) for i in range(1024)])
    cdc.quiet(1.2)
    cdc.send("push", **packet(2000))
    cdc.expect("resync")
    cdc.reset([])
    cdc.send("reset", sync=uuid.uuid4().hex.upper(), packets=[packet(i, 8) for i in range(1025)])
    cdc.expect("resync")
    cdc.reset([])
    cdc.write(b"x" * 32769 + b"\n")
    cdc.expect("hello")
    configure()
    cdc.reset([])
    cdc.write('{"type":"config","data":{"node":1,"mode":"manual","preview":false,"extra":"準備完了🐟"}}\n'.encode("utf-8"))
    cdc.expect("resync")
    cdc.reset([])
    cdc.write(b"x" * 32769)  # LF前に停止しても、超過行の破棄を継続する。
    cdc.expect("hello")
    cdc.expect("hello")  # 1秒間の停止をまたぐ。
    cdc.write(b'{"type":"config","data":{"node":1,"mode":"manual","preview":false}}\n')
    cdc.expect("hello")  # 同じ超過行の末尾をCONFIGとして受理していない。
    configure()
    cdc.reset([])
    print("PASS: valid UTF-8 and oversized-line discard across receive timeout", flush=True)
    boundary = b'{"type":"phase","data":{"phase":"Playing","extra":0}}'
    boundary = boundary[:-1] + b" " * (32768 - len(boundary)) + b"}\r\n\r\n\n"
    cdc.write(boundary)
    cdc.quiet(1.2)
    cdc.write(b'{"type":"push"')
    cdc.expect("hello")
    configure()
    cdc.reset([])
    print("PASS: capacity, CRLF/line boundary, oversized/partial line re-identification", flush=True)

    configure("auto")
    cdc.reset([packet(9999, 1)], "Preparing")
    cdc.send("identify")  # 確認表示の解除でキャッシュと表示が無効化される。
    assert cdc.expect("hello")["uid"] == uid
    configure()
    packets = [packet(1001, 2), packet(1002, 3), packet(1003, 4)]
    sync = cdc.reset(packets, "Preparing")
    if idle_seconds:
        print(f"WAIT: checking idle connection for {idle_seconds}s", flush=True)
        cdc.quiet(idle_seconds)
        cdc.send("push", **packet(1003, 4))
        cdc.expect("resync")
        sync = cdc.reset(packets, "Preparing")
        print("PASS: idle connection and retained cache", flush=True)
    if buttons:
        exercise_buttons(cdc, packets, sync)
        cdc.send("phase", phase="Finished")
        cdc.quiet()
    print("PASS: USB CDC smoke test complete", flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--port", default="/dev/ttyACM0")
    parser.add_argument("--buttons", action="store_true")
    parser.add_argument("--idle-seconds", type=float, default=0)
    args = parser.parse_args()
    cdc = CDC(args.port)
    try:
        exercise(cdc, args.buttons, args.idle_seconds)
    finally:
        cdc.close()


if __name__ == "__main__":
    main()
