import importlib.util
import json
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).parents[1]


def load(name: str):
    path = ROOT / "scripts" / f"{name}.py"
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


equivalence = load("resolve_qa_evidence_equivalents")
readjudication = load("merge_qa_readjudication")
supplement = load("verify_supplementary_qa_drafts")


def unit(unit_id: str, text: str, block_id: str = "doc:B0001", document: str = "doc.docx") -> dict:
    return {
        "unit_id": unit_id,
        "block_id": block_id,
        "document": document,
        "normalized": equivalence.own_content(text),
    }


class OwnContentTests(unittest.TestCase):
    def test_table_header_context_is_dropped(self):
        text = "表头: 序号 | 表名 | 描述\n数据行: 3 | EHS_PROJECT | 项目表"
        self.assertEqual("3|EHS_PROJECT|项目表", equivalence.own_content(text))

    def test_plain_paragraph_is_unchanged(self):
        self.assertEqual("系统应支持并发100人", equivalence.own_content("系统应支持并发 100 人"))

    def test_header_row_does_not_match_sibling_data_rows(self):
        header_unit = unit("doc:B0001#R001", "1、 | 表名 | T_A | 描述 | 甲表")
        data_unit = unit("doc:B0001#R002", "表头: 1、 | 表名 | T_A | 描述 | 甲表\n数据行: ID | NVARCHAR2 | 40")
        span = equivalence.own_content("1、 | 表名 | T_A | 描述 | 甲表")
        matched = [item["unit_id"] for item in (header_unit, data_unit) if span in item["normalized"]]
        self.assertEqual(["doc:B0001#R001"], matched)


class EquivalenceResolutionTests(unittest.TestCase):
    def record(self, span: str, unit_id: str = "doc:B0001#R001") -> dict:
        return {
            "gold_evidence_span": span,
            "gold_evidence_unit_id": unit_id,
            "gold_evidence_id": "doc:B0001",
            "source_document": "doc.docx",
        }

    def test_repeated_passage_becomes_equivalent_set(self):
        units = [
            unit("a:B1", "设备和终端必须反应快速", block_id="a:B1", document="a.docx"),
            unit("b:B2", "设备和终端必须反应快速", block_id="b:B2", document="b.docx"),
        ]
        result = equivalence.resolve(self.record("设备和终端必须反应快速", "a:B1"), units, 12)
        self.assertEqual("EQUIVALENT_SET", result["status"])
        self.assertEqual(2, result["occurrence_count"])
        self.assertTrue(result["cross_document"])
        self.assertTrue(result["declared_unit_included"])

    def test_unique_passage_stays_unique(self):
        units = [unit("a:B1", "本表用于记录印尼十三薪信息", block_id="a:B1")]
        result = equivalence.resolve(self.record("本表用于记录印尼十三薪信息", "a:B1"), units, 12)
        self.assertEqual("UNIQUE", result["status"])

    def test_pervasive_span_is_not_expanded(self):
        units = [unit(f"a:B{index}", "请求方式：HTTP，POST方法", block_id=f"a:B{index}") for index in range(20)]
        result = equivalence.resolve(self.record("请求方式：HTTP，POST方法", "a:B0"), units, 12)
        self.assertEqual("TOO_MANY_OCCURRENCES", result["status"])

    def test_short_span_is_not_expanded(self):
        result = equivalence.resolve(self.record("POST"), [unit("a:B1", "POST")], 12)
        self.assertEqual("SPAN_TOO_SHORT", result["status"])


class AnswerGroundingTests(unittest.TestCase):
    def span(self) -> str:
        return equivalence.own_content("organizationId | String | 最大100个字节 | 是 | 组织ID")

    def test_literal_answer(self):
        result = readjudication.answer_grounding("String | 最大100个字节", self.span())
        self.assertEqual("LITERAL", result["mode"])

    def test_paraphrased_table_row_is_grounded(self):
        result = readjudication.answer_grounding("类型为String，取值范围为最大100个字节，必填", self.span())
        self.assertEqual("PARAPHRASE_GROUNDED", result["mode"])

    def test_invented_fact_is_rejected(self):
        result = readjudication.answer_grounding("类型为Integer，取值范围为最大100个字节", self.span())
        self.assertEqual("NOT_GROUNDED", result["mode"])
        self.assertIn("Integer", result["ungrounded_segments"])

    def test_longest_glue_term_is_stripped_first(self):
        span = equivalence.own_content("护照生效时间 | VARCHAR2 | Y")
        result = readjudication.answer_grounding("字段类型为VARCHAR2，含义为护照生效时间", span)
        self.assertEqual("PARAPHRASE_GROUNDED", result["mode"])

    def test_glue_removal_is_symmetric(self):
        span = equivalence.own_content("基于并发100人的基础上响应时间不超过3秒")
        result = readjudication.answer_grounding("基于并发100人基础上响应时间不超过3秒", span)
        self.assertEqual("PARAPHRASE_GROUNDED", result["mode"])

    def test_required_flag_synonym_is_accepted(self):
        result = readjudication.answer_grounding("必填", self.span())
        self.assertEqual("PARAPHRASE_GROUNDED", result["mode"])


