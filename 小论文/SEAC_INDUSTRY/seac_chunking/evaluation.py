from __future__ import annotations

import re
import time
from collections import Counter

from .models import Chunk
from .semantic import CharTfidfEncoder
from .units import block_to_units


def predicted_boundaries(chunks: list[Chunk]) -> set[tuple[str, str]]:
    boundaries = set()
    by_document = {}
    for chunk in chunks:
        if chunk.metadata.get("boundary_chunk") is False:
            continue
        by_document.setdefault(chunk.document, []).append(chunk)
    for document_chunks in by_document.values():
        for left, right in zip(document_chunks, document_chunks[1:]):
            if left.source_unit_ids and right.source_unit_ids:
                boundaries.add((left.source_unit_ids[-1], right.source_unit_ids[0]))
    return boundaries


def evaluate_boundaries(chunks, candidates, corpus, allow_suggested_labels=False):
    predicted = predicted_boundaries(chunks)
    block_mapping = block_to_units(corpus)
    labels = []
    missing_human_labels = []
    excluded_noise = 0
    suggested_label_count = 0
    details = []
    for candidate in candidates:
        human = candidate.get("human_label", "")
        if human == "NOISE":
            excluded_noise += 1
            continue
        if human in {"BREAK", "NO_BREAK"}:
            label = human
        elif allow_suggested_labels and candidate.get("suggested_label") in {"BREAK", "NO_BREAK"}:
            label = candidate["suggested_label"]
            suggested_label_count += 1
        else:
            missing_human_labels.append(candidate.get("candidate_id", "<unknown>"))
            continue
        left = candidate["left_unit_id"]
        right = candidate["right_unit_id"]
        if "#R" not in left and left in block_mapping:
            left = block_mapping[left][-1]
        if "#R" not in right and right in block_mapping:
            right = block_mapping[right][0]
        boundary = (left, right)
        predicted_break = boundary in predicted
        labels.append((label, boundary))
        details.append(
            {
                "candidate_id": candidate.get("candidate_id", ""),
                "source_document": candidate.get("source_document", ""),
                "document_type": candidate.get("document_type", ""),
                "boundary_stratum": candidate.get("boundary_stratum", ""),
                "human_label": label,
                "predicted_label": "BREAK" if predicted_break else "NO_BREAK",
                "correct": (label == "BREAK") == predicted_break,
                "left_unit_id": left,
                "right_unit_id": right,
            }
        )
    if missing_human_labels:
        preview = ", ".join(missing_human_labels[:10])
        suffix = " ..." if len(missing_human_labels) > 10 else ""
        raise ValueError(
            f"{len(missing_human_labels)} breakpoint candidates lack an adjudicated human_label "
            f"(examples: {preview}{suffix}). Pass allow_suggested_labels=True only for exploratory runs."
        )
    if not labels:
        raise ValueError("No BREAK/NO_BREAK human labels are available for boundary evaluation.")
    tp = sum(label == "BREAK" and boundary in predicted for label, boundary in labels)
    fp = sum(label == "NO_BREAK" and boundary in predicted for label, boundary in labels)
    fn = sum(label == "BREAK" and boundary not in predicted for label, boundary in labels)
    tn = sum(label == "NO_BREAK" and boundary not in predicted for label, boundary in labels)
    precision = tp / (tp + fp) if tp + fp else 0.0
    recall = tp / (tp + fn) if tp + fn else 0.0
    f1 = 2 * precision * recall / (precision + recall) if precision + recall else 0.0
    return {
        "tp": tp,
        "fp": fp,
        "fn": fn,
        "tn": tn,
        "precision": precision,
        "recall": recall,
        "f1": f1,
        "evaluated_candidates": len(labels),
        "excluded_noise": excluded_noise,
        "suggested_label_count": suggested_label_count,
        "label_source": "suggested_or_human" if suggested_label_count else "human_only",
        "details": details,
    }


