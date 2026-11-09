package readfile

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/xml"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	legacydoc "github.com/bytexlee/doc"
	pdf "github.com/ledongthuc/pdf"
	legacyxls "github.com/nkiri/xls"
	"github.com/richardlehane/mscfb"
	"github.com/xuri/excelize/v2"
)

const (
	maxArchiveEntries = 2048
	maxExpandedBytes  = 128 << 20
	maxTableRows      = 5000
	maxPDFPages       = 10
)

type parsedDocument struct {
	content  string
	metadata map[string]any
}

func detectFormat(explicit, name, contentType string, data []byte) (string, error) {
	requested := strings.ToLower(strings.TrimSpace(explicit))
	if requested != "" && requested != "auto" {
		switch requested {
		case "pdf", "doc", "docx", "xls", "xlsx", "csv", "md", "txt":
			return requested, nil
		default:
			return "", fmt.Errorf("unsupported format %q", requested)
		}
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".pdf":
		return "pdf", nil
	case ".doc":
		return "doc", nil
	case ".docx":
		return "docx", nil
	case ".xls":
		return "xls", nil
	case ".xlsx":
		return "xlsx", nil
	case ".csv":
		return "csv", nil
	case ".md", ".markdown":
		return "md", nil
	case ".txt", ".text":
		return "txt", nil
	}
	declared, _, _ := mime.ParseMediaType(contentType)
	switch declared {
	case "application/pdf":
		return "pdf", nil
	case "application/msword":
		return "doc", nil
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return "docx", nil
	case "application/vnd.ms-excel":
		return "xls", nil
	case "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return "xlsx", nil
	case "text/csv":
		return "csv", nil
	case "text/markdown":
		return "md", nil
	case "text/plain":
		return "txt", nil
	}
	if bytes.HasPrefix(data, []byte("%PDF-")) {
		return "pdf", nil
	}
	if kind := openXMLKind(data); kind != "" {
		return kind, nil
	}
	if kind := compoundOfficeKind(data); kind != "" {
		return kind, nil
	}
	if utf8.Valid(bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})) {
		return "txt", nil
	}
	return "", fmt.Errorf("cannot determine a supported file format (name %q, content type %q, sniffed %q)",
		name, contentType, http.DetectContentType(data))
}

func compoundOfficeKind(data []byte) string {
	reader, err := mscfb.New(bytes.NewReader(data))
	if err != nil {
		return ""
	}
	var word, workbook bool
	for _, file := range reader.File {
		switch file.Name {
		case "WordDocument":
			word = true
		case "Workbook", "Book":
			workbook = true
		}
	}
	switch {
	case word:
		return "doc"
	case workbook:
		return "xls"
	default:
		return ""
	}
}

func openXMLKind(data []byte) string {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return ""
	}
	var document, workbook bool
	for _, f := range zr.File {
		switch f.Name {
		case "word/document.xml":
			document = true
		case "xl/workbook.xml":
			workbook = true
		}
	}
	switch {
	case document:
		return "docx"
	case workbook:
		return "xlsx"
	default:
		return ""
	}
}

func parsePlainText(data []byte) (parsedDocument, error) {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	if !utf8.Valid(data) {
		return parsedDocument{}, fmt.Errorf("text is not valid UTF-8")
	}
	return parsedDocument{content: string(data)}, nil
}

func parseCSV(data []byte) (parsedDocument, error) {
	r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})))
	r.FieldsPerRecord = -1
	r.ReuseRecord = true
	var b strings.Builder
	rows := 0
	truncated := false
	for {
		record, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return parsedDocument{}, err
		}
		if rows >= maxTableRows {
			truncated = true
			break
		}
		writeMarkdownRow(&b, record)
		if rows == 0 {
			separator := make([]string, len(record))
			for i := range separator {
				separator[i] = "---"
			}
			writeMarkdownRow(&b, separator)
		}
		rows++
	}
	return parsedDocument{
		content: strings.TrimSpace(b.String()),
		metadata: map[string]any{
			"rows": rows, "truncated": truncated,
		},
	}, nil
}

