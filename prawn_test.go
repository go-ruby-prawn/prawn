// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-ruby-prawn/prawn authors

package prawn

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rsc.io/pdf"
)

// ---- helpers ---------------------------------------------------------------

func mustRender(t *testing.T, d *Document) []byte {
	t.Helper()
	b, err := d.Render()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !bytes.HasPrefix(b, []byte("%PDF-")) {
		t.Fatal("missing %PDF header")
	}
	if !bytes.Contains(b, []byte("%%EOF")) {
		t.Fatal("missing EOF marker")
	}
	return b
}

func parse(t *testing.T, b []byte) *pdf.Reader {
	t.Helper()
	r, err := pdf.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatalf("pdf parse: %v", err)
	}
	return r
}

func pngBytes(t *testing.T, withAlpha bool) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 4, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 4; x++ {
			a := uint8(255)
			if withAlpha && x == 0 {
				a = 100
			}
			img.Set(x, y, color.NRGBA{R: uint8(x * 60), G: uint8(y * 80), B: 20, A: a})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func jpegBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 6))
	for y := 0; y < 6; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 30), G: uint8(y * 40), B: 90, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func fixedClock() func() {
	prev := SetClock(func() time.Time { return time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC) })
	return func() { SetClock(prev) }
}

// ---- document & pages ------------------------------------------------------

func TestNewDefaults(t *testing.T) {
	d := New(Options{})
	if d.PageWidth() != 612 || d.PageHeight() != 792 {
		t.Fatalf("size %v x %v", d.PageWidth(), d.PageHeight())
	}
	b := d.Bounds()
	if b.Width != 540 || b.Height != 720 || b.Left != 36 || b.Bottom != 36 {
		t.Fatalf("bounds %+v", b)
	}
	if d.Cursor() != 720 {
		t.Fatalf("cursor %v", d.Cursor())
	}
	if d.PageCount() != 1 || d.PageNumber() != 1 {
		t.Fatal("page count")
	}
	mustRender(t, d)
}

func TestPageSizesAndLayout(t *testing.T) {
	m := 10.0
	margins := [4]float64{1, 2, 3, 4}
	d := New(Options{PageSize: "A4", PageLayout: "landscape", Margin: &m})
	if d.PageWidth() <= d.PageHeight() {
		t.Fatal("landscape swap failed")
	}
	d2 := New(Options{PageWidth: 200, PageHeight: 100, Margins: &margins})
	if d2.PageWidth() != 200 || d2.PageHeight() != 100 {
		t.Fatal("custom size")
	}
	b := d2.Bounds()
	if b.Left != 4 || b.Bottom != 3 {
		t.Fatalf("margins %+v", b)
	}
}

func TestInvalidPageOptions(t *testing.T) {
	d := New(Options{PageLayout: "sideways"})
	if _, err := d.Render(); !errors.Is(err, ErrInvalidPageLayout) {
		t.Fatalf("want layout err, got %v", err)
	}
	d2 := New(Options{PageSize: "NOPE"})
	if err := d2.Error(); err == nil {
		t.Fatal("want unknown size error")
	}
	if _, err := d2.Render(); err == nil {
		t.Fatal("render should surface error")
	}
}

