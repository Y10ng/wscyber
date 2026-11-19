package webapi

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
	"google.golang.org/protobuf/proto"

	"github.com/chainreactors/cyber/aop/traffic"
)

// 证据图片：由已保存的 traffic.Flow 在服务端确定性渲染请求/响应 PNG（不经模型）。
// 两步：layoutEvidenceLines 生成行模型（遮盖、高亮、折行、截断说明），drawEvidenceLayout 只负责像素。

const (
	evidenceImageWidth     = 1200
	evidenceColumnWidth    = 599 // 两栏各 599px，中间 1px 分隔线。
	evidenceFontSize       = 13
	evidenceRowHeight      = 18
	evidenceHeaderHeight   = 30
	evidenceTitleHeight    = 24
	evidenceFooterHeight   = 28
	evidencePad            = 8
	evidenceGutterCells    = 4
	evidenceDefaultLines   = 60
	evidenceMinLines       = 10
	evidenceMaxLines       = 200
	evidenceMaxMarks       = 5
	evidenceMaxMarkRunes   = 100
	evidenceMaskKeepRunes  = 6
	evidenceMaskSuffix     = "…（已遮盖）"
	evidenceMissingGlyph   = "缺少可显示该字符的字体"
	evidenceFontEnv        = "WSCYBER_EVIDENCE_FONT"
	evidenceBinaryFraction = 0.10
)

var evidenceFallbackFontPaths = []string{
	"/usr/share/fonts/truetype/droid/DroidSansFallbackFull.ttf",
	"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
	"/usr/share/fonts/truetype/wqy/wqy-microhei.ttc",
}

var evidenceMaskedHeaders = map[string]bool{
	"authorization": true, "proxy-authorization": true, "cookie": true,
	"set-cookie": true, "x-api-key": true, "x-auth-token": true,
}

// evidenceImageMeta 是 Flow 之外、来自业务行的如实说明。
type evidenceImageMeta struct {
	Label     string // 页眉/页脚里的记录类型，默认“证据”；出口记录用“出口记录”。
	ID        string
	CreatedAt string // RFC3339（UTC）；无法解析时原样显示。
	Status    string // 业务状态（completed/unknown/denied/allowed…）。
	Reason    string // 失败/拒绝原因。
	Sent      *bool  // false=明确未发送；nil=未知。
	// 原件已截断：Kept 为 Flow 中实际保存的字节数，Observed>0 时为观察到的原始字节数。
	RequestTruncated  *evidenceTruncation
	ResponseTruncated *evidenceTruncation
}

type evidenceTruncation struct{ Kept, Observed int64 }

type evidenceImageOptions struct {
	Marks    []string
	MaxLines int // 0 表示默认 60。
	NoMask   bool
}

type evidenceSpan struct{ Start, End int } // 行内 rune 下标，[Start,End)。

// evidenceRow 是一个显示行（折行后）。
type evidenceRow struct {
	Number    string // 逻辑行号；续行与说明行为空。
	Text      string
	Highlight bool
	Marks     []evidenceSpan
	Note      bool // 灰字说明（截断、无响应、二进制等）。
}

type evidenceColumn struct {
	Title string
	Rows  []evidenceRow
}

type evidenceLayout struct {
	Header, HeaderRight string
	Request, Response   evidenceColumn
	Footer              []string
	Masked              bool
	TextCells           int
}

// --- 字体 ---

type evidenceFonts struct {
	mono, monoBold *sfnt.Font
	fallback       *sfnt.Font
	fallbackPath   string
	cellWidth      fixed.Int26_6
}

var (
	evidenceFontsOnce sync.Once
	evidenceFontsVal  *evidenceFonts
	evidenceFontsErr  error
)

