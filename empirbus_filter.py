from __future__ import annotations

import argparse
import json
import sys
import time
from collections import Counter
from pathlib import Path
from typing import Iterable

from capture_ws import LiveWebsocketClient

DEFAULT_WS_URL = "ws://192.168.1.1:8888/ws"
DEFAULT_SIGNALS_PATH = Path(__file__).with_name("signal-info.json")
FRAME_GROUPS: dict[str, set[int]] = {
    "lights": {
        3, 9, 10, 20, 21, 22, 23, 30, 32, 34, 36, 38, 40, 45, 46, 47, 48, 76, 227, 230, 233,
    },
    "heating": {
        14, 15, 26, 87, 88, 89, 90, 91, 92, 93, 95, 97, 98, 99, 100, 101, 102, 103, 104, 105,
        106, 107, 108, 109, 110, 111, 112, 113, 114, 115, 116, 117, 118, 119, 120,
    },
    "fuses": {
        2, 31, 33, 35, 37, 39, 41, 44, 49, 50, 51, 52, 53, 54, 55, 56, 57, 58, 59, 60, 62, 63,
        64, 66, 68, 69, 70, 71, 72, 73, 74, 75, 78, 79, 82, 83, 84, 85, 86, 124, 127, 128, 129,
        131, 132, 134, 135, 136, 137, 138, 139, 140, 141, 144, 145, 172, 177, 178, 179, 225,
        226, 228, 229, 231, 232, 235, 236,
    },
    "water": {
        4, 5, 12, 13, 24, 25, 27,
    },
    "options": {
        150, 151, 152, 153, 237, 238, 239, 240, 248, 294,
    },
    "power": {
        16, 17, 18, 19, 28, 29, 61, 65, 94, 96, 180, 181, 182, 183, 184, 185, 186, 189, 190,
        191, 192, 193, 194, 195, 196, 197, 199, 200, 201, 202, 203, 204, 205, 206, 207, 208,
        209, 210, 211, 212, 213, 214, 215, 216, 217, 218, 219, 220, 221, 222, 223,
    },
}


def load_signal_catalog(path: Path | None = None) -> dict[int, dict]:
    candidate = path or DEFAULT_SIGNALS_PATH
    if not candidate.exists():
        return {}
    payload = json.loads(candidate.read_text(encoding="utf-8"))
    return {item["signalId"]: item for item in payload if isinstance(item.get("signalId"), int)}


def classify_group(signal_id: int | None) -> str | None:
    if signal_id is None:
        return None
    for name, signal_ids in FRAME_GROUPS.items():
        if signal_id in signal_ids:
            return name
    return None


def normalize_live_frame(ts: float, direction: str, raw: str, catalog: dict[int, dict]) -> dict | None:
    try:
        parsed = json.loads(raw)
    except json.JSONDecodeError:
        return None
    if not isinstance(parsed, dict):
        return None
    data = parsed.get("data")
    if not isinstance(data, list):
        data = []
    signal_id = data[0] if data and isinstance(data[0], int) else None
    signal_info = catalog.get(signal_id) if isinstance(signal_id, int) else None
    return {
        "ts": float(ts),
        "direction": direction,
        "raw": raw,
        "parsed": parsed,
        "message_type": parsed.get("messagetype"),
        "message_cmd": parsed.get("messagecmd"),
        "size": parsed.get("size"),
        "data": data,
        "signal_id": signal_id,
        "signal_label": signal_info.get("description") if signal_info else None,
        "group": classify_group(signal_id),
    }


def frame_family_key(frame: dict) -> tuple:
    data = frame.get("data") or []
    return (
        frame.get("direction"),
        frame.get("message_type"),
        frame.get("message_cmd"),
        frame.get("size"),
        len(data),
        data[0] if len(data) > 0 else None,
        data[1] if len(data) > 1 else None,
    )


def classify_noisy_families(frames: list[dict], threshold: int = 3) -> set[tuple]:
    counts = Counter(frame_family_key(frame) for frame in frames)
    return {key for key, count in counts.items() if count >= threshold}


def iter_source_frames(args: argparse.Namespace, catalog: dict[int, dict]) -> Iterable[dict]:
    if args.source:
        yield from _iter_replay_frames(Path(args.source), catalog)
        return
    bootstrap_har = Path(args.bootstrap_from_har) if args.bootstrap_from_har else None
    client = LiveWebsocketClient(
        ws_url=args.ws_url,
        origin=args.origin,
        bootstrap_from_har=bootstrap_har,
        header_values=args.header or [],
        heartbeat_interval=float(args.heartbeat_interval),
        reconnect_delay=float(args.reconnect_delay),
    )
    for ts, direction, raw in client.iter_frames():
        frame = normalize_live_frame(ts, direction, raw, catalog)
        if frame:
            yield frame


def _iter_replay_frames(path: Path, catalog: dict[int, dict]) -> Iterable[dict]:
    if path.suffix.lower() == ".har":
        payload = json.loads(path.read_text(encoding="utf-8"))
        for entry in payload.get("log", {}).get("entries", []):
            for message in entry.get("_webSocketMessages", []):
                frame = normalize_live_frame(
                    float(message.get("time", 0.0)),
                    str(message.get("type", "receive")),
                    str(message.get("data", "")),
                    catalog,
                )
                if frame:
                    yield frame
        return
    for line in path.read_text(encoding="utf-8").splitlines():
        if not line.strip():
            continue
        record = json.loads(line)
        raw = record.get("raw")
        if not raw:
            continue
        frame = normalize_live_frame(
            float(record.get("ts", 0.0)),
            str(record.get("direction", "receive")),
            str(raw),
            catalog,
        )
        if frame:
            yield frame