class RewriteVerificationTests(unittest.TestCase):
    def units(self) -> list[dict]:
        return [unit("a:B1#R002", "表头: 参数 | 类型 | 必须\n数据行: size | Long | 是", block_id="a:B1")]

    def verdict(self, **overrides) -> dict:
        record = {
            "verdict": "REWRITE_PROPOSED",
            "difficulty": "Medium",
            "proposed_question": "机旁库储位查询接口的 size 参数类型和是否必填是什么？",
            "proposed_answer": "类型为Long，必填",
            "proposed_span": "size | Long | 是",
            "note": "补上接口名并收敛答案",
        }
        record.update(overrides)
        return record

    def test_grounded_rewrite_passes(self):
        check = readjudication.verify_rewrite({}, self.verdict(), self.units(), 12)
        self.assertEqual([], check["failures"])
        self.assertEqual(["a:B1#R002"], check["unit_ids"])

    def test_span_outside_corpus_fails(self):
        check = readjudication.verify_rewrite({}, self.verdict(proposed_span="size | Integer | 是"), self.units(), 12)
        self.assertIn("span_not_in_corpus", check["failures"])

    def test_ungrounded_answer_fails(self):
        check = readjudication.verify_rewrite({}, self.verdict(proposed_answer="类型为Integer"), self.units(), 12)
        self.assertIn("answer_not_grounded_in_span", check["failures"])

    def test_pervasive_span_fails(self):
        units = [unit(f"a:B{index}#R001", "数据行: size | Long | 是", block_id=f"a:B{index}") for index in range(20)]
        check = readjudication.verify_rewrite({}, self.verdict(), units, 12)
        self.assertIn("too_many_occurrences", check["failures"])


class DistinctivenessGateTests(unittest.TestCase):
    """The occurrence ceiling must outrank a content approval, whichever round made it."""

    def test_pervasive_span_is_not_distinctive(self):
        record = {"evidence_equivalence": {"status": "TOO_MANY_OCCURRENCES", "occurrence_count": 49}}
        self.assertTrue(readjudication.not_distinctive(record))

    def test_equivalent_set_stays_usable(self):
        record = {"evidence_equivalence": {"status": "EQUIVALENT_SET", "occurrence_count": 3}}
        self.assertFalse(readjudication.not_distinctive(record))

    def test_unique_span_stays_usable(self):
        record = {"evidence_equivalence": {"status": "UNIQUE", "occurrence_count": 1}}
        self.assertFalse(readjudication.not_distinctive(record))


class VerdictValidationTests(unittest.TestCase):
    def write(self, payload: dict) -> Path:
        path = Path(tempfile.mkdtemp()) / "P07.jsonl"
        path.write_text(json.dumps(payload, ensure_ascii=False) + "\n", encoding="utf-8")
        return path

    def valid(self) -> dict:
        return {
            "id": "P07-QA001",
            "verdict": "APPROVED_MULTI_EVIDENCE",
            "difficulty": "Easy",
            "proposed_question": "",
            "proposed_answer": "",
            "proposed_span": "",
            "note": "原文重复不再是拒绝理由",
        }

    def test_valid_record_is_accepted(self):
        self.assertEqual(1, len(readjudication.read_verdicts(self.write(self.valid()))))

    def test_rewrite_requires_a_proposed_question(self):
        payload = {**self.valid(), "verdict": "REWRITE_PROPOSED"}
        with self.assertRaisesRegex(ValueError, "rewrite without a proposed question"):
            readjudication.read_verdicts(self.write(payload))

    def test_non_rewrite_may_not_carry_proposals(self):
        payload = {**self.valid(), "proposed_question": "改写后的题干"}
        with self.assertRaisesRegex(ValueError, "carries proposal fields"):
            readjudication.read_verdicts(self.write(payload))


class HeaderReliabilityTests(unittest.TestCase):
    def test_informative_header_is_reliable(self):
        status = supplement.header_reliability("表头: 参数名称 | 参数类型 | 必须\n数据行: id | String | 是")
        self.assertEqual("RELIABLE", status["status"])

    def test_duplicate_labels_are_flagged(self):
        status = supplement.header_reliability(
            "表头: 系统归属 | 系统名称 | 系统代码 | 系统归属 | 备注\n数据行: 现有系统 | 五即化 | YD | 资讯部 | 环保"
        )
        self.assertEqual("DUPLICATE_HEADER_LABELS", status["status"])
        self.assertEqual(["系统归属"], status["duplicated"])

    def test_placeholder_labels_are_flagged(self):
        status = supplement.header_reliability("表头: 主档 | 主档 | 主档\n数据行: 起始时间 | startTime | String")
        self.assertEqual("UNINFORMATIVE_HEADER", status["status"])

    def test_prose_unit_is_out_of_scope(self):
        self.assertEqual("NOT_TABLE", supplement.header_reliability("系统应支持并发100人")["status"])

    def test_table_row_without_header_is_flagged(self):
        self.assertEqual("NO_HEADER", supplement.header_reliability("数据行: id | String | 是")["status"])


if __name__ == "__main__":
    unittest.main()