func TestGenerateFamily(t *testing.T) {
	defer fixedClock()()
	dir := t.TempDir()
	p := filepath.Join(dir, "a.pdf")
	if err := Generate(p, func(d *Document) error { d.Text("hi", nil); return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
	// block error path
	if err := Generate(p, func(d *Document) error { return errors.New("boom") }); err == nil {
		t.Fatal("want block error")
	}
	// GenerateTo
	var buf bytes.Buffer
	if err := GenerateTo(&buf, func(d *Document) error { d.Text("hi", nil); return nil }); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		t.Fatal("no output")
	}
	if err := GenerateTo(&buf, func(d *Document) error { return errors.New("x") }); err == nil {
		t.Fatal("want error")
	}
	// GenerateTo render error
	if err := GenerateTo(&buf, func(d *Document) error { d.FillColor("bad"); return nil }); err == nil {
		t.Fatal("want render error")
	}
	// GenerateWith
	p2 := filepath.Join(dir, "b.pdf")
	if err := GenerateWith(p2, Options{PageSize: "LEGAL"}, func(d *Document) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := GenerateWith(p2, Options{}, func(d *Document) error { return errors.New("x") }); err == nil {
		t.Fatal("want error")
	}
}

func TestGenerateToWriteError(t *testing.T) {
	err := GenerateTo(errWriter{}, func(d *Document) error { d.Text("x", nil); return nil })
	if err == nil {
		t.Fatal("want write error")
	}
}

type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestRenderFileError(t *testing.T) {
	d := New(Options{})
	if err := d.RenderFile("/no/such/dir/x.pdf"); err == nil {
		t.Fatal("want write error")
	}
	d2 := New(Options{PageLayout: "bad"})
	if err := d2.RenderFile(filepath.Join(t.TempDir(), "x.pdf")); err == nil {
		t.Fatal("want render error")
	}
}

func TestCursorMovement(t *testing.T) {
	d := New(Options{})
	d.MoveDown(10)
	d.MoveUp(4)
	if d.Cursor() != 714 {
		t.Fatalf("cursor %v", d.Cursor())
	}
	d.MoveCursorTo(100)
	if d.Cursor() != 100 {
		t.Fatal("cursor to")
	}
	seq := []string{}
	d.Pad(5, func() { seq = append(seq, "pad") })
	d.PadTop(5, func() { seq = append(seq, "top") })
	d.PadBottom(5, func() { seq = append(seq, "bottom") })
	if len(seq) != 3 {
		t.Fatal("pad blocks")
	}
}

func TestStartNewPage(t *testing.T) {
	defer fixedClock()()
	d := New(Options{})
	d.FillColor("112233")
	d.StartNewPage()
	if d.PageCount() != 2 {
		t.Fatal("page count")
	}
	if d.Cursor() != 720 {
		t.Fatal("cursor reset")
	}
	b := mustRender(t, d)
	if parse(t, b).NumPage() != 2 {
		t.Fatal("pages")
	}
}

func TestSkipPageCreation(t *testing.T) {
	d := New(Options{SkipPageCreation: true})
	if d.PageCount() != 0 {
		t.Fatalf("pages %d", d.PageCount())
	}
	d.StartNewPage()
	d.Text("x", nil)
	mustRender(t, d)
}

func TestAFMBaseNames(t *testing.T) {
	cases := []struct {
		fam   string
		style Style
		want  string
	}{
		{"Courier", StyleNormal, "Courier"},
		{"Courier", StyleBold, "Courier-Bold"},
		{"Courier", StyleItalic, "Courier-Oblique"},
		{"Courier", StyleBoldItalic, "Courier-BoldOblique"},
		{"Times", StyleItalic, "Times-Italic"},
		{"Times-Roman", StyleBoldItalic, "Times-BoldItalic"},
		{"Symbol", StyleNormal, "Symbol"},
		{"ZapfDingbats", StyleNormal, "ZapfDingbats"},
		{"Helvetica", StyleBold, "Helvetica-Bold"},
		{"arial", StyleItalic, "Helvetica-Oblique"},
	}
	for _, c := range cases {
		if got := afmBaseName(c.fam, c.style); got != c.want {
			t.Errorf("afmBaseName(%q,%v)=%q want %q", c.fam, c.style, got, c.want)
		}
	}
	if !isAFMFamily("Times") || isAFMFamily("Nonexistent") {
		t.Fatal("isAFMFamily")
	}
}

// ---- text ------------------------------------------------------------------

func TestFontSelection(t *testing.T) {
	d := New(Options{})
	d.Font("Times", StyleBold)
	if d.FontFamily() != "Times-Bold" || d.FontStyle() != StyleBold {
		t.Fatalf("font %s %v", d.FontFamily(), d.FontStyle())
	}
	d.Font("Times", StyleItalic)
	if d.FontStyle() != StyleItalic {
		t.Fatal("italic")
	}
	d.Font("Times", StyleBoldItalic)
	if d.FontStyle() != StyleBoldItalic {
		t.Fatal("bolditalic")
	}
	d.Font("Times", StyleNormal)
	if d.FontStyle() != StyleNormal {
		t.Fatal("normal")
	}
	d.Font("Nope", StyleNormal)
	if !errors.Is(d.Error(), ErrUnknownFont) {
		t.Fatal("unknown font")
	}
}

func TestFontSizeLeadingKerning(t *testing.T) {
	d := New(Options{})
	d.FontSize(18)
	if d.FontSizeValue() != 18 {
		t.Fatal("size")
	}
	d.Leading(3)
	if d.LeadingValue() != 3 {
		t.Fatal("leading")
	}
	d.SetKerning(false)
	if d.KerningEnabled() {
		t.Fatal("kerning")
	}
	if numToken(12) != "12" || numToken(12.5) != "12.5" {
		t.Fatalf("numToken %q %q", numToken(12), numToken(12.5))
	}
}

func TestWidthOfString(t *testing.T) {
	d := New(Options{})
	w := d.WidthOfString("Hello")
	if w <= 0 {
		t.Fatal("width")
	}
	// invalid winansi -> width 0
	if d.WidthOfString("") != 0 {
		t.Fatal("unmappable width should be 0")
	}
}

func TestTextRendering(t *testing.T) {
	defer fixedClock()()
	d := New(Options{})
	d.Text("Left aligned line", nil)
	d.Text("Centered", &TextOptions{Align: AlignCenter})
	d.Text("Right", &TextOptions{Align: AlignRight})
	d.Text("Justified text here", &TextOptions{Align: AlignJustify})
	d.Text("With color and size", &TextOptions{Size: 20, Color: "884422", Leading: 2})
	b := mustRender(t, d)
	r := parse(t, b)
	if len(r.Page(1).Content().Text) == 0 {
		t.Fatal("no text")
	}
}

func TestTextPagination(t *testing.T) {
	defer fixedClock()()
	d := New(Options{})
	long := strings.Repeat("word ", 2000)
	d.Text(long, nil)
	if d.PageCount() < 2 {
		t.Fatalf("expected pagination, pages=%d", d.PageCount())
	}
	mustRender(t, d)
}

func TestTextInvalidUTF8(t *testing.T) {
	d := New(Options{})
	d.Text("\xff\xfe", nil)
	if !errors.Is(d.Error(), ErrIncompatibleStringEncoding) {
		t.Fatal("want encoding error")
	}
	d2 := New(Options{})
	d2.DrawText("\xff\xfe", 0, 0, nil)
	if !errors.Is(d2.Error(), ErrIncompatibleStringEncoding) {
		t.Fatal("drawtext encoding")
	}
}

func TestTextUnmappableRune(t *testing.T) {
	d := New(Options{})
	d.Text("emoji \U0001F600", nil) // valid utf8 but not cp1252
	if !errors.Is(d.Error(), ErrIncompatibleStringEncoding) {
		t.Fatal("want incompatible encoding")
	}
}

func TestDrawTextOptions(t *testing.T) {
	defer fixedClock()()
	d := New(Options{})
	d.DrawText("abc", 10, 700, &TextOptions{Size: 16, Font: "Courier", StyleSet: true, Style: StyleBold, Color: "ff0000", KerningSet: true, Kerning: false})
	mustRender(t, d)
}

func TestTextBox(t *testing.T) {
	defer fixedClock()()
	d := New(Options{})
	// width <= 0 (on a throwaway doc: this sets a sticky error)
	dw := New(Options{})
	if dw.TextBox("x", TextBoxOptions{Width: 0}) != "x" {
		t.Fatal("bad width should return input")
	}
	if !errors.Is(dw.Error(), ErrCannotFit) {
		t.Fatal("want cannot fit for zero width")
	}
	// fits fully
	rest := d.TextBox("short", TextBoxOptions{X: 0, Y: 700, Width: 200, Height: 100})
	if rest != "" {
		t.Fatalf("want no overflow, got %q", rest)
	}
	// truncate overflow
	long := strings.Repeat("word ", 200)
	rest = d.TextBox(long, TextBoxOptions{X: 0, Y: 700, Width: 100, Height: 30})
	if rest == "" {
		t.Fatal("want truncated overflow")
	}
	// expand
	rest = d.TextBox(long, TextBoxOptions{X: 0, Y: 700, Width: 100, Height: 30, Overflow: OverflowExpand})
	if rest != "" {
		t.Fatal("expand should print all")
	}
	// error overflow
	d3 := New(Options{})
	d3.TextBox(long, TextBoxOptions{X: 0, Y: 700, Width: 100, Height: 30, Overflow: OverflowError})
	if !errors.Is(d3.Error(), ErrCannotFit) {
		t.Fatal("want cannot fit")
	}
	// invalid utf8
	d4 := New(Options{})
	if d4.TextBox("\xff", TextBoxOptions{Width: 100}) != "\xff" {
		t.Fatal("invalid utf8 returns input")
	}
	// unmappable
	d5 := New(Options{})
	if got := d5.TextBox("\U0001F600", TextBoxOptions{Width: 100}); got == "" {
		t.Fatal("unmappable returns input")
	}
	// with font override
	d.TextBox("styled", TextBoxOptions{Width: 200, Font: "Times", StyleSet: true, Style: StyleBold, Size: 14, Color: "00ff00", Align: AlignCenter})
	mustRender(t, d)
}

func TestWrapEdgeCases(t *testing.T) {
	d := New(Options{})
	lines := d.wrapText("", 100, 12, true)
	if len(lines) != 1 || lines[0] != "" {
		t.Fatal("empty para")
	}
	// single word wider than width still emitted
	lines = d.wrapText("supercalifragilistic", 5, 12, true)
	if len(lines) == 0 {
		t.Fatal("long word")
	}
	lines = d.wrapText("a\nb", 100, 12, true)
	if len(lines) != 2 {
		t.Fatal("newlines")
	}
}

// ---- color -----------------------------------------------------------------

func TestColors(t *testing.T) {
	defer fixedClock()()
	d := New(Options{})
	d.FillColor("#AABBCC")
	if d.FillColorValue() != "AABBCC" {
		t.Fatalf("fill %s", d.FillColorValue())
	}
	d.StrokeColor("102030")
	if d.StrokeColorValue() != "102030" {
		t.Fatal("stroke")
	}
	d.FillColorCMYK(0, 100, 50, 0)
	if d.FillColorValue() == "" {
		t.Fatal("cmyk hex")
	}
	d.StrokeColorCMYK(10, 20, 30, 40)
	mustRender(t, d)
	dbad := New(Options{})
	dbad.FillColor("bad")
	if dbad.Error() == nil {
		t.Fatal("bad fill")
	}
	d2 := New(Options{})
	d2.StrokeColor("nothex")
	if d2.Error() == nil {
		t.Fatal("bad stroke")
	}
}

func TestParseHexColor(t *testing.T) {
	if _, err := parseHexColor("12345"); err == nil {
		t.Fatal("short")
	}
	if _, err := parseHexColor("gggggg"); err == nil {
		t.Fatal("nonhex")
	}
}

// ---- graphics --------------------------------------------------------------

func TestGraphicsAll(t *testing.T) {
	defer fixedClock()()
	d := New(Options{})
	d.LineWidth(2)
	if d.LineWidthValue() != 2 {
		t.Fatal("lw")
	}
	d.CapStyle(1)
	d.JoinStyle(2)
	d.Dash(3, 2, 1)
	d.Undash()
	d.MoveTo(0, 0)
	d.LineTo(10, 10)
	d.CurveTo(20, 20, 12, 12, 18, 18)
	d.ClosePath()
	d.Stroke()
	d.Rectangle(0, 100, 50, 20)
	d.Fill()
	d.Line(0, 0, 5, 5)
	d.FillAndStroke()
	d.CloseAndStroke()
	d.Polygon([2]float64{0, 0}, [2]float64{10, 0}, [2]float64{5, 8})
	d.Stroke()
	d.Polygon() // empty no-op
	d.StrokeRectangle(1, 2, 3, 4)
	d.FillRectangle(1, 2, 3, 4)
	d.StrokeLine(0, 0, 1, 1)
	d.StrokeCircle(50, 50, 10)
	d.FillCircle(50, 50, 10)
	d.StrokeEllipse(50, 50, 10, 5)
	d.FillEllipse(50, 50, 10, 5)
	d.FillPolygon([2]float64{0, 0}, [2]float64{1, 0}, [2]float64{0, 1})
	d.StrokePolygon([2]float64{0, 0}, [2]float64{1, 0}, [2]float64{0, 1})
	d.StrokeBounds()
	mustRender(t, d)
}

// ---- transformations -------------------------------------------------------

func TestTransformations(t *testing.T) {
	defer fixedClock()()
	d := New(Options{})
	d.Rotate(45, func() { d.Text("r", nil) })
	d.RotateAbout(30, 100, 100, func() { d.Text("ra", nil) })
	d.Scale(1.5, func() { d.Text("s", nil) })
	d.ScaleAbout(2, 50, 50, func() { d.Text("sa", nil) })
	d.Translate(5, 5, func() { d.Text("t", nil) })
	d.TransformationMatrix(1, 0, 0, 1, 0, 0, nil) // no block
	d.SaveGraphicsState()
	d.RestoreGraphicsState()
	d.Transparency(0.5, 0.8, func() { d.Text("alpha", nil) })
	mustRender(t, d)
}

func TestRestoreWithoutSave(t *testing.T) {
	d := New(Options{})
	d.RestoreGraphicsState()
	if !errors.Is(d.Error(), ErrEmptyGraphicStateStack) {
		t.Fatal("want empty stack")
	}
}

func TestFinishLeftoverSaves(t *testing.T) {
	defer fixedClock()()
	d := New(Options{})
	d.SaveGraphicsState()
	d.SaveGraphicsState()
	mustRender(t, d) // finishPage must close leftover q's
}

// ---- bounding box / grid / repeaters --------------------------------------

func TestBoundingBoxAndGrid(t *testing.T) {
	defer fixedClock()()
	d := New(Options{})
	d.BoundingBox(50, 700, 200, 100, func() {
		d.Text("in box", nil)
		if d.Bounds().Width != 200 {
			t.Fatal("nested bounds width")
		}
	})
	if d.Cursor() != 600 {
		t.Fatalf("cursor after box %v", d.Cursor())
	}
	g := d.DefineGrid(3, 4, 10)
	if g.ColumnWidth() <= 0 || g.RowHeight() <= 0 {
		t.Fatal("grid dims")
	}
	g.Box(0, 0, func() { d.Text("cell", nil) })
	g.Box(1, 2, func() { d.Text("cell", nil) })
	mustRender(t, d)
}

func TestRepeatersAndNumbering(t *testing.T) {
	defer fixedClock()()
	d := New(Options{})
	d.Text("p1", nil)
	d.StartNewPage()
	d.Text("p2", nil)
	d.StartNewPage()
	d.Text("p3", nil)
	d.Repeat(func(p, total int) bool { return p%2 == 1 }, func() {
		d.DrawText("odd", 500, 20, &TextOptions{Size: 8})
	})
	d.NumberPages("<page> / <total>", 250, 20, &TextOptions{Size: 9})
	seen := 0
	d.Repeat(func(p, total int) bool { return true }, func() {
		if d.RepeatPageNumber() >= 1 && d.RepeatPageCount() == 3 {
			seen++
		}
	})
	b := mustRender(t, d)
	if parse(t, b).NumPage() != 3 {
		t.Fatal("pages")
	}
	if seen != 3 {
		t.Fatalf("repeat context seen=%d", seen)
	}
}

func TestReplacePlaceholders(t *testing.T) {
	if replacePlaceholders("<page>/<total> plain", 2, 5) != "2/5 plain" {
		t.Fatal("placeholders")
	}
	if replacePlaceholders("<pag", 1, 1) != "<pag" {
		t.Fatal("partial")
	}
}

func TestRunRepeatersNone(t *testing.T) {
	d := New(Options{})
	d.Text("x", nil)
	d.runRepeaters() // no repeaters -> early return
	mustRender(t, d)
}

// ---- table -----------------------------------------------------------------

func TestTable(t *testing.T) {
	defer fixedClock()()
	d := New(Options{})
	if d.Table(nil, TableOptions{}).Width != 0 {
		t.Fatal("empty table")
	}
	if d.Table([][]string{{}}, TableOptions{}).Width != 0 {
		t.Fatal("zero cols")
	}
	res := d.Table([][]string{
		{"Item", "Qty", "Price"},
		{"Widget", "3", "9.99"},
		{"", "1", "19.99"},
	}, TableOptions{Header: true})
	if res.Width == 0 || len(res.ColumnWidths) != 3 {
		t.Fatal("table result")
	}
	d.Table([][]string{{"a", "b"}}, TableOptions{
		ColumnWidths: []float64{100}, BorderWidth: -1, FontSize: 10, RowHeight: 30, CellPadding: 3, AtSet: true, AtX: 10, AtY: 500,
	})
	d.Font("Times", StyleNormal)
	d.Table([][]string{{"x"}}, TableOptions{Header: true})
	d.Font("Courier", StyleNormal)
	d.Table([][]string{{"x"}}, TableOptions{Header: true})
	mustRender(t, d)
}

func TestFoldHelpers(t *testing.T) {
	if !containsFold("Helvetica-Bold", "bold") {
		t.Fatal("fold contains")
	}
	if equalFold("ab", "abc") {
		t.Fatal("len")
	}
	if !equalFold("AbC", "aBc") {
		t.Fatal("fold eq")
	}
	if indexFold("xyz", "q") != -1 {
		t.Fatal("no match")
	}
}

// ---- formatted text --------------------------------------------------------

func TestFormattedText(t *testing.T) {
	defer fixedClock()()
	d := New(Options{})
	d.FormattedText([]FormattedFragment{
		{Text: "Bold ", Bold: true},
		{Text: "italic ", Italic: true},
		{Text: "big ", Size: 20},
		{Text: "colored ", Color: "ff0000"},
		{Text: "times", Font: "Times", Bold: true, Italic: true},
	}, nil)
	// wrapping
	d.FormattedText([]FormattedFragment{{Text: strings.Repeat("word ", 200)}}, nil)
	mustRender(t, d)
}

func TestFragmentStyle(t *testing.T) {
	cases := []struct {
		f    FormattedFragment
		want Style
	}{
		{FormattedFragment{Bold: true, Italic: true}, StyleBoldItalic},
		{FormattedFragment{Bold: true}, StyleBold},
		{FormattedFragment{Italic: true}, StyleItalic},
		{FormattedFragment{}, StyleNormal},
	}
	for _, c := range cases {
		if c.f.style() != c.want {
			t.Errorf("style %+v", c.f)
		}
	}
}

func TestTextInline(t *testing.T) {
	defer fixedClock()()
	d := New(Options{})
	d.TextInline("plain <b>bold</b> and <i>italic</i> and <strong>s</strong> <em>e</em>", nil)
	d.TextInline(`<color rgb="ff0000">red</color> normal`, nil)
	d.TextInline("unclosed <b>bold no end", nil)
	d.TextInline("bad <tag with no close bracket", nil)
	d.TextInline("</b></i></color> stray closers", nil)
	mustRender(t, d)
}

func TestParseInline(t *testing.T) {
	frags := parseInline("a<b>b</b>c")
	if len(frags) != 3 {
		t.Fatalf("frags %d", len(frags))
	}
	if !frags[1].Bold {
		t.Fatal("bold frag")
	}
	if parseColorTag(`color rgb="00ff00"`) != "00ff00" {
		t.Fatal("color tag")
	}
	if parseColorTag("color") != "" {
		t.Fatal("no rgb")
	}
	if parseColorTag(`color rgb="xx"`) != "" {
		t.Fatal("short rgb")
	}
}

// ---- images ----------------------------------------------------------------

func TestImages(t *testing.T) {
	defer fixedClock()()
	dir := t.TempDir()
	pngP := filepath.Join(dir, "img.png")
	os.WriteFile(pngP, pngBytes(t, false), 0o644)
	alphaP := filepath.Join(dir, "alpha.png")
	os.WriteFile(alphaP, pngBytes(t, true), 0o644)
	jpgP := filepath.Join(dir, "img.jpg")
	os.WriteFile(jpgP, jpegBytes(t), 0o644)

	d := New(Options{})
	r := d.Image(pngP, ImageOptions{})
	if r.Width == 0 {
		t.Fatal("png size")
	}
	d.Image(alphaP, ImageOptions{AtX: 100, AtY: 400, AtSet: true})
	d.Image(jpgP, ImageOptions{Width: 100})
	d.Image(pngP, ImageOptions{Height: 50})
	d.Image(pngP, ImageOptions{FitW: 80, FitH: 80})
	d.Image(pngP, ImageOptions{Width: 40, Height: 40})
	d.Image(pngP, ImageOptions{}) // cache hit
	b := mustRender(t, d)
	r2 := parse(t, b)
	xobj := r2.Page(1).Resources().Key("XObject")
	if xobj.Kind() != pdf.Dict {
		t.Fatal("no xobject")
	}
	mustRender(t, d)
}

func TestImageReaderAndErrors(t *testing.T) {
	defer fixedClock()()
	d := New(Options{})
	d.ImageReader(bytes.NewReader(pngBytes(t, false)), "png", ImageOptions{})
	d.ImageReader(bytes.NewReader(jpegBytes(t)), "jpeg", ImageOptions{})
	mustRender(t, d)

	d2 := New(Options{})
	d2.Image("/nonexistent.png", ImageOptions{})
	if d2.Error() == nil {
		t.Fatal("missing file")
	}
	d3 := New(Options{})
	d3.ImageReader(bytes.NewReader([]byte("gif")), "gif", ImageOptions{})
	if !errors.Is(d3.Error(), ErrUnsupportedImageType) {
		t.Fatal("unsupported type")
	}
	d4 := New(Options{})
	d4.ImageReader(bytes.NewReader([]byte("notpng")), "png", ImageOptions{})
	if d4.Error() == nil {
		t.Fatal("bad png")
	}
	d5 := New(Options{})
	d5.ImageReader(bytes.NewReader([]byte{0xff, 0xd8, 0x00}), "jpg", ImageOptions{})
	if d5.Error() == nil {
		t.Fatal("bad jpeg")
	}
	d6 := New(Options{})
	d6.ImageReader(errReader{}, "png", ImageOptions{})
	if d6.Error() == nil {
		t.Fatal("read error")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("io") }

func TestImageTypeFromName(t *testing.T) {
	if imageTypeFromName("a.PNG") != "png" || imageTypeFromName("a.JPEG") != "jpg" ||
		imageTypeFromName("a.jpg") != "jpg" || imageTypeFromName("a.gif") != "" {
		t.Fatal("type from name")
	}
}

func TestJPEGInfo(t *testing.T) {
	if _, _, _, err := jpegInfo([]byte{0, 1}); err == nil {
		t.Fatal("not jpeg")
	}
	if _, _, _, err := jpegInfo([]byte{0xff, 0xd8, 0xff, 0xd9}); err == nil {
		t.Fatal("no frame")
	}
}

func TestResolveImageSize(t *testing.T) {
	if w, _ := resolveImageSize(100, 50, ImageOptions{}); w != 100 {
		t.Fatal("natural")
	}
	if w, h := resolveImageSize(100, 50, ImageOptions{FitW: 50, FitH: 50}); w != 50 || h != 25 {
		t.Fatalf("fit %v %v", w, h)
	}
	if _, h := resolveImageSize(100, 50, ImageOptions{Width: 200}); h != 100 {
		t.Fatal("width scale")
	}
	if w, _ := resolveImageSize(100, 50, ImageOptions{Height: 100}); w != 200 {
		t.Fatal("height scale")
	}
}

// ---- TTF -------------------------------------------------------------------

func loadGoFont(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "goregular.ttf"))
	if err != nil {
		t.Skip("no test TTF")
	}
	return b
}

func TestTTFEmbedding(t *testing.T) {
	defer fixedClock()()
	d := New(Options{})
	ttf := loadGoFont(t)
	if err := d.RegisterFontTTF("GoRegular", ttf); err != nil {
		t.Fatal(err)
	}
	d.Font("GoRegular", StyleNormal)
	if d.FontFamily() != "GoRegular" {
		t.Fatalf("family %s", d.FontFamily())
	}
	d.Text("Accented: café résumé naïve ñ — em dash", nil)
	d.MoveDown(20)
	if d.WidthOfString("Hello") <= 0 {
		t.Fatal("ttf width")
	}
	b := mustRender(t, d)
	r := parse(t, b)
	// verify a Type0 font and FontFile2 exist
	if !bytes.Contains(b, []byte("/Type0")) || !bytes.Contains(b, []byte("/CIDFontType2")) {
		t.Fatal("no composite font")
	}
	if !bytes.Contains(b, []byte("FontFile2")) {
		t.Fatal("no embedded font file")
	}
	if !bytes.Contains(b, []byte("/ToUnicode")) {
		t.Fatal("no ToUnicode")
	}
	_ = r
}

func TestTTFInvalid(t *testing.T) {
	d := New(Options{})
	if err := d.RegisterFontTTF("bad", []byte("xx")); err == nil {
		t.Fatal("truncated")
	}
	// missing tables: a minimal offset table with 0 tables
	hdr := []byte{0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	if err := d.RegisterFontTTF("empty", hdr); err == nil {
		t.Fatal("missing tables")
	}
}

func TestGidBytes(t *testing.T) {
	b := gidBytes([]uint16{1, 258})
	if len(b) != 4 || b[0] != 0 || b[1] != 1 || b[2] != 1 || b[3] != 2 {
		t.Fatalf("gidBytes %v", b)
	}
}

func TestSanitizeName(t *testing.T) {
	if sanitizeName("Go Regular/(x)") != "GoRegularx" {
		t.Fatalf("sanitize %q", sanitizeName("Go Regular/(x)"))
	}
	if sanitizeName("   ") != "Embedded" {
		t.Fatal("empty sanitize")
	}
}

// ---- cmap subtable parsers (white-box, crafted inputs) ---------------------

func TestCmapFormats(t *testing.T) {
	tf := &ttfFont{cmap: map[rune]uint16{}}

	// format 6: first=65, count=2 -> gids 10,11
	f6 := make([]byte, 14)
	be16put(f6[0:], 6)
	be16put(f6[6:], 65)
	be16put(f6[8:], 2)
	be16put(f6[10:], 10)
	be16put(f6[12:], 11)
	tf.parseCmap6(f6)
	if tf.cmap['A'] != 10 || tf.cmap['B'] != 11 {
		t.Fatal("cmap6")
	}

	// format 0: 262 bytes, byte i -> glyph
	f0 := make([]byte, 262)
	f0[6+66] = 42
	tf.parseCmap0(f0)
	if tf.cmap['B'] != 42 {
		t.Fatal("cmap0")
	}

	// format 12: one group 0x41..0x42 -> startGID 100
	f12 := make([]byte, 16+12)
	be32put(f12[12:], 1)
	be32put(f12[16:], 0x41)
	be32put(f12[20:], 0x42)
	be32put(f12[24:], 100)
	tf.parseCmap12(f12)
	if tf.cmap['A'] != 100 || tf.cmap['B'] != 101 {
		t.Fatal("cmap12")
	}
}

func be16put(b []byte, v uint16) { b[0] = byte(v >> 8); b[1] = byte(v) }
func be32put(b []byte, v uint32) {
	b[0] = byte(v >> 24)
	b[1] = byte(v >> 16)
	b[2] = byte(v >> 8)
	b[3] = byte(v)
}

func TestTableChecksum(t *testing.T) {
	if tableChecksum([]byte{0, 0, 0, 1}) != 1 {
		t.Fatal("aligned")
	}
	if tableChecksum([]byte{0, 0, 0, 1, 2}) != 1+(2<<24) {
		t.Fatalf("tail %d", tableChecksum([]byte{0, 0, 0, 1, 2}))
	}
}

// ---- pdfcore serialization -------------------------------------------------

func TestFormatReal(t *testing.T) {
	cases := map[float64]string{
		2.0: "2", 738.768: "738.768", 0.5: "0.5", -0.0: "0", 200.0: "200", 0.86603: "0.86603",
	}
	for in, want := range cases {
		if got := formatReal(in); got != want {
			t.Errorf("formatReal(%v)=%q want %q", in, got, want)
		}
	}
}

func TestSerializeTypes(t *testing.T) {
	var buf bytes.Buffer
	serializeObject(&buf, nil)
	serializeObject(&buf, true)
	serializeObject(&buf, false)
	serializeObject(&buf, 42)
	serializeObject(&buf, 1.5)
	serializeObject(&buf, pdfName("Na#me"))
	serializeObject(&buf, pdfRef{7})
	serializeObject(&buf, pdfLiteral("a(b)\\c\r"))
	serializeObject(&buf, pdfHex([]byte{0xDE, 0xAD}))
	serializeObject(&buf, pdfArray{1, pdfName("X")})
	serializeObject(&buf, pdfDict{"B": 1, "A": 2})
	if buf.Len() == 0 {
		t.Fatal("empty")
	}
}

func TestSerializePanic(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("want panic")
		}
	}()
	var buf bytes.Buffer
	serializeObject(&buf, struct{}{})
}