func parseXLSX(data []byte, requestedSheet string) (parsedDocument, error) {
	if err := validateArchive(data); err != nil {
		return parsedDocument{}, err
	}
	book, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return parsedDocument{}, err
	}
	defer func() { _ = book.Close() }()
	sheets := book.GetSheetList()
	if len(sheets) == 0 {
		return parsedDocument{}, fmt.Errorf("workbook contains no worksheets")
	}
	sheet := strings.TrimSpace(requestedSheet)
	if sheet == "" {
		sheet = sheets[0]
	}
	found := false
	for _, candidate := range sheets {
		if candidate == sheet {
			found = true
			break
		}
	}
	if !found {
		return parsedDocument{}, fmt.Errorf("sheet %q does not exist; available sheets: %s", sheet, strings.Join(sheets, ", "))
	}
	rows, err := book.Rows(sheet)
	if err != nil {
		return parsedDocument{}, err
	}
	defer func() { _ = rows.Close() }()
	var b strings.Builder
	count := 0
	truncated := false
	for rows.Next() {
		if count >= maxTableRows {
			truncated = true
			break
		}
		columns, err := rows.Columns()
		if err != nil {
			return parsedDocument{}, err
		}
		writeMarkdownRow(&b, columns)
		if count == 0 {
			separator := make([]string, len(columns))
			for i := range separator {
				separator[i] = "---"
			}
			writeMarkdownRow(&b, separator)
		}
		count++
	}
	if err := rows.Error(); err != nil {
		return parsedDocument{}, err
	}
	return parsedDocument{
		content: strings.TrimSpace(b.String()),
		metadata: map[string]any{
			"sheets": sheets, "sheet": sheet, "rows": count, "truncated": truncated,
		},
	}, nil
}

func parseXLS(data []byte, requestedSheet string) (parsed parsedDocument, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			parsed = parsedDocument{}
			err = fmt.Errorf("legacy XLS parser failed: %v", recovered)
		}
	}()
	book, err := legacyxls.Read(bytes.NewReader(data))
	if err != nil {
		return parsedDocument{}, err
	}
	sheets := book.SheetList()
	if len(sheets) == 0 {
		return parsedDocument{}, fmt.Errorf("workbook contains no worksheets")
	}
	requestedSheet = strings.TrimSpace(requestedSheet)
	var sheet *legacyxls.Sheet
	if requestedSheet == "" {
		sheet = book.Sheet(0)
	} else {
		sheet = book.SheetByName(requestedSheet)
	}
	if sheet == nil {
		return parsedDocument{}, fmt.Errorf("sheet %q does not exist; available sheets: %s",
			requestedSheet, strings.Join(sheets, ", "))
	}

	var b strings.Builder
	rows := 0
	truncated := false
	for rowIndex := 0; rowIndex < sheet.RowCount(); rowIndex++ {
		if rows >= maxTableRows {
			truncated = true
			break
		}
		row := sheet.Row(rowIndex)
		if row == nil || row.CellCount() == 0 {
			continue
		}
		columnCount := min(row.CellCount(), 256)
		cells := make([]string, columnCount)
		for column := range columnCount {
			if cell := row.Cell(column); cell != nil {
				cells[column] = cell.Value()
			}
		}
		writeMarkdownRow(&b, cells)
		if rows == 0 {
			separator := make([]string, columnCount)
			for i := range separator {
				separator[i] = "---"
			}
			writeMarkdownRow(&b, separator)
		}
		rows++
	}
	return parsedDocument{
		content: strings.TrimSpace(b.String()),
		metadata: map[string]any{
			"sheets": sheets, "sheet": sheet.Name(), "rows": rows, "truncated": truncated,
		},
	}, nil
}

func writeMarkdownRow(b *strings.Builder, cells []string) {
	b.WriteString("| ")
	for _, cell := range cells {
		cell = strings.ReplaceAll(cell, "\\", "\\\\")
		cell = strings.ReplaceAll(cell, "|", "\\|")
		cell = strings.ReplaceAll(cell, "\r\n", "<br>")
		cell = strings.ReplaceAll(cell, "\n", "<br>")
		b.WriteString(cell)
		b.WriteString(" | ")
	}
	b.WriteByte('\n')
}

