from __future__ import annotations

from abc import ABC, abstractmethod
from collections import Counter

from .models import Chunk, Unit
from .semantic import CharTfidfEncoder
from .text import is_noise_text, percentile, split_structured_text, split_tokens, token_count, tokenize


KEY_TERMS = (
    "必须", "不得", "至少", "最大", "最小", "仅限", "默认", "主键", "外键", "是否为空",
    "请求方式", "请求路径", "参数", "返回", "字段", "状态", "接口", "表名", "流程", "约束",
)


def _chunk(method: str, document: str, index: int, units: list[Unit], text: str | None = None, metadata=None):
    rendered = text if text is not None else "\n".join(unit.text for unit in units)
    return Chunk(
        chunk_id=f"{method}:{document}:C{index:04d}",
        method=method,
        document=document,
        text=rendered,
        token_count=token_count(rendered),
        source_unit_ids=list(dict.fromkeys(unit.unit_id for unit in units)),
        source_block_ids=list(dict.fromkeys(unit.source_block_id for unit in units)),
        heading_paths=list(dict.fromkeys(unit.heading_path for unit in units if unit.heading_path)),
        metadata=metadata or {},
    )


def _contextualize(chunk: Chunk, document_type: str):
    heading = chunk.heading_paths[-1] if chunk.heading_paths else ""
    prefix_parts = [f"文档类型: {document_type}"]
    if heading:
        prefix_parts.append(f"标题路径: {heading}")
    chunk.metadata["retrieval_text"] = "\n".join(prefix_parts + [chunk.text])
    return chunk


def contextual_retrieval_text(chunk: Chunk, document_type: str = "") -> str:
    """Build the same structural retrieval context for every chunking method."""
    heading = chunk.heading_paths[-1] if chunk.heading_paths else ""
    parts = []
    if document_type:
        parts.append(f"文档类型: {document_type}")
    if heading:
        parts.append(f"标题路径: {heading}")
        parts.append(f"核心主题: {heading.split(' > ')[-1]}")
    parts.append(chunk.text)
    return "\n".join(parts)


class BaseChunker(ABC):
    name = "base"

    def __init__(self, max_tokens=512, overlap=0):
        self.max_tokens = max_tokens
        self.overlap = overlap

    def fit(self, corpus: dict[str, list[Unit]]):
        return self

    @abstractmethod
    def chunk_document(self, document: str, units: list[Unit]) -> list[Chunk]:
        raise NotImplementedError

    def chunk_corpus(self, corpus: dict[str, list[Unit]]) -> list[Chunk]:
        self.fit(corpus)
        return [chunk for document, units in corpus.items() for chunk in self.chunk_document(document, units)]


class FixedSizeChunker(BaseChunker):
    name = "fixed"

    def chunk_document(self, document, units):
        all_tokens = []
        unit_ranges = []
        for unit in units:
            start = len(all_tokens)
            all_tokens.extend(tokenize(unit.text))
            unit_ranges.append((start, len(all_tokens), unit))
        step = max(1, self.max_tokens - self.overlap)
        chunks = []
        for index, start in enumerate(range(0, len(all_tokens), step), 1):
            end = min(start + self.max_tokens, len(all_tokens))
            piece = " ".join(all_tokens[start:end])
            sources = [unit for unit_start, unit_end, unit in unit_ranges if unit_start < end and unit_end > start]
            chunks.append(_chunk(self.name, document, index, sources, piece))
        return chunks


