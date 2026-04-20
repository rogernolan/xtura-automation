from __future__ import annotations

import argparse
import json
import queue
import threading
import time
from pathlib import Path

DEFAULT_BOOTSTRAP_MESSAGES = [
    "{\"messagetype\":96,\"messagecmd\":0,\"size\":306,\"data\":[152,0,1,0,4,0,5,0,6,0,7,0,9,0,10,0,11,0,20,0,21,0,22,0,23,0,27,0,30,0,31,0,32,0,33,0,34,0,35,0,36,0,37,0,38,0,39,0,40,0,41,0,51,0,52,0,53,0,54,0,55,0,56,0,57,0,58,0,59,0,60,0,68,0,69,0,70,0,71,0,72,0,73,0,78,0,79,0,83,0,76,0,49,0,50,0,225,0,226,0,227,0,228,0,229,0,230,0,231,0,232,0,233,0,45,0,46,0,47,0,48,0,77,0,84,0,85,0,177,0,178,0,172,0,179,0,2,0,38,1,3,0,237,0,238,0,239,0,12,0,13,0,61,0,62,0,63,0,14,0,66,0,25,0,24,0,74,0,75,0,101,0,102,0,103,0,105,0,106,0,107,0,108,0,110,0,113,0,114,0,115,0,119,0,97,0,87,0,88,0,89,0,90,0,91,0,92,0,96,0,99,0,98,0,248,0,153,0,15,0,16,0,17,0,18,0,19,0,111,0,93,0,95,0,240,0,26,0,200,0,201,0,202,0,203,0,204,0,205,0,206,0,208,0,209,0,211,0,212,0,213,0,214,0,215,0,216,0,217,0,218,0,199,0,221,0,222,0,223,0,181,0,182,0,185,0,183,0,197,0,196,0,28,0,220,0,195,0,180,0,189,0,191,0,190,0]}",
    "{\"messagetype\":96,\"messagecmd\":1,\"size\":2,\"data\":[0,0]}",
]
DEFAULT_HEARTBEAT_MESSAGE = "{\"messagetype\":128,\"messagecmd\":0,\"size\":1,\"data\":[0]}"


def build_headers(header_values: list[str] | None, cookie_values: list[str] | None) -> list[str]:
    headers = list(header_values or [])
    cookies = list(cookie_values or [])
    if cookies:
        headers.append(f"Cookie: {'; '.join(cookies)}")
    return headers


class Recorder:
    def __init__(self, path: Path) -> None:
        self.path = path
        self.path.parent.mkdir(parents=True, exist_ok=True)
        self._fh = self.path.open("a", encoding="utf-8")

    def write_frame(self, direction: str, raw: str, ts: float | None = None) -> None:
        try:
            parsed = json.loads(raw)
        except json.JSONDecodeError:
            parsed = None
        self._write_line(
            {
                "ts": time.time() if ts is None else ts,
                "direction": direction,
                "raw": raw,
                "parsed": parsed,
            }
        )

    def write_marker(self, label: str, ts: float | None = None) -> None:
        self._write_line(
            {
                "ts": time.time() if ts is None else ts,
                "marker": label,
            }
        )

    def _write_line(self, payload: dict) -> None:
        self._fh.write(json.dumps(payload) + "\n")
        self._fh.flush()

    def close(self) -> None:
        self._fh.close()


def extract_bootstrap_messages_from_har(path: Path) -> tuple[list[str], str | None]:
    startup = extract_startup_sequence_from_har(path)
    payload = json.loads(path.read_text(encoding="utf-8"))
    entries = payload.get("log", {}).get("entries", [])
    messages = entries[0].get("_webSocketMessages", []) if entries else []
    bootstrap = startup["initial_sends"]
    heartbeat: str | None = None
    for message in messages:
        if message.get("type") != "send":
            continue
        raw = str(message.get("data", ""))
        try:
            parsed = json.loads(raw)
        except json.JSONDecodeError:
            continue
        if parsed.get("messagetype") == 128 and heartbeat is None:
            heartbeat = raw
    return bootstrap, heartbeat