def _rank_chunks(question, chunks, encoder, chunk_vectors):
    query = encoder.encode(question)
    scored = []
    for chunk, chunk_vector in zip(chunks, chunk_vectors):
        scored.append((encoder.similarity_vectors(query, chunk_vector), chunk))
    return [chunk for _, chunk in sorted(scored, key=lambda item: item[0], reverse=True)]


def _rank_hierarchical(question, chunks, encoder, chunk_vectors, parent_weight=0.35):
    query = encoder.encode(question)
    parents = {
        chunk.chunk_id: chunk
        for chunk in chunks
        if chunk.metadata.get("retrieval_level") == "parent"
    }
    parent_scores = {}
    child_scores = {}
    for chunk, chunk_vector in zip(chunks, chunk_vectors):
        score = encoder.similarity_vectors(query, chunk_vector)
        if chunk.metadata.get("retrieval_level") == "parent":
            parent_scores[chunk.chunk_id] = score
        else:
            parent_id = chunk.metadata.get("parent_chunk_id", chunk.chunk_id)
            child_scores[parent_id] = max(child_scores.get(parent_id, float("-inf")), score)
    scores = {
        parent_id: parent_weight * parent_score + (1 - parent_weight) * child_scores.get(parent_id, parent_score)
        for parent_id, parent_score in parent_scores.items()
    }
    return [
        parents[parent_id]
        for parent_id, _ in sorted(scores.items(), key=lambda item: item[1], reverse=True)
        if parent_id in parents
    ]


def _as_list(value):
    if value is None or value == "":
        return []
    return list(value) if isinstance(value, (list, tuple, set)) else [value]


def _gold_evidence_ids(item, evidence_mode):
    unit_ids = _as_list(item.get("gold_evidence_unit_ids")) or _as_list(item.get("gold_evidence_unit_id"))
    block_ids = _as_list(item.get("gold_evidence_ids")) or _as_list(item.get("gold_evidence_id"))
    if evidence_mode == "unit":
        if not unit_ids:
            raise ValueError(f'{item.get("qa_id", "<unknown>")} lacks gold_evidence_unit_id for unit-level evaluation.')
        return unit_ids, "unit"
    if evidence_mode == "block":
        if not block_ids:
            raise ValueError(f'{item.get("qa_id", "<unknown>")} lacks gold_evidence_id for block-level evaluation.')
        return block_ids, "block"
    if unit_ids:
        return unit_ids, "unit"
    if block_ids:
        return block_ids, "block"
    raise ValueError(f'{item.get("qa_id", "<unknown>")} has no gold evidence identifier.')


def _chunk_matches_evidence(chunk, evidence_ids, evidence_level):
    if evidence_level == "unit":
        return any(evidence_id in chunk.source_unit_ids for evidence_id in evidence_ids)
    return any(
        evidence_id in chunk.source_block_ids or evidence_id in chunk.source_unit_ids
        for evidence_id in evidence_ids
    )


def _normalize_span_text(text):
    return re.sub(r"\s+", "", str(text or ""))


def _span_rank(ranked, item):
    spans = _as_list(item.get("gold_evidence_spans")) or _as_list(item.get("gold_evidence_span"))
    normalized_spans = [_normalize_span_text(span) for span in spans if _normalize_span_text(span)]
    if not normalized_spans:
        return None, False
    for index, chunk in enumerate(ranked, 1):
        text = _normalize_span_text(chunk.text)
        if any(span in text for span in normalized_spans):
            return index, True
    return None, True