class RecursiveCharacterChunker(BaseChunker):
    name = "recursive"
    separators = ("\n\n", "\n", "。", "；", "，", " ")

    def _split(self, text, separators):
        if token_count(text) <= self.max_tokens:
            return [text]
        if not separators:
            return split_tokens(text, self.max_tokens, self.overlap)
        separator, rest = separators[0], separators[1:]
        pieces = text.split(separator)
        if len(pieces) == 1:
            return self._split(text, rest)
        results, current = [], ""
        for piece in pieces:
            candidate = piece if not current else current + separator + piece
            if token_count(candidate) <= self.max_tokens:
                current = candidate
            else:
                if current:
                    results.extend(self._split(current, rest))
                current = piece
        if current:
            results.extend(self._split(current, rest))
        return results

    def chunk_document(self, document, units):
        text = "\n\n".join(unit.text for unit in units)
        pieces = self._split(text, self.separators)
        chunks = []
        unit_ranges = []
        cursor = 0
        for unit in units:
            start = text.find(unit.text, cursor)
            if start < 0:
                start = cursor
            end = start + len(unit.text)
            unit_ranges.append((start, end, unit))
            cursor = end
        cursor = 0
        for index, piece in enumerate(pieces, 1):
            start = text.find(piece, cursor)
            if start < 0:
                start = cursor
            end = start + len(piece)
            sources = [unit for unit_start, unit_end, unit in unit_ranges if unit_start < end and unit_end > start]
            cursor = end
            chunks.append(_chunk(self.name, document, index, sources, piece))
        return chunks


def _map_text_pieces(method, document, units, text, pieces):
    unit_ranges = []
    cursor = 0
    for unit in units:
        start = text.find(unit.text, cursor)
        if start < 0:
            start = cursor
        end = start + len(unit.text)
        unit_ranges.append((start, end, unit))
        cursor = end
    chunks = []
    previous_start = 0
    for index, piece in enumerate(pieces, 1):
        start = text.find(piece, previous_start)
        if start < 0:
            start = text.find(piece)
        if start < 0:
            start = previous_start
        end = start + len(piece)
        sources = [unit for unit_start, unit_end, unit in unit_ranges if unit_start < end and unit_end > start]
        chunks.append(_chunk(method, document, index, sources, piece))
        previous_start = start + 1
    return chunks


class LangChainRecursiveChunker(BaseChunker):
    """Official LangChain RecursiveCharacterTextSplitter baseline."""

    name = "langchain_recursive"

    def chunk_document(self, document, units):
        try:
            from langchain_text_splitters import RecursiveCharacterTextSplitter
        except ImportError as exc:
            raise RuntimeError(
                "langchain-text-splitters is required for the langchain_recursive baseline."
            ) from exc
        text = "\n\n".join(unit.text for unit in units)
        splitter = RecursiveCharacterTextSplitter(
            chunk_size=self.max_tokens,
            chunk_overlap=self.overlap,
            length_function=token_count,
            separators=["\n\n", "\n", "。", "；", "，", " ", ""],
            keep_separator=False,
        )
        return _map_text_pieces(self.name, document, units, text, splitter.split_text(text))


class LlamaIndexSentenceChunker(BaseChunker):
    """Official LlamaIndex SentenceSplitter baseline."""

    name = "llamaindex_sentence"

    def chunk_document(self, document, units):
        try:
            from llama_index.core.node_parser import SentenceSplitter
        except ImportError as exc:
            raise RuntimeError(
                "llama-index-core is required for the llamaindex_sentence baseline."
            ) from exc
        text = "\n\n".join(unit.text for unit in units)
        splitter = SentenceSplitter(
            chunk_size=self.max_tokens,
            chunk_overlap=self.overlap,
            tokenizer=tokenize,
        )
        return _map_text_pieces(self.name, document, units, text, splitter.split_text(text))


class StructureOnlyChunker(BaseChunker):
    name = "structure"

    def chunk_document(self, document, units):
        chunks, current = [], []
        for unit in units:
            heading_break = unit.block_type == "heading" and current
            group_break = current and unit.protected_group and current[-1].protected_group != unit.protected_group
            too_large = current and sum(item.token_count for item in current) + unit.token_count > self.max_tokens
            if heading_break or group_break or too_large:
                chunks.append(_chunk(self.name, document, len(chunks) + 1, current))
                current = []
            if unit.token_count > self.max_tokens:
                for piece in split_tokens(unit.text, self.max_tokens, 0):
                    chunks.append(_chunk(self.name, document, len(chunks) + 1, [unit], piece))
            else:
                current.append(unit)
        if current:
            chunks.append(_chunk(self.name, document, len(chunks) + 1, current))
        return chunks


