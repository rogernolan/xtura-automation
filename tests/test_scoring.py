import unittest

from empirbus_filter import FRAME_GROUPS, classify_noisy_families, frame_family_key, load_signal_catalog, normalize_live_frame


class ScoringTests(unittest.TestCase):
    def test_signal_catalog_maps_known_signal_to_water_group(self) -> None:
        catalog = load_signal_catalog()
        frame = normalize_live_frame(1.0, "receive", '{"messagetype":16,"messagecmd":5,"size":8,"data":[12,0,1]}', catalog)
        assert frame is not None
        self.assertEqual("water", frame["group"])
        self.assertEqual("Fresh Water Value %", frame["signal_label"])

    def test_frame_family_key_uses_structural_fields(self) -> None:
        frame = normalize_live_frame(1.0, "receive", '{"messagetype":16,"messagecmd":0,"size":3,"data":[48,0,1]}', {})
        assert frame is not None
        self.assertEqual(("receive", 16, 0, 3, 3, 48, 0), frame_family_key(frame))

    def test_classify_noisy_families_marks_repeated_family_after_threshold(self) -> None:
        frames = [
            normalize_live_frame(1.0, "receive", '{"messagetype":16,"messagecmd":5,"size":8,"data":[12,0,1]}', {}),
            normalize_live_frame(2.0, "receive", '{"messagetype":16,"messagecmd":5,"size":8,"data":[12,0,2]}', {}),
            normalize_live_frame(3.0, "receive", '{"messagetype":16,"messagecmd":5,"size":8,"data":[12,0,3]}', {}),
        ]
        noisy = classify_noisy_families([frame for frame in frames if frame is not None], threshold=3)
        self.assertIn(("receive", 16, 5, 8, 3, 12, 0), noisy)

    def test_group_definitions_cover_requested_sets(self) -> None:
        self.assertIn("lights", FRAME_GROUPS)
        self.assertIn("heating", FRAME_GROUPS)
        self.assertIn("fuses", FRAME_GROUPS)
        self.assertIn("water", FRAME_GROUPS)
        self.assertIn("options", FRAME_GROUPS)
        self.assertIn("power", FRAME_GROUPS)
