"""Checks for repeated completion detection and nullable COPY durations."""

import unittest

from analyze import summarize


class AnalyzeTests(unittest.TestCase):
    def test_repeated_completion_and_null_duration(self):
        start = 1_700_000_000_000_000_000
        profile = {
            "manifest": {
                "buildId": "example",
                "startedAtUnixNanos": str(start),
                "commands": {
                    "copy": {"name": "COPY note.txt .", "startedAtUnixNanos": str(start),
                             "endedAtUnixNanos": str(start + 840_000_000_000)},
                    "unstarted": {"name": "COPY missing.txt ."},
                    "other": {"name": "RUN echo COPY probe"},
                },
            },
            "commandDurationNanos": {"copy": 840_000_000_000, "unstarted": None},
        }
        def delta(change):
            return {"deltaManifest": {"fields": {"commands": {"copy": change}}}}
        deltas = [
            delta({"startedAtUnixNanos": str(start)}),
            delta({"endedAtUnixNanos": str(start + 1_000_000)}),
            delta({"endedAtUnixNanos": str(start + 1_000_000)}),
            {"deltaFormattedLog": {"data": "ignored"}},
            delta({"endedAtUnixNanos": str(start + 840_000_000_000)}),
        ]
        report = summarize(profile, deltas)
        self.assertNotIn("RUN echo COPY probe", report)
        self.assertIn("span=840.000000s", report)
        self.assertIn("first-recorded-interval=0.001000s", report)
        self.assertIn("start-deltas=1 end-deltas=3 distinct-end-times=2", report)
        self.assertIn("0.001000s, 840.000000s", report)
        self.assertIn("span=unknown start-offset=unknown end-offset=unknown", report)

    def test_one_completion_is_not_reported_as_repeated(self):
        report = summarize({"manifest": {}, "commandDurationNanos": {"cached": 0}}, [])
        self.assertIn("None in this capture.", report)
        self.assertIn("0.000000s", report)


if __name__ == "__main__":
    unittest.main()