class SemanticChunker(BaseChunker):
    name = "semantic"

    def __init__(self, max_tokens=512, overlap=0, breakpoint_percentile=0.8, encoder=None):
        super().__init__(max_tokens, overlap)
        self.breakpoint_percentile = breakpoint_percentile
        self.encoder = encoder or CharTfidfEncoder()

    def fit(self, corpus):
        self.encoder.fit([unit.text for units in corpus.values() for unit in units])
        return self

    def distances(self, units):
        vectors = self.encoder.encode_many([unit.text for unit in units])
        return [1 - self.encoder.similarity_vectors(vectors[index - 1], vectors[index]) for index in range(1, len(vectors))]

    def chunk_document(self, document, units):
        if not units:
            return []
        distances = self.distances(units)
        threshold = percentile(distances, self.breakpoint_percentile)
        chunks, current = [], []
        for index, unit in enumerate(units):
            if unit.token_count > self.max_tokens:
                if current:
                    chunks.append(_chunk(self.name, document, len(chunks) + 1, current, metadata={"threshold": threshold}))
                    current = []
                for piece in split_tokens(unit.text, self.max_tokens, 0):
                    chunks.append(_chunk(self.name, document, len(chunks) + 1, [unit], piece, metadata={"threshold": threshold}))
                continue
            too_large = sum(item.token_count for item in current) + unit.token_count > self.max_tokens
            semantic_break = index > 0 and distances[index - 1] >= threshold
            if current and (too_large or semantic_break):
                chunks.append(_chunk(self.name, document, len(chunks) + 1, current, metadata={"threshold": threshold}))
                current = []
            current.append(unit)
        if current:
            chunks.append(_chunk(self.name, document, len(chunks) + 1, current, metadata={"threshold": threshold}))
        return chunks


