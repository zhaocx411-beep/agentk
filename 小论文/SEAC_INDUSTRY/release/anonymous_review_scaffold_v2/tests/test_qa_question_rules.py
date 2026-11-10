"""Cover the §6.1 self-containment rule and the paths that apply it.

The rule has to separate a section number from a date and from a quantity using
nothing but the question text, so the discriminating cases are tested directly
rather than through the importer.
"""

from __future__ import annotations

import importlib.util
import json
import sys
import unittest
from pathlib import Path

from openpyxl import load_workbook


ROOT = Path(__file__).parents[1]
sys.path.insert(0, str(ROOT / "scripts"))


def load(name: str):
    spec = importlib.util.spec_from_file_location(name, ROOT / "scripts" / f"{name}.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


rules = load("qa_question_rules")


class SectionNumberTests(unittest.TestCase):
    def test_a_leading_section_number_is_a_violation(self):
        for question in (
            "2.1.2.1获取各点位的水位数据接口的请求路径是什么？",
            "2.1.2.2备件入库接口中参数 relaOrderNo 的类型是什么？",
            "3.2能碳管理平台的建设描述了什么实现或处理过程？",
            "  1.1 项目背景说明了什么？",
        ):
            with self.subTest(question=question):
                self.assertTrue(rules.violates_self_containment(question))

    def test_a_date_is_not_a_section_number(self):
        """A four-digit year cannot be the first component, so dates stay in scope."""

        for question in (
            "2025.7.22召开的安全预警平台需求交流会议的地点在哪里？",
            "2025.9.3召开的会议的记录人是谁？",
            "2025年11月10日下午召开的需求调研会议的地点在哪里？",
            "0708需求调研会议的地点在哪里？",
        ):
            with self.subTest(question=question):
                self.assertFalse(rules.violates_self_containment(question))

    def test_a_quantity_is_not_a_section_number(self):
        for question in ("1.5秒内必须返回什么？", "2.5万吨的产能对应什么配置？"):
            with self.subTest(question=question):
                self.assertFalse(rules.violates_self_containment(question))

    def test_a_self_contained_question_is_untouched(self):
        self.assertFalse(rules.violates_self_containment("能碳管理平台的报表如何实现无代码扩展？"))
        self.assertFalse(rules.violates_self_containment(""))

    def test_the_verdict_names_the_rule_and_the_offending_prefix(self):
        verdict = rules.classify("2.1.2.1获取设备列表接口中参数 code 的类型是什么？")
        self.assertTrue(verdict["excluded"])
        self.assertEqual("RULE_6_1", verdict["rule"])
        self.assertEqual("2.1.2.1", verdict["prefix"])
        clean = rules.classify("甲方需为乙方现场办公提供哪些条件？")
        self.assertFalse(clean["excluded"])
        self.assertEqual("", clean["prefix"])


class RealWorkbookTests(unittest.TestCase):
    """The rule was written against one frozen pack; pin what it selects there."""

    WORKBOOK = ROOT / "outputs/qa_approval_v1/SEAC_QA_approval.xlsx"

    def setUp(self) -> None:
        if not self.WORKBOOK.is_file():
            self.skipTest("the approval workbook is not present")
        workbook = load_workbook(self.WORKBOOK)
        headers = [cell.value for cell in workbook[workbook.sheetnames[1]][1]]
        self.question_column = headers.index("question") + 1
        self.status_column = headers.index("approval_status") + 1
        self.rows = []
        for name in workbook.sheetnames:
            if name == "Guide":
                continue
            sheet = workbook[name]
            for index in range(2, sheet.max_row + 1):
                if not sheet.cell(row=index, column=1).value:
                    continue
                self.rows.append(
                    (
                        sheet.cell(row=index, column=1).value,
                        sheet.cell(row=index, column=self.question_column).value or "",
                        sheet.cell(row=index, column=self.status_column).value,
                    )
                )

    def test_the_rule_selects_exactly_the_thirteen_numbered_questions(self):
        selected = [key for key, question, _ in self.rows if rules.violates_self_containment(question)]
        self.assertEqual(225, len(self.rows))
        self.assertEqual(
            [
                "P03-QA004", "P03-QA005", "P03-QA006", "P03-QA007",
                "P04-QA010", "P04-QA011", "P04-QA012", "P04-QA013",
                "P05-QA010", "P05-QA012", "P05-QA013", "P05-QA014",
                "P10-QA013",
            ],
            sorted(selected),
        )

    def test_ten_of_the_selected_rows_were_approved_by_the_reviewer(self):
        """The rule changes the outcome only for rows the reviewer had approved."""

        overridden = [
            key
            for key, question, status in self.rows
            if rules.violates_self_containment(question) and status == "APPROVED"
        ]
        self.assertEqual(10, len(overridden))

    def test_no_date_prefixed_question_is_selected(self):
        for key, question, _ in self.rows:
            if "召开" in question:
                with self.subTest(key=key):
                    self.assertFalse(rules.violates_self_containment(question))


class FormalFileTests(unittest.TestCase):
    """The written QA files must not contain a question the rule rules out."""

    def test_no_promoted_question_begins_with_a_section_number(self):
        checked = 0
        for path in sorted((ROOT / "workbench").glob("P*/qa_approved.json")):
            for record in json.loads(path.read_text(encoding="utf-8")):
                checked += 1
                with self.subTest(qa=record.get("qa_id"), project=path.parent.name):
                    self.assertFalse(rules.violates_self_containment(record.get("question", "")))
        if not checked:
            self.skipTest("no formal QA file has been written yet")


if __name__ == "__main__":
    unittest.main()
