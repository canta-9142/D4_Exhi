"""PLAYER_PLAYING_BENCHを実機で計測。検証用の全描画・画素比較は計測時間から除外する。"""

import argparse
from collections import defaultdict
import json
from pathlib import Path
import statistics
from player_cdc import CDC


def run(cdc, records):
    cdc.write(b"\n")
    cdc.discard_startup()
    cdc.write(b"?\n")
    ready = cdc.expect("playing_ready")
    records.append({"type": "playing_ready", "data": ready})
    assert ready == {"ok": True, "psram_bytes": 614400, "dma_bytes": 20480}, ready
    cdc.write(b"b\n")
    cases = defaultdict(list)
    while True:
        message = cdc.read(30)
        assert message, "Playing benchmark timed out"
        records.append(message)
        if message["type"] == "playing_done":
            assert message["data"] == {"samples": 320, "reference_match": True, "buffer_states_valid": True}, message
            break
        assert message["type"] == "playing_sample", message
        data = message["data"]
        for key in ("draw_us", "pack_us", "scan_read_us", "scan_wait_us", "transfer_us", "frame_us", "bytes", "spans"):
            assert type(data[key]) is int and data[key] >= 0, data
        if data["scene"] == "idle":
            assert data["bytes"] == data["spans"] == data["scan_read_us"] == 0, data
        if data["scene"] == "finished":
            assert data["bytes"] > 0 and data["spans"] > 0, data
        assert data["bytes"] <= 307200, data
        if data["scene"] not in ("preparing", "phases", "results", "preview"):
            assert data["bytes"] < 307200, data
        assert data["frame_us"] >= data["draw_us"] + data["scan_read_us"] + data["scan_wait_us"] + data["transfer_us"], data
        cases[data["scene"]].append(data)
    assert set(cases) == {"idle", "cards", "buttons", "numbers", "mixed", "preparing", "phases", "finished", "results", "preview"}, cases.keys()
    assert all(len(rows) == 32 for rows in cases.values())
    print("PASS: 320 partial frames match a fresh full redraw, including Preparing, Finished, preview and phase changes; both buffer histories valid")
    print("scene    draw/ms pack/ms scan-wait/ms transfer/ms frame/ms p95/ms bytes")
    for scene, rows in cases.items():
        median = lambda key: statistics.median(row[key] for row in rows)
        p95 = sorted(row["frame_us"] for row in rows)[30] / 1000
        print(f"{scene:<8} {median('draw_us')/1000:7.3f} {median('pack_us')/1000:7.3f} "
              f"{median('scan_wait_us')/1000:12.3f} {median('transfer_us')/1000:11.3f} "
              f"{median('frame_us')/1000:8.3f} {p95:6.3f} {median('bytes'):6.0f}")
    print("Packing overlaps DMA; its time is partly included in transfer/ms. No buffer synchronization copies.")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--port", default="/dev/ttyACM0")
    parser.add_argument("--log", type=Path)
    args = parser.parse_args()
    records = []
    cdc = CDC(args.port)
    try:
        run(cdc, records)
    finally:
        cdc.close()
        if args.log:
            args.log.write_text("".join(json.dumps(record, separators=(",", ":")) + "\n" for record in records))


if __name__ == "__main__":
    main()
