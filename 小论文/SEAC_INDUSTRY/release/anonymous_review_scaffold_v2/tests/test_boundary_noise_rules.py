from __future__ import annotations

import importlib.util
import unittest
from pathlib import Path


ROOT = Path(__file__).parents[1]


def load(name: str):
    spec = importlib.util.spec_from_file_location(name, ROOT / "scripts" / f"{name}.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


rules = load("boundary_noise_rules")


class StructuralLabelTests(unittest.TestCase):
    """§4.1.1 fires only when the unit is nothing but a form label."""

    def test_bare_labels_are_evidence_free(self):
        for text in ("输出", "处理逻辑", "返回信息", "请求信息样例", "无", "输入参数"):
            self.assertTrue(rules.structural_label(text), text)

    def test_trailing_punctuation_does_not_hide_a_label(self):
        self.assertTrue(rules.structural_label("处理逻辑："))
        self.assertTrue(rules.structural_label("无。"))

    def test_a_label_with_content_is_not_a_bare_label(self):
        for text in ("返回信息参数：orderId", "处理逻辑：先校验押金再作废申请", "输出结果为空时返回 5000"):
            self.assertFalse(rules.structural_label(text), text)

    def test_short_content_units_survive(self):
        """§4.1 warns that short text is not automatically noise."""

        for text in ("approvalStatus", "取餐柜管理箱门接口", "同步", "code：5000"):
            self.assertFalse(rules.structural_label(text), text)


class ConversionFragmentTests(unittest.TestCase):
    """§4.1.2 fires on punctuation left over from document conversion."""

    def test_isolated_punctuation_is_a_fragment(self):
        for text in ("}", "],", "{", '"', "()", "-- --", "1.", "2 . 3"):
            self.assertTrue(rules.conversion_fragment(text), text)

    def test_json_with_keys_is_content(self):
        self.assertFalse(rules.conversion_fragment('{"success":true}'))

    def test_text_with_punctuation_is_content(self):
        self.assertFalse(rules.conversion_fragment("失败返回信息：{}"))

    def test_empty_unit_is_not_reported_as_a_fragment(self):
        """An empty unit is handled by the chunker's own noise notion, not here."""

        self.assertFalse(rules.conversion_fragment(""))
        self.assertFalse(rules.conversion_fragment("   "))


class SignatureBlockTests(unittest.TestCase):
    """§4.1.3 fires on the furniture that closes a meeting record."""

    def test_organisation_sign_off_is_furniture(self):
        for text in ("示例省甲方钢铁控股集团有限公司：", "示例省乙方钢铁集团股份有限公司：", "示例省丙方信息科技股份有限公司："):
            self.assertTrue(rules.signature_block(text), text)

    def test_bare_date_line_is_furniture(self):
        for text in ("2025年4月14日", "2025-04-14", "2025/4/14"):
            self.assertTrue(rules.signature_block(text), text)

    def test_sign_off_roles_are_furniture(self):
        self.assertTrue(rules.signature_block("记录人："))
        self.assertTrue(rules.signature_block("盖章"))

    def test_a_sentence_about_an_organisation_is_content(self):
        text = "示例省甲方钢铁控股集团有限公司负责提供能耗计费的历史数据并确认口径。"
        self.assertFalse(rules.signature_block(text))

    def test_an_organisation_without_a_colon_is_content(self):
        """A company name inside a requirement is evidence, not a sign-off."""

        self.assertFalse(rules.signature_block("示例省丙方信息科技股份有限公司"))

    def test_long_lines_are_never_signatures(self):
        self.assertFalse(rules.signature_block("参加人员：甲一、乙二、张工、李工、王工、赵工、孙工："))


class ClassifyTests(unittest.TestCase):
    """§4.1 says either side being evidence-free settles the boundary."""

    def test_either_side_triggers_the_rule(self):
        determined, reason = rules.classify("}", "取餐柜管理箱门接口")
        self.assertTrue(determined)
        self.assertEqual("LEFT_CONVERSION_FRAGMENT", reason)

        determined, reason = rules.classify("审批结果参数表说明如下", "处理逻辑")
        self.assertTrue(determined)
        self.assertEqual("RIGHT_STRUCTURAL_LABEL", reason)

    def test_two_content_units_are_left_to_judgment(self):
        determined, reason = rules.classify(
            "退宿申请提交后由宿管审批，审批通过即释放床位。",
            "清退能耗费用在退宿当日立即生成结算单。",
        )
        self.assertFalse(determined)
        self.assertIsNone(reason)

    def test_table_rows_with_data_are_left_to_judgment(self):
        left = "表头: 参数名称 | 参数类型 | 取值范围 数据行: approvalStatus | String | 1 - 通过 2 - 驳回"
        right = "表头: 序号 | 返回码 | 说明 数据行: 2 | code：5000 | 失败原因"
        determined, _ = rules.classify(left, right)
        self.assertFalse(determined)

    def test_the_left_side_is_reported_before_the_right(self):
        determined, reason = rules.classify("}", "输出")
        self.assertTrue(determined)
        self.assertTrue(reason.startswith("LEFT_"))

    def test_chunker_noise_stays_in_force(self):
        determined, reason = rules.classify("目录", "第一章 概述")
        self.assertTrue(determined)
        self.assertTrue(reason.startswith("LEFT_"))


if __name__ == "__main__":
    unittest.main()
