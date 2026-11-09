package render

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	stddraw "image/draw"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	platformmedia "agentcut/platform/media"
)

const maxSubtitleEntries = 512

type subtitleEntry struct {
	start time.Duration
	end   time.Duration
	text  string
}

const (
	styledSubtitlePayloadVersion = 1
	maxStyledSubtitlePayloadSize = 1 << 20
	maxStyledSubtitleTextSize    = 16 << 10
)

type styledSubtitlePayload struct {
	Version int                   `json:"version"`
	Entries []styledSubtitleEntry `json:"entries"`
}

type styledSubtitleEntry struct {
	Start  float64             `json:"start"`
	End    float64             `json:"end"`
	Text   string              `json:"text"`
	Style  styledSubtitleStyle `json:"style"`
	Legacy bool                `json:"legacy,omitempty"`
}

type styledSubtitleStyle struct {
	FontFamily      string  `json:"fontFamily"`
	FontSize        float64 `json:"fontSize"`
	TextColor       string  `json:"textColor"`
	BackgroundColor string  `json:"backgroundColor"`
	OutlineColor    string  `json:"outlineColor"`
	OutlineWidth    float64 `json:"outlineWidth"`
	Bold            bool    `json:"bold"`
	Italic          bool    `json:"italic"`
	Align           string  `json:"align"`
	X               float64 `json:"x"`
	Y               float64 `json:"y"`
	MaxWidth        float64 `json:"maxWidth"`
	LineHeight      float64 `json:"lineHeight"`
}

func parseStyledSubtitlePayload(raw []byte) (styledSubtitlePayload, error) {
	if len(raw) == 0 || len(raw) > maxStyledSubtitlePayloadSize {
		return styledSubtitlePayload{}, fmt.Errorf("styled subtitle payload must be within 1 byte..1 MiB")
	}

	var payload styledSubtitlePayload
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return styledSubtitlePayload{}, fmt.Errorf("decode styled subtitle payload: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return styledSubtitlePayload{}, err
	}
	if payload.Version != styledSubtitlePayloadVersion {
		return styledSubtitlePayload{}, fmt.Errorf("styled subtitle version must be %d", styledSubtitlePayloadVersion)
	}
	if len(payload.Entries) == 0 || len(payload.Entries) > maxSubtitleEntries {
		return styledSubtitlePayload{}, fmt.Errorf("styled subtitle must contain 1..%d entries", maxSubtitleEntries)
	}
	for i := range payload.Entries {
		if err := validateStyledSubtitleEntry(payload.Entries[i]); err != nil {
			return styledSubtitlePayload{}, fmt.Errorf("styled subtitle entry %d: %w", i+1, err)
		}
	}
	return payload, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("styled subtitle payload must contain exactly one JSON value")
		}
		return fmt.Errorf("decode styled subtitle payload: %w", err)
	}
	return nil
}

func validateStyledSubtitleEntry(entry styledSubtitleEntry) error {
	if !finiteInRange(entry.Start, 0, float64(platformmedia.MaxMediaDurationSec)) ||
		!finiteInRange(entry.End, 0, float64(platformmedia.MaxMediaDurationSec)) ||
		entry.End <= entry.Start {
		return fmt.Errorf("timing is outside the render duration limit")
	}
	if strings.TrimSpace(entry.Text) == "" || len(entry.Text) > maxStyledSubtitleTextSize {
		return fmt.Errorf("text must be non-empty and at most 16 KiB")
	}
	style := entry.Style
	switch style.FontFamily {
	case "default", "sans", "serif", "mono":
	default:
		return fmt.Errorf("fontFamily must be default, sans, serif, or mono")
	}
	if !finiteInRange(style.FontSize, 8, 200) {
		return fmt.Errorf("fontSize must be within 8..200")
	}
	if !isHexColor(style.TextColor, false) {
		return fmt.Errorf("textColor must be #RRGGBB")
	}
	if !isHexColor(style.BackgroundColor, true) {
		return fmt.Errorf("backgroundColor must be empty or #RRGGBBAA")
	}
	if !isHexColor(style.OutlineColor, false) {
		return fmt.Errorf("outlineColor must be #RRGGBB")
	}
	if !finiteInRange(style.OutlineWidth, 0, 8) {
		return fmt.Errorf("outlineWidth must be within 0..8")
	}
	switch style.Align {
	case "left", "center", "right":
	default:
		return fmt.Errorf("align must be left, center, or right")
	}
	if !finiteInRange(style.X, 0, 1) || !finiteInRange(style.Y, 0, 1) {
		return fmt.Errorf("x and y must be within 0..1")
	}
	if !finiteInRange(style.MaxWidth, 0.1, 1) {
		return fmt.Errorf("maxWidth must be within 0.1..1")
	}
	if !finiteInRange(style.LineHeight, 0.8, 3) {
		return fmt.Errorf("lineHeight must be within 0.8..3")
	}
	return nil
}