def extract_startup_sequence_from_har(path: Path) -> dict[str, list[str]]:
    payload = json.loads(path.read_text(encoding="utf-8"))
    entries = payload.get("log", {}).get("entries", [])
    messages = entries[0].get("_webSocketMessages", []) if entries else []
    bootstrap: list[str] = []
    post_first_receive_sends: list[str] = []
    seen_receive = False
    collecting_post_receive = False
    for message in messages:
        if message.get("type") == "receive":
            seen_receive = True
            if collecting_post_receive:
                break
            continue
        if message.get("type") != "send":
            continue
        raw = str(message.get("data", ""))
        try:
            parsed = json.loads(raw)
        except json.JSONDecodeError:
            continue
        if not seen_receive:
            bootstrap.append(raw)
            continue
        if parsed.get("messagetype") == 128:
            continue
        collecting_post_receive = True
        post_first_receive_sends.append(raw)
    return {"initial_sends": bootstrap, "post_first_receive_sends": post_first_receive_sends}


def format_close_marker(close_status_code: object, close_msg: object) -> str:
    code = "unknown" if close_status_code is None else str(close_status_code)
    message = "" if close_msg is None else str(close_msg)
    return f"closed: code={code} message={message}"


class LiveWebsocketClient:
    def __init__(
        self,
        ws_url: str,
        origin: str | None = None,
        bootstrap_from_har: Path | None = None,
        header_values: list[str] | None = None,
        cookie_values: list[str] | None = None,
        heartbeat_interval: float = 4.0,
        reconnect_delay: float = 1.0,
    ) -> None:
        self.ws_url = ws_url
        self.origin = origin
        self.headers = build_headers(header_values, cookie_values)
        self.heartbeat_interval = heartbeat_interval
        self.reconnect_delay = reconnect_delay
        self.bootstrap_messages: list[str] = list(DEFAULT_BOOTSTRAP_MESSAGES)
        self.heartbeat_message: str | None = DEFAULT_HEARTBEAT_MESSAGE
        if bootstrap_from_har and bootstrap_from_har.exists():
            self.bootstrap_messages, self.heartbeat_message = extract_bootstrap_messages_from_har(bootstrap_from_har)

    def iter_frames(self, catalog: dict[int, dict] | None = None):
        del catalog
        import websocket

        while True:
            frame_queue: queue.Queue[tuple[float, str, str] | None] = queue.Queue()
            heartbeat_stop = threading.Event()

            def send_json(ws_app: websocket.WebSocketApp, raw: str) -> None:
                ws_app.send(raw)

            def heartbeat_loop(ws_app: websocket.WebSocketApp) -> None:
                while not heartbeat_stop.wait(self.heartbeat_interval):
                    if self.heartbeat_message:
                        send_json(ws_app, self.heartbeat_message)

            def on_message(ws_app: websocket.WebSocketApp, message: str) -> None:
                del ws_app
                frame_queue.put((time.time(), "receive", message))

            def on_open(ws_app: websocket.WebSocketApp) -> None:
                for raw in self.bootstrap_messages:
                    send_json(ws_app, raw)
                if self.heartbeat_message:
                    thread = threading.Thread(target=heartbeat_loop, args=(ws_app,), daemon=True)
                    thread.start()

            def on_close(ws_app: websocket.WebSocketApp, close_status_code: object, close_msg: object) -> None:
                del ws_app, close_status_code, close_msg
                heartbeat_stop.set()
                frame_queue.put(None)

            ws_app = websocket.WebSocketApp(
                self.ws_url,
                header=self.headers,
                on_message=on_message,
                on_open=on_open,
                on_close=on_close,
            )
            thread = threading.Thread(target=lambda: ws_app.run_forever(origin=self.origin), daemon=True)
            thread.start()

            while True:
                item = frame_queue.get()
                if item is None:
                    break
                yield item
            time.sleep(self.reconnect_delay)


def _marker_loop(recorder: Recorder) -> None:
    try:
        while True:
            label = input().strip()
            if not label:
                label = "marker"
            recorder.write_marker(label)
    except EOFError:
        return


