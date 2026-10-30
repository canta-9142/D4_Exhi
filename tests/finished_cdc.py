"""Finished/previewのUSB CDC検証。--visualで表示確認を継続する。"""

import argparse
import select
import sys
import uuid
from player_cdc import CDC


# ベストスコアの項目（summaryの必須項目）。nullは記録を使えず対象者もいないとき。
NO_BESTS = {"gameBest": None, "gameBestNodes": [], "hourBest": None, "hourBestStart": None,
            "hourBestMine": False, "allTimeBest": None, "allTimeBestMine": False}


def sample_bests(kind, score, eligible):
    """③の目視用。other: 他人のベスト、mine: 全部自分（ノードAとCの同点）、max: 最大幅、none: すべてnull。"""
    if kind == "none":
        return dict(NO_BESTS)
    if kind == "max":
        best, nodes, start = 2147483647, list(range(1, 9)), 0
    elif kind == "mine":
        best, nodes, start = score, [1, 3], 23
    else:
        best, nodes, start = 410, [3], 13
    mine = kind == "mine" and eligible
    return {"gameBest": best, "gameBestNodes": nodes, "hourBest": best, "hourBestStart": start,
            "hourBestMine": mine, "allTimeBest": best if kind != "other" else 520, "allTimeBestMine": mine}


def result(cdc, sync, score=2147483647, rank=2, eligible=True, top=None, bests=None, bottom=False):
    cdc.send("summary", sync=sync, score=score, rank=rank, eligible=eligible, topPercent=top, bottomHalf=bottom,
             **(bests if bests is not None else NO_BESTS))


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
    valid = {**NO_BESTS, "score": 1, "rank": 1, "eligible": True, "topPercent": None, "bottomHalf": False}
    invalid = [{**NO_BESTS, "bottomHalf": False, **fields} for fields in invalid] + [
        {**valid, **changes} for changes in (
            {"gameBest": -1}, {"gameBest": 1}, {"gameBestNodes": [1]}, {"gameBestNodes": None},
            {"gameBest": 1, "gameBestNodes": [2, 1]}, {"gameBest": 1, "gameBestNodes": [9]},
            {"hourBest": 1}, {"hourBestStart": 13}, {"hourBest": 1, "hourBestStart": 24},
            {"hourBestMine": True}, {"hourBestMine": None}, {"allTimeBest": 2, "allTimeBestMine": True},
            # bottomHalfは対象者でtopPercentがnullのときだけtrue。
            {"bottomHalf": 1}, {"bottomHalf": None}, {"topPercent": 12, "bottomHalf": True},
            {"rank": None, "eligible": False, "bottomHalf": True},
        )
    ] + [{key: value for key, value in valid.items() if key != missing} for missing in [*NO_BESTS, "bottomHalf"]]
    for fields in invalid:
        cdc.send("summary", sync=sync, **fields)
        cdc.expect("resync")
        sync = cdc.reset([], "Finished")
        result(cdc, sync)
        cdc.quiet(.05)
    for top in (1, 50):
        result(cdc, sync, top=top)
    for kind in ("other", "mine", "max"):
        result(cdc, sync, bests=sample_bests(kind, 2147483647, True))
    result(cdc, sync, bottom=True)
    cdc.quiet(.05)
    print("PASS: invalid summary fields request resync; topPercent 1 and 50, bottomHalf and best scores accepted",
          flush=True)

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
    top = None
    bottom = False
    best_kind = "other"
    mode = "manual"
    sync = ""

    def send_result():
        result(cdc, sync, score, rank, eligible, top, sample_bests(best_kind, score, eligible), bottom)

    def synchronize():
        nonlocal sync
        sync = cdc.reset([], phase)
        if phase == "Finished" and not preview:
            send_result()

    def resynchronize():
        nonlocal preview
        if preview:
            preview = False
            configure(cdc, mode)
        synchronize()

    configure(cdc, mode)
    synchronize()
    print("READY: final score 2147483647 / RANK 2", flush=True)
    print("Commands: animate/unranked/preview/result/top N/bottom/bests KIND/reset/duplicate/resync/stop", flush=True)
    while True:
        if select.select([sys.stdin], [], [], 0)[0]:
            line = sys.stdin.readline()
            if not line or line.strip() == "stop":
                return
            command = line.strip()
            if command == "duplicate":
                send_result()
            elif command == "animate":
                preview, phase, score, rank, eligible = False, "Playing", 345, 2, True
                mode = "manual"
                configure(cdc, mode)
                synchronize()
                phase = "Finished"
                cdc.send("phase", phase=phase)
                cdc.status(score, 0, True)
                send_result()
            elif command in ("unranked", "preview", "result", "reset"):
                preview = command == "preview"
                phase = "Preparing" if command == "reset" else "Finished"
                score, rank, eligible = (400, None, False) if command == "unranked" else (2147483647, 2, True)
                top, bottom = None, False
                mode = "auto" if preview else "manual"
                configure(cdc, mode, preview)
                synchronize()
            elif command.startswith("top ") and command[4:].isdigit() and 1 <= int(command[4:]) <= 50:
                # 上位%を付けた結果を静止表示で出す。animateはこの値を保って終了遷移を演出する。
                preview, phase, score, rank, eligible, top = False, "Finished", 345, 2, True, int(command[4:])
                bottom = False
                mode = "manual"
                configure(cdc, mode)
                synchronize()
            elif command == "bottom":
                # 下位50%の結果（値を伏せてbottomHalfだけ）を静止表示で出す。animateもこの値を保つ。
                preview, phase, score, rank, eligible, top, bottom = False, "Finished", 345, 2, True, None, True
                mode = "manual"
                configure(cdc, mode)
                synchronize()
            elif command.startswith("bests ") and command[6:] in ("other", "mine", "max", "none"):
                # ③の値を変える。以後の結果にも同じ種類のベストを付ける。新しい世代なので①から静止表示で出す。
                best_kind = command[6:]
                synchronize()
            elif command == "resync":
                resynchronize()
            print(f"STATE: {phase} preview={preview} score={score} rank={rank} topPercent={top} bottomHalf={bottom} "
                  f"bests={best_kind}", flush=True)
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