func loadEvidenceFonts() (*evidenceFonts, error) {
	evidenceFontsOnce.Do(func() {
		fonts := &evidenceFonts{}
		var err error
		if fonts.mono, err = opentype.Parse(gomono.TTF); err != nil {
			evidenceFontsErr = fmt.Errorf("加载 gomono: %w", err)
			return
		}
		if fonts.monoBold, err = opentype.Parse(gomonobold.TTF); err != nil {
			evidenceFontsErr = fmt.Errorf("加载 gomonobold: %w", err)
			return
		}
		face, err := opentype.NewFace(fonts.mono, &opentype.FaceOptions{Size: evidenceFontSize, DPI: 72, Hinting: font.HintingNone})
		if err != nil {
			evidenceFontsErr = err
			return
		}
		advance, ok := face.GlyphAdvance('0')
		_ = face.Close()
		if !ok || advance <= 0 {
			evidenceFontsErr = errors.New("gomono 缺少数字字形")
			return
		}
		fonts.cellWidth = advance
		candidates := evidenceFallbackFontPaths
		if env := strings.TrimSpace(os.Getenv(evidenceFontEnv)); env != "" {
			candidates = append([]string{env}, candidates...)
		}
		for _, path := range candidates {
			if fallback := parseFallbackFont(path); fallback != nil {
				fonts.fallback, fonts.fallbackPath = fallback, path
				break
			}
		}
		evidenceFontsVal = fonts
	})
	return evidenceFontsVal, evidenceFontsErr
}

func parseFallbackFont(path string) *sfnt.Font {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	if parsed, err := opentype.Parse(data); err == nil {
		return parsed
	}
	collection, err := opentype.ParseCollection(data)
	if err != nil || collection.NumFonts() == 0 {
		return nil
	}
	parsed, err := collection.Font(0)
	if err != nil {
		return nil
	}
	return parsed
}

// evidenceTextCells：每栏正文可容纳的等宽格数（与字体单元宽度一致）。
func evidenceTextCells(cellWidth fixed.Int26_6) int {
	width := evidenceColumnWidth - 3*evidencePad
	gutter := fixed.Int26_6(evidenceGutterCells) * cellWidth
	return int((fixed.I(width) - gutter) / cellWidth)
}

// --- 选项 ---

func normalizeEvidenceImageOptions(opts evidenceImageOptions) (evidenceImageOptions, error) {
	if opts.MaxLines == 0 {
		opts.MaxLines = evidenceDefaultLines
	}
	if opts.MaxLines < evidenceMinLines || opts.MaxLines > evidenceMaxLines {
		return opts, fmt.Errorf("max_lines须为%d至%d", evidenceMinLines, evidenceMaxLines)
	}
	marks := make([]string, 0, len(opts.Marks))
	for _, mark := range opts.Marks {
		if mark == "" {
			continue
		}
		if !utf8.ValidString(mark) || utf8.RuneCountInString(mark) > evidenceMaxMarkRunes || strings.ContainsAny(mark, "\r\n") {
			return opts, fmt.Errorf("mark须为单行且不超过%d字", evidenceMaxMarkRunes)
		}
		marks = append(marks, mark)
	}
	if len(marks) > evidenceMaxMarks {
		return opts, fmt.Errorf("mark最多%d个", evidenceMaxMarks)
	}
	opts.Marks = marks
	return opts, nil
}

func evidenceImageOptionsFromQuery(query url.Values) (evidenceImageOptions, error) {
	opts := evidenceImageOptions{Marks: query["mark"]}
	if raw := query.Get("max_lines"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			return opts, fmt.Errorf("max_lines须为%d至%d", evidenceMinLines, evidenceMaxLines)
		}
		opts.MaxLines = parsed
	}
	switch query.Get("mask") {
	case "", "1", "true":
	case "0", "false":
		opts.NoMask = true
	default:
		return opts, errors.New("mask只能是0或1")
	}
	return normalizeEvidenceImageOptions(opts)
}

// --- 行模型 ---

type evidenceLogicalLine struct {
	text      string
	highlight bool
	note      bool
	number    bool
}

