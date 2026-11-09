package render

import (
	"encoding/json"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/image/font/basicfont"
)

func validStyledSubtitlePayload() styledSubtitlePayload {
	return styledSubtitlePayload{
		Version: styledSubtitlePayloadVersion,
		Entries: []styledSubtitleEntry{{
			Start: 0.25,
			End:   2.5,
			Text:  "第一行\nSecond line",
			Style: styledSubtitleStyle{
				FontFamily:      "sans",
				FontSize:        32,
				TextColor:       "#FFFFFF",
				BackgroundColor: "#10203080",
				OutlineColor:    "#000000",
				OutlineWidth:    2,
				Bold:            true,
				Italic:          true,
				Align:           "center",
				X:               0.5,
				Y:               0.75,
				MaxWidth:        0.8,
				LineHeight:      1.2,
			},
		}},
	}
}

func marshalStyledPayload(t *testing.T, payload styledSubtitlePayload) []byte {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestParseStyledSubtitlePayloadV1(t *testing.T) {
	raw := marshalStyledPayload(t, validStyledSubtitlePayload())
	payload, err := parseStyledSubtitlePayload(raw)
	if err != nil {
		t.Fatalf("parseStyledSubtitlePayload() error = %v", err)
	}
	if payload.Version != 1 || len(payload.Entries) != 1 {
		t.Fatalf("unexpected payload: %#v", payload)
	}
	entry := payload.Entries[0]
	if entry.Text != "第一行\nSecond line" || entry.Style.FontFamily != "sans" || !entry.Style.Bold {
		t.Fatalf("unexpected entry: %#v", entry)
	}
}

func TestParseStyledSubtitlePayloadRejectsUnknownAndTrailingFields(t *testing.T) {
	base := `{"version":1,"entries":[{"start":0,"end":1,"text":"x","style":{"fontFamily":"sans","fontSize":20,"textColor":"#FFFFFF","backgroundColor":"","outlineColor":"#000000","outlineWidth":0,"bold":false,"italic":false,"align":"left","x":0,"y":0,"maxWidth":1,"lineHeight":1}}]}`
	for name, raw := range map[string]string{
		"top level": strings.Replace(base, `"version":1`, `"version":1,"future":true`, 1),
		"entry":     strings.Replace(base, `"text":"x"`, `"text":"x","future":true`, 1),
		"style":     strings.Replace(base, `"fontFamily":"sans"`, `"fontFamily":"sans","future":true`, 1),
		"trailing":  base + `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseStyledSubtitlePayload([]byte(raw)); err == nil {
				t.Fatal("expected strict JSON error")
			}
		})
	}
}

func TestParseStyledSubtitlePayloadValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*styledSubtitlePayload)
	}{
		{"version", func(p *styledSubtitlePayload) { p.Version = 2 }},
		{"empty entries", func(p *styledSubtitlePayload) { p.Entries = nil }},
		{"too many entries", func(p *styledSubtitlePayload) {
			p.Entries = make([]styledSubtitleEntry, maxSubtitleEntries+1)
		}},
		{"negative start", func(p *styledSubtitlePayload) { p.Entries[0].Start = -1 }},
		{"end before start", func(p *styledSubtitlePayload) { p.Entries[0].End = p.Entries[0].Start }},
		{"empty text", func(p *styledSubtitlePayload) { p.Entries[0].Text = " \n " }},
		{"long text", func(p *styledSubtitlePayload) {
			p.Entries[0].Text = strings.Repeat("x", maxStyledSubtitleTextSize+1)
		}},
		{"font family", func(p *styledSubtitlePayload) { p.Entries[0].Style.FontFamily = "/tmp/user.ttf" }},
		{"font size low", func(p *styledSubtitlePayload) { p.Entries[0].Style.FontSize = 7.99 }},
		{"font size high", func(p *styledSubtitlePayload) { p.Entries[0].Style.FontSize = 200.01 }},
		{"text color", func(p *styledSubtitlePayload) { p.Entries[0].Style.TextColor = "white" }},
		{"background alpha required", func(p *styledSubtitlePayload) {
			p.Entries[0].Style.BackgroundColor = "#112233"
		}},
		{"outline color", func(p *styledSubtitlePayload) { p.Entries[0].Style.OutlineColor = "#1234" }},
		{"outline width", func(p *styledSubtitlePayload) { p.Entries[0].Style.OutlineWidth = 8.1 }},
		{"align", func(p *styledSubtitlePayload) { p.Entries[0].Style.Align = "justify" }},
		{"x", func(p *styledSubtitlePayload) { p.Entries[0].Style.X = 1.01 }},
		{"y", func(p *styledSubtitlePayload) { p.Entries[0].Style.Y = -0.01 }},
		{"max width", func(p *styledSubtitlePayload) { p.Entries[0].Style.MaxWidth = 0.09 }},
		{"line height", func(p *styledSubtitlePayload) { p.Entries[0].Style.LineHeight = 3.01 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload := validStyledSubtitlePayload()
			test.mutate(&payload)
			if _, err := parseStyledSubtitlePayload(marshalStyledPayload(t, payload)); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestStyledSubtitleEntryRejectsNonFiniteValues(t *testing.T) {
	entry := validStyledSubtitlePayload().Entries[0]
	entry.Style.X = math.Inf(1)
	if err := validateStyledSubtitleEntry(entry); err == nil {
		t.Fatal("expected non-finite value error")
	}
}

func TestParseStyledSubtitlePayloadSizeLimit(t *testing.T) {
	if _, err := parseStyledSubtitlePayload(make([]byte, maxStyledSubtitlePayloadSize+1)); err == nil {
		t.Fatal("expected payload size error")
	}
}

func TestStyledSubtitleWrapsCJKAndPreservesExplicitLines(t *testing.T) {
	lines := wrapStyledSubtitleText(basicfont.Face7x13, "中文字符ABCD\n第二行", 22)
	if len(lines) < 2 {
		t.Fatalf("expected multiline output, got %q", lines)
	}
	if strings.Join(lines, "") != "中文字符ABCD第二行" {
		t.Fatalf("wrapped text changed content: %q", lines)
	}
}

func TestRenderStyledSubtitleBackgroundOutlineAndPosition(t *testing.T) {
	payload := validStyledSubtitlePayload()
	entry := payload.Entries[0]
	entry.Text = "AB\nCD"
	entry.Style.FontSize = 20
	entry.Style.BackgroundColor = "#20406080"
	entry.Style.OutlineColor = "#FF0000"
	entry.Style.OutlineWidth = 2
	entry.Style.TextColor = "#FFFFFF"
	entry.Style.Align = "right"
	entry.Style.X = 0.9
	entry.Style.Y = 0.2
	entry.Style.MaxWidth = 0.4
	entry.Style.Bold = true
	entry.Style.Italic = true

	canvas, err := renderStyledSubtitle(entry, basicfont.Face7x13, 200, 120)
	if err != nil {
		t.Fatalf("renderStyledSubtitle() error = %v", err)
	}

	// A right-aligned 80 px box anchored at x=180 begins at x=100.
	if alphaAt(canvas, 100, 24) == 0 {
		t.Fatal("expected RGBA background at anchored box origin")
	}
	if alphaAt(canvas, 99, 24) != 0 || alphaAt(canvas, 180, 24) != 0 {
		t.Fatal("background escaped its anchored text box")
	}
	if countPixels(canvas, func(c color.RGBA) bool {
		return c.A > 0 && c.R > c.G*2 && c.R > c.B*2
	}) == 0 {
		t.Fatal("expected red outline pixels")
	}
	if countPixels(canvas, func(c color.RGBA) bool {
		return c.A > 0 && c.R > 220 && c.G > 220 && c.B > 220
	}) == 0 {
		t.Fatal("expected white text pixels")
	}
}

func TestRasterizeStyledSubtitlesWritesCJKPNG(t *testing.T) {
	payload := validStyledSubtitlePayload()
	directory := t.TempDir()
	paths, err := rasterizeStyledSubtitles(payload.Entries, 320, 180, directory)
	if err != nil {
		if strings.Contains(err.Error(), "no installed server-owned") {
			t.Skipf("server has no CJK font: %v", err)
		}
		t.Fatalf("rasterizeStyledSubtitles() error = %v", err)
	}
	if len(paths) != 1 || filepath.Dir(paths[0]) != directory {
		t.Fatalf("unexpected paths: %q", paths)
	}
	info, err := os.Stat(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Fatal("styled subtitle PNG is empty")
	}
}

func TestLegacySRTBehaviorRemainsAvailable(t *testing.T) {
	raw := []byte("1\n00:00:00,250 --> 00:00:02,500\n第一行\nSecond line\n")
	entries, err := parseSRT(raw)
	if err != nil {
		t.Fatalf("parseSRT() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries", len(entries))
	}
	if entries[0].start != 250*time.Millisecond ||
		entries[0].end != 2500*time.Millisecond ||
		entries[0].text != "第一行\nSecond line" {
		t.Fatalf("unexpected legacy entry: %#v", entries[0])
	}
}

func TestStyledSubtitleLegacyFlagUsesLegacyRasterizer(t *testing.T) {
	entry := validStyledSubtitlePayload().Entries[0]
	entry.Text = "legacy caption"
	entry.Legacy = true
	paths, err := rasterizeStyledSubtitles([]styledSubtitleEntry{entry}, 320, 180, t.TempDir())
	if err != nil {
		if strings.Contains(err.Error(), "no installed subtitle font") {
			t.Skipf("server has no subtitle font: %v", err)
		}
		t.Fatalf("rasterizeStyledSubtitles() error = %v", err)
	}
	if len(paths) != 1 || filepath.Base(paths[0]) != "subtitle-000.png" {
		t.Fatalf("legacy flag did not use legacy rasterizer: %q", paths)
	}
}

func alphaAt(img *image.RGBA, x, y int) uint8 {
	return color.RGBAModel.Convert(img.At(x, y)).(color.RGBA).A
}

func countPixels(img *image.RGBA, match func(color.RGBA) bool) int {
	count := 0
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			if match(color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)) {
				count++
			}
		}
	}
	return count
}