func TestContentOpPanic(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("want panic")
		}
	}()
	c := &content{}
	c.op(struct{}{})
}

func TestPdfDateTZ(t *testing.T) {
	tm := time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("x", 3600))
	got := pdfDate(tm)
	if !strings.HasPrefix(got, "D:20260102030405+01'00'") {
		t.Fatalf("date %q", got)
	}
}

func TestCompressStreams(t *testing.T) {
	defer fixedClock()()
	d := New(Options{CompressStreams: true})
	d.Text("compressed content", nil)
	b := mustRender(t, d)
	if !bytes.Contains(b, []byte("FlateDecode")) {
		t.Fatal("no flate")
	}
	parse(t, b)
}

func TestRenderIdempotent(t *testing.T) {
	defer fixedClock()()
	d := New(Options{})
	d.Text("x", nil)
	b1 := mustRender(t, d)
	b2, err := d.Render() // second render: finalized guard
	if err != nil {
		t.Fatal(err)
	}
	if len(b1) != len(b2) {
		t.Fatal("non-idempotent render")
	}
}

func TestResourceDictHelper(t *testing.T) {
	if resourceDict(map[string]bool{}, nil) != nil {
		t.Fatal("empty -> nil")
	}
	if resourceDict(map[string]bool{"F1.0": true}, map[string]pdfRef{}) != nil {
		t.Fatal("no refs -> nil")
	}
}