// layoutEvidenceLines 把 Flow 变成两栏显示行；不依赖像素，便于测试遮盖/高亮/截断。
func layoutEvidenceLines(flow *traffic.Flow, meta evidenceImageMeta, opts evidenceImageOptions, textCells int) (*evidenceLayout, error) {
	opts, err := normalizeEvidenceImageOptions(opts)
	if err != nil {
		return nil, err
	}
	if flow == nil {
		flow = &traffic.Flow{}
	}
	if textCells < 20 {
		return nil, errors.New("栏宽过小")
	}
	label := meta.Label
	if label == "" {
		label = "证据"
	}
	layout := &evidenceLayout{TextCells: textCells}
	request := flow.GetRequest()
	headerCells := (evidenceImageWidth - 2*evidencePad) * textCells / (evidenceColumnWidth - 3*evidencePad) // 近似全宽格数
	layout.HeaderRight = label + " " + meta.ID
	method, rawURL := request.GetMethod(), request.GetUrl()
	layout.Header = truncateCells(strings.TrimSpace(sanitizeEvidenceText(method+" "+rawURL)), headerCells-runeCellsOf(layout.HeaderRight)-4)

	masked, sensitive := false, false
	requestLines := []evidenceLogicalLine{}
	if request == nil {
		requestLines = append(requestLines, evidenceLogicalLine{text: "（无请求记录）", note: true})
	} else {
		requestLines = append(requestLines, evidenceLogicalLine{text: requestLineText(request), highlight: true, number: true})
		for _, header := range request.GetHeaders() {
			text, m, s := headerLineText(header, !opts.NoMask)
			masked, sensitive = masked || m, sensitive || s
			requestLines = append(requestLines, evidenceLogicalLine{text: text, number: true})
		}
		requestLines = appendBodyLines(requestLines, request.GetBody())
	}
	responseLines := []evidenceLogicalLine{}
	response := flow.GetResponse()
	if response == nil || response.GetStatusCode() == 0 {
		responseLines = append(responseLines, evidenceLogicalLine{text: noResponseText(flow, meta), note: true})
	} else {
		responseLines = append(responseLines, evidenceLogicalLine{text: statusLineText(request, response), highlight: true, number: true})
		for _, header := range response.GetHeaders() {
			text, m, s := headerLineText(header, !opts.NoMask)
			masked, sensitive = masked || m, sensitive || s
			responseLines = append(responseLines, evidenceLogicalLine{text: text, number: true})
		}
		responseLines = appendBodyLines(responseLines, response.GetBody())
	}

	var requestCut, responseCut bool
	layout.Request, requestCut = buildEvidenceColumn("Request", requestLines, meta.RequestTruncated, opts, textCells)
	layout.Response, responseCut = buildEvidenceColumn("Response", responseLines, meta.ResponseTruncated, opts, textCells)
	layout.Masked = masked

	footer := []string{"wscyber", label + " " + meta.ID}
	if created := formatEvidenceTime(meta.CreatedAt); created != "" {
		footer = append(footer, created)
	}
	if requestCut || responseCut {
		footer = append(footer, fmt.Sprintf("每栏最多显示 %d 行", opts.MaxLines))
	}
	if meta.RequestTruncated != nil || meta.ResponseTruncated != nil {
		footer = append(footer, "原件已截断")
	}
	if masked {
		footer = append(footer, "已遮盖敏感头")
	} else if sensitive {
		footer = append(footer, "敏感头未遮盖")
	}
	layout.Footer = footer
	return layout, nil
}

func requestLineText(request *traffic.HttpRequest) string {
	target := request.GetUrl()
	if parsed, err := url.Parse(target); err == nil && parsed.IsAbs() {
		target = parsed.RequestURI()
	}
	return strings.TrimSpace(request.GetMethod() + " " + target + " " + evidenceProtocol(request))
}

func evidenceProtocol(request *traffic.HttpRequest) string {
	if protocol := request.GetProtocol(); protocol != "" {
		return protocol
	}
	return "HTTP/1.1"
}

// statusLineText：Flow 不保存响应协议版本，沿用请求协议（缺省 HTTP/1.1）。
func statusLineText(request *traffic.HttpRequest, response *traffic.HttpResponse) string {
	return strings.TrimSpace(fmt.Sprintf("%s %d %s", evidenceProtocol(request), response.GetStatusCode(), response.GetReasonPhrase()))
}

// headerLineText 返回显示文本、是否遮盖、是否为敏感头。
func headerLineText(header *traffic.Header, mask bool) (string, bool, bool) {
	name, value := header.GetName(), header.GetValue()
	if !evidenceMaskedHeaders[strings.ToLower(name)] {
		return name + ": " + value, false, false
	}
	if !mask {
		return name + ": " + value, false, true
	}
	return name + ": " + maskEvidenceValue(value), true, true
}

