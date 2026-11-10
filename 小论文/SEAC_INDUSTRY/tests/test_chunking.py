import unittest

from seac_chunking.chunkers import (
    FixedSizeChunker,
    ProposedChunker,
    ProposedV6Chunker,
    SeacNoContinuityChunker,
    SeacNoDensityChunker,
    SeacNoHeadingContextChunker,
    SeacNoTableChildrenChunker,
    SemanticChunker,
    StructureOnlyChunker,
)
from seac_chunking.evaluation import evaluate_boundaries, evaluate_retrieval, predicted_boundaries
from seac_chunking.models import Chunk, Unit
from seac_chunking.text import is_noise_text, token_count
from seac_chunking.text import split_structured_text


def unit(unit_id, text, block_type="paragraph", heading="", group=""):
    return Unit(unit_id, unit_id.split("#")[0], "doc", "SRS", block_type, heading, text, token_count(text), group)


class ChunkingTests(unittest.TestCase):
    def test_structure_chunker_breaks_at_heading(self):
        units = [
            unit("B1", "第一段", heading="A"),
            unit("B2", "新章节", block_type="heading", heading="B"),
            unit("B3", "第二段", heading="B"),
        ]
        chunks = StructureOnlyChunker(max_tokens=100).chunk_document("doc", units)
        self.assertEqual(2, len(chunks))
        self.assertEqual(["B1"], chunks[0].source_unit_ids)

    def test_proposed_breaks_at_heading(self):
        units = [
            unit("B1", "第一段", heading="A"),
            unit("B2", "新章节", block_type="heading", heading="B"),
            unit("B3", "第二段", heading="B"),
        ]
        chunker = ProposedChunker(max_tokens=100, breakpoint_percentile=1.0)
        chunker.fit({"doc": units})
        chunks = chunker.chunk_document("doc", units)
        self.assertEqual(["B1"], chunks[0].source_unit_ids)
        self.assertEqual(["B2", "B3"], chunks[1].source_unit_ids)

    def test_proposed_keeps_table_rows_together(self):
        units = [
            unit("T1#R001", "表头 字段名 类型", group="T1"),
            unit("T1#R002", "ID VARCHAR2 主键", group="T1"),
            unit("T1#R003", "NAME VARCHAR2 名称", group="T1"),
        ]
        chunker = ProposedChunker(max_tokens=100, breakpoint_percentile=0.0)
        chunker.encoder.fit([item.text for item in units])
        chunks = chunker.chunk_document("doc", units)
        self.assertEqual(1, len(chunks))
        self.assertIn("文档类型:", chunks[0].metadata["retrieval_text"])

    def test_fixed_chunker_preserves_source_mapping(self):
        units = [unit("B1", "甲" * 20), unit("B2", "乙" * 20)]
        chunks = FixedSizeChunker(max_tokens=25, overlap=0).chunk_document("doc", units)
        self.assertIn("B1", chunks[0].source_unit_ids)
        self.assertTrue(any("B2" in chunk.source_unit_ids for chunk in chunks))

    def test_semantic_and_proposed_enforce_max_tokens(self):
        units = [unit("B1", "测试字段" * 100)]
        for chunker in (SemanticChunker(max_tokens=30), ProposedChunker(max_tokens=30)):
            chunker.fit({"doc": units})
            chunks = chunker.chunk_document("doc", units)
            self.assertTrue(chunks)
            self.assertTrue(all(chunk.token_count <= 30 for chunk in chunks))

    def test_structured_long_unit_split_prefers_sentence_boundaries(self):
        text = "第一条要求。" * 10 + "第二条要求。" * 10
        pieces = split_structured_text(text, target_tokens=30, max_tokens=50)
        self.assertGreater(len(pieces), 1)
        self.assertTrue(all(token_count(piece) <= 50 for piece in pieces))

    def test_v6_creates_parent_and_precise_table_row_children(self):
        units = [
            unit("T1#R001", "表头: 参数名称 | 参数类型 | 必须", group="T1"),
            unit("T1#R002", "数据行: formCodeId | String | 是", group="T1"),
            unit("T1#R003", "数据行: creator | String | 是", group="T1"),
        ]
        chunker = ProposedV6Chunker(max_tokens=100, breakpoint_percentile=1.0)
        chunker.fit({"doc": units})
        chunks = chunker.chunk_document("doc", units)
        parents = [chunk for chunk in chunks if chunk.metadata.get("retrieval_level") == "parent"]
        children = [chunk for chunk in chunks if chunk.metadata.get("retrieval_level") == "child"]
        self.assertEqual(1, len(parents))
        self.assertEqual(3, len(children))
        self.assertTrue(any("formCodeId" in child.text for child in children))
        self.assertTrue(all(child.metadata["parent_chunk_id"] == parents[0].chunk_id for child in children))

    def test_v6_children_do_not_change_parent_boundaries(self):
        units = [
            unit("B1", "第一章节内容", heading="A"),
            unit("B2", "第二章节", block_type="heading", heading="B"),
            unit("B3", "第二章节内容", heading="B"),
        ]
        chunker = ProposedV6Chunker(max_tokens=100, breakpoint_percentile=1.0)
        chunker.fit({"doc": units})
        chunks = chunker.chunk_document("doc", units)
        self.assertEqual({("B1", "B2")}, predicted_boundaries(chunks))

    def test_noise_detection_filters_footer_and_navigation(self):
        self.assertTrue(is_noise_text("2025-09-22 版权所有©示例省丙方信息科技股份有限公司 第 424页，共 428页"))
        self.assertTrue(is_noise_text("输出"))
        self.assertFalse(is_noise_text("输出参数包括返回码和返回提示信息。"))

    def test_v6_splits_oversized_table_row_children(self):
        units = [unit("T1#R001", "超长字段说明。" * 100, group="T1")]
        chunker = ProposedV6Chunker(max_tokens=512, child_target_tokens=30, child_max_tokens=40)
        chunker.fit({"doc": units})
        chunks = chunker.chunk_document("doc", units)
        children = [chunk for chunk in chunks if chunk.metadata.get("retrieval_level") == "child"]
        self.assertGreater(len(children), 1)
        self.assertTrue(all(child.token_count <= 40 for child in children))

    def test_v6_retrieval_ranks_and_returns_parent(self):
        units = [
            unit("B1", "通用说明内容", heading="A"),
            unit("B2", "精确参数 formCodeId 必填", heading="A"),
        ]
        chunker = ProposedV6Chunker(max_tokens=100, breakpoint_percentile=1.0)
        chunker.fit({"doc": units})
        chunks = chunker.chunk_document("doc", units)
        result = evaluate_retrieval(
            chunks,
            [{"qa_id": "Q1", "question": "formCodeId 是否必填？", "gold_evidence_id": "B2"}],
        )
        self.assertEqual(1.0, result["hit_rate@1"])
        self.assertIn("index_seconds", result)
        self.assertIn("query_latency_ms_p95", result)

    def test_no_table_children_ablation_returns_parents_only(self):
        units = [
            unit("T1#R001", "表头 字段 类型", block_type="table_row", group="T1"),
            unit("T1#R002", "NAME VARCHAR2", block_type="table_row", group="T1"),
        ]
        chunker = SeacNoTableChildrenChunker(max_tokens=100)
        chunker.fit({"doc": units})
        chunks = chunker.chunk_document("doc", units)
        self.assertTrue(chunks)
        self.assertTrue(all(chunk.metadata.get("retrieval_level") == "parent" for chunk in chunks))

    def test_no_heading_context_ablation_removes_retrieval_prefix(self):
        units = [unit("B1", "正文内容", heading="第一章")]
        chunker = SeacNoHeadingContextChunker(max_tokens=100)
        chunker.fit({"doc": units})
        chunks = chunker.chunk_document("doc", units)
        self.assertTrue(all("标题路径:" not in chunk.metadata["retrieval_text"] for chunk in chunks))

    def test_density_ablation_removes_score_and_dense_target_signal(self):
        chunker = SeacNoDensityChunker()
        self.assertEqual(0.0, chunker.density_weight)
        self.assertFalse(chunker.density_adaptive_target_enabled)

    def test_continuity_ablation_removes_both_numeric_penalties(self):
        chunker = SeacNoContinuityChunker()
        self.assertEqual(0.0, chunker.continuity_penalty)
        self.assertEqual(0.0, chunker.protected_group_penalty)

    def test_boundary_evaluation_rejects_provisional_labels_by_default(self):
        units = [unit("B1", "第一段"), unit("B2", "第二段")]
        corpus = {"doc": units}
        chunks = StructureOnlyChunker(max_tokens=100).chunk_document("doc", units)
        candidates = [
            {
                "candidate_id": "BP001",
                "left_unit_id": "B1",
                "right_unit_id": "B2",
                "human_label": "",
                "suggested_label": "NO_BREAK",
            }
        ]
        with self.assertRaisesRegex(ValueError, "lack an adjudicated human_label"):
            evaluate_boundaries(chunks, candidates, corpus)
        result = evaluate_boundaries(chunks, candidates, corpus, allow_suggested_labels=True)
        self.assertEqual(1, result["suggested_label_count"])

    def test_boundary_evaluation_excludes_human_noise_labels(self):
        units = [unit("B1", "第一段"), unit("B2", "第二段"), unit("B3", "第三段")]
        corpus = {"doc": units}
        chunks = StructureOnlyChunker(max_tokens=100).chunk_document("doc", units)
        candidates = [
            {"candidate_id": "BP001", "left_unit_id": "B1", "right_unit_id": "B2", "human_label": "NOISE"},
            {"candidate_id": "BP002", "left_unit_id": "B2", "right_unit_id": "B3", "human_label": "NO_BREAK"},
        ]
        result = evaluate_boundaries(chunks, candidates, corpus)
        self.assertEqual(1, result["excluded_noise"])
        self.assertEqual(1, result["evaluated_candidates"])
        self.assertEqual("human_only", result["label_source"])
        self.assertEqual(1, len(result["details"]))
        self.assertIn(result["details"][0]["predicted_label"], {"BREAK", "NO_BREAK"})

    def test_unit_level_evidence_does_not_credit_wrong_table_row(self):
        chunks = [
            Chunk("C1", "test", "doc", "表头: 字段 | 类型\n数据行: NAME | VARCHAR2", 8, ["T1#R002"], ["T1"]),
            Chunk("C2", "test", "doc", "表头: 字段 | 类型\n数据行: ID | NUMBER", 8, ["T1#R003"], ["T1"]),
        ]
        questions = [
            {
                "qa_id": "Q1",
                "question": "NAME 字段是什么类型？",
                "gold_evidence_id": "T1",
                "gold_evidence_unit_id": "T1#R003",
                "gold_evidence_span": "ID | NUMBER",
            }
        ]
        block_result = evaluate_retrieval(chunks, questions, evidence_mode="block")
        unit_result = evaluate_retrieval(chunks, questions, evidence_mode="unit")
        self.assertEqual(1.0, block_result["hit_rate@1"])
        self.assertEqual(0.0, unit_result["hit_rate@1"])
        self.assertEqual(1.0, unit_result["hit_rate@3"])
        self.assertEqual(1.0, unit_result["span_hit_rate@3"])


if __name__ == "__main__":
    unittest.main()
