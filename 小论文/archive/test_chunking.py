import unittest

from seac_chunking.chunkers import FixedSizeChunker, ProposedChunker, ProposedV6Chunker, SemanticChunker, StructureOnlyChunker
from seac_chunking.evaluation import evaluate_retrieval, evaluate_two_stage_retrieval, predicted_boundaries
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
        self.assertTrue(is_noise_text("2025-09-22 版权所有©江苏金恒信息科技股份有限公司 第 424页，共 428页"))
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

    def test_row_all_requires_every_gold_row(self):
        chunks = [
            Chunk(
                chunk_id="C1",
                method="test",
                document="doc",
                text="alpha evidence",
                token_count=2,
                source_unit_ids=["T1#R001"],
                source_block_ids=["T1"],
                metadata={"source_row_ids": ["T1#R001"]},
            ),
            Chunk(
                chunk_id="C2",
                method="test",
                document="doc",
                text="beta evidence",
                token_count=2,
                source_unit_ids=["T1#R002"],
                source_block_ids=["T1"],
                metadata={"source_row_ids": ["T1#R002"]},
            ),
        ]
        questions = [
            {
                "qa_id": "Q1",
                "question": "alpha beta",
                "gold_evidence_id": "T1",
                "metadata": {"gold_table_row_ids": ["T1#R001", "T1#R002"]},
            }
        ]
        any_row = evaluate_retrieval(chunks, questions, top_k=(1, 2), evidence_mode="row")
        all_rows = evaluate_retrieval(chunks, questions, top_k=(1, 2), evidence_mode="row_all")
        self.assertEqual(1.0, any_row["hit_rate@1"])
        self.assertEqual(0.0, all_rows["hit_rate@1"])
        self.assertEqual(1.0, all_rows["hit_rate@2"])

    def test_finance_query_boost_uses_row_metadata(self):
        chunks = [
            Chunk(
                chunk_id="C1",
                method="test",
                document="doc",
                text="financial table row",
                token_count=3,
                source_unit_ids=["R2018"],
                source_block_ids=["T1"],
                metadata={"row_header": "Revenue", "column_header": "2018"},
            ),
            Chunk(
                chunk_id="C2",
                method="test",
                document="doc",
                text="financial table row",
                token_count=3,
                source_unit_ids=["R2019"],
                source_block_ids=["T1"],
                metadata={"row_header": "Revenue", "column_header": "2019"},
            ),
        ]
        result = evaluate_retrieval(
            chunks,
            [{"qa_id": "Q1", "question": "2019 revenue", "gold_evidence_id": "R2019"}],
            finance_query_boost={
                "enabled": True,
                "year_boost": 0.05,
                "metric_boost": 0.05,
                "value_boost": 0.0,
                "max_boost": 0.10,
            },
        )
        self.assertEqual(1.0, result["hit_rate@1"])

    def test_two_stage_retrieval_routes_then_scores_chunks(self):
        chunks = [
            Chunk(
                chunk_id="C1",
                method="test",
                document="doc-a",
                text="alpha revenue evidence 100",
                token_count=4,
                source_unit_ids=["B1"],
                source_block_ids=["B1"],
            ),
            Chunk(
                chunk_id="C2",
                method="test",
                document="doc-b",
                text="beta cost evidence 200",
                token_count=4,
                source_unit_ids=["B2"],
                source_block_ids=["B2"],
            ),
        ]
        questions = [
            {
                "qa_id": "Q1",
                "question": "alpha revenue",
                "source_document": "doc-a",
                "gold_evidence_id": "B1",
            }
        ]
        result = evaluate_two_stage_retrieval(
            chunks,
            questions,
            {"doc-a": "alpha company revenue report", "doc-b": "beta company cost report"},
            doc_top_k=1,
        )
        self.assertEqual(1.0, result["document_recall@1"])
        self.assertEqual(1.0, result["hit_rate@1"])


if __name__ == "__main__":
    unittest.main()
