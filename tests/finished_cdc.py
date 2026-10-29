"""Finished/previewのUSB CDC検証。--visualで表示確認を継続する。"""

import argparse
import select
import sys
import uuid
from player_cdc import CDC


def result(cdc, sync, score=2147483647, rank=2, eligible=True, top=None):
    cdc.send("summary", sync=sync, score=score, rank=rank, eligible=eligible, topPercent=top)


def configure(cdc, mode="manual", preview=False):
    cdc.send("config", node=1, mode=mode, preview=preview)
    cdc.expect("resync")


def exercise(cdc):
    cdc.write(b"\n")
    cdc.discard_startup()
    cdc.send("identify")
    uid = cdc.expect("hello")["uid"]
    configure(cdc)
    sync = cdc.reset([], "Playing")
    cdc.send("phase", phase="Finished")
    cdc.status(2147483647, 0, True)
    result(cdc, sync)
    cdc.quiet(3.2)
    result(cdc, sync)
    cdc.quiet()
    print("PASS: full-range summary, duplicate result, silent Finished", flush=True)

    # 別世代の結果と重複通知・通常RESETでは、最初の取得期限を延ばさない。
    sync = cdc.reset([], "Finished")
    cdc.quiet(1)
    result(cdc, uuid.uuid4().hex.upper())
    sync = cdc.reset([], "Finished")
    cdc.expect("resync", timeout=2.5)
    sync = cdc.reset([], "Finished")
    result(cdc, sync, 400, None, False)
    cdc.quiet(3.2)
    print("PASS: stale generation ignored, result deadline, recovery, not ranked", flush=True)

    invalid = [
        {"score": -1, "rank": 1, "eligible": True},
        {"score": 2147483648, "rank": 1, "eligible": True},
        {"score": 1.0, "rank": 1, "eligible": True},
        {"score": 1, "rank": 0, "eligible": True},
        {"score": 1, "rank": 9, "eligible": True},
        {"score": 1, "rank": None, "eligible": True},
        {"score": 1, "rank": 1, "eligible": False},
        {"score": 1, "eligible": False},
        {"score": 1, "rank": None, "eligible": 0},
    ]
    invalid = [dict(fields, topPercent=None) for fields in invalid] + [
        {"score": 1, "rank": 1, "eligible": True},
        {"score": 1, "rank": 1, "eligible": True, "topPercent": 0},
        {"score": 1, "rank": 1, "eligible": True, "topPercent": 51},
        {"score": 1, "rank": 1, "eligible": True, "topPercent": 12.0},
        {"score": 1, "rank": 1, "eligible": True, "topPercent": "12"},
        {"score": 1, "rank": None, "eligible": False, "topPercent": 12},
    ]
    for fields in invalid:
        cdc.send("summary", sync=sync, **fields)
        cdc.expect("resync")
        sync = cdc.reset([], "Finished")
        result(cdc, sync)
        cdc.quiet(.05)
    for top in (1, 50):
        result(cdc, sync, top=top)
    cdc.quiet(.05)
    print("PASS: invalid summary fields request resync; topPercent 1 and 50 accepted", flush=True)

    configure(cdc, "auto", True)
    sync = cdc.reset([], "Finished")
    result(cdc, sync)
    cdc.quiet(3.2)
    configure(cdc, "auto")
    sync = cdc.reset([], "Finished")
    result(cdc, sync, 400, None, False)
    cdc.quiet(3.2)
    # 現在autoでも確定順位はサーバー通知のまま表示する。
    result(cdc, sync, 345, 2, True)
    cdc.quiet()
    print("PASS: preview has no summary deadline; normal auto retains server eligibility", flush=True)

    for fields in ({"node": 1, "mode": "manual", "preview": True},
                   {"node": 1, "mode": "auto", "preview": 0}):
        cdc.send("config", **fields)
        assert cdc.expect("hello")["uid"] == uid
    cdc.send("config", node=1, mode="manual")
    assert cdc.expect("hello")["uid"] == uid
    print("PASS: mandatory preview and invalid config combinations", flush=True)


def visual(cdc):
    phase, score, rank, eligible, preview = "Finished", 2147483647, 2, True, False
    mode = "manual"
    sync = ""

    def synchronize():
        nonlocal sync
        sync = cdc.reset([], phase)
        if phase == "Finished" and not preview:
            result(cdc, sync, score, rank, eligible)

    def resynchronize():
        nonlocal preview
        if preview:
            preview = False
            configure(cdc, mode)
        synchronize()

    configure(cdc, mode)
    synchronize()
    print("READY: final score 2147483647 / RANK 2", flush=True)
    print("Commands: animate/unranked/preview/result/reset/duplicate/resync/stop", flush=True)
    while True:
        if select.select([sys.stdin], [], [], 0)[0]:
            line = sys.stdin.readline()
            if not line or line.strip() == "stop":
                return
            command = line.strip()
            if command == "duplicate":
                result(cdc, sync, score, rank, eligible)
            elif command == "animate":
                preview, phase, score, rank, eligible = False, "Playing", 345, 2, True
                mode = "manual"
                configure(cdc, mode)
                synchronize()
                phase = "Finished"
                cdc.send("phase", phase=phase)
                cdc.status(score, 0, True)
                result(cdc, sync, score, rank, eligible)
            elif command in ("unranked", "preview", "result", "reset"):
                preview = command == "preview"
                phase = "Preparing" if command == "reset" else "Finished"
                score, rank, eligible = (400, None, False) if command == "unranked" else (2147483647, 2, True)
                mode = "auto" if preview else "manual"
                configure(cdc, mode, preview)
                synchronize()
            elif command == "resync":
                resynchronize()
            print(f"STATE: {phase} preview={preview} score={score} rank={rank}", flush=True)
        message = cdc.read(.05)
        if message:
            assert message["type"] == "resync", message
            resynchronize()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--port", default="/dev/ttyACM0")
    parser.add_argument("--visual", action="store_true")
    args = parser.parse_args()
    cdc = CDC(args.port)
    try:
        exercise(cdc)
        if args.visual:
            visual(cdc)
    finally:
        try:
            cdc.discard_startup()
            cdc.send("identify")
            cdc.expect("hello")
        finally:
            cdc.close()


if __name__ == "__main__":
    main()