func parseDOCX(data []byte) (parsedDocument, error) {
	if err := validateArchive(data); err != nil {
		return parsedDocument{}, err
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return parsedDocument{}, err
	}
	var document *zip.File
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			document = f
			break
		}
	}
	if document == nil {
		return parsedDocument{}, fmt.Errorf("DOCX is missing word/document.xml")
	}
	r, err := document.Open()
	if err != nil {
		return parsedDocument{}, err
	}
	defer func() { _ = r.Close() }()
	decoder := xml.NewDecoder(io.LimitReader(r, maxExpandedBytes+1))
	var b strings.Builder
	paragraphs, tables := 0, 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return parsedDocument{}, err
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "p":
			text, style, err := readDOCXParagraph(decoder, start)
			if err != nil {
				return parsedDocument{}, err
			}
			if strings.TrimSpace(text) != "" {
				writeDOCXParagraph(&b, text, style)
				paragraphs++
			}
		case "tbl":
			table, err := readDOCXTable(decoder, start)
			if err != nil {
				return parsedDocument{}, err
			}
			if table != "" {
				b.WriteString(table)
				b.WriteString("\n\n")
				tables++
			}
		}
	}
	return parsedDocument{
		content:  strings.TrimSpace(b.String()),
		metadata: map[string]any{"paragraphs": paragraphs, "tables": tables},
	}, nil
}

func parseDOC(data []byte) (parsed parsedDocument, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			parsed = parsedDocument{}
			err = fmt.Errorf("legacy DOC parser failed: %v", recovered)
		}
	}()
	textReader, err := legacydoc.ParseDoc(bytes.NewReader(data))
	if err != nil {
		return parsedDocument{}, err
	}
	text, err := io.ReadAll(io.LimitReader(textReader, maxExpandedBytes+1))
	if err != nil {
		return parsedDocument{}, err
	}
	if len(text) > maxExpandedBytes {
		return parsedDocument{}, fmt.Errorf("DOC text exceeds %d bytes", maxExpandedBytes)
	}
	content := strings.TrimSpace(sanitizeExtractedText(string(text)))
	if content == "" {
		return parsedDocument{}, fmt.Errorf("DOC contains no extractable text")
	}
	return parsedDocument{
		content:  content,
		metadata: map[string]any{"method": "legacy-doc-text"},
	}, nil
}

func readDOCXParagraph(decoder *xml.Decoder, start xml.StartElement) (string, string, error) {
	var b strings.Builder
	style := ""
	for {
		token, err := decoder.Token()
		if err != nil {
			return "", "", err
		}
		switch current := token.(type) {
		case xml.StartElement:
			switch current.Name.Local {
			case "t":
				var text string
				if err := decoder.DecodeElement(&text, &current); err != nil {
					return "", "", err
				}
				b.WriteString(text)
			case "tab":
				b.WriteByte('\t')
			case "br", "cr":
				b.WriteByte('\n')
			case "pStyle":
				for _, attr := range current.Attr {
					if attr.Name.Local == "val" {
						style = attr.Value
					}
				}
			}
		case xml.EndElement:
			if current.Name == start.Name {
				return b.String(), style, nil
			}
		}
	}
}

func writeDOCXParagraph(b *strings.Builder, text, style string) {
	lower := strings.ToLower(style)
	if strings.HasPrefix(lower, "heading") {
		level, _ := strconv.Atoi(strings.TrimPrefix(lower, "heading"))
		if level < 1 || level > 6 {
			level = 2
		}
		b.WriteString(strings.Repeat("#", level))
		b.WriteByte(' ')
	}
	b.WriteString(strings.TrimSpace(text))
	b.WriteString("\n\n")
}

func readDOCXTable(decoder *xml.Decoder, start xml.StartElement) (string, error) {
	var rows [][]string
	var row []string
	for {
		token, err := decoder.Token()
		if err != nil {
			return "", err
		}
		switch current := token.(type) {
		case xml.StartElement:
			switch current.Name.Local {
			case "tr":
				row = nil
			case "tc":
				text, err := readDOCXCell(decoder, current)
				if err != nil {
					return "", err
				}
				row = append(row, text)
			}
		case xml.EndElement:
			switch current.Name.Local {
			case "tr":
				if len(row) > 0 {
					rows = append(rows, row)
				}
			case start.Name.Local:
				var b strings.Builder
				for i, cells := range rows {
					writeMarkdownRow(&b, cells)
					if i == 0 {
						separator := make([]string, len(cells))
						for j := range separator {
							separator[j] = "---"
						}
						writeMarkdownRow(&b, separator)
					}
				}
				return strings.TrimSpace(b.String()), nil
			}
		}
	}
}