class ProposedChunker(SemanticChunker):
    name = "proposed"

    def __init__(
        self,
        max_tokens=512,
        overlap=0,
        breakpoint_percentile=0.8,
        semantic_weight=0.45,
        structure_weight=0.30,
        density_weight=0.25,
        continuity_penalty=0.35,
        protected_group_penalty=1.0,
        structure_heading_signal=1.0,
        structure_path_change_signal=0.7,
        structure_group_change_signal=0.5,
        density_change_scale=10.0,
        table_ratio_threshold_adjustment=0.08,
        heading_ratio_threshold_adjustment=-0.05,
        key_density_cutoff=0.03,
        density_adaptive_target_enabled=True,
        soft_target_tokens=320,
        dense_target_tokens=224,
        min_chunk_tokens=48,
        heading_context_mode="full_and_leaf",
        adaptive_threshold_enabled=True,
        encoder=None,
    ):
        super().__init__(max_tokens, overlap, breakpoint_percentile, encoder=encoder)
        self.semantic_weight = semantic_weight
        self.structure_weight = structure_weight
        self.density_weight = density_weight
        self.continuity_penalty = continuity_penalty
        self.protected_group_penalty = protected_group_penalty
        self.structure_heading_signal = structure_heading_signal
        self.structure_path_change_signal = structure_path_change_signal
        self.structure_group_change_signal = structure_group_change_signal
        self.density_change_scale = density_change_scale
        self.table_ratio_threshold_adjustment = table_ratio_threshold_adjustment
        self.heading_ratio_threshold_adjustment = heading_ratio_threshold_adjustment
        self.key_density_cutoff = key_density_cutoff
        self.density_adaptive_target_enabled = density_adaptive_target_enabled
        self.soft_target_tokens = soft_target_tokens
        self.dense_target_tokens = dense_target_tokens
        self.min_chunk_tokens = min_chunk_tokens
        self.heading_context_mode = heading_context_mode
        self.adaptive_threshold_enabled = adaptive_threshold_enabled

    def _density(self, unit):
        hits = sum(unit.text.count(term) for term in KEY_TERMS)
        return hits / max(1, unit.token_count)

    def scores(self, units):
        vectors = self.encoder.encode_many([unit.text for unit in units])
        scores = []
        for index, (left, right) in enumerate(zip(units, units[1:]), 1):
            semantic = 1 - self.encoder.similarity_vectors(vectors[index - 1], vectors[index])
            structure = 0.0
            if right.block_type == "heading":
                structure += self.structure_heading_signal
            if left.heading_path != right.heading_path:
                structure += self.structure_path_change_signal
            if left.protected_group != right.protected_group and (left.protected_group or right.protected_group):
                structure += self.structure_group_change_signal
            density = min(
                1.0,
                abs(self._density(left) - self._density(right)) * self.density_change_scale,
            )
            penalty = 0.0
            if left.heading_path and left.heading_path == right.heading_path:
                penalty += self.continuity_penalty
            if left.protected_group and left.protected_group == right.protected_group:
                penalty += self.protected_group_penalty
            score = (
                self.semantic_weight * semantic
                + self.structure_weight * min(1.0, structure)
                + self.density_weight * density
                - penalty
            )
            scores.append(
                {
                    "score": score,
                    "semantic": semantic,
                    "structure": min(1.0, structure),
                    "density_change": density,
                    "continuity_penalty": penalty,
                }
            )
        return scores

    def chunk_document(self, document, units):
        if not units:
            return []
        score_details = self.scores(units)
        scores = [item["score"] for item in score_details]
        base_threshold = percentile(scores, self.breakpoint_percentile)
        table_ratio = sum(unit.block_type == "table_row" for unit in units) / len(units)
        heading_ratio = sum(unit.block_type == "heading" for unit in units) / len(units)
        adaptive_threshold = (
            base_threshold
            + self.table_ratio_threshold_adjustment * table_ratio
            + self.heading_ratio_threshold_adjustment * heading_ratio
            if self.adaptive_threshold_enabled
            else base_threshold
        )
        chunks, current = [], []

        def emit(items, text=None):
            chunk = _chunk(
                self.name,
                document,
                len(chunks) + 1,
                items,
                text,
                metadata={"adaptive_threshold": adaptive_threshold},
            )
            contextualized = (
                _contextualize(chunk, items[0].document_type if items else "")
                if self.heading_context_mode != "none"
                else chunk
            )
            if self.heading_context_mode == "none":
                contextualized.metadata["retrieval_text"] = contextualized.text
            heading = contextualized.heading_paths[-1] if contextualized.heading_paths else ""
            if heading and self.heading_context_mode == "full_and_leaf":
                leaf = heading.split(" > ")[-1]
                contextualized.metadata["retrieval_text"] = (
                    f"核心主题: {leaf}\n{contextualized.metadata['retrieval_text']}"
                )
            chunks.append(contextualized)

        for index, unit in enumerate(units):
            if unit.token_count > self.max_tokens:
                if current:
                    emit(current)
                    current = []
                for piece in split_structured_text(unit.text, self.dense_target_tokens, self.max_tokens):
                    emit([unit], piece)
                continue
            current_tokens = sum(item.token_count for item in current)
            candidate_tokens = current_tokens + unit.token_count
            too_large = candidate_tokens > self.max_tokens
            scored_break = index > 0 and score_details[index - 1]["score"] >= adaptive_threshold
            protected = bool(current and current[-1].protected_group and current[-1].protected_group == unit.protected_group)
            heading_break = bool(current and unit.block_type == "heading")
            current_density = sum(self._density(item) for item in current) / len(current) if current else 0.0
            target_tokens = (
                self.dense_target_tokens
                if self.density_adaptive_target_enabled
                and current_density >= self.key_density_cutoff
                else self.soft_target_tokens
            )
            heading_change = bool(current and current[-1].heading_path != unit.heading_path)
            heading_anchor = bool(current and current[-1].block_type == "heading")
            under_minimum = bool(current and current_tokens < self.min_chunk_tokens)
            adaptive_size_break = (
                candidate_tokens > target_tokens
                and not protected
                and (
                    heading_change
                    or (
                        self.density_adaptive_target_enabled
                        and current_density >= self.key_density_cutoff
                    )
                )
            )
            optional_break = (adaptive_size_break or (scored_break and not protected)) and not heading_anchor and not under_minimum
            if current and (too_large or heading_break or optional_break):
                emit(current)
                current = []
            current.append(unit)
        if current:
            emit(current)
        return chunks