func finiteInRange(value, minimum, maximum float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= minimum && value <= maximum
}

func isHexColor(value string, allowEmptyRGBA bool) bool {
	if allowEmptyRGBA && value == "" {
		return true
	}
	expectedLength := 7
	if allowEmptyRGBA {
		expectedLength = 9
	}
	if len(value) != expectedLength || value[0] != '#' {
		return false
	}
	for _, r := range value[1:] {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

func parseSRT(raw []byte) ([]subtitleEntry, error) {
	normalized := strings.ReplaceAll(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\r", "\n")
	scanner := bufio.NewScanner(strings.NewReader(normalized))
	scanner.Buffer(make([]byte, 4096), int(maxSubtitleBytes))
	var blocks [][]string
	var current []string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			if len(current) > 0 {
				blocks = append(blocks, current)
				current = nil
			}
			continue
		}
		current = append(current, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan subtitle: %w", err)
	}
	if len(current) > 0 {
		blocks = append(blocks, current)
	}
	if len(blocks) == 0 || len(blocks) > maxSubtitleEntries {
		return nil, fmt.Errorf("subtitle must contain 1..%d entries", maxSubtitleEntries)
	}
	entries := make([]subtitleEntry, 0, len(blocks))
	for i, block := range blocks {
		if len(block) < 3 {
			return nil, fmt.Errorf("subtitle entry %d is incomplete", i+1)
		}
		if _, err := strconv.Atoi(strings.TrimSpace(block[0])); err != nil {
			return nil, fmt.Errorf("subtitle entry %d index is invalid", i+1)
		}
		times := strings.Split(block[1], " --> ")
		if len(times) != 2 {
			return nil, fmt.Errorf("subtitle entry %d timing is invalid", i+1)
		}
		start, err := parseSRTTime(strings.TrimSpace(times[0]))
		if err != nil {
			return nil, fmt.Errorf("subtitle entry %d start: %w", i+1, err)
		}
		end, err := parseSRTTime(strings.TrimSpace(times[1]))
		if err != nil {
			return nil, fmt.Errorf("subtitle entry %d end: %w", i+1, err)
		}
		if end <= start || end > time.Duration(platformmedia.MaxMediaDurationSec)*time.Second {
			return nil, fmt.Errorf("subtitle entry %d timing is outside the render duration limit", i+1)
		}
		text := strings.TrimSpace(strings.Join(block[2:], "\n"))
		if text == "" || len(text) > 16<<10 {
			return nil, fmt.Errorf("subtitle entry %d text is invalid", i+1)
		}
		entries = append(entries, subtitleEntry{start: start, end: end, text: text})
	}
	return entries, nil
}

func parseSRTTime(value string) (time.Duration, error) {
	var hour, minute, second, millis int
	if _, err := fmt.Sscanf(value, "%d:%d:%d,%d", &hour, &minute, &second, &millis); err != nil ||
		hour < 0 || minute < 0 || minute > 59 || second < 0 || second > 59 ||
		millis < 0 || millis > 999 {
		return 0, fmt.Errorf("invalid timestamp %q", value)
	}
	return time.Duration(hour)*time.Hour + time.Duration(minute)*time.Minute +
		time.Duration(second)*time.Second + time.Duration(millis)*time.Millisecond, nil
}

func rasterizeSubtitles(entries []subtitleEntry, width, height int, directory string) ([]string, error) {
	texts := make([]string, len(entries))
	for i := range entries {
		texts[i] = entries[i].text
	}
	fontSize := float64(height) / 14
	if fontSize < 20 {
		fontSize = 20
	}
	if fontSize > 64 {
		fontSize = 64
	}
	face, err := loadSubtitleFace(texts, fontSize)
	if err != nil {
		return nil, err
	}
	defer face.Close()

	paths := make([]string, len(entries))
	for i, entry := range entries {
		canvas := image.NewRGBA(image.Rect(0, 0, width, height))
		lines := wrapSubtitleText(face, entry.text, width*8/10)
		metrics := face.Metrics()
		lineHeight := (metrics.Height + fixed.I(4)).Ceil()
		totalHeight := len(lines) * lineHeight
		firstBaseline := height - height/10 - totalHeight + metrics.Ascent.Ceil()
		maxLineWidth := 0
		for _, line := range lines {
			if measured := font.MeasureString(face, line).Ceil(); measured > maxLineWidth {
				maxLineWidth = measured
			}
		}
		padding := 8
		background := image.Rect(
			(width-maxLineWidth)/2-padding,
			firstBaseline-metrics.Ascent.Ceil()-padding,
			(width+maxLineWidth)/2+padding,
			firstBaseline-metrics.Ascent.Ceil()+totalHeight+padding,
		)
		stddraw.Draw(canvas, background, &image.Uniform{C: color.RGBA{A: 150}}, image.Point{}, stddraw.Over)

		for lineIndex, line := range lines {
			lineWidth := font.MeasureString(face, line).Ceil()
			x := (width - lineWidth) / 2
			y := firstBaseline + lineIndex*lineHeight
			for _, offset := range []image.Point{{-2, 0}, {2, 0}, {0, -2}, {0, 2}} {
				drawSubtitleLine(canvas, face, line, x+offset.X, y+offset.Y, color.RGBA{A: 230})
			}
			drawSubtitleLine(canvas, face, line, x, y, color.White)
		}
		path := filepath.Join(directory, fmt.Sprintf("subtitle-%03d.png", i))
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return nil, fmt.Errorf("create subtitle image: %w", err)
		}
		encodeErr := png.Encode(file, canvas)
		closeErr := file.Close()
		if encodeErr != nil {
			return nil, fmt.Errorf("encode subtitle image: %w", encodeErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close subtitle image: %w", closeErr)
		}
		paths[i] = path
	}
	return paths, nil
}

func rasterizeStyledSubtitles(entries []styledSubtitleEntry, width, height int, directory string) ([]string, error) {
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("styled subtitle canvas dimensions must be positive")
	}
	if len(entries) == 0 || len(entries) > maxSubtitleEntries {
		return nil, fmt.Errorf("styled subtitle must contain 1..%d entries", maxSubtitleEntries)
	}

	paths := make([]string, len(entries))
	var legacyEntries []subtitleEntry
	var legacyIndexes []int
	for i, entry := range entries {
		if err := validateStyledSubtitleEntry(entry); err != nil {
			return nil, fmt.Errorf("styled subtitle entry %d: %w", i+1, err)
		}
		if entry.Legacy {
			legacyEntries = append(legacyEntries, subtitleEntry{
				start: time.Duration(entry.Start * float64(time.Second)),
				end:   time.Duration(entry.End * float64(time.Second)),
				text:  entry.Text,
			})
			legacyIndexes = append(legacyIndexes, i)
		}
	}
	if len(legacyEntries) > 0 {
		legacyPaths, err := rasterizeSubtitles(legacyEntries, width, height, directory)
		if err != nil {
			return nil, fmt.Errorf("rasterize legacy styled subtitle entries: %w", err)
		}
		for i, path := range legacyPaths {
			paths[legacyIndexes[i]] = path
		}
	}

	for i, entry := range entries {
		if entry.Legacy {
			continue
		}
		face, err := loadStyledSubtitleFace(entry.Style.FontFamily, []string{entry.Text}, entry.Style.FontSize)
		if err != nil {
			return nil, fmt.Errorf("load styled subtitle font: %w", err)
		}
		canvas, renderErr := renderStyledSubtitle(entry, face, width, height)
		closeErr := face.Close()
		if renderErr != nil {
			return nil, renderErr
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close styled subtitle font: %w", closeErr)
		}

		path := filepath.Join(directory, fmt.Sprintf("styled-subtitle-%03d.png", i))
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return nil, fmt.Errorf("create styled subtitle image: %w", err)
		}
		encodeErr := png.Encode(file, canvas)
		closeErr = file.Close()
		if encodeErr != nil {
			return nil, fmt.Errorf("encode styled subtitle image: %w", encodeErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close styled subtitle image: %w", closeErr)
		}
		paths[i] = path
	}
	return paths, nil
}

func renderStyledSubtitle(entry styledSubtitleEntry, face font.Face, width, height int) (*image.RGBA, error) {
	style := entry.Style
	textColor, err := parseStyledColor(style.TextColor)
	if err != nil {
		return nil, err
	}
	outlineColor, err := parseStyledColor(style.OutlineColor)
	if err != nil {
		return nil, err
	}
	var backgroundColor color.RGBA
	if style.BackgroundColor != "" {
		backgroundColor, err = parseStyledColor(style.BackgroundColor)
		if err != nil {
			return nil, err
		}
	}

	boxWidth := clampInt(int(math.Round(style.MaxWidth*float64(width))), 1, width)
	boxLeft := styledBoxLeft(style.Align, int(math.Round(style.X*float64(width))), boxWidth)
	boxLeft = clampInt(boxLeft, 0, width-boxWidth)

	outlineRadius := int(math.Ceil(style.OutlineWidth))
	boldRadius := 0
	if style.Bold {
		boldRadius = clampInt(int(math.Round(style.FontSize/40)), 1, 4)
	}
	italicExtra := 0
	if style.Italic {
		italicExtra = int(math.Ceil(style.FontSize * 0.22))
	}
	margin := outlineRadius + boldRadius
	maxTextWidth := boxWidth - 2*margin - italicExtra
	if maxTextWidth < 1 {
		maxTextWidth = 1
	}
	lines := wrapStyledSubtitleText(face, entry.Text, maxTextWidth)
	metrics := face.Metrics()
	lineAdvance := int(math.Round(style.FontSize * style.LineHeight))
	if lineAdvance < 1 {
		lineAdvance = 1
	}
	textHeight := metrics.Height.Ceil()
	if textHeight < 1 {
		textHeight = int(math.Ceil(style.FontSize))
	}
	boxHeight := textHeight + (len(lines)-1)*lineAdvance + 2*margin
	if boxHeight < 1 {
		boxHeight = 1
	}
	boxTop := int(math.Round(style.Y * float64(height)))
	if boxHeight >= height {
		boxTop = 0
	} else {
		boxTop = clampInt(boxTop, 0, height-boxHeight)
	}

	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	if style.BackgroundColor != "" && backgroundColor.A != 0 {
		background := image.Rect(boxLeft, boxTop, boxLeft+boxWidth, boxTop+boxHeight)
		stddraw.Draw(canvas, background, &image.Uniform{C: backgroundColor}, image.Point{}, stddraw.Over)
	}

	firstBaseline := boxTop + margin + metrics.Ascent.Ceil()
	for lineIndex, line := range lines {
		lineWidth := font.MeasureString(face, line).Ceil()
		var x int
		switch style.Align {
		case "left":
			x = boxLeft + margin
		case "right":
			x = boxLeft + boxWidth - margin - italicExtra - lineWidth
		default:
			x = boxLeft + (boxWidth-italicExtra-lineWidth)/2
		}
		x = clampInt(x, 0, width-1)
		baseline := firstBaseline + lineIndex*lineAdvance
		mask := styledTextMask(face, line, x, baseline, canvas.Bounds(), boldRadius, style.Italic)
		if outlineRadius > 0 {
			drawDilatedMask(canvas, mask, outlineRadius, outlineColor)
		}
		stddraw.DrawMask(canvas, canvas.Bounds(), &image.Uniform{C: textColor}, image.Point{}, mask, image.Point{}, stddraw.Over)
	}
	return canvas, nil
}

func styledBoxLeft(align string, anchor, boxWidth int) int {
	switch align {
	case "center":
		return anchor - boxWidth/2
	case "right":
		return anchor - boxWidth
	default:
		return anchor
	}
}

func wrapStyledSubtitleText(face font.Face, text string, maxWidth int) []string {
	var lines []string
	for _, explicitLine := range strings.Split(text, "\n") {
		if explicitLine == "" {
			lines = append(lines, " ")
			continue
		}
		var current strings.Builder
		for _, r := range explicitLine {
			candidate := current.String() + string(r)
			if current.Len() > 0 && font.MeasureString(face, candidate).Ceil() > maxWidth {
				lines = append(lines, current.String())
				current.Reset()
			}
			current.WriteRune(r)
		}
		if current.Len() > 0 {
			lines = append(lines, current.String())
		}
	}
	if len(lines) == 0 {
		return []string{" "}
	}
	return lines
}

func styledTextMask(
	face font.Face,
	text string,
	x, baseline int,
	bounds image.Rectangle,
	boldRadius int,
	italic bool,
) *image.Alpha {
	base := image.NewAlpha(bounds)
	drawer := font.Drawer{
		Dst: base, Src: image.White, Face: face,
		Dot: fixed.P(x, baseline),
	}
	drawer.DrawString(text)

	mask := base
	if italic {
		mask = shearTextMask(base, face.Metrics(), x, baseline, font.MeasureString(face, text).Ceil())
	}
	if boldRadius > 0 {
		bold := image.NewAlpha(bounds)
		for offset := 0; offset <= boldRadius; offset++ {
			stddraw.Draw(bold, bounds.Add(image.Pt(offset, 0)), mask, image.Point{}, stddraw.Over)
		}
		mask = bold
	}
	return mask
}

func shearTextMask(base *image.Alpha, metrics font.Metrics, x, baseline, measuredWidth int) *image.Alpha {
	sheared := image.NewAlpha(base.Bounds())
	top := baseline - metrics.Ascent.Ceil()
	bottom := baseline + metrics.Descent.Ceil()
	if bottom <= top {
		bottom = top + 1
	}
	sourceBounds := image.Rect(x, top, x+measuredWidth+1, bottom).Intersect(base.Bounds())
	for sourceY := sourceBounds.Min.Y; sourceY < sourceBounds.Max.Y; sourceY++ {
		shift := int(math.Round(float64(bottom-sourceY) * 0.22))
		for sourceX := sourceBounds.Min.X; sourceX < sourceBounds.Max.X; sourceX++ {
			alpha := base.AlphaAt(sourceX, sourceY).A
			targetX := sourceX + shift
			if alpha == 0 || targetX < sheared.Rect.Min.X || targetX >= sheared.Rect.Max.X {
				continue
			}
			index := sheared.PixOffset(targetX, sourceY)
			if alpha > sheared.Pix[index] {
				sheared.Pix[index] = alpha
			}
		}
	}
	return sheared
}

func drawDilatedMask(dst stddraw.Image, mask *image.Alpha, radius int, source color.Color) {
	uniform := &image.Uniform{C: source}
	for offsetY := -radius; offsetY <= radius; offsetY++ {
		for offsetX := -radius; offsetX <= radius; offsetX++ {
			if offsetX*offsetX+offsetY*offsetY > radius*radius {
				continue
			}
			stddraw.DrawMask(
				dst,
				mask.Bounds().Add(image.Pt(offsetX, offsetY)),
				uniform,
				image.Point{},
				mask,
				image.Point{},
				stddraw.Over,
			)
		}
	}
}

func parseStyledColor(value string) (color.RGBA, error) {
	if len(value) != 7 && len(value) != 9 {
		return color.RGBA{}, fmt.Errorf("invalid styled subtitle color %q", value)
	}
	parsed, err := strconv.ParseUint(value[1:], 16, 32)
	if err != nil {
		return color.RGBA{}, fmt.Errorf("invalid styled subtitle color %q", value)
	}
	if len(value) == 7 {
		return color.RGBA{
			R: uint8(parsed >> 16),
			G: uint8(parsed >> 8),
			B: uint8(parsed),
			A: 255,
		}, nil
	}
	return color.RGBA{
		R: uint8(parsed >> 24),
		G: uint8(parsed >> 16),
		B: uint8(parsed >> 8),
		A: uint8(parsed),
	}, nil
}

func clampInt(value, minimum, maximum int) int {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func rasterizeTitleText(text string, width, height int, directory string) (string, error) {
	fontSize := float64(height) / 10
	if fontSize < 24 {
		fontSize = 24
	}
	if fontSize > 96 {
		fontSize = 96
	}
	face, err := loadSubtitleFace([]string{text}, fontSize)
	if err != nil {
		return "", err
	}
	defer face.Close()
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	lines := wrapSubtitleText(face, text, width*8/10)
	metrics := face.Metrics()
	lineHeight := (metrics.Height + fixed.I(6)).Ceil()
	totalHeight := len(lines) * lineHeight
	firstBaseline := (height-totalHeight)/2 + metrics.Ascent.Ceil()
	for lineIndex, line := range lines {
		lineWidth := font.MeasureString(face, line).Ceil()
		x := (width - lineWidth) / 2
		y := firstBaseline + lineIndex*lineHeight
		for _, offset := range []image.Point{{-2, 0}, {2, 0}, {0, -2}, {0, 2}} {
			drawSubtitleLine(canvas, face, line, x+offset.X, y+offset.Y, color.RGBA{A: 230})
		}
		drawSubtitleLine(canvas, face, line, x, y, color.White)
	}
	path := filepath.Join(directory, "title.png")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", fmt.Errorf("create title image: %w", err)
	}
	encodeErr := png.Encode(file, canvas)
	closeErr := file.Close()
	if encodeErr != nil {
		return "", fmt.Errorf("encode title image: %w", encodeErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("close title image: %w", closeErr)
	}
	return path, nil
}

func drawSubtitleLine(dst stddraw.Image, face font.Face, text string, x, y int, source color.Color) {
	drawer := font.Drawer{
		Dst: dst, Src: image.NewUniform(source), Face: face,
		Dot: fixed.P(x, y),
	}
	drawer.DrawString(text)
}

func wrapSubtitleText(face font.Face, text string, maxWidth int) []string {
	var lines []string
	for _, explicitLine := range strings.Split(text, "\n") {
		var current strings.Builder
		for _, r := range explicitLine {
			candidate := current.String() + string(r)
			if current.Len() > 0 && font.MeasureString(face, candidate).Ceil() > maxWidth {
				lines = append(lines, current.String())
				current.Reset()
			}
			current.WriteRune(r)
		}
		if current.Len() > 0 {
			lines = append(lines, current.String())
		}
	}
	if len(lines) == 0 {
		return []string{" "}
	}
	return lines
}

type closeableFace interface {
	font.Face
	Close() error
}

func loadSubtitleFace(texts []string, size float64) (closeableFace, error) {
	candidates := []string{}
	if configured := strings.TrimSpace(os.Getenv("AGENTCUT_SUBTITLE_FONT")); configured != "" {
		candidates = append(candidates, configured)
	}
	candidates = append(candidates,
		"/System/Library/Fonts/Hiragino Sans GB.ttc",
		"/System/Library/Fonts/STHeiti Medium.ttc",
		"/System/Library/Fonts/PingFang.ttc",
		"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
		"/usr/share/fonts/opentype/noto/NotoSansCJKsc-Regular.otf",
		"/usr/share/fonts/truetype/noto/NotoSansCJK-Regular.ttc",
		"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
	)
	var lastErr error
	for _, path := range candidates {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if len(raw) == 0 || len(raw) > 128<<20 {
			lastErr = fmt.Errorf("subtitle font size is outside (0, 128 MiB]")
			continue
		}
		collection, err := opentype.ParseCollection(raw)
		if err != nil {
			lastErr = err
			continue
		}
		for i := 0; i < collection.NumFonts(); i++ {
			parsed, err := collection.Font(i)
			if err != nil {
				lastErr = err
				continue
			}
			face, err := opentype.NewFace(parsed, &opentype.FaceOptions{
				Size: size, DPI: 72, Hinting: font.HintingFull,
			})
			if err != nil {
				lastErr = err
				continue
			}
			if faceHasText(face, texts) {
				return face.(closeableFace), nil
			}
			_ = face.Close()
		}
	}
	if configured := strings.TrimSpace(os.Getenv("AGENTCUT_SUBTITLE_FONT")); configured != "" {
		return nil, fmt.Errorf("configured AGENTCUT_SUBTITLE_FONT cannot render all subtitle glyphs: %v", lastErr)
	}
	return nil, fmt.Errorf("no installed subtitle font can render all glyphs; set AGENTCUT_SUBTITLE_FONT")
}

func loadStyledSubtitleFace(family string, texts []string, size float64) (closeableFace, error) {
	candidates := make([]string, 0, 16)
	if configured := strings.TrimSpace(os.Getenv("AGENTCUT_SUBTITLE_FONT")); configured != "" {
		candidates = append(candidates, configured)
	}
	switch family {
	case "serif":
		candidates = append(candidates,
			"/System/Library/Fonts/Times.ttc",
			"/usr/share/fonts/truetype/dejavu/DejaVuSerif.ttf",
			"/usr/share/fonts/opentype/noto/NotoSerif-Regular.ttf",
		)
	case "mono":
		candidates = append(candidates,
			"/System/Library/Fonts/Menlo.ttc",
			"/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
			"/usr/share/fonts/opentype/noto/NotoSansMono-Regular.ttf",
		)
	default:
		candidates = append(candidates,
			"/System/Library/Fonts/Helvetica.ttc",
			"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
			"/usr/share/fonts/opentype/noto/NotoSans-Regular.ttf",
		)
	}
	candidates = append(candidates,
		"/System/Library/Fonts/Hiragino Sans GB.ttc",
		"/System/Library/Fonts/STHeiti Medium.ttc",
		"/System/Library/Fonts/PingFang.ttc",
		"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
		"/usr/share/fonts/opentype/noto/NotoSansCJKsc-Regular.otf",
		"/usr/share/fonts/truetype/noto/NotoSansCJK-Regular.ttc",
		"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
	)
	return loadFaceFromCandidates(candidates, texts, size, family)
}

func loadFaceFromCandidates(candidates, texts []string, size float64, family string) (closeableFace, error) {
	var lastErr error
	for _, path := range candidates {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if len(raw) == 0 || len(raw) > 128<<20 {
			lastErr = fmt.Errorf("subtitle font size is outside (0, 128 MiB]")
			continue
		}
		collection, err := opentype.ParseCollection(raw)
		if err != nil {
			lastErr = err
			continue
		}
		for i := 0; i < collection.NumFonts(); i++ {
			parsed, err := collection.Font(i)
			if err != nil {
				lastErr = err
				continue
			}
			face, err := opentype.NewFace(parsed, &opentype.FaceOptions{
				Size: size, DPI: 72, Hinting: font.HintingFull,
			})
			if err != nil {
				lastErr = err
				continue
			}
			if faceHasText(face, texts) {
				return face.(closeableFace), nil
			}
			_ = face.Close()
		}
	}
	if configured := strings.TrimSpace(os.Getenv("AGENTCUT_SUBTITLE_FONT")); configured != "" {
		return nil, fmt.Errorf("configured AGENTCUT_SUBTITLE_FONT cannot render all %s subtitle glyphs: %v", family, lastErr)
	}
	return nil, fmt.Errorf("no installed server-owned %s subtitle font can render all glyphs; set AGENTCUT_SUBTITLE_FONT", family)
}

func faceHasText(face font.Face, texts []string) bool {
	for _, text := range texts {
		for _, r := range text {
			if unicode.IsSpace(r) {
				continue
			}
			if _, ok := face.GlyphAdvance(r); !ok {
				return false
			}
		}
	}
	return true
}