// maskEvidenceValue 保留前 6 个字符；值很短时最多保留一半，避免短值全文可见。
func maskEvidenceValue(value string) string {
	runes := []rune(value)
	keep := evidenceMaskKeepRunes
	if keep > len(runes)/2 {
		keep = len(runes) / 2
	}
	return string(runes[:keep]) + evidenceMaskSuffix
}

func appendBodyLines(lines []evidenceLogicalLine, body []byte) []evidenceLogicalLine {
	if len(body) == 0 {
		return lines
	}
	lines = append(lines, evidenceLogicalLine{number: true})
	if evidenceBodyIsBinary(body) {
		return append(lines, evidenceLogicalLine{text: fmt.Sprintf("（二进制内容 %d 字节，未渲染）", len(body)), note: true})
	}
	text := strings.TrimSuffix(string(body), "\n")
	for _, line := range strings.Split(text, "\n") {
		lines = append(lines, evidenceLogicalLine{text: strings.TrimSuffix(line, "\r"), number: true})
	}
	return lines
}

func evidenceBodyIsBinary(body []byte) bool {
	if bytes.IndexByte(body, 0) >= 0 {
		return true
	}
	invalid := 0
	for rest := body; len(rest) > 0; {
		r, size := utf8.DecodeRune(rest)
		if r == utf8.RuneError && size == 1 {
			invalid++
		}
		rest = rest[size:]
	}
	return float64(invalid) > evidenceBinaryFraction*float64(len(body))
}

func noResponseText(flow *traffic.Flow, meta evidenceImageMeta) string {
	reason := strings.TrimSpace(meta.Reason)
	if reason == "" {
		reason = strings.TrimSpace(flow.GetError())
	}
	if meta.Sent != nil && !*meta.Sent {
		if reason == "" {
			reason = meta.Status
		}
		return "（未发送：" + strings.TrimPrefix(reason, "未发送：") + "）"
	}
	parts := []string{}
	if meta.Status != "" {
		parts = append(parts, meta.Status)
	}
	if reason != "" {
		parts = append(parts, reason)
	}
	if len(parts) == 0 {
		parts = append(parts, "未记录原因")
	}
	return "（无响应：" + strings.Join(parts, " · ") + "）"
}

func buildEvidenceColumn(title string, lines []evidenceLogicalLine, truncation *evidenceTruncation, opts evidenceImageOptions, textCells int) (evidenceColumn, bool) {
	column := evidenceColumn{Title: title}
	number, partialLine := 0, 0
	cut := false
	for index, line := range lines {
		text := sanitizeEvidenceText(line.text)
		highlight := line.highlight
		var spans []evidenceSpan
		if !line.note {
			spans = findEvidenceMarks(text, opts.Marks)
			highlight = highlight || len(spans) > 0
		}
		label := ""
		if line.number {
			number++
			label = strconv.Itoa(number)
		}
		rows := wrapEvidenceRow(text, spans, textCells)
		for rowIndex, row := range rows {
			if len(column.Rows) >= opts.MaxLines {
				cut = true
				if rowIndex > 0 {
					partialLine = number
				}
				break
			}
			row.Highlight, row.Note = highlight, line.note
			if rowIndex == 0 {
				row.Number = label
			}
			column.Rows = append(column.Rows, row)
		}
		if cut {
			// 当前逻辑行（未显示或只显示了前半部分）及其后的行都计入未显示。
			note := fmt.Sprintf("… 其余 %d 行未显示", len(lines)-index)
			if partialLine > 0 {
				note += fmt.Sprintf("（含第 %d 行后半部分）", partialLine)
			}
			column.Rows = appendNoteRows(column.Rows, note, textCells)
			break
		}
	}
	if truncation != nil {
		note := fmt.Sprintf("原件已截断（仅保存前 %d 字节）", truncation.Kept)
		if truncation.Observed > truncation.Kept {
			note = fmt.Sprintf("原件已截断（仅保存前 %d 字节，共 %d 字节）", truncation.Kept, truncation.Observed)
		}
		column.Rows = appendNoteRows(column.Rows, note, textCells)
	}
	return column, cut
}

func appendNoteRows(rows []evidenceRow, text string, textCells int) []evidenceRow {
	for _, row := range wrapEvidenceRow(text, nil, textCells) {
		row.Note = true
		rows = append(rows, row)
	}
	return rows
}

