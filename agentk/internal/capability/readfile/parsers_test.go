package readfile

import (
	"archive/zip"
	"bytes"
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestDetectAndParsePlainFormats(t *testing.T) {
	if got, err := detectFormat("auto", "notes.MD", "application/octet-stream", []byte("# hi")); err != nil || got != "md" {
		t.Fatalf("detect markdown = %q, %v", got, err)
	}
	if got, err := detectFormat("", "download", "application/pdf", []byte("x")); err != nil || got != "pdf" {
		t.Fatalf("detect PDF MIME = %q, %v", got, err)
	}
	if got, err := detectFormat("", "legacy.DOC", "application/octet-stream", []byte("x")); err != nil || got != "doc" {
		t.Fatalf("detect DOC extension = %q, %v", got, err)
	}
	if got, err := detectFormat("", "download", "application/vnd.ms-excel", []byte("x")); err != nil || got != "xls" {
		t.Fatalf("detect XLS MIME = %q, %v", got, err)
	}
	doc, err := parseCSV([]byte("name,note\nAlice,\"a|b\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc.content, "| name | note |") ||
		!strings.Contains(doc.content, "| Alice | a\\|b |") {
		t.Fatalf("CSV markdown = %q", doc.content)
	}
}

func TestLegacyParsersRejectMalformedFiles(t *testing.T) {
	if _, err := parseDOC([]byte("not a DOC")); err == nil {
		t.Fatal("malformed DOC must fail")
	}
	if _, err := parseXLS([]byte("not an XLS"), ""); err == nil {
		t.Fatal("malformed XLS must fail")
	}
}

func TestParseDOCXPreservesParagraphAndTableOrder(t *testing.T) {
	data := zipFixture(t, map[string]string{
		"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`,
		"word/document.xml": `<?xml version="1.0" encoding="UTF-8"?>
			<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
			  <w:body>
			    <w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>Report</w:t></w:r></w:p>
			    <w:tbl>
			      <w:tr><w:tc><w:p><w:r><w:t>Name</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>Value</w:t></w:r></w:p></w:tc></w:tr>
			      <w:tr><w:tc><w:p><w:r><w:t>A</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>10</w:t></w:r></w:p></w:tc></w:tr>
			    </w:tbl>
			    <w:p><w:r><w:t>Done</w:t></w:r></w:p>
			  </w:body>
			</w:document>`,
	})
	doc, err := parseDOCX(data)
	if err != nil {
		t.Fatal(err)
	}
	wantParts := []string{"# Report", "| Name | Value |", "| A | 10 |", "Done"}
	last := -1
	for _, part := range wantParts {
		at := strings.Index(doc.content, part)
		if at <= last {
			t.Fatalf("DOCX content order/content mismatch for %q: %s", part, doc.content)
		}
		last = at
	}
	if doc.metadata["paragraphs"] != 2 || doc.metadata["tables"] != 1 {
		t.Fatalf("DOCX metadata = %#v", doc.metadata)
	}
}

func TestParseXLSXSelectsSheet(t *testing.T) {
	book := excelize.NewFile()
	defaultSheet := book.GetSheetName(0)
	if err := book.SetCellValue(defaultSheet, "A1", "ignored"); err != nil {
		t.Fatal(err)
	}
	if _, err := book.NewSheet("Data"); err != nil {
		t.Fatal(err)
	}
	if err := book.SetSheetRow("Data", "A1", &[]any{"name", "value"}); err != nil {
		t.Fatal(err)
	}
	if err := book.SetSheetRow("Data", "A2", &[]any{"A", 10}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := book.Write(&buf); err != nil {
		t.Fatal(err)
	}
	_ = book.Close()

	doc, err := parseXLSX(buf.Bytes(), "Data")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc.content, "| name | value |") || !strings.Contains(doc.content, "| A | 10 |") {
		t.Fatalf("XLSX content = %q", doc.content)
	}
	if doc.metadata["sheet"] != "Data" {
		t.Fatalf("XLSX metadata = %#v", doc.metadata)
	}
}

func TestParsePageSelection(t *testing.T) {
	pages, err := parsePageSelection("4,1-2,2", 5)
	if err != nil {
		t.Fatal(err)
	}
	if got := ints(pages); got != "1,2,4" {
		t.Fatalf("pages = %s", got)
	}
	if _, err := parsePageSelection("1-11", 20); err == nil {
		t.Fatal("more than ten pages must fail")
	}
	if _, err := parsePageSelection("0", 5); err == nil {
		t.Fatal("page zero must fail")
	}
}

func TestContentWindowUsesRunes(t *testing.T) {
	content, total, next := contentWindow("甲乙丙丁", 1, 2)
	if content != "乙丙" || total != 4 || next != 3 {
		t.Fatalf("window = %q, %d, %d", content, total, next)
	}
}

func TestPDFTextQualityAndSanitization(t *testing.T) {
	if got := sanitizeExtractedText("hello\x00world\n"); got != "helloworld\n" {
		t.Fatalf("sanitized text = %q", got)
	}
	var fragmented strings.Builder
	for i := 0; i < 30; i++ {
		fragmented.WriteString("字\n")
	}
	if usefulText(fragmented.String()) {
		t.Fatal("one-character-per-line extraction must use vision fallback")
	}
	if usefulText(strings.Repeat("ÎøÏ¤ÐÝ§Á", 10)) {
		t.Fatal("missing-ToUnicode mojibake must use vision fallback")
	}
	if !usefulText(strings.Repeat("这是正常的一行文本内容，用于验证数字 PDF 的提取质量。\n", 5)) {
		t.Fatal("normal extracted text must remain useful")
	}
}

func TestPDFMetadataOffersNextPageRange(t *testing.T) {
	metadata := pdfMetadata(37, []int{1, 2, 3}, "text")
	if metadata["hasMorePages"] != true || metadata["nextPages"] != "4-13" {
		t.Fatalf("PDF metadata = %#v", metadata)
	}
	metadata = pdfMetadata(3, []int{3}, "vision")
	if metadata["hasMorePages"] != false {
		t.Fatalf("last-page metadata = %#v", metadata)
	}
}

func TestPDFVisionRejectsCharacterOffset(t *testing.T) {
	_, err := (&Activity{}).parsePDF(context.Background(), nil, ActivityInput{
		Mode: "vision", Offset: 1,
	})
	if err == nil || !strings.Contains(err.Error(), "paginate with pages") {
		t.Fatalf("err = %v", err)
	}
}

func zipFixture(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func ints(values []int) string {
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = strconv.Itoa(value)
	}
	return strings.Join(parts, ",")
}
