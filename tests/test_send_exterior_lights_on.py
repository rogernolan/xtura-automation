import unittest

from send_exterior_lights_on import (
    DEFAULT_HEADERS,
    DEFAULT_READY_MESSAGES,
    DEFAULT_WS_URL,
    DEFAULT_ORIGIN,
    EXTERIOR_LIGHTS_ON_MESSAGE,
    send_exterior_lights_on,
)
from capture_ws import DEFAULT_BOOTSTRAP_MESSAGES


class FakeConnection:
    def __init__(self) -> None:
        self.sent: list[str] = []
        self.closed = False
        self.recv_calls = 0
        self.timeout_values: list[float] = []
        self.recv_queue = ["{\"messagetype\":48,\"messagecmd\":0,\"size\":5,\"data\":[1,2,3,4,5]}"]

    def send(self, raw: str) -> None:
        self.sent.append(raw)

    def recv(self) -> str:
        self.recv_calls += 1
        if not self.recv_queue:
            raise TimeoutError("timed out")
        item = self.recv_queue.pop(0)
        if isinstance(item, BaseException):
            raise item
        return item

    def settimeout(self, timeout: float) -> None:
        self.timeout_values.append(timeout)

    def close(self) -> None:
        self.closed = True


class SendExteriorLightsOnTests(unittest.TestCase):
    def test_send_exterior_lights_on_replays_full_startup_sequence_then_closes(self) -> None:
        connection = FakeConnection()
        captured: dict[str, object] = {}

        def fake_connector(url: str, *, header: list[str], origin: str | None, timeout: float) -> FakeConnection:
            captured["url"] = url
            captured["header"] = header
            captured["origin"] = origin
            captured["timeout"] = timeout
            return connection

        send_exterior_lights_on(
            ws_url="ws://example.test/ws",
            headers=["X-Test: one"],
            origin="http://example.test",
            bootstrap_messages=["{\"messagetype\":96}", "{\"messagetype\":96,\"messagecmd\":1}"],
            ready_messages=["{\"messagetype\":49}", "{\"messagetype\":49}"],
            timeout=2.5,
            post_send_delay=0.0,
            settle_max_wait=0.0,
            connector=fake_connector,
        )

        self.assertEqual("ws://example.test/ws", captured["url"])
        self.assertEqual(["X-Test: one"], captured["header"])
        self.assertEqual("http://example.test", captured["origin"])
        self.assertEqual(2.5, captured["timeout"])
        self.assertEqual(
            [
                "{\"messagetype\":96}",
                "{\"messagetype\":96,\"messagecmd\":1}",
                "{\"messagetype\":49}",
                "{\"messagetype\":49}",
                EXTERIOR_LIGHTS_ON_MESSAGE,
            ],
            connection.sent,
        )
        self.assertEqual(1, connection.recv_calls)
        self.assertEqual([2.5], connection.timeout_values)
        self.assertTrue(connection.closed)

    def test_send_exterior_lights_on_reads_followup_frames_when_requested(self) -> None:
        connection = FakeConnection()
        connection.recv_queue = [
            "{\"messagetype\":48,\"messagecmd\":0,\"size\":5,\"data\":[1,2,3,4,5]}",
            "{\"messagetype\":16,\"messagecmd\":0,\"size\":3,\"data\":[48,0,1]}",
            "{\"messagetype\":16,\"messagecmd\":0,\"size\":3,\"data\":[47,0,1]}",
        ]
        traces: list[str] = []

        send_exterior_lights_on(
            ws_url="ws://example.test/ws",
            headers=[],
            origin=None,
            bootstrap_messages=["{\"messagetype\":96}"],
            ready_messages=["{\"messagetype\":49}"],
            timeout=1.0,
            post_send_delay=0.0,
            read_count_after_send=2,
            settle_max_wait=0.0,
            trace=traces.append,
            connector=lambda *args, **kwargs: connection,
        )

        self.assertEqual(3, connection.recv_calls)
        self.assertEqual([1.0, 1.0, 1.0], connection.timeout_values)
        self.assertIn('recv(after-send): {"messagetype":16,"messagecmd":0,"size":3,"data":[48,0,1]}', traces)
        self.assertIn('recv(after-send): {"messagetype":16,"messagecmd":0,"size":3,"data":[47,0,1]}', traces)

    def test_send_exterior_lights_on_waits_for_quiet_before_toggle(self) -> None:
        connection = FakeConnection()
        connection.recv_queue = [
            "{\"messagetype\":48,\"messagecmd\":0,\"size\":5,\"data\":[1,2,3,4,5]}",
            "{\"messagetype\":16,\"messagecmd\":0,\"size\":3,\"data\":[190,0,0]}",
            TimeoutError("timed out"),
        ]
        traces: list[str] = []

        send_exterior_lights_on(
            ws_url="ws://example.test/ws",
            headers=[],
            origin=None,
            bootstrap_messages=["{\"messagetype\":96}"],
            ready_messages=["{\"messagetype\":49}"],
            timeout=1.0,
            post_send_delay=0.0,
            settle_quiet_period=0.2,
            settle_max_wait=2.0,
            trace=traces.append,
            connector=lambda *args, **kwargs: connection,
            time_source=lambda: 0.0,
        )

        self.assertEqual(
            [
                "{\"messagetype\":96}",
                "{\"messagetype\":49}",
                EXTERIOR_LIGHTS_ON_MESSAGE,
            ],
            connection.sent,
        )
        self.assertIn("settle(start)", traces)
        self.assertIn("settle(done:quiet)", traces)
        self.assertEqual([1.0, 0.2, 0.2], connection.timeout_values)

    def test_send_exterior_lights_on_reads_light_frames_for_time_window(self) -> None:
        connection = FakeConnection()
        connection.recv_queue = [
            "{\"messagetype\":48,\"messagecmd\":0,\"size\":5,\"data\":[1,2,3,4,5]}",
            "{\"messagetype\":16,\"messagecmd\":0,\"size\":3,\"data\":[190,0,0]}",
            "{\"messagetype\":16,\"messagecmd\":0,\"size\":3,\"data\":[48,0,1]}",
            "{\"messagetype\":16,\"messagecmd\":0,\"size\":3,\"data\":[47,0,1]}",
            TimeoutError("timed out"),
        ]
        traces: list[str] = []
        ticks = iter([0.0, 0.0, 0.1, 0.2, 0.4, 0.8, 0.9, 1.0, 1.1, 1.2])

        send_exterior_lights_on(
            ws_url="ws://example.test/ws",
            headers=[],
            origin=None,
            bootstrap_messages=["{\"messagetype\":96}"],
            ready_messages=[],
            timeout=1.0,
            post_send_delay=0.0,
            settle_max_wait=0.0,
            read_seconds_after_send=1.0,
            trace=traces.append,
            connector=lambda *args, **kwargs: connection,
            time_source=lambda: next(ticks),
        )

        self.assertNotIn('recv(after-send): {"messagetype":16,"messagecmd":0,"size":3,"data":[190,0,0]}', traces)
        self.assertIn('recv(after-send): {"messagetype":16,"messagecmd":0,"size":3,"data":[48,0,1]}', traces)
        self.assertIn('recv(after-send): {"messagetype":16,"messagecmd":0,"size":3,"data":[47,0,1]}', traces)

    def test_module_defaults_match_live_empirbus_connection_defaults(self) -> None:
        self.assertEqual("ws://192.168.1.1:8888/ws", DEFAULT_WS_URL)
        self.assertEqual("http://192.168.1.1:8888", DEFAULT_ORIGIN)
        self.assertEqual(
            [
                "User-Agent: Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36",
                "Cache-Control: no-cache",
                "Pragma: no-cache",
                "Accept-Language: en-GB,en-US;q=0.9,en;q=0.8",
            ],
            DEFAULT_HEADERS,
        )
        self.assertEqual(2, len(DEFAULT_BOOTSTRAP_MESSAGES))
        self.assertEqual(
            "{\"messagetype\":96,\"messagecmd\":1,\"size\":2,\"data\":[0,0]}",
            DEFAULT_BOOTSTRAP_MESSAGES[1],
        )
        self.assertEqual(
            [
                "{\"messagetype\":49,\"messagecmd\":1,\"size\":3,\"data\":[0,0,0]}",
                "{\"messagetype\":49,\"messagecmd\":1,\"size\":3,\"data\":[0,0,0]}",
            ],
            DEFAULT_READY_MESSAGES,
        )
        self.assertEqual(
            "{\"messagetype\":17,\"messagecmd\":0,\"size\":3,\"data\":[47,0,3]}",
            EXTERIOR_LIGHTS_ON_MESSAGE,
        )