func findEvidenceMarks(text string, marks []string) []evidenceSpan {
	var spans []evidenceSpan
	for _, mark := range marks {
		markRunes := utf8.RuneCountInString(mark)
		for offset := 0; offset <= len(text); {
			index := strings.Index(text[offset:], mark)
			if index < 0 {
				break
			}
			start := utf8.RuneCountInString(text[:offset+index])
			spans = append(spans, evidenceSpan{Start: start, End: start + markRunes})
			offset += index + len(mark)
		}
	}
	return spans
}

// wrapEvidenceRow 按等宽格折行，并把 mark 区间切到各显示行。
func wrapEvidenceRow(text string, spans []evidenceSpan, textCells int) []evidenceRow {
	runes := []rune(text)
	if len(runes) == 0 {
		return []evidenceRow{{}}
	}
	var rows []evidenceRow
	start := 0
	for start < len(runes) {
		end, cells := start, 0
		for end < len(runes) {
			width := runeCells(runes[end])
			if cells+width > textCells && end > start {
				break
			}
			cells += width
			end++
		}
		row := evidenceRow{Text: string(runes[start:end])}
		for _, span := range spans {
			s, e := max(span.Start, start), min(span.End, end)
			if s < e {
				row.Marks = append(row.Marks, evidenceSpan{Start: s - start, End: e - start})
			}
		}
		rows = append(rows, row)
		start = end
	}
	return rows
}

// sanitizeEvidenceText：制表符展开为 4 格对齐空格，其他控制字符显示为“·”，非法 UTF-8 显示为 U+FFFD。
func sanitizeEvidenceText(text string) string {
	text = strings.ToValidUTF8(text, "�")
	var builder strings.Builder
	cells := 0
	for _, r := range text {
		switch {
		case r == '\t':
			pad := 4 - cells%4
			builder.WriteString(strings.Repeat(" ", pad))
			cells += pad
			continue
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0):
			r = '·'
		}
		builder.WriteRune(r)
		cells += runeCells(r)
	}
	return builder.String()
}

// runeCells：东亚宽字符占 2 格，其余 1 格。
func runeCells(r rune) int {
	switch {
	case r >= 0x1100 && r <= 0x115F, r >= 0x2E80 && r <= 0x303E, r >= 0x3041 && r <= 0x33FF,
		r >= 0x3400 && r <= 0x4DBF, r >= 0x4E00 && r <= 0x9FFF, r >= 0xA000 && r <= 0xA4CF,
		r >= 0xAC00 && r <= 0xD7A3, r >= 0xF900 && r <= 0xFAFF, r >= 0xFE30 && r <= 0xFE4F,
		r >= 0xFF00 && r <= 0xFF60, r >= 0xFFE0 && r <= 0xFFE6, r >= 0x1F300 && r <= 0x1F64F,
		r >= 0x1F900 && r <= 0x1F9FF, r >= 0x20000 && r <= 0x3FFFD:
		return 2
	}
	return 1
}

func runeCellsOf(text string) int {
	cells := 0
	for _, r := range text {
		cells += runeCells(r)
	}
	return cells
}

func truncateCells(text string, limit int) string {
	if limit < 1 {
		limit = 1
	}
	if runeCellsOf(text) <= limit {
		return text
	}
	var builder strings.Builder
	cells := 0
	for _, r := range text {
		width := runeCells(r)
		if cells+width > limit-1 {
			break
		}
		builder.WriteRune(r)
		cells += width
	}
	return builder.String() + "…"
}

func formatEvidenceTime(raw string) string {
	if raw == "" {
		return ""
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return raw
	}
	return parsed.UTC().Format("2006-01-02 15:04:05 UTC")
}

// --- 绘制 ---

var (
	evidenceColorBackground = color.RGBA{0xff, 0xff, 0xff, 0xff}
	evidenceColorText       = color.RGBA{0x1e, 0x1e, 0x1e, 0xff}
	evidenceColorMuted      = color.RGBA{0x88, 0x88, 0x88, 0xff}
	evidenceColorLineNumber = color.RGBA{0xa0, 0xa0, 0xa0, 0xff}
	evidenceColorDivider    = color.RGBA{0xcc, 0xcc, 0xcc, 0xff}
	evidenceColorTitleBar   = color.RGBA{0xee, 0xee, 0xee, 0xff}
	evidenceColorHighlight  = color.RGBA{0xff, 0xf3, 0xb0, 0xff}
	evidenceColorMark       = color.RGBA{0xd9, 0x48, 0x0f, 0xff}
)

