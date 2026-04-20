from __future__ import annotations

import argparse
import json
import time
from typing import Callable, Protocol

from capture_ws import DEFAULT_BOOTSTRAP_MESSAGES, build_headers


DEFAULT_WS_URL = "ws://192.168.1.1:8888/ws"
DEFAULT_ORIGIN = "http://192.168.1.1:8888"
DEFAULT_HEADERS = [
    "User-Agent: Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36",
    "Cache-Control: no-cache",
    "Pragma: no-cache",
    "Accept-Language: en-GB,en-US;q=0.9,en;q=0.8",
]
DEFAULT_READY_MESSAGES = [
    "{\"messagetype\":49,\"messagecmd\":1,\"size\":3,\"data\":[0,0,0]}",
    "{\"messagetype\":49,\"messagecmd\":1,\"size\":3,\"data\":[0,0,0]}",
]
EXTERIOR_LIGHTS_ON_MESSAGE = json.dumps(
    {"messagetype": 17, "messagecmd": 0, "size": 3, "data": [47, 0, 3]},
    separators=(",", ":"),
)


class Connection(Protocol):
    def send(self, raw: str) -> None: ...

    def recv(self) -> str: ...

    def settimeout(self, timeout: float) -> None: ...

    def close(self) -> None: ...


Connector = Callable[..., Connection]
Trace = Callable[[str], None]


def _is_timeout_exception(exc: BaseException) -> bool:
    return isinstance(exc, TimeoutError) or exc.__class__.__name__.endswith("TimeoutException")


def _extract_signal_id(raw: str) -> int | None:
    try:
        payload = json.loads(raw)
    except json.JSONDecodeError:
        return None
    data = payload.get("data")
    if not isinstance(data, list) or not data:
        return None
    signal_id = data[0]
    return signal_id if isinstance(signal_id, int) else None


def send_exterior_lights_on(
    *,
    ws_url: str,
    headers: list[str],
    origin: str | None,
    bootstrap_messages: list[str],
    ready_messages: list[str],
    timeout: float,
    post_send_delay: float,
    read_count_after_send: int = 0,
    settle_quiet_period: float = 0.75,
    settle_max_wait: float = 5.0,
    read_seconds_after_send: float = 0.0,
    trace: Trace | None = None,
    connector: Connector | None = None,
    time_source: Callable[[], float] = time.monotonic,
) -> None:
    if connector is None:
        import websocket

        connector = websocket.create_connection
    logger = trace or (lambda _message: None)
    logger(f"connect: {ws_url}")
    connection = connector(ws_url, header=headers, origin=origin, timeout=timeout)
    try:
        for raw in bootstrap_messages:
            logger(f"send(startup): {raw}")
            connection.send(raw)
        if ready_messages:
            connection.settimeout(timeout)
            logger("recv(wait-first-frame)")
            first_frame = connection.recv()
            logger(f"recv(first-frame): {first_frame}")
            for raw in ready_messages:
                logger(f"send(ready): {raw}")
                connection.send(raw)
        if settle_max_wait > 0 and settle_quiet_period > 0:
            logger("settle(start)")
            settle_deadline = time_source() + settle_max_wait
            while True:
                remaining = settle_deadline - time_source()
                if remaining <= 0:
                    logger("settle(done:max-wait)")
                    break
                connection.settimeout(min(timeout, settle_quiet_period, remaining))
                try:
                    settled_frame = connection.recv()
                except BaseException as exc:
                    if _is_timeout_exception(exc):
                        logger("settle(done:quiet)")
                        break
                    raise
                signal_id = _extract_signal_id(settled_frame)
                if signal_id in {47, 48}:
                    logger(f"recv(settle): {settled_frame}")
                    logger("settle(done:signal)")
                    break
        logger(f"send(toggle): {EXTERIOR_LIGHTS_ON_MESSAGE}")
        connection.send(EXTERIOR_LIGHTS_ON_MESSAGE)
        for _ in range(read_count_after_send):
            connection.settimeout(timeout)
            try:
                response = connection.recv()
            except BaseException as exc:
                if _is_timeout_exception(exc):
                    break
                raise
            logger(f"recv(after-send): {response}")
        if read_seconds_after_send > 0:
            read_deadline = time_source() + read_seconds_after_send
            while True:
                remaining = read_deadline - time_source()
                if remaining <= 0:
                    break
                connection.settimeout(min(timeout, remaining))
                try:
                    response = connection.recv()
                except BaseException as exc:
                    if _is_timeout_exception(exc):
                        continue
                    raise
                if _extract_signal_id(response) in {47, 48}:
                    logger(f"recv(after-send): {response}")
        if post_send_delay > 0:
            time.sleep(post_send_delay)
    finally:
        logger("close")
        connection.close()


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="send_exterior_lights_on.py",
        description="Connect to the Garmin websocket, send the exterior-lights action frame, and disconnect.",
    )
    parser.add_argument("--ws-url", default=DEFAULT_WS_URL, help="websocket URL to connect to")
    parser.add_argument("--origin", default=DEFAULT_ORIGIN, help="Origin header value")
    parser.add_argument("--header", action="append", default=[], help="extra request header in 'Name: value' form")
    parser.add_argument("--cookie", action="append", default=[], help="cookie pair to include, for example session=abc")
    parser.add_argument("--timeout", type=float, default=5.0, help="connection timeout in seconds")
    parser.add_argument("--post-send-delay", type=float, default=0.25, help="seconds to wait after sending before close")
    parser.add_argument("--read-count-after-send", type=int, default=0, help="number of frames to read and print after sending")
    parser.add_argument("--settle-quiet-period", type=float, default=0.75, help="seconds of silence that count as settled before sending")
    parser.add_argument("--settle-max-wait", type=float, default=5.0, help="maximum seconds to wait for the session to settle before sending")
    parser.add_argument("--read-seconds-after-send", type=float, default=0.0, help="seconds to keep reading after send and print only signal 47/48 frames")
    parser.add_argument("--debug", action="store_true", help="print sent and received frames during the exchange")
    return parser


def main(argv: list[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)

    headers = list(DEFAULT_HEADERS)
    headers.extend(build_headers(args.header, args.cookie))

    trace = print if args.debug else None
    try:
        send_exterior_lights_on(
            ws_url=args.ws_url,
            headers=headers,
            origin=args.origin,
            bootstrap_messages=list(DEFAULT_BOOTSTRAP_MESSAGES),
            ready_messages=list(DEFAULT_READY_MESSAGES),
            timeout=float(args.timeout),
            post_send_delay=float(args.post_send_delay),
            read_count_after_send=int(args.read_count_after_send),
            settle_quiet_period=float(args.settle_quiet_period),
            settle_max_wait=float(args.settle_max_wait),
            read_seconds_after_send=float(args.read_seconds_after_send),
            trace=trace,
        )
    except Exception as exc:
        if args.debug:
            print(f"error: {type(exc).__name__}: {exc}")
        raise
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
