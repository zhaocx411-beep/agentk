import argparse
import json
import re
import sys
from pathlib import Path

from docx import Document
from docx.table import Table
from docx.text.paragraph import Paragraph
from openpyxl import load_workbook


PROJECT_ID = "P01"


def clean(value):
    if value is None:
        return ""
    return re.sub(r"\s+", " ", str(value)).strip()


def approx_tokens(text):
    chinese = len(re.findall(r"[\u4e00-\u9fff]", text))
    latin_or_number = len(re.findall(r"[A-Za-z0-9_]+", text))
    punctuation = len(re.findall(r"[^\w\s\u4e00-\u9fff]", text))
    return chinese + latin_or_number + punctuation


def iter_docx_blocks(document):
    body = document.element.body
    paragraph_lookup = {p._p: p for p in document.paragraphs}
    table_lookup = {t._tbl: t for t in document.tables}
    for child in body.iterchildren():
        if child in paragraph_lookup:
            yield "paragraph", paragraph_lookup[child]
        elif child in table_lookup:
            yield "table", table_lookup[child]


def is_heading(paragraph):
    style = paragraph.style.name if paragraph.style else ""
    text = clean(paragraph.text)
    numbered_heading = bool(
        len(text) <= 60
        and re.match(r"^\d+(?:\.\d+)*\.?\s*[A-Za-z\u4e00-\u9fff]", text)
        and not re.match(r"^\d+[、）)]", text)
    )
    return style.startswith("Heading") or style.startswith("标题") or numbered_heading


def heading_level(paragraph):
    style = paragraph.style.name if paragraph.style else ""
    match = re.search(r"(\d+)", style)
    if match:
        return int(match.group(1))
    text = clean(paragraph.text)
    number = re.match(r"^(\d+(?:\.\d+)*)", text)
    return min(6, number.group(1).count(".") + 1) if number else 1


def extract_docx(path):
    document = Document(path)
    blocks = []
    heading_path = []
    paragraph_count = 0
    heading_count = 0
    table_count = 0
    table_rows = 0
    all_text = []

    for block_type, block in iter_docx_blocks(document):
        if block_type == "paragraph":
            text = clean(block.text)
            if not text:
                continue
            paragraph_count += 1
            all_text.append(text)
            heading = is_heading(block)
            if heading:
                heading_count += 1
                level = heading_level(block)
                heading_path = heading_path[: level - 1]
                heading_path.append(text)
            blocks.append(
                {
                    "block_id": f"{path.stem}:B{len(blocks)+1:04d}",
                    "block_index": len(blocks) + 1,
                    "block_type": "heading" if heading else "paragraph",
                    "heading_path": " > ".join(heading_path),
                    "text": text,
                    "char_count": len(text),
                }
            )
        else:
            rows = []
            for row in block.rows:
                cells = [clean(cell.text) for cell in row.cells]
                if any(cells):
                    rows.append(cells)
                    all_text.extend(cells)
            if not rows:
                continue
            table_count += 1
            table_rows += len(rows)
            preview = " | ".join(rows[0])
            if len(rows) > 1:
                preview += " || " + " | ".join(rows[1])
            blocks.append(
                {
                    "block_id": f"{path.stem}:B{len(blocks)+1:04d}",
                    "block_index": len(blocks) + 1,
                    "block_type": "table",
                    "heading_path": " > ".join(heading_path),
                    "table_index": table_count,
                    "row_count": len(rows),
                    "column_count": max(len(r) for r in rows),
                    "rows": rows,
                    "text": preview,
                    "char_count": sum(len(cell) for row in rows for cell in row),
                }
            )

    corpus_text = "\n".join(all_text)
    return {
        "project_id": PROJECT_ID,
        "document_type": classify_document(path.name),
        "file_name": path.name,
        "file_path": str(path),
        "paragraphs": paragraph_count,
        "tables": table_count,
        "table_rows": table_rows,
        "headings": heading_count,
        "characters": len(corpus_text),
        "tokens_approx": approx_tokens(corpus_text),
        "token_definition": "Approx.: Chinese characters + Latin/number terms + punctuation",
        "confidentiality": "待确认/待脱敏",
        "blocks": blocks,
    }