// ---- afm runtime -----------------------------------------------------------

func TestEncodeWinAnsi(t *testing.T) {
	b, err := encodeWinAnsi("AZ")
	if err != nil || b[0] != 'A' {
		t.Fatal("ascii")
	}
	b, err = encodeWinAnsi("é") // latin1 0xE9
	if err != nil || b[0] != 0xE9 {
		t.Fatalf("latin1 %v", b)
	}
	b, err = encodeWinAnsi("€") // cp1252 0x80
	if err != nil || b[0] != 0x80 {
		t.Fatalf("euro %v", b)
	}
	if _, err := encodeWinAnsi("\U0001F600"); err == nil {
		t.Fatal("unmappable")
	}
}

func TestAFMWidthKern(t *testing.T) {
	f := afmFonts["Helvetica"]
	b := []byte("AV") // has a kern pair
	if f.widthOf(b, 12, true) >= f.widthOf(b, 12, false) {
		t.Fatal("kerned should be <= unkerned")
	}
	if f.height(12) <= 0 || f.ascenderScaled(12) <= 0 {
		t.Fatal("metrics")
	}
	runs := kernRuns([]byte{}, f)
	if len(runs) != 1 {
		t.Fatal("empty kern runs")
	}
	if tjOperand([]byte("AB"), f, false) == "" {
		t.Fatal("tj non-kern")
	}
}

// ---- errors ----------------------------------------------------------------

func TestErrors(t *testing.T) {
	e := newError("Custom", "msg %d", 1)
	if e.Kind() != "Custom" || e.Error() != "Prawn::Errors::Custom: msg 1" {
		t.Fatalf("error %q", e.Error())
	}
	bare := &prawnError{kind: "Bare"}
	if bare.Error() != "Prawn::Errors::Bare" {
		t.Fatal("bare error")
	}
}

func TestWriterFunc(t *testing.T) {
	var got []byte
	w := writerFunc(func(p []byte) (int, error) { got = append(got, p...); return len(p), nil })
	w.Write([]byte("hi"))
	if string(got) != "hi" {
		t.Fatal("writerFunc")
	}
}

func TestSetClockNil(t *testing.T) {
	prev := SetClock(nil)
	SetClock(prev)
}