class ProposedV6Chunker(ProposedChunker):
    """Hierarchical v6: retrieve precise children and return coherent v5 parents."""

    name = "proposed_v6"

    def __init__(
        self,
        *args,
        child_target_tokens=112,
        child_max_tokens=176,
        generate_children=True,
        generate_table_children=True,
        **kwargs,
    ):
        super().__init__(*args, **kwargs)
        self.child_target_tokens = child_target_tokens
        self.child_max_tokens = child_max_tokens
        self.generate_children = generate_children
        self.generate_table_children = generate_table_children

    def _child_texts(self, unit: Unit):
        if is_noise_text(unit.text):
            return []
        if unit.block_type == "heading":
            return []
        if unit.block_type == "table_row":
            if not self.generate_table_children:
                return []
            if unit.token_count <= self.child_max_tokens:
                return [unit.text]
            return split_structured_text(unit.text, self.child_target_tokens, self.child_max_tokens)
        return split_structured_text(unit.text, self.child_target_tokens, self.child_max_tokens)

    def chunk_document(self, document, units):
        parents = super().chunk_document(document, units)
        unit_lookup = {unit.unit_id: unit for unit in units}
        output = []
        for parent_index, parent in enumerate(parents, 1):
            parent.chunk_id = f"{self.name}:{document}:P{parent_index:04d}"
            parent.method = self.name
            parent.metadata["retrieval_level"] = "parent"
            parent.metadata["boundary_chunk"] = True
            parent.metadata["retrieval_text"] = (
                contextual_retrieval_text(
                    parent,
                    next((unit_lookup[item].document_type for item in parent.source_unit_ids if item in unit_lookup), ""),
                )
                if self.heading_context_mode != "none"
                else parent.text
            )
            output.append(parent)

            if not self.generate_children:
                continue

            child_index = 0
            for unit_id in parent.source_unit_ids:
                unit = unit_lookup.get(unit_id)
                if not unit:
                    continue
                # A long source unit may already have been split into multiple parents.
                # Derive children from that parent's slice to avoid duplicating the full unit.
                if len(parent.source_unit_ids) == 1 and parent.text != unit.text:
                    pieces = (
                        []
                        if is_noise_text(parent.text)
                        else split_structured_text(parent.text, self.child_target_tokens, self.child_max_tokens)
                    )
                else:
                    pieces = self._child_texts(unit)
                for piece in pieces:
                    child_index += 1
                    child = _chunk(
                        self.name,
                        document,
                        child_index,
                        [unit],
                        piece,
                        metadata={
                            "retrieval_level": "child",
                            "boundary_chunk": False,
                            "parent_chunk_id": parent.chunk_id,
                        },
                    )
                    child.chunk_id = f"{parent.chunk_id}:S{child_index:03d}"
                    child.metadata["retrieval_text"] = (
                        contextual_retrieval_text(child, unit.document_type)
                        if self.heading_context_mode != "none"
                        else child.text
                    )
                    output.append(child)

            contains_table_rows = any(
                unit_lookup[item].block_type == "table_row"
                for item in parent.source_unit_ids
                if item in unit_lookup
            )
            if (
                child_index == 0
                and not is_noise_text(parent.text)
                and (self.generate_table_children or not contains_table_rows)
            ):
                child = _chunk(
                    self.name,
                    document,
                    1,
                    [unit_lookup[item] for item in parent.source_unit_ids if item in unit_lookup],
                    parent.text,
                    metadata={
                        "retrieval_level": "child",
                        "boundary_chunk": False,
                        "parent_chunk_id": parent.chunk_id,
                    },
                )
                child.chunk_id = f"{parent.chunk_id}:S001"
                child.metadata["retrieval_text"] = (
                    contextual_retrieval_text(
                        child,
                        next((unit_lookup[item].document_type for item in parent.source_unit_ids if item in unit_lookup), ""),
                    )
                    if self.heading_context_mode != "none"
                    else child.text
                )
                output.append(child)
        return output