// renderEvidenceImage 渲染证据 PNG；同一 Flow + 同一参数输出字节相同（不写时间戳等元数据）。
func renderEvidenceImage(flow *traffic.Flow, meta evidenceImageMeta, opts evidenceImageOptions) ([]byte, error) {
	fonts, err := loadEvidenceFonts()
	if err != nil {
		return nil, err
	}
	layout, err := layoutEvidenceLines(flow, meta, opts, evidenceTextCells(fonts.cellWidth))
	if err != nil {
		return nil, err
	}
	img, _, err := drawEvidenceLayout(layout, fonts)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.DefaultCompression}).Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

type evidenceCanvas struct {
	img                         *image.RGBA
	fonts                       *evidenceFonts
	regular, bold, fallbackFace font.Face
	buf                         sfnt.Buffer
	missing                     bool
}

func (c *evidenceCanvas) close() {
	for _, face := range []font.Face{c.regular, c.bold, c.fallbackFace} {
		if face != nil {
			_ = face.Close()
		}
	}
}

// drawEvidenceLayout 返回图片与是否出现了缺字形（已在页脚说明）。
func drawEvidenceLayout(layout *evidenceLayout, fonts *evidenceFonts) (*image.RGBA, bool, error) {
	rows := max(len(layout.Request.Rows), len(layout.Response.Rows), 1)
	contentTop := evidenceHeaderHeight + evidenceTitleHeight + 4
	height := contentTop + rows*evidenceRowHeight + evidencePad + evidenceFooterHeight
	canvas := &evidenceCanvas{img: image.NewRGBA(image.Rect(0, 0, evidenceImageWidth, height)), fonts: fonts}
	defer canvas.close()
	options := &opentype.FaceOptions{Size: evidenceFontSize, DPI: 72, Hinting: font.HintingNone}
	var err error
	if canvas.regular, err = opentype.NewFace(fonts.mono, options); err != nil {
		return nil, false, err
	}
	if canvas.bold, err = opentype.NewFace(fonts.monoBold, options); err != nil {
		return nil, false, err
	}
	if fonts.fallback != nil {
		if canvas.fallbackFace, err = opentype.NewFace(fonts.fallback, options); err != nil {
			return nil, false, err
		}
	}
	fill(canvas.img, canvas.img.Bounds(), evidenceColorBackground)

	// 页眉：METHOD URL + 右侧证据 ID。
	rightCells := runeCellsOf(layout.HeaderRight)
	rightX := evidenceImageWidth - evidencePad - int(fixed.Int26_6(rightCells)*fonts.cellWidth>>6)
	canvas.text(evidencePad, 20, layout.Header, nil, evidenceColorText, true)
	canvas.text(rightX, 20, layout.HeaderRight, nil, evidenceColorMuted, false)
	fill(canvas.img, image.Rect(0, evidenceHeaderHeight-1, evidenceImageWidth, evidenceHeaderHeight), evidenceColorDivider)

	for index, column := range []evidenceColumn{layout.Request, layout.Response} {
		left := index * (evidenceColumnWidth + 1)
		right := left + evidenceColumnWidth
		fill(canvas.img, image.Rect(left, evidenceHeaderHeight, right, evidenceHeaderHeight+evidenceTitleHeight), evidenceColorTitleBar)
		canvas.text(left+evidencePad, evidenceHeaderHeight+17, column.Title, nil, evidenceColorText, true)
		gutterRight := left + evidencePad + int(fixed.Int26_6(evidenceGutterCells)*fonts.cellWidth>>6)
		textLeft := gutterRight + evidencePad
		for rowIndex, row := range column.Rows {
			top := contentTop + rowIndex*evidenceRowHeight
			baseline := top + 13
			if row.Highlight {
				fill(canvas.img, image.Rect(left, top, right, top+evidenceRowHeight), evidenceColorHighlight)
			}
			if row.Number != "" {
				numberX := gutterRight - int(fixed.Int26_6(len(row.Number))*fonts.cellWidth>>6)
				canvas.text(numberX, baseline, row.Number, nil, evidenceColorLineNumber, false)
			}
			textColor := evidenceColorText
			if row.Note {
				textColor = evidenceColorMuted
			}
			canvas.text(textLeft, baseline, row.Text, row.Marks, textColor, false)
		}
	}
	// 中间分隔线贯穿标题条与内容。
	fill(canvas.img, image.Rect(evidenceColumnWidth, evidenceHeaderHeight, evidenceColumnWidth+1, height-evidenceFooterHeight), evidenceColorDivider)

	footerTop := height - evidenceFooterHeight
	fill(canvas.img, image.Rect(0, footerTop, evidenceImageWidth, footerTop+1), evidenceColorDivider)
	footer := append([]string(nil), layout.Footer...)
	missing := canvas.missing || canvas.footerMissing(footer)
	if missing {
		footer = append(footer, evidenceMissingGlyph)
	}
	footerCells := int(fixed.I(evidenceImageWidth-2*evidencePad) / fonts.cellWidth)
	canvas.text(evidencePad, footerTop+18, truncateCells(strings.Join(footer, " · "), footerCells), nil, evidenceColorMuted, false)
	return canvas.img, missing, nil
}

