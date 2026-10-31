"""通常ファームウェアの長押し実機確認。標準入力: start/reset/auto/manual/accept/reject/ignore/rank/gain/stop。

startではサーバーと同じく、status.countdownを3・2・1・0と1秒ごとに送ってからパケットを追加する。
カウントダウンが終わると暫定順位を4位とし、受理したSENDごとに発信者の15点をgainsで送る。
rank N（1〜8、noneでnull）で順位を、gain N...（1〜15を16個まで）で獲得点を送る。gainはscoreにも足す。
"""

import argparse
import json
import os
from pathlib import Path
import select
import sys
import time
import uuid
from player_cdc import CDC


def run(cdc, args):
    mode, phase, ready, score, rank = "manual", "Preparing", False, 0, None
    countdown, countdown_at = 0, None
    sync = ""
    ids = []
    next_pid, last_rid = 1001, -1
    ack_at = None
    ack = args.ack
    requests = 0

    def record(message):
        if args.log:
            with args.log.open("a") as output:
                output.write(json.dumps(message, separators=(",", ":")) + "\n")

    def status(gains=()):
        cdc.status(score=score, remaining=120, ready=ready, countdown=countdown, rank=rank, gains=gains)

    def synchronize():
        nonlocal sync, ack_at
        ack_at = None
        sync = uuid.uuid4().hex.upper()
        cdc.send("reset", sync=sync, packets=[{"pid": pid, "dst": 2} for pid in ids])
        cdc.send("phase", phase=phase)
        status()

    cdc.write(b"\n")
    cdc.discard_startup()
    cdc.send("identify")
    hello = cdc.expect("hello")
    record({"type": "hello", "data": hello})
    cdc.send("config", node=1, mode=mode, preview=False)
    cdc.expect("resync")
    synchronize()
    print("READY: Preparing/manual. Release both ends, then hold both for 2 seconds.", flush=True)
    print("Commands: start/reset/auto/manual/accept/reject/ignore/rank N|none/gain N.../stop", flush=True)
    started = time.monotonic()
    input_buffer = ""
    while time.monotonic() - started < args.seconds:
        if select.select([sys.stdin], [], [], 0)[0]:
            raw = os.read(sys.stdin.fileno(), 4096)
            if not raw:
                break
            input_buffer += raw.decode()
            while "\n" in input_buffer:
                command, input_buffer = input_buffer.split("\n", 1)
                command = command.strip()
                words = command.split()
                if command == "stop":
                    return
                if command in ("accept", "reject", "ignore"):
                    ack = command
                elif len(words) == 2 and words[0] == "rank" and words[1] in ("none", *"12345678"):
                    rank = None if words[1] == "none" else int(words[1])
                    status()
                elif (2 <= len(words) <= 17 and words[0] == "gain" and
                      all(word.isdigit() and 1 <= int(word) <= 15 for word in words[1:])):
                    gains = [int(word) for word in words[1:]]
                    score += sum(gains)
                    status(gains)
                elif command == "start":
                    if mode == "manual" and not ready:
                        print("Start refused: manual node is not ready", flush=True)
                        continue
                    phase = "Playing"
                    ack_at = None
                    countdown, countdown_at = 3, time.monotonic() + 1
                    cdc.send("phase", phase=phase)
                    status()
                elif command in ("reset", "auto", "manual"):
                    phase, ready, score, rank, ids = "Preparing", False, 0, None, []
                    countdown, countdown_at = 0, None
                    if command != "reset":
                        mode = command
                        cdc.send("config", node=1, mode=mode, preview=False)
                    cdc.send("phase", phase=phase)
                    synchronize()
                else:
                    continue
                record({"type": "command", "data": {"command": command}})
                print(f"STATE: {phase}/{mode} ready={ready} score={score} rank={rank}", flush=True)
        message = cdc.read(.05)
        if message:
            record(message)
            kind, data = message["type"], message["data"]
            if kind == "ready":
                assert data == {"sync": sync}, message
                assert phase == "Preparing" and mode == "manual" and not ready, message
                requests += 1
                if ack == "accept":
                    ready = True
                    ack_at = time.monotonic() + args.ack_delay
                elif ack == "reject":
                    status()
                print(f"PASS: READY #{requests}; response={ack}; remains Preparing", flush=True)
            elif kind == "resync":
                synchronize()
                print(f"PASS: resync; ready={ready}; release ends before retry", flush=True)
            elif kind == "send":
                assert phase == "Playing" and mode == "manual" and not countdown and ids, message
                assert data["sync"] == sync and data["pid"] == ids[0] and data["rid"] > last_rid, message
                assert data["to"] in (2, 9, 11, 8), message
                last_rid = data["rid"]
                cdc.result(data)
                ids.pop(0)
                score += 15
                status([15])
                print("PASS: Playing SEND/RESULT", flush=True)
            else:
                raise AssertionError(message)
        if ack_at is not None and time.monotonic() >= ack_at:
            status()
            ack_at = None
            print("STATUS ready=true; waiting for administrator start command", flush=True)
        if countdown_at is not None and time.monotonic() >= countdown_at:
            countdown -= 1
            countdown_at = countdown_at + 1 if countdown else None
            if not countdown:
                rank = 4
            status()
            print(f"STATUS countdown={countdown}", flush=True)
            if not countdown and not ids:
                for _ in range(4):
                    ids.append(next_pid)
                    cdc.send("push", pid=next_pid, dst=2)
                    next_pid += 1


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--port", default="/dev/ttyACM0")
    parser.add_argument("--seconds", type=float, default=600)
    parser.add_argument("--ack", choices=("accept", "reject", "ignore"), default="accept")
    parser.add_argument("--ack-delay", type=float, default=1)
    parser.add_argument("--log", type=Path)
    args = parser.parse_args()
    cdc = CDC(args.port)
    try:
        run(cdc, args)
    except KeyboardInterrupt:
        print("Stopping Preparing test", flush=True)
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