def classify_document(file_name):
    if "纪要" in file_name:
        return "Meeting"
    if "接口" in file_name:
        return "API"
    if "数据库" in file_name:
        return "DB"
    if "需求规格" in file_name:
        return "SRS"
    if "详细设计" in file_name:
        return "Detailed Design"
    if "技术协议" in file_name:
        return "Technical Agreement"
    if "调研报告" in file_name:
        return "Research Report"
    if "方案建议书" in file_name:
        return "Proposal"
    return "Other"


def extract_xlsx(path):
    workbook = load_workbook(path, read_only=True, data_only=True)
    blocks = []
    nonempty_rows = 0
    all_text = []

    def table_name(row):
        for index, cell in enumerate(row[:-1]):
            if cell == "表名":
                for value in row[index + 1 :]:
                    if value:
                        return value
        return ""

    def append_block(sheet_title, rows, name=""):
        if not rows:
            return
        preview = " | ".join(rows[0])
        if len(rows) > 1:
            preview += " || " + " | ".join(rows[1])
        block_index = len(blocks) + 1
        blocks.append(
            {
                "block_id": f"{path.stem}:B{block_index:04d}",
                "block_index": block_index,
                "block_type": "table" if name else "worksheet",
                "heading_path": f"{sheet_title} > {name}" if name else sheet_title,
                "sheet_name": sheet_title,
                "table_name": name,
                "row_count": len(rows),
                "column_count": max(len(r) for r in rows),
                "rows": rows,
                "text": preview,
                "char_count": sum(len(cell) for row in rows for cell in row),
            }
        )

    for sheet in workbook.worksheets:
        segments = []
        current_rows = []
        current_name = ""
        for row in sheet.iter_rows(values_only=True):
            cells = [clean(value) for value in row]
            if any(cells):
                nonempty_rows += 1
                all_text.extend(cells)
                name = table_name(cells)
                if name and current_rows:
                    segments.append((current_name, current_rows))
                    current_rows = []
                if name:
                    current_name = name
                current_rows.append(cells)
        if current_rows:
            segments.append((current_name, current_rows))
        for name, rows in segments:
            append_block(sheet.title, rows, name)
    corpus_text = "\n".join(all_text)
    return {
        "project_id": PROJECT_ID,
        "document_type": classify_document(path.name),
        "file_name": path.name,
        "file_path": str(path),
        "paragraphs": 0,
        "tables": len(blocks),
        "table_rows": nonempty_rows,
        "headings": len(blocks),
        "characters": len(corpus_text),
        "tokens_approx": approx_tokens(corpus_text),
        "token_definition": "Approx.: Chinese characters + Latin/number terms + punctuation",
        "confidentiality": "待确认/待脱敏",
        "blocks": blocks,
    }


def main():
    global PROJECT_ID
    parser = argparse.ArgumentParser()
    parser.add_argument("--project-id", default="P01")
    parser.add_argument("--project-dir", default="镔鑫智慧安全二期")
    parser.add_argument("--output-dir")
    args = parser.parse_args()
    PROJECT_ID = args.project_id
    project_dir = Path(args.project_dir)
    output_dir = Path(args.output_dir or f"workbench/{PROJECT_ID}")
    output_dir.mkdir(parents=True, exist_ok=True)
    documents = []
    for path in sorted(project_dir.iterdir()):
        if path.suffix.lower() == ".docx":
            documents.append(extract_docx(path))
        elif path.suffix.lower() == ".xlsx":
            documents.append(extract_xlsx(path))

    with (output_dir / "corpus.json").open("w", encoding="utf-8") as handle:
        json.dump(documents, handle, ensure_ascii=False, indent=2)

    stats = [{key: value for key, value in doc.items() if key != "blocks"} for doc in documents]
    with (output_dir / "document_statistics.json").open("w", encoding="utf-8") as handle:
        json.dump(stats, handle, ensure_ascii=False, indent=2)

    for doc in documents:
        safe_name = re.sub(r"[^\w.-]+", "_", Path(doc["file_name"]).stem)
        with (output_dir / f"{safe_name}.txt").open("w", encoding="utf-8") as handle:
            for block in doc["blocks"]:
                handle.write(
                    f'[{block["block_id"]}] [{block["block_type"]}] '
                    f'[{block.get("heading_path", "")}]\n{block["text"]}\n\n'
                )
    print(json.dumps(stats, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    sys.exit(main())
