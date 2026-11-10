from __future__ import annotations

import unittest

from scripts.prepare_calibration_sample import DISAGREEMENT_QUOTAS, QUOTAS, select_calibration


class CalibrationSelectionTest(unittest.TestCase):
    def test_selection_respects_frozen_strata_and_disagreements(self) -> None:
        rows = []
        counter = 1
        for (stratum, label), quota in QUOTAS.items():
            disagreement_quota = DISAGREEMENT_QUOTAS.get((stratum, label), 0)
            for index in range(max(quota + 3, disagreement_quota)):
                rows.append(
                    {
                        "candidate_id": f"BP{counter:04d}",
                        "source_document": f"doc{index % 3}.docx",
                        "boundary_type": stratum,
                        "human_label": label,
                        "first_annotator_label": label,
                        "second_annotator_label": "BREAK" if index < disagreement_quota and label != "BREAK" else (
                            "NO_BREAK" if index < disagreement_quota else label
                        ),
                    }
                )
                counter += 1
        selected = select_calibration(rows)
        self.assertEqual(len(selected), 30)
        self.assertEqual(len({row["candidate_id"] for row in selected}), 30)
        self.assertEqual(
            sum(row["first_annotator_label"] != row["second_annotator_label"] for row in selected),
            sum(DISAGREEMENT_QUOTAS.values()),
        )


if __name__ == "__main__":
    unittest.main()
