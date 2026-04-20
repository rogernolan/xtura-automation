import json
import tempfile
import unittest
from contextlib import redirect_stdout
from io import StringIO
from pathlib import Path
from unittest.mock import patch

from empirbus_filter import DEFAULT_WS_URL, main


class CliTests(unittest.TestCase):
    def test_main_accepts_single_group_filter_and_source_file(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            source = Path(tmpdir) / "capture.ndjson"
            source.write_text("", encoding="utf-8")

            exit_code = main(["--source", str(source), "-f", "lights", "--learn-seconds", "1"])

        self.assertEqual(0, exit_code)

    def test_stream_command_shows_matching_non_noisy_frame_after_learning(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            source = Path(tmpdir) / "capture.ndjson"
            source.write_text(
                "\n".join(
                    [
                        json.dumps({"ts": 1.0, "direction": "receive", "raw": '{"messagetype":16,"messagecmd":5,"size":8,"data":[12,0,1]}'}),
                        json.dumps({"ts": 2.0, "direction": "receive", "raw": '{"messagetype":16,"messagecmd":5,"size":8,"data":[12,0,2]}'}),
                        json.dumps({"ts": 31.0, "direction": "receive", "raw": '{"messagetype":16,"messagecmd":0,"size":3,"data":[27,0,1]}'}),
                    ]
                )
                + "\n",
                encoding="utf-8",
            )
            output = StringIO()
            with redirect_stdout(output):
                exit_code = main(["--source", str(source), "-f", "water", "--learn-seconds", "30"])

        self.assertEqual(0, exit_code)
        self.assertIn('label="Waterpump"', output.getvalue())

    def test_stream_command_suppresses_matching_noisy_frame_after_learning(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            source = Path(tmpdir) / "capture.ndjson"
            source.write_text(
                "\n".join(
                    [
                        json.dumps({"ts": 1.0, "direction": "receive", "raw": '{"messagetype":16,"messagecmd":5,"size":8,"data":[12,0,1]}'}),
                        json.dumps({"ts": 2.0, "direction": "receive", "raw": '{"messagetype":16,"messagecmd":5,"size":8,"data":[12,0,2]}'}),
                        json.dumps({"ts": 3.0, "direction": "receive", "raw": '{"messagetype":16,"messagecmd":5,"size":8,"data":[12,0,3]}'}),
                        json.dumps({"ts": 31.0, "direction": "receive", "raw": '{"messagetype":16,"messagecmd":5,"size":8,"data":[12,0,4]}'}),
                    ]
                )
                + "\n",
                encoding="utf-8",
            )
            output = StringIO()
            with redirect_stdout(output):
                exit_code = main(["--source", str(source), "-f", "water", "--learn-seconds", "30"])

        self.assertEqual(0, exit_code)
        self.assertNotIn('label="Fresh Water Value %"', output.getvalue())

    def test_stream_command_prints_matching_send_frames_during_learning(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            source = Path(tmpdir) / "capture.ndjson"
            source.write_text(
                "\n".join(
                    [
                        json.dumps({"ts": 1.0, "direction": "send", "raw": '{"messagetype":16,"messagecmd":0,"size":3,"data":[47,0,1]}'}),
                        json.dumps({"ts": 2.0, "direction": "receive", "raw": '{"messagetype":16,"messagecmd":5,"size":8,"data":[12,0,2]}'}),
                    ]
                )
                + "\n",
                encoding="utf-8",
            )
            output = StringIO()
            with redirect_stdout(output):
                exit_code = main(["--source", str(source), "-f", "lights", "--learn-seconds", "30"])

        self.assertEqual(0, exit_code)
        self.assertIn('send type=16 cmd=0 signal=47 label="All Exterior Lights On"', output.getvalue())

    def test_stream_command_summarizes_suppressed_noisy_burst(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            source = Path(tmpdir) / "capture.ndjson"
            source.write_text(
                "\n".join(
                    [
                        json.dumps({"ts": 1.0, "direction": "receive", "raw": '{"messagetype":16,"messagecmd":5,"size":8,"data":[12,0,1]}'}),
                        json.dumps({"ts": 2.0, "direction": "receive", "raw": '{"messagetype":16,"messagecmd":5,"size":8,"data":[12,0,2]}'}),
                        json.dumps({"ts": 3.0, "direction": "receive", "raw": '{"messagetype":16,"messagecmd":5,"size":8,"data":[12,0,3]}'}),
                        json.dumps({"ts": 31.0, "direction": "receive", "raw": '{"messagetype":16,"messagecmd":5,"size":8,"data":[12,0,4]}'}),
                        json.dumps({"ts": 32.0, "direction": "receive", "raw": '{"messagetype":16,"messagecmd":5,"size":8,"data":[12,0,5]}'}),
                        json.dumps({"ts": 35.0, "direction": "receive", "raw": '{"messagetype":16,"messagecmd":0,"size":3,"data":[27,0,1]}'}),
                    ]
                )
                + "\n",
                encoding="utf-8",
            )
            output = StringIO()
            with redirect_stdout(output):
                exit_code = main(["--source", str(source), "-f", "water", "--learn-seconds", "30"])

        self.assertEqual(0, exit_code)
        rendered = output.getvalue()
        self.assertIn("burst suppressed=2", rendered)
        self.assertIn("families=1", rendered)
        self.assertIn('label="Waterpump"', rendered)

    def test_correlate_sends_mode_prints_likely_related_receive_frames_after_send(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            source = Path(tmpdir) / "capture.ndjson"
            source.write_text(
                "\n".join(
                    [
                        json.dumps({"ts": 1.0, "direction": "receive", "raw": '{"messagetype":16,"messagecmd":0,"size":3,"data":[48,0,1]}'}),
                        json.dumps({"ts": 2.0, "direction": "receive", "raw": '{"messagetype":16,"messagecmd":0,"size":3,"data":[48,0,0]}'}),
                        json.dumps({"ts": 3.0, "direction": "receive", "raw": '{"messagetype":16,"messagecmd":0,"size":3,"data":[48,0,1]}'}),
                        json.dumps({"ts": 31.0, "direction": "send", "raw": '{"messagetype":17,"messagecmd":0,"size":3,"data":[47,0,3]}'}),
                        json.dumps({"ts": 31.1, "direction": "receive", "raw": '{"messagetype":16,"messagecmd":0,"size":3,"data":[48,0,1]}'}),
                        json.dumps({"ts": 31.2, "direction": "receive", "raw": '{"messagetype":16,"messagecmd":0,"size":3,"data":[190,0,0]}'}),
                    ]
                )
                + "\n",
                encoding="utf-8",
            )
            output = StringIO()
            with redirect_stdout(output):
                exit_code = main(
                    [
                        "--source",
                        str(source),
                        "-f",
                        "lights",
                        "--learn-seconds",
                        "30",
                        "--correlate-sends",
                        "--response-window-seconds",
                        "0.5",
                    ]
                )

        self.assertEqual(0, exit_code)
        rendered = output.getvalue()
        self.assertIn('send type=17 cmd=0 signal=47', rendered)
        self.assertIn('receive type=16 cmd=0 signal=48 label="All Exterior Lights Off"', rendered)
        self.assertNotIn('receive type=16 cmd=0 signal=190', rendered)

    def test_correlate_sends_mode_bypasses_noise_suppression_for_related_response(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            source = Path(tmpdir) / "capture.ndjson"
            source.write_text(
                "\n".join(
                    [
                        json.dumps({"ts": 1.0, "direction": "receive", "raw": '{"messagetype":16,"messagecmd":0,"size":3,"data":[48,0,1]}'}),
                        json.dumps({"ts": 2.0, "direction": "receive", "raw": '{"messagetype":16,"messagecmd":0,"size":3,"data":[48,0,0]}'}),
                        json.dumps({"ts": 3.0, "direction": "receive", "raw": '{"messagetype":16,"messagecmd":0,"size":3,"data":[48,0,1]}'}),
                        json.dumps({"ts": 31.0, "direction": "send", "raw": '{"messagetype":17,"messagecmd":0,"size":3,"data":[47,0,3]}'}),
                        json.dumps({"ts": 31.1, "direction": "receive", "raw": '{"messagetype":16,"messagecmd":0,"size":3,"data":[48,0,0]}'}),
                    ]
                )
                + "\n",
                encoding="utf-8",
            )
            output = StringIO()
            with redirect_stdout(output):
                exit_code = main(
                    [
                        "--source",
                        str(source),
                        "-f",
                        "lights",
                        "--learn-seconds",
                        "30",
                        "--correlate-sends",
                        "--response-window-seconds",
                        "0.5",
                    ]
                )

        self.assertEqual(0, exit_code)
        rendered = output.getvalue()
        self.assertIn('receive type=16 cmd=0 signal=48 label="All Exterior Lights Off"', rendered)
        self.assertNotIn("burst suppressed=1", rendered)

    def test_stream_source_reads_har_file(self) -> None:
        payload = {
            "log": {
                "entries": [
                    {
                        "_webSocketMessages": [
                            {"type": "receive", "time": 1.0, "data": '{"messagetype":16,"messagecmd":0,"size":3,"data":[48,0,1]}'},
                            {"type": "receive", "time": 31.0, "data": '{"messagetype":16,"messagecmd":0,"size":3,"data":[47,0,1]}'},
                        ]
                    }
                ]
            }
        }
        with tempfile.TemporaryDirectory() as tmpdir:
            source = Path(tmpdir) / "capture.har"
            source.write_text(json.dumps(payload), encoding="utf-8")
            output = StringIO()
            with redirect_stdout(output):
                exit_code = main(["--source", str(source), "-f", "lights", "--learn-seconds", "30"])

        self.assertEqual(0, exit_code)
        self.assertIn("All Exterior Lights On", output.getvalue())

    def test_help_mentions_single_group_filter(self) -> None:
        output = StringIO()
        with redirect_stdout(output):
            with self.assertRaises(SystemExit):
                main(["--help"])
        self.assertIn("-f {fuses,heating,lights,options,power,water}", output.getvalue())

    def test_cli_defaults_no_longer_require_bootstrap_har_file(self) -> None:
        self.assertEqual("ws://192.168.1.1:8888/ws", DEFAULT_WS_URL)

    def test_live_websocket_path_normalizes_tuple_frames_before_streaming(self) -> None:
        fake_frames = [
            (1.0, "receive", '{"messagetype":16,"messagecmd":5,"size":8,"data":[12,0,1]}'),
            (2.0, "receive", '{"messagetype":16,"messagecmd":5,"size":8,"data":[12,0,2]}'),
            (31.0, "receive", '{"messagetype":16,"messagecmd":0,"size":3,"data":[27,0,1]}'),
        ]

        class FakeClient:
            def __init__(self, *args, **kwargs) -> None:
                del args, kwargs

            def iter_frames(self):
                yield from fake_frames

        output = StringIO()
        with patch("empirbus_filter.LiveWebsocketClient", FakeClient):
            with redirect_stdout(output):
                exit_code = main(["-f", "water", "--learn-seconds", "30"])

        self.assertEqual(0, exit_code)
        self.assertIn('label="Waterpump"', output.getvalue())

    def test_live_websocket_path_shows_matching_send_frames_when_client_yields_them(self) -> None:
        fake_frames = [
            (1.0, "receive", '{"messagetype":16,"messagecmd":0,"size":3,"data":[48,0,1]}'),
            (2.0, "receive", '{"messagetype":16,"messagecmd":0,"size":3,"data":[48,0,0]}'),
            (3.0, "receive", '{"messagetype":16,"messagecmd":0,"size":3,"data":[48,0,1]}'),
            (31.0, "send", '{"messagetype":17,"messagecmd":0,"size":3,"data":[47,0,3]}'),
            (31.1, "receive", '{"messagetype":16,"messagecmd":0,"size":3,"data":[48,0,1]}'),
        ]

        class FakeClient:
            def __init__(self, *args, **kwargs) -> None:
                del args, kwargs

            def iter_frames(self):
                yield from fake_frames

        output = StringIO()
        with patch("empirbus_filter.LiveWebsocketClient", FakeClient):
            with redirect_stdout(output):
                exit_code = main(["-f", "lights", "--learn-seconds", "30", "--correlate-sends"])

        self.assertEqual(0, exit_code)
        rendered = output.getvalue()
        self.assertIn('send type=17 cmd=0 signal=47', rendered)
        self.assertIn('receive type=16 cmd=0 signal=48 label="All Exterior Lights Off"', rendered)
