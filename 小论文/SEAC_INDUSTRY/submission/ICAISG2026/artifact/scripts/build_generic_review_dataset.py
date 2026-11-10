import argparse
import json
import re
from collections import Counter, defaultdict
from pathlib import Path

from boundary_candidate_utils import make_candidates


TYPE_QUOTAS = {
    "Meeting fact": 6,
    "API": 8,
    "API parameter": 5,
    "Database": 6,
    "Constraint": 6,
    "Feature": 6,
    "Process": 3,
    "Numeric constraint": 5,
}


def block_text(block):
    if block.get("rows"):
        return " || ".join(" | ".join(str(cell) for cell in row) for row in block["rows"])
    return block.get("text", "")


def subject(block, fallback):
    heading = block.get("heading_path", "")
    return heading.split(" > ")[-1] if heading else fallback


def sentence_with_keyword(text, keywords):
    for sentence in re.split(r"[。；;\n]", text):
        sentence = " ".join(sentence.split()).strip("，,：: ")
        if 20 <= len(sentence) <= 180 and any(keyword in sentence for keyword in keywords):
            return sentence
    return ""


def numeric_sentence(text):
    for sentence in re.split(r"[。；;\n]", text):
        sentence = " ".join(sentence.split()).strip("，,：: ")
        if 20 <= len(sentence) <= 180 and re.search(r"\d+(?:\.\d+)?\s*(?:套|台|个|小时|天|分钟|秒|%)", sentence):
            return sentence
    return ""


def add(pool, seen, document, block, question, answer, question_type, evidence=None):
    answer = " ".join(str(answer).split()).strip()
    evidence = " ".join(str(evidence if evidence is not None else answer).split()).strip()
    key = (question, answer)
    if not answer or len(answer) > 240 or key in seen:
        return
    seen.add(key)
    item = {
            "question": question,
            "reference_answer": answer,
            "source_document": document["file_name"],
            "source_section": block.get("heading_path", ""),
            "gold_evidence_id": block["block_id"],
            "gold_evidence_span": evidence,
            "question_type": question_type,
            "draft_origin": "rule_generated_for_human_review",
            "approval_status": "PENDING",
            "privacy_flags": [],
        }
    if block.get("rows"):
        normalized_evidence = " ".join(evidence.split())
        matches = []
        for row_index, row in enumerate(block["rows"], 1):
            row_text = " | ".join(str(cell) for cell in row)
            if " ".join(row_text.split()) == normalized_evidence:
                matches.append(row_index)
        if len(matches) == 1:
            item["gold_evidence_unit_id"] = f'{block["block_id"]}#R{matches[0]:03d}'
            item["evidence_alignment_status"] = "UNIQUE_EXACT_ROW"
        else:
            item["gold_evidence_unit_id"] = ""
            item["evidence_alignment_status"] = "HUMAN_ALIGNMENT_REQUIRED"
    else:
        item["gold_evidence_unit_id"] = block["block_id"]
        item["evidence_alignment_status"] = "BLOCK_IS_UNIT"
    if re.search(r"https?://|(?:10|172|192)\.\d+\.\d+\.\d+", evidence, re.I):
        item["privacy_flags"].append("INTERNAL_NETWORK_OR_URL")
    if re.search(r"[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}", evidence):
        item["privacy_flags"].append("EMAIL")
    pool.append(item)


def meeting_candidates(document, pool, seen):
    meeting_name = Path(document["file_name"]).stem
    date_suffix = re.search(r"(\d{4})\s*$", meeting_name)
    meeting_label = f"{date_suffix.group(1)}需求调研会议" if date_suffix else "本次需求调研会议"
    for block in document["blocks"]:
        for row in block.get("rows", []):
            cells = [str(cell).strip() for cell in row]
            for label, question in (("时间", f"{meeting_label}的时间是什么？"), ("地点", f"{meeting_label}的地点在哪里？")):
                if label in cells:
                    index = cells.index(label)
                    if index + 1 < len(cells) and cells[index + 1]:
                        add(pool, seen, document, block, question, cells[index + 1], "Meeting fact", " | ".join(cells))


def api_candidates(document, pool, seen):
    blocks = document["blocks"]
    current_topic = "该接口"
    for index, block in enumerate(blocks):
        text = block.get("text", "")
        if (
            text.endswith("接口")
            and 4 <= len(text) <= 40
            and text not in {"接口定义", "外部接口", "内部接口", "返回接口"}
        ):
            current_topic = text
        topic = current_topic if current_topic != "该接口" else subject(block, "该接口")
        method = re.search(r"请求方式[：:]\s*([A-Z]+)", text, re.I)
        if method:
            add(pool, seen, document, block, f"{topic}使用什么请求方式？", method.group(1).upper(), "API", text)
        if re.search(r"请求路径[：:]\s*$", text):
            for next_block in blocks[index + 1 : index + 3]:
                path = next_block.get("text", "").strip()
                if re.match(r"https?://", path):
                    add(pool, seen, document, next_block, f"{topic}的请求路径是什么？", path, "API", path)
                    break
        rows = block.get("rows", [])
        if len(rows) >= 2 and {"参数名称", "参数类型"}.issubset(set(rows[0])):
            header = rows[0]
            name_i, type_i = header.index("参数名称"), header.index("参数类型")
            required_i = header.index("必须") if "必须" in header else None
            range_i = header.index("取值范围") if "取值范围" in header else None
            for row in rows[1:3]:
                if max(name_i, type_i) >= len(row) or not row[name_i]:
                    continue
                pieces = [f"类型为{row[type_i]}"]
                if range_i is not None and range_i < len(row) and row[range_i]:
                    pieces.append(f"取值范围为{row[range_i]}")
                if required_i is not None and required_i < len(row) and row[required_i]:
                    pieces.append("必填" if row[required_i] == "是" else "非必填")
                add(
                    pool,
                    seen,
                    document,
                    block,
                    f"{topic}中参数 {row[name_i]} 的类型、取值范围和必填要求是什么？",
                    "，".join(pieces),
                    "API parameter",
                    " | ".join(str(cell) for cell in row),
                )


