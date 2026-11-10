import fs from "node:fs/promises";
import { SpreadsheetFile, Workbook } from "@oai/artifact-tool";

const base = "workbench/P01";
const outputPath = "outputs/P01_镔鑫智慧安全二期_数据标注Review.xlsx";
const stats = JSON.parse(await fs.readFile(`${base}/document_statistics.json`, "utf8"));
const breakpoints = JSON.parse(await fs.readFile(`${base}/candidate_breakpoints.json`, "utf8"));
const questions = JSON.parse(await fs.readFile(`${base}/qa_review_set.json`, "utf8"));

const wb = Workbook.create();

const colors = {
  navy: "#17365D",
  blue: "#D9EAF7",
  paleBlue: "#EEF5FB",
  orange: "#FCE4D6",
  green: "#E2F0D9",
  gray: "#F2F2F2",
  white: "#FFFFFF",
  red: "#F4CCCC",
  text: "#1F2937",
};

function title(sheet, text, subtitle, width) {
  sheet.getRange(`A1:${width}1`).merge();
  sheet.getRange("A1").values = [[text]];
  sheet.getRange("A1").format = {
    fill: colors.navy,
    font: { bold: true, color: colors.white, size: 16 },
    rowHeight: 30,
    verticalAlignment: "center",
  };
  sheet.getRange(`A2:${width}2`).merge();
  sheet.getRange("A2").values = [[subtitle]];
  sheet.getRange("A2").format = {
    fill: colors.paleBlue,
    font: { color: colors.text, italic: true, size: 10 },
    rowHeight: 28,
    wrapText: true,
    verticalAlignment: "center",
  };
}

function header(sheet, range) {
  sheet.getRange(range).format = {
    fill: colors.navy,
    font: { bold: true, color: colors.white },
    rowHeight: 28,
    wrapText: true,
    verticalAlignment: "center",
  };
}

function body(sheet, range) {
  sheet.getRange(range).format = {
    font: { color: colors.text, size: 10 },
    wrapText: true,
    verticalAlignment: "top",
  };
}

const readme = wb.worksheets.add("README");
title(readme, "P01 镔鑫智慧安全二期：数据整理与人工 Review 工作簿", "自动抽取草案，不代表最终人工真值。请优先检查脱敏、候选断点标签和问答证据。", "H");
readme.getRange("A4:B12").values = [
  ["项目", "P01 / 镔鑫智慧安全二期"],
  ["源文件数", stats.length],
  ["候选断点数", breakpoints.length],
  ["问答草案数", questions.length],
  ["建议优先级 1", "确认所有文档是否可用于论文，并完成脱敏；特别注意接口地址、人员姓名、内部网络信息。"],
  ["建议优先级 2", "在“候选断点”表填写 human_label：BREAK 或 NO_BREAK，并将 review_status 改为“已确认”。"],
  ["建议优先级 3", "逐条核验“问答标注”的答案与证据，删除过于简单、重复或可能泄密的问题。"],
  ["Token 口径", "当前为近似值：中文字符 + 英文/数字词项 + 标点。正式实验应换成最终嵌入模型对应 tokenizer。"],
  ["数据用途", "本工作簿用于第一轮人工 review；正式实验前应导出机器可读 CSV/JSON，并保留版本号。"],
];
readme.getRange("A4:A12").format = { fill: colors.blue, font: { bold: true, color: colors.text }, verticalAlignment: "top" };
body(readme, "B4:B12");
readme.getRange("A14:H14").merge();
readme.getRange("A14").values = [["推荐 Review 顺序：先检查文档统计与保密状态，再标注断点，最后核验 QA。不要仅依据 suggested_label 直接作为金标准。"]];
readme.getRange("A14").format = { fill: colors.orange, font: { bold: true, color: colors.text }, wrapText: true, rowHeight: 34 };
readme.getRange("A:A").format.columnWidth = 22;
readme.getRange("B:B").format.columnWidth = 78;
readme.freezePanes.freezeRows(2);

const statsSheet = wb.worksheets.add("文档统计");
title(statsSheet, "文档统计", "每行对应一个源文件；confidentiality 当前均为“待确认/待脱敏”。", "L");
const statsHeaders = ["project_id", "document_type", "file_name", "paragraphs", "tables", "table_rows", "headings", "characters", "tokens_approx", "token_definition", "confidentiality", "review_notes"];
statsSheet.getRange("A4:L4").values = [statsHeaders];
header(statsSheet, "A4:L4");
statsSheet.getRange(`A5:L${stats.length + 4}`).values = stats.map((x) => [
  x.project_id, x.document_type, x.file_name, x.paragraphs, x.tables, x.table_rows,
  x.headings, x.characters, x.tokens_approx, x.token_definition, x.confidentiality, "",
]);
body(statsSheet, `A5:L${stats.length + 4}`);
statsSheet.getRange(`A5:L${stats.length + 4}`).format.rowHeight = 38;
statsSheet.getRange(`K5:K${stats.length + 4}`).format.fill = colors.orange;
statsSheet.getRange("A:A").format.columnWidth = 12;
statsSheet.getRange("B:B").format.columnWidth = 20;
statsSheet.getRange("C:C").format.columnWidth = 48;
statsSheet.getRange("D:I").format.columnWidth = 14;
statsSheet.getRange("J:J").format.columnWidth = 42;
statsSheet.getRange("K:K").format.columnWidth = 20;
statsSheet.getRange("L:L").format.columnWidth = 36;
statsSheet.freezePanes.freezeRows(4);

