"""LinuxのUSB CDC実機テスト。--buttons時だけスイッチ操作が必要。"""

import argparse
import fcntl
import json
import os
import select
import termios
import time
import tty


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
                try:
                    message = json.loads(line)
                except (ValueError, UnicodeDecodeError):
                    print("boot/log:", line.decode(errors="replace"), flush=True)
                    continue
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

    def close(self):
        fcntl.ioctl(self.fd, termios.TIOCNXCL)
        os.close(self.fd)


def packet(pid, dst=2):
    return {"pid": pid, "dst": dst}


def exercise_buttons(cdc, packets):
    # 左端を4回、1秒以上間隔を空けて押す。押下は各段階60秒待つ。
    for expected_pid, result in [(1001, False), (1001, True), (1002, True), (1003, None)]:
        print(f"WAIT: press leftmost switch (expected pid={expected_pid}, result={result})", flush=True)
        data = cdc.expect("send", timeout=60)
        assert data == {"pid": expected_pid, "to": 2}, data
        if result is not None:
            cdc.send("result", ok=result)
        else:
            cdc.expect("resync", timeout=5)
            cdc.send("result", ok=True)  # RESETより前に旧要求の応答を送り切る。
            cdc.send("reset", packets=packets)
            cdc.quiet()
    print("PASS: physical switches, failed/successful RESULT, send timeout", flush=True)


def exercise(cdc, buttons, idle_seconds):
    # 再実行時にすでにCONFIG済みでも再同期から開始できる。
    cdc.write(b"\n?\n")
    initial = cdc.read(5)
    assert initial and initial.get("type") in ("hello", "resync"), (
        "No startup handshake: check LCD/sprite initialization and USB CDC", initial
    )
    if initial["type"] == "hello":
        uid = initial["data"]["uid"]
        assert len(uid) == 12 and int(uid, 16) >= 0
        print("PASS: hello uid=" + uid, flush=True)
    # 起動直後に蓄積したHELLOを読み切る。
    while cdc.read(0.1) is not None:
        pass

    def reset(packets):
        cdc.send("reset", packets=packets)
        cdc.quiet()

    # 行の途中では適用せず、改行後にCONFIGを処理する。
    cdc.write(b'{"type":"config","data":')
    time.sleep(0.05)
    cdc.write(b'{"node":1}}\r\n')
    cdc.expect("resync")
    reset([packet(10)])
    cdc.send("push", **packet(11, 3))
    cdc.quiet()
    cdc.send("push", **packet(11, 3))
    cdc.expect("resync")
    print("PASS: split CONFIG, RESET, PUSH, duplicate detection", flush=True)

    reset([packet(10)])
    cdc.send("push", pid=-1, dst=2)
    cdc.expect("resync")
    reset([packet(10)])
    cdc.send("reset", packets=[packet(20), {}])
    cdc.expect("resync")
    reset([])
    cdc.send("result", ok=True)  # 未要求の成功応答では何もしない。
    cdc.quiet()
    cdc.send("result", ok=1)
    cdc.expect("resync")
    print("PASS: malformed packets, RESET validation, unsolicited RESULT", flush=True)

    reset([packet(i, 8) for i in range(1024)])
    cdc.quiet(1.2)  # 改行欠落による受信タイムアウトも検出する。
    cdc.send("push", **packet(2000))
    cdc.expect("resync")
    reset([])
    cdc.send("reset", packets=[packet(i, 8) for i in range(1025)])
    cdc.expect("resync")
    reset([])
    cdc.write(b"x" * 32769 + b"\n")
    cdc.expect("resync")
    reset([])
    cdc.write(b'{"type":"push"')
    cdc.expect("resync")
    reset([])
    print("PASS: 1024-packet RESET, queue overflow, oversized line recovery", flush=True)

    packets = [packet(1001, 2), packet(1002, 3), packet(1003, 4)]
    reset(packets)
    if idle_seconds:
        print(f"WAIT: checking idle connection for {idle_seconds}s", flush=True)
        cdc.quiet(idle_seconds)
        cdc.send("push", **packet(1003, 4))
        cdc.expect("resync")  # 待機後もキューを保持し、重複IDを検出する。
        reset(packets)
        print("PASS: idle connection and retained cache", flush=True)
    if buttons:
        exercise_buttons(cdc, packets)
    print("PASS: USB CDC smoke test; node A with packets 1001/1002/1003 loaded", flush=True)


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
