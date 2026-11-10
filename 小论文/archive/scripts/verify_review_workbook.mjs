import fs from "node:fs/promises";
import { FileBlob, SpreadsheetFile } from "@oai/artifact-tool";

const input = "outputs/P01_镔鑫智慧安全二期_数据标注Review.xlsx";
const previewDir = "workbench/P01/previews";
const workbook = await SpreadsheetFile.importXlsx(await FileBlob.load(input));

const stats = await workbook.inspect({
  kind: "table",
  range: "文档统计!A1:L11",
  include: "values,formulas",
  tableMaxRows: 12,
  tableMaxCols: 12,
});
const qa = await workbook.inspect({
  kind: "table",
  range: "问答标注!A4:M10",
  include: "values,formulas",
  tableMaxRows: 8,
  tableMaxCols: 13,
});
const errors = await workbook.inspect({
  kind: "match",
  searchTerm: "#REF!|#DIV/0!|#VALUE!|#NAME\\?|#N/A",
  options: { useRegex: true, maxResults: 100 },
  summary: "formula error scan",
});

await fs.mkdir(previewDir, { recursive: true });
for (const [name, range] of [
  ["README", "A1:H14"],
  ["文档统计", "A1:L11"],
  ["候选断点", "A1:O10"],
  ["问答标注", "A1:M10"],
]) {
  const image = await workbook.render({ sheetName: name, range, scale: 0.8 });
  const bytes = typeof image.bytes === "function" ? await image.bytes() : new Uint8Array(await image.arrayBuffer());
  await fs.writeFile(`${previewDir}/${name}.png`, bytes);
}

console.log(stats.ndjson);
console.log(qa.ndjson);
console.log(errors.ndjson);