def _print_learning_header(group: str, ws_url: str) -> None:
    print(f"learning 30s [{group}] {ws_url}")


def _print_learning_glyph(seen_families: set[tuple], family_key: tuple, glyph_count: int) -> int:
    glyph = "!" if family_key not in seen_families else "."
    seen_families.add(family_key)
    print(glyph, end="", flush=True)
    glyph_count += 1
    if glyph_count % 80 == 0:
        print()
    return glyph_count


def _flush_learning_line(glyph_count: int) -> int:
    if glyph_count:
        print()
    return 0


def _format_frame(frame: dict) -> str:
    stamp = time.strftime("%H:%M:%S", time.localtime(frame["ts"])) + f".{int((frame['ts'] % 1) * 1000):03d}"
    label = frame.get("signal_label")
    label_part = f' label="{label}"' if label else ""
    return (
        f"{stamp} {frame['direction']} type={frame.get('message_type')} cmd={frame.get('message_cmd')} "
        f"signal={frame.get('signal_id')}{label_part} data={frame.get('data')}"
    )


def _format_burst_summary(suppressed_count: int, family_count: int, first_ts: float, last_ts: float) -> str:
    duration = max(0.0, last_ts - first_ts)
    return f"burst suppressed={suppressed_count} families={family_count} span={duration:.3f}s"


def run_stream(args: argparse.Namespace) -> int:
    catalog = load_signal_catalog(Path(args.signals) if args.signals else None)
    frames = iter_source_frames(args, catalog)
    learn_seconds = float(args.learn_seconds)
    threshold = int(args.noise_threshold)
    selected_group = args.filter_group

    learn_frames: list[dict] = []
    seen_families: set[tuple] = set()
    glyph_count = 0
    learn_cutoff_ts: float | None = None
    learning_started = False
    noisy_families: set[tuple] | None = None
    suppressed_count = 0
    suppressed_families: set[tuple] = set()
    suppressed_first_ts: float | None = None
    suppressed_last_ts: float | None = None

    _print_learning_header(selected_group, args.ws_url)

    for frame in frames:
        if frame.get("group") != selected_group:
            continue
        if not learning_started:
            learn_cutoff_ts = frame["ts"] + learn_seconds
            learning_started = True

        if frame.get("direction") == "send":
            glyph_count = _flush_learning_line(glyph_count)
            if suppressed_count and suppressed_first_ts is not None and suppressed_last_ts is not None:
                print(_format_burst_summary(suppressed_count, len(suppressed_families), suppressed_first_ts, suppressed_last_ts))
                suppressed_count = 0
                suppressed_families.clear()
                suppressed_first_ts = None
                suppressed_last_ts = None
            print(_format_frame(frame))
            continue

        if learn_cutoff_ts is not None and frame["ts"] < learn_cutoff_ts:
            learn_frames.append(frame)
            glyph_count = _print_learning_glyph(seen_families, frame_family_key(frame), glyph_count)
            continue

        if noisy_families is None:
            noisy_families = classify_noisy_families(learn_frames, threshold=threshold)
        glyph_count = _flush_learning_line(glyph_count)
        family_key = frame_family_key(frame)
        if family_key in noisy_families:
            suppressed_count += 1
            suppressed_families.add(family_key)
            suppressed_first_ts = frame["ts"] if suppressed_first_ts is None else suppressed_first_ts
            suppressed_last_ts = frame["ts"]
            continue
        if suppressed_count and suppressed_first_ts is not None and suppressed_last_ts is not None:
            print(_format_burst_summary(suppressed_count, len(suppressed_families), suppressed_first_ts, suppressed_last_ts))
            suppressed_count = 0
            suppressed_families.clear()
            suppressed_first_ts = None
            suppressed_last_ts = None
        print(_format_frame(frame))

    glyph_count = _flush_learning_line(glyph_count)
    if suppressed_count and suppressed_first_ts is not None and suppressed_last_ts is not None:
        print(_format_burst_summary(suppressed_count, len(suppressed_families), suppressed_first_ts, suppressed_last_ts))
    return 0


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="empirbus_filter.py",
        description="Live EmpirBus CLI that learns noisy frame families for one signal group and then streams decoded frames.",
    )
    parser.add_argument("-f", "--filter-group", choices=sorted(FRAME_GROUPS), required=True, help="select one signal group: lights|heating|fuses|water|options|power")
    parser.add_argument("--ws-url", default=DEFAULT_WS_URL, help="websocket URL to connect to")
    parser.add_argument("--origin", default="http://192.168.1.1:8888", help="Origin header for the websocket connection")
    parser.add_argument("--bootstrap-from-har", help="optional HAR file to override the built-in bootstrap and heartbeat messages")
    parser.add_argument("--signals", help="optional signal-info.json override")
    parser.add_argument("--source", help="optional HAR or NDJSON file to replay instead of connecting live")
    parser.add_argument("--learn-seconds", type=float, default=30.0, help=argparse.SUPPRESS)
    parser.add_argument("--noise-threshold", type=int, default=3, help=argparse.SUPPRESS)
    parser.add_argument("--heartbeat-interval", type=float, default=4.0, help=argparse.SUPPRESS)
    parser.add_argument("--reconnect-delay", type=float, default=1.0, help=argparse.SUPPRESS)
    parser.add_argument("--header", action="append", default=[], help=argparse.SUPPRESS)
    return parser


def main(argv: list[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    return run_stream(args)


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
