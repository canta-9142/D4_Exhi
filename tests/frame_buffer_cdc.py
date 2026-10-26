"""PSRAM実機ベンチ。--mode diff-benchmarkで全画面比較による差分転送を測る。"""

import argparse
from collections import defaultdict
import json
from pathlib import Path
import statistics

from player_cdc import CDC


def differential_results(cdc, records):
    cases = defaultdict(list)
    while True:
        message = cdc.read(30)
        assert message, "Differential benchmark response timed out"
        records.append(message)
        if message["type"] == "diff_done":
            assert message["data"] == {"samples": 256, "front_unchanged": True}, message
            break
        assert message["type"] == "diff_sample", message
        data = message["data"]
        for key in ("draw_us", "compare_us", "pack_us", "submit_us", "wait_us", "transfer_us", "frame_us", "bytes", "spans", "dirty_tiles"):
            assert type(data[key]) is int and data[key] >= 0, data
        assert data["frame_us"] >= data["draw_us"] + data["compare_us"] + data["transfer_us"], data
        assert data["bytes"] == data["dirty_tiles"] * 16 * 16 * 2, data
        assert data["transfer_us"] >= data["bytes"] * 0.1 * 0.9, data
        if data["strategy"] == "full":
            assert (data["compare_us"], data["bytes"], data["spans"]) == (0, 307200, 30), data
        else:
            assert data["strategy"] == "diff" and data["compare_us"] > 0, data
            if data["scene"] == "static":
                assert data["bytes"] == data["spans"] == data["transfer_us"] == 0, data
            elif data["scene"] == "small":
                assert 0 < data["bytes"] <= 8192, data
            elif data["scene"] == "scattered":
                assert (data["bytes"], data["spans"], data["dirty_tiles"]) == (2048, 4, 4), data
            elif data["scene"] == "full":
                assert (data["bytes"], data["spans"], data["dirty_tiles"]) == (307200, 30, 600), data
        cases[data["scene"], data["strategy"]].append(data)
    expected = {(scene, strategy) for scene in ("static", "small", "scattered", "full") for strategy in ("full", "diff")}
    assert set(cases) == expected and all(len(rows) == 32 for rows in cases.values()), cases.keys()
    print("PASS: 256 frames, static transfer skipped, sparse changes across the screen, dense changes, front unchanged")
    print("scene      strategy  draw/ms  compare/ms  pack/ms  transfer/ms  frame/ms  p95/ms  bytes  spans")
    for scene in ("static", "small", "scattered", "full"):
        for strategy in ("full", "diff"):
            rows = cases[scene, strategy]
            median = lambda key: statistics.median(row[key] for row in rows)
            p95 = sorted(row["frame_us"] for row in rows)[30] / 1000
            print(f"{scene:<10} {strategy:<8} {median('draw_us')/1000:7.3f} {median('compare_us')/1000:11.3f} "
                  f"{median('pack_us')/1000:8.3f} {median('transfer_us')/1000:12.3f} {median('frame_us')/1000:9.3f} "
                  f"{p95:7.3f} {median('bytes'):6.0f} {median('spans'):6.0f}")
    print("Both strategies redraw the whole screen; packing overlaps DMA, drawing finishes before comparison/transfer.")
    print("frame/ms excludes checksum verification, USB output, and the scheduler yield.")


def run(cdc, mode, records):
    cdc.write(b"\n")
    cdc.discard_startup()
    cdc.write(b"?\n")
    ready = cdc.expect("bench_ready")
    records.append({"type": "bench_ready", "data": ready})
    assert ready["ok"] and ready["self_test"], ready
    assert (ready["width"], ready["height"], ready["psram_bytes"], ready["dma_bytes"]) == (480, 320, 614400, 20480), ready
    print("PASS: 600 KiB in PSRAM, two 10 KiB DMA buffers, transpose/color/page isolation", flush=True)
    if mode == "info":
        return

    commands = {"benchmark": b"b\n", "pattern": b"p\n", "full": b"f\n", "partial": b"r\n", "stop": b"s\n",
                "diff-benchmark": b"d\n", "diff-static": b"u\n", "diff-small": b"m\n",
                "diff-scattered": b"n\n", "diff-full": b"c\n"}
    cdc.write(commands[mode])
    if mode == "diff-benchmark":
        differential_results(cdc, records)
        return
    if mode != "benchmark":
        reply = cdc.expect("bench_mode")
        records.append({"type": "bench_mode", "data": reply})
        assert reply["mode"].encode() == commands[mode][:1], reply
        return

    cases = defaultdict(list)
    while True:
        message = cdc.read(30)
        assert message, "Benchmark response timed out"
        records.append(message)
        if message["type"] == "bench_done":
            assert message["data"] == {"samples": 128, "front_unchanged": True}, message
            break
        assert message["type"] == "bench_sample", message
        data = message["data"]
        assert type(data["pipeline"]) is bool, data
        for key in ("draw_us", "pack_us", "submit_us", "wait_us", "transfer_us", "frame_us"):
            assert type(data[key]) is int and data[key] >= 0, data
        assert data["frame_us"] >= data["transfer_us"] > 0 and data["draw_us"] > 0, data
        # SPI 80 MHzの画素データ量から求める下限。10%はクロック・計測の余裕。
        assert data["transfer_us"] >= data["width"] * data["height"] * 0.2 * 0.9, data
        cases[data["width"], data["height"], data["pipeline"]].append(data)

    expected = {(w, h, pipeline) for w, h in ((200, 240), (480, 320)) for pipeline in (False, True)}
    assert set(cases) == expected and all(len(rows) == 32 for rows in cases.values()), cases.keys()
    print("PASS: 128 frames, front unchanged while drawing back", flush=True)
    print("area       pipeline  draw/ms  pack/ms  submit+wait/ms  transfer/ms  frame/ms  frame-p95/ms")
    for (width, height, pipeline), rows in sorted(cases.items()):
        median = lambda key: statistics.median(row[key] for row in rows) / 1000
        dma_ms = statistics.median(row["submit_us"] + row["wait_us"] for row in rows) / 1000
        p95_ms = sorted(row["frame_us"] for row in rows)[30] / 1000
        print(f"{width}x{height:<3}  {str(pipeline):<8}  {median('draw_us'):7.3f}  {median('pack_us'):7.3f}  "
              f"{dma_ms:14.3f}  {median('transfer_us'):11.3f}  {median('frame_us'):8.3f}  {p95_ms:12.3f}")
    print("pipeline transfer/ms includes drawing the next image; submit+wait/ms excludes DMA time hidden by CPU work.")
    print("frame/ms excludes checksums, USB output, and the 1 ms scheduler yield.")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--port", default="/dev/ttyACM0")
    parser.add_argument("--mode", choices=("benchmark", "pattern", "full", "partial", "stop", "info",
                                          "diff-benchmark", "diff-static", "diff-small", "diff-scattered", "diff-full"),
                        default="benchmark")
    parser.add_argument("--log", type=Path)
    args = parser.parse_args()
    records = []
    cdc = CDC(args.port)
    try:
        run(cdc, args.mode, records)
    finally:
        cdc.close()
        if args.log:
            args.log.write_text("".join(json.dumps(record, separators=(",", ":")) + "\n" for record in records))


if __name__ == "__main__":
    main()