func readDOCXCell(decoder *xml.Decoder, start xml.StartElement) (string, error) {
	var parts []string
	var current strings.Builder
	for {
		token, err := decoder.Token()
		if err != nil {
			return "", err
		}
		switch item := token.(type) {
		case xml.StartElement:
			switch item.Name.Local {
			case "t":
				var text string
				if err := decoder.DecodeElement(&text, &item); err != nil {
					return "", err
				}
				current.WriteString(text)
			case "tab":
				current.WriteByte('\t')
			case "br", "cr":
				current.WriteByte('\n')
			}
		case xml.EndElement:
			switch item.Name.Local {
			case "p":
				if text := strings.TrimSpace(current.String()); text != "" {
					parts = append(parts, text)
				}
				current.Reset()
			case start.Name.Local:
				if text := strings.TrimSpace(current.String()); text != "" {
					parts = append(parts, text)
				}
				return strings.Join(parts, "<br>"), nil
			}
		}
	}
}

func validateArchive(data []byte) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	if len(zr.File) > maxArchiveEntries {
		return fmt.Errorf("archive contains too many entries (%d > %d)", len(zr.File), maxArchiveEntries)
	}
	var expanded uint64
	for _, f := range zr.File {
		expanded += f.UncompressedSize64
		if expanded > maxExpandedBytes {
			return fmt.Errorf("archive expands beyond %d bytes", maxExpandedBytes)
		}
	}
	return nil
}

func parsePDFText(data []byte, rawPages string) (string, int, []int, bool, error) {
	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", 0, nil, false, err
	}
	pageCount := reader.NumPage()
	pages, err := parsePageSelection(rawPages, pageCount)
	if err != nil {
		return "", pageCount, nil, false, err
	}
	var b strings.Builder
	allUseful := true
	for _, pageNumber := range pages {
		text, err := reader.Page(pageNumber).GetPlainText(nil)
		if err != nil {
			return "", pageCount, pages, false, fmt.Errorf("page %d: %w", pageNumber, err)
		}
		text = sanitizeExtractedText(text)
		if !usefulText(text) {
			allUseful = false
		}
		fmt.Fprintf(&b, "## Page %d\n\n%s\n\n", pageNumber, strings.TrimSpace(text))
	}
	return strings.TrimSpace(b.String()), pageCount, pages, allUseful, nil
}

func sanitizeExtractedText(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return -1
		}
		return r
	}, text)
}

func parsePageSelection(raw string, pageCount int) ([]int, error) {
	if pageCount <= 0 {
		return nil, fmt.Errorf("PDF contains no pages")
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		end := min(pageCount, maxPDFPages)
		pages := make([]int, end)
		for i := range pages {
			pages[i] = i + 1
		}
		return pages, nil
	}
	var pages []int
	seen := map[int]bool{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("invalid pages %q", raw)
		}
		start, end := 0, 0
		if left, right, ok := strings.Cut(part, "-"); ok {
			var err error
			if start, err = strconv.Atoi(strings.TrimSpace(left)); err != nil {
				return nil, fmt.Errorf("invalid page range %q", part)
			}
			if end, err = strconv.Atoi(strings.TrimSpace(right)); err != nil {
				return nil, fmt.Errorf("invalid page range %q", part)
			}
		} else {
			var err error
			if start, err = strconv.Atoi(part); err != nil {
				return nil, fmt.Errorf("invalid page %q", part)
			}
			end = start
		}
		if start < 1 || end < start || end > pageCount {
			return nil, fmt.Errorf("page range %q is outside 1-%d", part, pageCount)
		}
		for page := start; page <= end; page++ {
			if !seen[page] {
				seen[page] = true
				pages = append(pages, page)
				if len(pages) > maxPDFPages {
					return nil, fmt.Errorf("at most %d PDF pages may be read per call", maxPDFPages)
				}
			}
		}
	}
	sort.Ints(pages)
	return pages, nil
}