func (c *evidenceCanvas) footerMissing(parts []string) bool {
	for _, part := range parts {
		for _, r := range part {
			if c.face(r, false) == nil && r != ' ' {
				return true
			}
		}
	}
	return false
}

func fill(img *image.RGBA, rect image.Rectangle, value color.Color) {
	draw.Draw(img, rect, image.NewUniform(value), image.Point{}, draw.Src)
}

func hasGlyph(f *sfnt.Font, buf *sfnt.Buffer, r rune) bool {
	if f == nil {
		return false
	}
	index, err := f.GlyphIndex(buf, r)
	return err == nil && index != 0
}

// face 逐字形选择：gomono 有字形用 gomono，否则用回退字体；都没有返回 nil。
func (c *evidenceCanvas) face(r rune, bold bool) font.Face {
	if hasGlyph(c.fonts.mono, &c.buf, r) {
		if bold {
			return c.bold
		}
		return c.regular
	}
	if c.fallbackFace != nil && hasGlyph(c.fonts.fallback, &c.buf, r) {
		return c.fallbackFace
	}
	return nil
}

// text 在等宽格上逐字绘制；marks 内的字加粗橙色。
func (c *evidenceCanvas) text(x, baseline int, text string, marks []evidenceSpan, base color.RGBA, bold bool) {
	cell := c.fonts.cellWidth
	dot := fixed.I(x)
	index := 0
	for _, r := range text {
		cells := fixed.Int26_6(runeCells(r))
		width := cells * cell
		marked := false
		for _, span := range marks {
			if index >= span.Start && index < span.End {
				marked = true
				break
			}
		}
		index++
		if r == ' ' {
			dot += width
			continue
		}
		textColor, useBold := base, bold
		if marked {
			textColor, useBold = evidenceColorMark, true
		}
		face := c.face(r, useBold)
		if face == nil {
			c.missing = true
			left := dot.Round() + 1
			right := (dot + width).Round() - 1
			c.box(image.Rect(left, baseline-10, right, baseline+1), textColor)
			dot += width
			continue
		}
		offset := fixed.Int26_6(0)
		if advance, ok := face.GlyphAdvance(r); ok && advance < width {
			offset = (width - advance) / 2
		}
		drawer := font.Drawer{Dst: c.img, Src: image.NewUniform(textColor), Face: face, Dot: fixed.Point26_6{X: dot + offset, Y: fixed.I(baseline)}}
		drawer.DrawString(string(r))
		if useBold && face == c.fallbackFace { // 回退字体无粗体：右移 1px 再画一次。
			drawer.Dot = fixed.Point26_6{X: dot + offset + fixed.I(1), Y: fixed.I(baseline)}
			drawer.DrawString(string(r))
		}
		dot += width
	}
}