def _run_capture(args: argparse.Namespace) -> int:
    import websocket

    recorder = Recorder(Path(args.out))
    headers = build_headers(args.header, args.cookie)
    bootstrap_messages = list(args.send_json or DEFAULT_BOOTSTRAP_MESSAGES)
    heartbeat_message = args.heartbeat_json or DEFAULT_HEARTBEAT_MESSAGE

    if args.bootstrap_from_har:
        har_bootstrap, har_heartbeat = extract_bootstrap_messages_from_har(Path(args.bootstrap_from_har))
        if not bootstrap_messages:
            bootstrap_messages = har_bootstrap
        if heartbeat_message is None:
            heartbeat_message = har_heartbeat

    def send_json(ws_app: websocket.WebSocketApp, raw: str) -> None:
        ws_app.send(raw)
        recorder.write_frame("send", raw)

    marker_thread = threading.Thread(target=_marker_loop, args=(recorder,), daemon=True)
    marker_thread.start()
    try:
        recorder.write_marker("starting capture")
        while True:
            heartbeat_stop = threading.Event()
            reconnect_requested = {"value": True}

            def heartbeat_loop(ws_app: websocket.WebSocketApp, interval: float, raw: str) -> None:
                while not heartbeat_stop.wait(interval):
                    try:
                        send_json(ws_app, raw)
                    except Exception as exc:  # pragma: no cover - defensive for live runtime
                        recorder.write_marker(f"heartbeat error: {exc}")
                        return

            def on_message(ws_app: websocket.WebSocketApp, message: str) -> None:
                recorder.write_frame("receive", message)

            def on_error(ws_app: websocket.WebSocketApp, error: object) -> None:
                recorder.write_marker(f"error: {error}")

            def on_open(ws_app: websocket.WebSocketApp) -> None:
                recorder.write_marker("connected")
                for raw in bootstrap_messages:
                    send_json(ws_app, raw)
                if heartbeat_message:
                    thread = threading.Thread(
                        target=heartbeat_loop,
                        args=(ws_app, float(args.heartbeat_interval), heartbeat_message),
                        daemon=True,
                    )
                    thread.start()

            def on_close(ws_app: websocket.WebSocketApp, close_status_code: object, close_msg: object) -> None:
                heartbeat_stop.set()
                recorder.write_marker(format_close_marker(close_status_code, close_msg))
                if args.no_reconnect:
                    reconnect_requested["value"] = False
                    return
                recorder.write_marker(f"reconnecting in {float(args.reconnect_delay):.1f}s")

            ws_app = websocket.WebSocketApp(
                args.ws_url,
                header=headers,
                on_message=on_message,
                on_error=on_error,
                on_open=on_open,
                on_close=on_close,
            )
            ws_app.run_forever(origin=args.origin)
            if not reconnect_requested["value"]:
                break
            time.sleep(float(args.reconnect_delay))
    except KeyboardInterrupt:
        recorder.write_marker("stopped: keyboard interrupt")
    finally:
        recorder.close()
    return 0


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="capture_ws.py",
        description="Connect to a websocket and append inbound frames plus manual markers to an NDJSON capture file.",
    )
    parser.add_argument("--ws-url", required=True, help="websocket URL to connect to")
    parser.add_argument("--out", required=True, help="NDJSON file to append capture records to")
    parser.add_argument("--header", action="append", default=[], help="extra request header in 'Name: value' form")
    parser.add_argument("--cookie", action="append", default=[], help="cookie pair to include, for example session=abc")
    parser.add_argument("--origin", help="optional Origin header")
    parser.add_argument("--send-json", action="append", default=[], help="JSON payload to send immediately after connect")
    parser.add_argument("--heartbeat-json", help="JSON payload to send repeatedly after connect")
    parser.add_argument("--heartbeat-interval", type=float, default=4.0, help="seconds between heartbeat payloads")
    parser.add_argument("--bootstrap-from-har", help="Chrome HAR file to extract initial send frames and heartbeat from")
    parser.add_argument("--reconnect-delay", type=float, default=1.0, help="seconds to wait before reconnecting after a close")
    parser.add_argument("--no-reconnect", action="store_true", help="exit instead of reconnecting after a websocket close")
    return parser


def main(argv: list[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    return _run_capture(args)


if __name__ == "__main__":
    raise SystemExit(main())