const bpSheet = wb.worksheets.add("候选断点");
title(bpSheet, "候选断点人工标注", "suggested_label 仅为自动预标注；请在 human_label 填写 BREAK / NO_BREAK，并记录判断理由。", "O");
const bpHeaders = [
  "candidate_id", "project_id", "source_document", "boundary_type", "left_unit_id", "right_unit_id",
  "left_section", "right_section", "left_evidence", "right_evidence", "suggested_label", "suggested_reason",
  "human_label", "review_status", "review_notes",
];
bpSheet.getRange("A4:O4").values = [bpHeaders];
header(bpSheet, "A4:O4");
bpSheet.getRange(`A5:O${breakpoints.length + 4}`).values = breakpoints.map((x) => bpHeaders.map((h) => x[h] ?? ""));
body(bpSheet, `A5:O${breakpoints.length + 4}`);
bpSheet.getRange(`A5:O${breakpoints.length + 4}`).format.rowHeight = 64;
bpSheet.getRange(`K5:L${breakpoints.length + 4}`).format.fill = colors.gray;
bpSheet.getRange(`M5:O${breakpoints.length + 4}`).format.fill = colors.orange;
bpSheet.getRange(`M5:M${breakpoints.length + 4}`).dataValidation = { rule: { type: "list", values: ["BREAK", "NO_BREAK", "UNCERTAIN"] } };
bpSheet.getRange(`N5:N${breakpoints.length + 4}`).dataValidation = { rule: { type: "list", values: ["待人工复核", "已确认", "需讨论", "删除"] } };
for (const [col, width] of Object.entries({ A: 12, B: 10, C: 42, D: 24, E: 34, F: 34, G: 42, H: 42, I: 60, J: 60, K: 18, L: 40, M: 18, N: 18, O: 42 })) {
  bpSheet.getRange(`${col}:${col}`).format.columnWidth = width;
}
bpSheet.freezePanes.freezeRows(4);
bpSheet.freezePanes.freezeColumns(4);

const qaSheet = wb.worksheets.add("问答标注");
title(qaSheet, "问答与金标准证据 Review", "32 条候选问答均带来源块编号；请重点检查答案是否唯一、证据是否充分、是否需要脱敏。", "M");
const qaHeaders = [
  "qa_id", "project_id", "question", "reference_answer", "source_document", "source_section",
  "gold_evidence_id", "gold_evidence_span", "question_type", "difficulty", "review_status", "review_notes", "keep_for_experiment",
];
qaSheet.getRange("A4:M4").values = [qaHeaders];
header(qaSheet, "A4:M4");
qaSheet.getRange(`A5:M${questions.length + 4}`).values = questions.map((x) => qaHeaders.map((h) => x[h] ?? ""));
body(qaSheet, `A5:M${questions.length + 4}`);
qaSheet.getRange(`A5:M${questions.length + 4}`).format.rowHeight = 52;
qaSheet.getRange(`G5:I${questions.length + 4}`).format.fill = colors.gray;
qaSheet.getRange(`J5:M${questions.length + 4}`).format.fill = colors.orange;
qaSheet.getRange(`J5:J${questions.length + 4}`).dataValidation = { rule: { type: "list", values: ["Easy", "Medium", "Hard", "待人工标注"] } };
qaSheet.getRange(`K5:K${questions.length + 4}`).dataValidation = { rule: { type: "list", values: ["待人工复核", "已确认", "需修改", "删除"] } };
qaSheet.getRange(`M5:M${questions.length + 4}`).dataValidation = { rule: { type: "list", values: ["YES", "NO", "待定"] } };
for (const [col, width] of Object.entries({ A: 11, B: 10, C: 48, D: 48, E: 44, F: 46, G: 42, H: 62, I: 22, J: 18, K: 18, L: 42, M: 20 })) {
  qaSheet.getRange(`${col}:${col}`).format.columnWidth = width;
}
qaSheet.freezePanes.freezeRows(4);
qaSheet.freezePanes.freezeColumns(2);

await fs.mkdir("outputs", { recursive: true });
const file = await SpreadsheetFile.exportXlsx(wb);
await file.save(outputPath);
console.log(outputPath);