// box 画“□”：缺字形时的占位方框（不依赖任何字体）。
func (c *evidenceCanvas) box(rect image.Rectangle, value color.RGBA) {
	if rect.Dx() < 2 || rect.Dy() < 2 {
		return
	}
	fill(c.img, image.Rect(rect.Min.X, rect.Min.Y, rect.Max.X, rect.Min.Y+1), value)
	fill(c.img, image.Rect(rect.Min.X, rect.Max.Y-1, rect.Max.X, rect.Max.Y), value)
	fill(c.img, image.Rect(rect.Min.X, rect.Min.Y, rect.Min.X+1, rect.Max.Y), value)
	fill(c.img, image.Rect(rect.Max.X-1, rect.Min.Y, rect.Max.X, rect.Max.Y), value)
}

// --- API ---

func writeEvidencePNG(w http.ResponseWriter, flowPB []byte, meta evidenceImageMeta, opts evidenceImageOptions) {
	var flow traffic.Flow
	if err := proto.Unmarshal(flowPB, &flow); err != nil {
		writeJSON(w, 422, map[string]string{"error": "Flow原件无法解码"})
		return
	}
	data, err := renderEvidenceImage(&flow, meta, opts)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "渲染证据图片失败"})
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(200)
	_, _ = w.Write(data)
}

// handleEvidenceImage：GET /api/webapi/tasks/{id}/evidence/{evidence_id}/image.png?mark=&max_lines=&mask=
func (c *Capability) handleEvidenceImage(w http.ResponseWriter, r *http.Request) {
	taskID, err := taskIDFromPath(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	opts, err := evidenceImageOptionsFromQuery(r.URL.Query())
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	model, err := c.store.evidenceByID(r.Context(), taskID, r.PathValue("evidence_id"))
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "读取证据失败"})
		return
	}
	if model == nil {
		writeJSON(w, 404, map[string]string{"error": "任务内证据不存在"})
		return
	}
	writeEvidencePNG(w, model.FlowPB, requestEvidenceImageMeta(*model), opts)
}

func requestEvidenceImageMeta(model requestModel) evidenceImageMeta {
	meta := evidenceImageMeta{Label: "证据", ID: model.ID, CreatedAt: model.CreatedAt, Status: model.Status, Reason: model.Reason, Sent: nullableBool(model.Sent)}
	// capture_truncated：响应 body 只保存前 maxCaptureBytes。preview_truncated 只涉及工具返回的文本预览，Flow 原件未截断。
	if model.CaptureTruncated {
		meta.ResponseTruncated = &evidenceTruncation{Kept: maxCaptureBytes}
		var flow traffic.Flow
		if proto.Unmarshal(model.FlowPB, &flow) == nil && flow.GetResponse() != nil {
			meta.ResponseTruncated.Kept = int64(len(flow.GetResponse().GetBody()))
		}
	}
	return meta
}

// handleEgressImage：GET /api/webapi/tasks/{id}/egress/{flow_id}/image.png（同参数）。
func (c *Capability) handleEgressImage(w http.ResponseWriter, r *http.Request) {
	taskID, ok := c.egressTaskFromPath(w, r)
	if !ok {
		return
	}
	opts, err := evidenceImageOptionsFromQuery(r.URL.Query())
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	model, err := c.store.egressFlowByID(r.Context(), taskID, r.PathValue("flow_id"))
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "读取出口记录失败"})
		return
	}
	if model == nil {
		writeJSON(w, 404, map[string]string{"error": "任务内出口记录不存在"})
		return
	}
	writeEvidencePNG(w, model.FlowPB, egressEvidenceImageMeta(*model), opts)
}

func egressEvidenceImageMeta(model egressFlowModel) evidenceImageMeta {
	meta := evidenceImageMeta{Label: "出口记录", ID: model.ID, CreatedAt: model.CreatedAt, Status: model.Decision, Reason: model.Error, Sent: nullableBool(model.Sent)}
	if model.PreviewTruncated {
		var flow traffic.Flow
		if proto.Unmarshal(model.FlowPB, &flow) == nil {
			if kept := int64(len(flow.GetRequest().GetBody())); model.RequestBytes > kept {
				meta.RequestTruncated = &evidenceTruncation{Kept: kept, Observed: model.RequestBytes}
			}
			if kept := int64(len(flow.GetResponse().GetBody())); model.ResponseBytes > kept {
				meta.ResponseTruncated = &evidenceTruncation{Kept: kept, Observed: model.ResponseBytes}
			}
		}
	}
	return meta
}