class SeacNoSemanticChunker(ProposedV6Chunker):
    name = "seac_no_semantic"

    def __init__(self, *args, **kwargs):
        kwargs["semantic_weight"] = 0.0
        super().__init__(*args, **kwargs)


class SeacNoDensityChunker(ProposedV6Chunker):
    name = "seac_no_density"

    def __init__(self, *args, **kwargs):
        kwargs["density_weight"] = 0.0
        kwargs["density_adaptive_target_enabled"] = False
        super().__init__(*args, **kwargs)


class SeacNoContinuityChunker(ProposedV6Chunker):
    name = "seac_no_continuity"

    def __init__(self, *args, **kwargs):
        kwargs["continuity_penalty"] = 0.0
        kwargs["protected_group_penalty"] = 0.0
        super().__init__(*args, **kwargs)


class SeacNoAdaptiveThresholdChunker(ProposedV6Chunker):
    name = "seac_no_adaptive_threshold"

    def __init__(self, *args, **kwargs):
        kwargs["adaptive_threshold_enabled"] = False
        super().__init__(*args, **kwargs)


class SeacNoHeadingContextChunker(ProposedV6Chunker):
    name = "seac_no_heading_context"

    def __init__(self, *args, **kwargs):
        kwargs["heading_context_mode"] = "none"
        super().__init__(*args, **kwargs)


class SeacNoTableChildrenChunker(ProposedV6Chunker):
    name = "seac_no_table_children"

    def __init__(self, *args, **kwargs):
        kwargs["generate_table_children"] = False
        super().__init__(*args, **kwargs)


class SeacNoFusionChunker(ProposedV6Chunker):
    name = "seac_no_fusion"


def build_chunker(name: str, **kwargs) -> BaseChunker:
    chunkers = {
        "fixed": FixedSizeChunker,
        "recursive": RecursiveCharacterChunker,
        "langchain_recursive": LangChainRecursiveChunker,
        "llamaindex_sentence": LlamaIndexSentenceChunker,
        "structure": StructureOnlyChunker,
        "semantic": SemanticChunker,
        "proposed": ProposedChunker,
        "proposed_v6": ProposedV6Chunker,
        "seac_no_semantic": SeacNoSemanticChunker,
        "seac_no_density": SeacNoDensityChunker,
        "seac_no_continuity": SeacNoContinuityChunker,
        "seac_no_adaptive_threshold": SeacNoAdaptiveThresholdChunker,
        "seac_no_heading_context": SeacNoHeadingContextChunker,
        "seac_no_table_children": SeacNoTableChildrenChunker,
        "seac_no_fusion": SeacNoFusionChunker,
    }
    if name not in chunkers:
        raise ValueError(f"Unknown chunker {name!r}. Available: {', '.join(chunkers)}")
    return chunkers[name](**kwargs)
