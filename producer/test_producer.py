"""Tests for the producer. Run with: python -m unittest discover -s producer"""

import json
import os
import subprocess
import sys
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
PRODUCER = os.path.join(HERE, "producer.py")


def run(*args: str) -> None:
    subprocess.run([sys.executable, PRODUCER, *args], check=True, capture_output=True)


class ProducerTest(unittest.TestCase):
    def setUp(self) -> None:
        self.dir = tempfile.mkdtemp()
        self.db = os.path.join(self.dir, "p.duckdb")
        self.out = os.path.join(self.dir, "f.json")
        run("init", "--db", self.db, "--features", "110")

    def features(self, *extra: str) -> dict:
        run("features", "--db", self.db, "--out", self.out, *extra)
        with open(self.out) as f:
            return json.load(f)

    def test_normal_file(self) -> None:
        doc = self.features()
        self.assertEqual(doc["schema"], "bot-features/1")
        self.assertEqual(len(doc["features"]), 110)
        self.assertTrue(all(len(f["fields"]) == 20 for f in doc["features"]))

    def test_permission_change_duplicates_rows(self) -> None:
        run("grant-r0", "--db", self.db)
        self.assertEqual(len(self.features()["features"]), 220)
        run("revoke-r0", "--db", self.db)
        self.assertEqual(len(self.features()["features"]), 110)

    def test_extra_field(self) -> None:
        doc = self.features("--extra-field", "v1", "--extra-count", "5")
        arity = [len(f["fields"]) for f in doc["features"]]
        self.assertEqual(arity.count(21), 5)
        self.assertEqual(doc["features"][0]["fields"][20], "v1")

    def test_blank_quota_policy(self) -> None:
        out = os.path.join(self.dir, "p.json")
        run("policy", "--out", out, "--blank-quota", "3")
        with open(out) as f:
            doc = json.load(f)
        blank = [p for p in doc["policies"] if p["enforce_quota"] and p["quota"] is None]
        self.assertEqual(len(blank), 3)


if __name__ == "__main__":
    unittest.main()