def evaluate_retrieval(
    chunks,
    questions,
    top_k=(1, 3, 5),
    encoder=None,
    contextualize_all=False,
    document_types=None,
    hierarchy_parent_weight=0.35,
    evidence_mode="auto",
):
    if evidence_mode not in {"auto", "block", "unit"}:
        raise ValueError(f"Unsupported evidence_mode: {evidence_mode}")
    encoder = encoder or CharTfidfEncoder()
    hierarchical = any(chunk.metadata.get("retrieval_level") == "parent" for chunk in chunks)
    retrieval_chunks = chunks if hierarchical else [chunk for chunk in chunks if chunk.metadata.get("retrieval_level") != "parent"]
    if contextualize_all:
        from .chunkers import contextual_retrieval_text

        document_types = document_types or {}
        retrieval_texts = [
            contextual_retrieval_text(chunk, document_types.get(chunk.document, ""))
            for chunk in retrieval_chunks
        ]
    else:
        retrieval_texts = [chunk.metadata.get("retrieval_text", chunk.text) for chunk in retrieval_chunks]
    index_started = time.perf_counter()
    encoder.fit(retrieval_texts + [item["question"] for item in questions])
    chunk_vectors = encoder.encode_many(retrieval_texts)
    index_seconds = time.perf_counter() - index_started
    reciprocal_ranks = []
    hits = Counter()
    span_reciprocal_ranks = []
    span_hits = Counter()
    evidence_level_counts = Counter()
    query_latencies_ms = []
    details = []
    for item in questions:
        query_started = time.perf_counter()
        ranked = (
            _rank_hierarchical(
                item["question"],
                retrieval_chunks,
                encoder,
                chunk_vectors,
                parent_weight=hierarchy_parent_weight,
            )
            if hierarchical
            else _rank_chunks(item["question"], retrieval_chunks, encoder, chunk_vectors)
        )
        query_latency_ms = (time.perf_counter() - query_started) * 1000
        query_latencies_ms.append(query_latency_ms)
        evidence_ids, evidence_level = _gold_evidence_ids(item, evidence_mode)
        evidence_level_counts[evidence_level] += 1
        relevant_ranks = [
            index
            for index, chunk in enumerate(ranked, 1)
            if _chunk_matches_evidence(chunk, evidence_ids, evidence_level)
        ]
        rank = min(relevant_ranks) if relevant_ranks else None
        reciprocal_ranks.append(1 / rank if rank else 0.0)
        for k in top_k:
            hits[k] += int(rank is not None and rank <= k)
        span_rank, has_span = _span_rank(ranked, item)
        if has_span:
            span_reciprocal_ranks.append(1 / span_rank if span_rank else 0.0)
            for k in top_k:
                span_hits[k] += int(span_rank is not None and span_rank <= k)
        details.append(
            {
                "qa_id": item["qa_id"],
                "question_type": item.get("question_type", ""),
                "source_document": item.get("source_document", ""),
                "rank": rank,
                "span_rank": span_rank if has_span else None,
                "has_gold_span": has_span,
                "evidence_level": evidence_level,
                "gold_evidence_ids": evidence_ids,
                "top_chunk_ids": [chunk.chunk_id for chunk in ranked[: max(top_k)]],
                "query_latency_ms": query_latency_ms,
            }
        )
    total = max(1, len(questions))
    output = {
        "mrr": sum(reciprocal_ranks) / total,
        **{f"hit_rate@{k}": hits[k] / total for k in top_k},
        "evidence_level_counts": dict(evidence_level_counts),
        "index_seconds": index_seconds,
        "query_latency_ms_mean": sum(query_latencies_ms) / len(query_latencies_ms) if query_latencies_ms else 0.0,
        "query_latency_ms_p50": _percentile_value(query_latencies_ms, 0.50),
        "query_latency_ms_p95": _percentile_value(query_latencies_ms, 0.95),
        "details": details,
    }
    if span_reciprocal_ranks:
        span_total = len(span_reciprocal_ranks)
        output.update(
            {
                "span_question_count": span_total,
                "span_mrr": sum(span_reciprocal_ranks) / span_total,
                **{f"span_hit_rate@{k}": span_hits[k] / span_total for k in top_k},
            }
        )
    return output


def _percentile_value(values, q):
    if not values:
        return 0.0
    ordered = sorted(values)
    position = (len(ordered) - 1) * q
    lower = int(position)
    upper = min(lower + 1, len(ordered) - 1)
    fraction = position - lower
    return ordered[lower] * (1 - fraction) + ordered[upper] * fraction
