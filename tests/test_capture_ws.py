import json
import tempfile
import unittest
from pathlib import Path

from capture_ws import (
    DEFAULT_BOOTSTRAP_MESSAGES,
    DEFAULT_HEARTBEAT_MESSAGE,
    Recorder,
    build_headers,
    extract_bootstrap_messages_from_har,
    extract_startup_sequence_from_har,
    format_close_marker,
)


class CaptureWsTests(unittest.TestCase):
    def test_build_headers_merges_header_and_cookie_inputs(self) -> None:
        headers = build_headers(
            header_values=["X-Test: one", "X-Trace: two"],
            cookie_values=["a=1", "b=2"],
        )

        self.assertIn("X-Test: one", headers)
        self.assertIn("X-Trace: two", headers)
        self.assertIn("Cookie: a=1; b=2", headers)

    def test_recorder_writes_frame_and_marker_records(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            out = Path(tmpdir) / "capture.ndjson"
            recorder = Recorder(out)

            recorder.write_frame("receive", "{\"messagetype\":48,\"messagecmd\":5,\"size\":0,\"data\":[]}", ts=1.5)
            recorder.write_marker("clicked exterior lights", ts=2.0)
            recorder.close()

            lines = [json.loads(line) for line in out.read_text(encoding="utf-8").splitlines()]

        self.assertEqual("receive", lines[0]["direction"])
        self.assertEqual(48, lines[0]["parsed"]["messagetype"])
        self.assertEqual("clicked exterior lights", lines[1]["marker"])

    def test_extract_bootstrap_messages_from_har_returns_initial_send_frames_and_heartbeat(self) -> None:
        payload = {
            "log": {
                "entries": [
                    {
                        "_webSocketMessages": [
                            {"type": "send", "data": "{\"messagetype\":96,\"messagecmd\":0,\"size\":2,\"data\":[1,0]}"},
                            {"type": "send", "data": "{\"messagetype\":96,\"messagecmd\":1,\"size\":2,\"data\":[0,0]}"},
                            {"type": "receive", "data": "{\"messagetype\":48,\"messagecmd\":0,\"size\":5,\"data\":[1,2,3,4,5]}"},
                            {"type": "send", "data": "{\"messagetype\":128,\"messagecmd\":0,\"size\":1,\"data\":[0]}"},
                        ]
                    }
                ]
            }
        }

        with tempfile.TemporaryDirectory() as tmpdir:
            path = Path(tmpdir) / "sample.har"
            path.write_text(json.dumps(payload), encoding="utf-8")

            bootstrap, heartbeat = extract_bootstrap_messages_from_har(path)

        self.assertEqual(
            [
                "{\"messagetype\":96,\"messagecmd\":0,\"size\":2,\"data\":[1,0]}",
                "{\"messagetype\":96,\"messagecmd\":1,\"size\":2,\"data\":[0,0]}",
            ],
            bootstrap,
        )
        self.assertEqual("{\"messagetype\":128,\"messagecmd\":0,\"size\":1,\"data\":[0]}", heartbeat)

    def test_extract_startup_sequence_from_har_returns_initial_and_ready_sends(self) -> None:
        payload = {
            "log": {
                "entries": [
                    {
                        "_webSocketMessages": [
                            {"type": "send", "data": "{\"messagetype\":96,\"messagecmd\":0,\"size\":2,\"data\":[1,0]}"},
                            {"type": "send", "data": "{\"messagetype\":96,\"messagecmd\":1,\"size\":2,\"data\":[0,0]}"},
                            {"type": "receive", "data": "{\"messagetype\":48,\"messagecmd\":0,\"size\":5,\"data\":[1,2,3,4,5]}"},
                            {"type": "send", "data": "{\"messagetype\":49,\"messagecmd\":1,\"size\":3,\"data\":[0,0,0]}"},
                            {"type": "send", "data": "{\"messagetype\":49,\"messagecmd\":1,\"size\":3,\"data\":[0,0,0]}"},
                            {"type": "receive", "data": "{\"messagetype\":16,\"messagecmd\":0,\"size\":3,\"data\":[48,0,1]}"},
                        ]
                    }
                ]
            }
        }

        with tempfile.TemporaryDirectory() as tmpdir:
            path = Path(tmpdir) / "sample.har"
            path.write_text(json.dumps(payload), encoding="utf-8")

            startup = extract_startup_sequence_from_har(path)

        self.assertEqual(
            [
                "{\"messagetype\":96,\"messagecmd\":0,\"size\":2,\"data\":[1,0]}",
                "{\"messagetype\":96,\"messagecmd\":1,\"size\":2,\"data\":[0,0]}",
            ],
            startup["initial_sends"],
        )
        self.assertEqual(
            [
                "{\"messagetype\":49,\"messagecmd\":1,\"size\":3,\"data\":[0,0,0]}",
                "{\"messagetype\":49,\"messagecmd\":1,\"size\":3,\"data\":[0,0,0]}",
            ],
            startup["post_first_receive_sends"],
        )

    def test_format_close_marker_includes_code_and_message(self) -> None:
        self.assertEqual("closed: code=1000 message=normal closure", format_close_marker(1000, "normal closure"))
        self.assertEqual("closed: code=unknown message=", format_close_marker(None, None))

    def test_module_defaults_include_hardcoded_startup_and_heartbeat(self) -> None:
        self.assertEqual(2, len(DEFAULT_BOOTSTRAP_MESSAGES))
        self.assertEqual(
            "{\"messagetype\":96,\"messagecmd\":1,\"size\":2,\"data\":[0,0]}",
            DEFAULT_BOOTSTRAP_MESSAGES[1],
        )
        self.assertEqual(
            "{\"messagetype\":128,\"messagecmd\":0,\"size\":1,\"data\":[0]}",
            DEFAULT_HEARTBEAT_MESSAGE,
        )