def database_candidates(document, pool, seen):
    for block in document["blocks"]:
        for row in block.get("rows", []):
            cells = [str(cell).strip() for cell in row]
            if "表名" not in cells or "表的描述" not in cells:
                continue
            name_i, desc_i = cells.index("表名"), cells.index("表的描述")
            if name_i + 1 >= len(cells) or desc_i + 1 >= len(cells):
                continue
            table_name, description = cells[name_i + 1], cells[desc_i + 1]
            if table_name and description:
                add(pool, seen, document, block, f"{table_name} 表用于记录什么信息？", description, "Database", " | ".join(cells))


def prose_candidates(document, pool, seen):
    for block in document["blocks"]:
        if block.get("rows"):
            continue
        text = " ".join(block.get("text", "").split())
        topic = subject(block, document["document_type"])
        constraint = sentence_with_keyword(text, ("至少", "必须", "默认", "不得", "最多", "仅限"))
        if constraint:
            add(pool, seen, document, block, f"{topic}中规定了什么约束？", constraint, "Constraint", constraint)
        feature = sentence_with_keyword(text, ("支持", "提供", "具备"))
        if feature:
            add(pool, seen, document, block, f"{topic}支持或提供什么能力？", feature, "Feature", feature)
        process = sentence_with_keyword(text, ("实现", "流程", "分配任务", "进行"))
        if process:
            add(pool, seen, document, block, f"{topic}描述了什么实现或处理过程？", process, "Process", process)
        numeric = numeric_sentence(text)
        if numeric:
            add(pool, seen, document, block, f"{topic}中描述了什么数量、时长或比例要求？", numeric, "Numeric constraint", numeric)


def balanced_sample(candidates, project_id, limit):
    by_type_doc = defaultdict(lambda: defaultdict(list))
    for item in candidates:
        by_type_doc[item["question_type"]][item["source_document"]].append(item)

    output = []
    selected_questions = set()
    doc_counts = Counter()
    max_per_doc = max(6, limit // 3)
    for question_type, quota in TYPE_QUOTAS.items():
        docs = sorted(by_type_doc[question_type])
        offsets = Counter()
        for _ in range(min(quota, limit - len(output))):
            eligible = [
                doc
                for doc in docs
                if offsets[doc] < len(by_type_doc[question_type][doc]) and doc_counts[doc] < max_per_doc
            ]
            if not eligible:
                break
            doc = min(eligible, key=lambda name: (doc_counts[name], offsets[name], name))
            item = by_type_doc[question_type][doc][offsets[doc]]
            offsets[doc] += 1
            if item["question"] in selected_questions:
                continue
            selected_questions.add(item["question"])
            doc_counts[doc] += 1
            output.append(item)

    for item in candidates:
        if len(output) >= limit:
            break
        type_count = sum(selected["question_type"] == item["question_type"] for selected in output)
        type_limit = TYPE_QUOTAS.get(item["question_type"], 4) + 1
        if (
            item["question"] in selected_questions
            or doc_counts[item["source_document"]] >= max_per_doc
            or type_count >= type_limit
        ):
            continue
        selected_questions.add(item["question"])
        doc_counts[item["source_document"]] += 1
        output.append(item)

    for index, item in enumerate(output, 1):
        item.update(
            {
                "qa_id": f"QA{index:03d}",
                "project_id": project_id,
                "difficulty": "待人工标注",
                "review_status": "DRAFT_NOT_APPROVED",
                "review_notes": "",
            }
        )
    return output


def questions(documents, project_id, limit=36):
    candidates = []
    seen = set()
    for document in documents:
        if document["document_type"] == "Meeting":
            meeting_candidates(document, candidates, seen)
        if document["document_type"] == "API":
            api_candidates(document, candidates, seen)
        if document["document_type"] == "DB":
            database_candidates(document, candidates, seen)
        if document["document_type"] in {"Detailed Design", "SRS", "Research Report", "Technical Agreement", "Proposal"}:
            prose_candidates(document, candidates, seen)
    return balanced_sample(candidates, project_id, limit)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--project-id", required=True)
    parser.add_argument("--workbench-dir", required=True)
    parser.add_argument("--qa-limit", type=int, default=36)
    args = parser.parse_args()
    output_dir = Path(args.workbench_dir)
    documents = json.loads((output_dir / "corpus.json").read_text(encoding="utf-8"))
    candidates = make_candidates(documents)
    qa = questions(documents, args.project_id, args.qa_limit)
    (output_dir / "candidate_breakpoints.json").write_text(json.dumps(candidates, ensure_ascii=False, indent=2), encoding="utf-8")
    (output_dir / "qa_review_set.json").write_text(json.dumps(qa, ensure_ascii=False, indent=2), encoding="utf-8")
    print(f"candidate_breakpoints={len(candidates)}")
    print(f"qa_items={len(qa)}")
    print("qa_types=" + json.dumps(Counter(item["question_type"] for item in qa), ensure_ascii=False))
    print("qa_documents=" + json.dumps(Counter(item["source_document"] for item in qa), ensure_ascii=False))


if __name__ == "__main__":
    main()
