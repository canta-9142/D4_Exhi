"""Finished/previewのUSB CDC検証。--visualで表示確認を継続する。

連続運転の結果（順位なし、eligible=true・rank=null、window=30）を既定とし、一斉ゲーム方式の
順位付き・対象外の結果と、練習の終わり（Finishedでround=tutorial、summaryなし）も確かめる。
"""

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


def result(cdc, sync, score=2147483647, rank=None, eligible=True, top=None, bests=None, bottom=False):
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
    cdc.status(2147483647, 0, round="normal", window=30)
    result(cdc, sync)
    cdc.quiet(3.2)
    result(cdc, sync)
    cdc.quiet()
    print("PASS: full-range round summary (rank null), duplicate result, silent Finished", flush=True)

    # 一斉ゲーム方式の結果（順位付き、window=0）と、練習の終わり（summaryを待たない）。
    sync = cdc.reset([], "Finished")
    result(cdc, sync, rank=2)
    cdc.quiet(3.2)
    cdc.reset([], "Finished", with_status=False)
    cdc.status(round="tutorial")
    cdc.quiet(3.2)
    print("PASS: ranked simultaneous result; practice over in Finished waits for no summary", flush=True)

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
    # 対象者のrank=nullはラウンドの正しい組合せ。対象者の1〜8も一斉ゲーム方式で正しい。
    for rank in (None, 1, 8):
        result(cdc, sync, 1, rank)
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
    """結果の画面の目視確認。既定は連続運転の結果（rank=null、window=30）。

    animate: 通常の終了遷移で演出、ranked: 一斉ゲーム方式の順位2、unranked: 対象外、result: 既定へ戻す、
    preview: 確認表示、reset: 待機（Idle）へ移す、top N・bottom・bests KIND: ②・③の値、
    window N: 段階の残り秒数（0〜30。1以上でmanualなら両端に「もう一度遊ぶ」）、
    practice: 一斉ゲーム方式の練習の終わり（round=tutorial、summaryなし）、duplicate・resync・stop。
    """
    phase, score, rank, eligible, preview = "Finished", 2147483647, None, True, False
    top = None
    bottom = False
    best_kind = "other"
    mode = "manual"
    window, practice = 30, False
    sync = ""

    def send_result():
        result(cdc, sync, score, rank, eligible, top, sample_bests(best_kind, score, eligible), bottom)

    def send_status():
        if phase == "Finished":
            cdc.status(0 if practice else score, 0, round="tutorial" if practice else "normal",
                       window=0 if practice else window)
        else:
            cdc.status()

    def synchronize():
        nonlocal sync
        sync = cdc.reset([], phase, with_status=False)
        send_status()
        if phase == "Finished" and not preview and not practice:
            send_result()

    def resynchronize():
        nonlocal preview
        if preview:
            preview = False
            configure(cdc, mode)
        synchronize()

    configure(cdc, mode)
    synchronize()
    print("READY: final score 2147483647 / round result (no rank) / window 30", flush=True)
    print("Commands: animate/ranked/unranked/preview/result/top N/bottom/bests KIND/window N/practice/reset/"
          "duplicate/resync/stop", flush=True)
    while True:
        if select.select([sys.stdin], [], [], 0)[0]:
            line = sys.stdin.readline()
            if not line or line.strip() == "stop":
                return
            command = line.strip()
            if command == "duplicate":
                send_result()
            elif command == "animate":
                preview, phase, score, practice = False, "Playing", 345, False
                mode = "manual"
                configure(cdc, mode)
                synchronize()
                phase = "Finished"
                cdc.send("phase", phase=phase)
                send_status()
                send_result()
            elif command in ("ranked", "unranked", "preview", "result", "reset"):
                preview, practice = command == "preview", False
                phase = "Idle" if command == "reset" else "Finished"
                score, rank, eligible = {"unranked": (400, None, False), "ranked": (2147483647, 2, True)}.get(
                    command, (2147483647, None, True))
                top, bottom = None, False
                mode = "auto" if preview else "manual"
                configure(cdc, mode, preview)
                synchronize()
            elif command == "practice":
                preview, phase, practice, mode = False, "Finished", True, "manual"
                configure(cdc, mode)
                synchronize()
            elif command.startswith("window ") and command[7:].isdigit() and int(command[7:]) <= 30:
                # 段階の残り秒数だけを変える。同じ世代のstatusなので、結果とページはそのまま。
                window = int(command[7:])
                send_status()
            elif command.startswith("top ") and command[4:].isdigit() and 1 <= int(command[4:]) <= 50:
                # 上位%を付けた結果を静止表示で出す。animateはこの値を保って終了遷移を演出する。
                preview, phase, score, top, practice = False, "Finished", 345, int(command[4:]), False
                bottom = False
                mode = "manual"
                configure(cdc, mode)
                synchronize()
            elif command == "bottom":
                # 下位50%の結果（値を伏せてbottomHalfだけ）を静止表示で出す。animateもこの値を保つ。
                preview, phase, score, top, bottom, practice = False, "Finished", 345, None, True, False
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
                  f"bests={best_kind} window={window} practice={practice}", flush=True)
        message = cdc.read(.05)
        if message and message["type"] == "ready":
            # 両端の長押し（もう一度遊ぶ）。この確認では段階を変えず、拒否と同じくstatusだけを返す。
            assert message["data"] == {"sync": sync} and phase == "Finished" and window >= 1 and mode == "manual"
            print("PASS: READY from the result (PLAY AGAIN); kept on the result", flush=True)
            send_status()
        elif message:
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
