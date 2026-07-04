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

	rpdf "rsc.io/pdf"
)

// TestMain pins the clock so every generated PDF is byte-for-byte reproducible
// and timezone independent, and confirms the seam is restored afterwards.
func TestMain(m *testing.M) {
	prev := SetClock(func() time.Time {
		return time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	})
	code := m.Run()
	SetClock(prev)
	os.Exit(code)
}

// --- verification harness (generate then parse back) -------------------------

// mustReader parses PDF bytes with the pure-Go rsc.io/pdf reader and asserts the
// document is well-formed per the spec: %PDF- header, cross-reference table,
// trailer and %%EOF.
func mustReader(t *testing.T, b []byte) *rpdf.Reader {
	t.Helper()
	if !bytes.HasPrefix(b, []byte("%PDF-")) {
		t.Fatalf("missing %%PDF- header: %q", b[:min(8, len(b))])
	}
	for _, marker := range []string{"xref", "trailer", "startxref", "%%EOF"} {
		if !bytes.Contains(b, []byte(marker)) {
			t.Fatalf("well-formedness: missing %q", marker)
		}
	}
	r, err := rpdf.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatalf("reader: %v", err)
	}
	return r
}

// pageText concatenates every glyph rsc.io/pdf extracted from page n (1-indexed)
// in draw order, so tests can substring-match the text that was drawn.
func pageText(r *rpdf.Reader, n int) string {
	var sb strings.Builder
	for _, tx := range r.Page(n).Content().Text {
		sb.WriteString(tx.S)
	}
	return sb.String()
}

// pageTexts returns the raw per-glyph Text records for page n.
func pageTexts(r *rpdf.Reader, n int) []rpdf.Text {
	return r.Page(n).Content().Text
}

// textHas reports whether page n contains want. The rsc.io/pdf reader emits one
// record per glyph and does not preserve inter-word spaces, so the comparison is
// made with all spaces removed.
func textHas(r *rpdf.Reader, n int, want string) bool {
	strip := func(s string) string { return strings.ReplaceAll(s, " ", "") }
	return strings.Contains(strip(pageText(r, n)), strip(want))
}

func renderDoc(t *testing.T, d *Document) []byte {
	t.Helper()
	b, err := d.Render()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return b
}

func makePNG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 8, 6))
	for x := 0; x < 8; x++ {
		for y := 0; y < 6; y++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 30), G: uint8(y * 40), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func makeJPEG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 8, 6))
	for x := 0; x < 8; x++ {
		for y := 0; y < 6; y++ {
			img.Set(x, y, color.RGBA{R: 200, G: uint8(y * 40), B: 50, A: 255})
		}
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, nil)
	return buf.Bytes()
}

// --- text / font / color -----------------------------------------------------

func TestTextGenerateAndParse(t *testing.T) {
	d := New(Options{})
	d.Text("Hello Prawn", &TextOptions{Size: 24, Style: StyleBold, StyleSet: true})
	b := renderDoc(t, d)

	r := mustReader(t, b)
	if r.NumPage() != 1 {
		t.Fatalf("pages = %d, want 1", r.NumPage())
	}
	if !textHas(r, 1, "Hello Prawn") {
		t.Fatalf("page text = %q, want to contain %q", pageText(r, 1), "Hello Prawn")
	}
	for _, tx := range pageTexts(r, 1) {
		if tx.FontSize != 24 {
			t.Fatalf("font size = %v, want 24", tx.FontSize)
		}
		if !strings.Contains(tx.Font, "Bold") {
			t.Fatalf("font = %q, want bold", tx.Font)
		}
	}
	// Options were per-call: the document font is back to the default.
	if d.FontSizeValue() != 12 || d.FontStyle() != StyleNormal {
		t.Fatalf("state not restored: size=%v style=%v", d.FontSizeValue(), d.FontStyle())
	}
}

func TestTextWrappingAndPagination(t *testing.T) {
	d := New(Options{})
	// Push the cursor near the bottom so flowing text spills onto a new page.
	d.MoveCursorTo(20)
	long := strings.Repeat("wrap this sentence onto several lines ", 20)
	d.Text(long, nil)
	b := renderDoc(t, d)
	r := mustReader(t, b)
	if r.NumPage() < 2 {
		t.Fatalf("expected multi-page flow, got %d pages", r.NumPage())
	}
	if d.PageCount() != r.NumPage() {
		t.Fatalf("PageCount %d != reader %d", d.PageCount(), r.NumPage())
	}
}

func TestTextAlignmentAndExplicitNewlines(t *testing.T) {
	for _, a := range []Align{AlignLeft, AlignCenter, AlignRight, AlignJustify} {
		d := New(Options{})
		startCursor := d.Cursor()
		d.Text("a\n\nb", &TextOptions{Align: a, Leading: 3})
		// Three lines (a, blank, b) advance the cursor thrice.
		lh := d.lineHeight(12, 3)
		if got := startCursor - d.Cursor(); got < 3*lh-0.01 || got > 3*lh+0.01 {
			t.Fatalf("cursor advance = %v, want %v", got, 3*lh)
		}
		b := renderDoc(t, d)
		r := mustReader(t, b)
		if txt := pageText(r, 1); !strings.Contains(txt, "a") || !strings.Contains(txt, "b") {
			t.Fatalf("align %v: text = %q", a, txt)
		}
	}
}

func TestDrawText(t *testing.T) {
	d := New(Options{})
	before := d.Cursor()
	d.DrawText("At Point", 100, 200, &TextOptions{Font: "Times", StyleSet: true, Style: StyleItalic, Color: "0000FF"})
	if d.Cursor() != before {
		t.Fatalf("draw_text moved the cursor")
	}
	r := mustReader(t, renderDoc(t, d))
	if !textHas(r, 1, "At Point") {
		t.Fatal("draw_text not found")
	}
}

func TestFontFamilyAndColorAccessors(t *testing.T) {
	d := New(Options{})
	d.Font("Times", StyleBold)
	if d.FontFamily() != "Times" || d.FontStyle() != StyleBold {
		t.Fatal("font not set")
	}
	d.FontSize(18)
	d.Leading(2)
	if d.FontSizeValue() != 18 {
		t.Fatal("size not set")
	}
	d.FillColor("#FF8800")
	d.StrokeColor("112233")
	if d.FillColorValue() != "FF8800" {
		t.Fatalf("fill = %q", d.FillColorValue())
	}
	if d.StrokeColorValue() != "112233" {
		t.Fatalf("stroke = %q", d.StrokeColorValue())
	}
}

func TestUnknownFont(t *testing.T) {
	d := New(Options{})
	d.Font("Comic Sans", StyleNormal)
	if !errors.Is(d.Error(), ErrUnknownFont) {
		t.Fatalf("err = %v, want ErrUnknownFont", d.Error())
	}
	if _, err := d.Render(); !errors.Is(err, ErrUnknownFont) {
		t.Fatalf("render err = %v", err)
	}
}

func TestTextOptionUnknownFontAndBadColor(t *testing.T) {
	d := New(Options{})
	d.Text("x", &TextOptions{Font: "Nope"})
	if !errors.Is(d.Error(), ErrUnknownFont) {
		t.Fatalf("err = %v", d.Error())
	}
	d2 := New(Options{})
	d2.Text("x", &TextOptions{Color: "xyz"})
	if d2.Error() == nil {
		t.Fatal("want bad-color error")
	}
}

func TestInvalidUTF8(t *testing.T) {
	bad := string([]byte{0xff, 0xfe})
	for _, fn := range []func(*Document){
		func(d *Document) { d.Text(bad, nil) },
		func(d *Document) { d.DrawText(bad, 0, 0, nil) },
		func(d *Document) { d.TextBox(bad, TextBoxOptions{Width: 100}) },
	} {
		d := New(Options{})
		fn(d)
		if !errors.Is(d.Error(), ErrIncompatibleStringEncoding) {
			t.Fatalf("err = %v, want ErrIncompatibleStringEncoding", d.Error())
		}
	}
}

func TestBadColorForms(t *testing.T) {
	for _, bad := range []string{"12345", "GGGGGG"} {
		d := New(Options{})
		d.FillColor(bad)
		if d.Error() == nil {
			t.Fatalf("color %q: want error", bad)
		}
		d2 := New(Options{})
		d2.StrokeColor(bad)
		if d2.Error() == nil {
			t.Fatalf("stroke color %q: want error", bad)
		}
	}
}

// --- text_box ---------------------------------------------------------------

func TestTextBoxFits(t *testing.T) {
	d := New(Options{})
	leftover := d.TextBox("short text", TextBoxOptions{X: 0, Y: 400, Width: 300, Height: 100, Align: AlignCenter})
	if leftover != "" {
		t.Fatalf("leftover = %q, want empty", leftover)
	}
	if d.Error() != nil {
		t.Fatal(d.Error())
	}
	r := mustReader(t, renderDoc(t, d))
	if !textHas(r, 1, "short text") {
		t.Fatal("text_box text missing")
	}
}

func TestTextBoxTruncateAndExpand(t *testing.T) {
	long := strings.Repeat("many words that wrap ", 30)
	// Truncate: a short box returns the unprinted remainder.
	d := New(Options{})
	leftover := d.TextBox(long, TextBoxOptions{X: 0, Y: 200, Width: 120, Height: 30})
	if leftover == "" {
		t.Fatal("expected leftover from truncated box")
	}
	// Expand: draws everything, no leftover.
	d2 := New(Options{})
	leftover2 := d2.TextBox(long, TextBoxOptions{X: 0, Y: 200, Width: 120, Height: 30, Overflow: OverflowExpand})
	if leftover2 != "" {
		t.Fatalf("expand leftover = %q", leftover2)
	}
}

func TestTextBoxErrors(t *testing.T) {
	d := New(Options{})
	d.TextBox("x", TextBoxOptions{Width: 0})
	if !errors.Is(d.Error(), ErrCannotFit) {
		t.Fatalf("width<=0 err = %v", d.Error())
	}
	d2 := New(Options{})
	long := strings.Repeat("overflowing content ", 40)
	d2.TextBox(long, TextBoxOptions{Width: 120, Height: 20, Overflow: OverflowError})
	if !errors.Is(d2.Error(), ErrCannotFit) {
		t.Fatalf("overflow err = %v", d2.Error())
	}
}

// --- graphics ---------------------------------------------------------------

func TestGraphicsPrimitives(t *testing.T) {
	d := New(Options{})
	d.LineWidth(2)
	if d.LineWidthValue() != 2 {
		t.Fatal("line width")
	}
	d.FillColor("00AA00")
	d.StrokeColor("AA0000")
	d.FillRectangle(10, 100, 80, 40)
	d.StrokeRectangle(10, 100, 80, 40)
	d.StrokeLine(0, 0, 100, 100)
	d.FillCircle(150, 150, 20)
	d.StrokeCircle(150, 150, 20)
	d.FillEllipse(200, 200, 30, 15)
	d.StrokeEllipse(200, 200, 30, 15)
	d.StrokeBounds()
	// Build a path then paint it with fill_and_stroke.
	d.Rectangle(300, 300, 40, 40)
	d.Line(300, 300, 340, 340)
	d.Circle(320, 320, 10)
	d.Ellipse(320, 320, 12, 8)
	d.FillAndStroke()

	b := renderDoc(t, d)
	r := mustReader(t, b)
	if len(r.Page(1).Content().Rect) == 0 {
		t.Fatal("no rectangles parsed back")
	}
	// Graphics operators appear in the uncompressed content stream.
	for _, op := range []string{" re", " l", " c", " f", " S"} {
		if !bytes.Contains(b, []byte(op)) {
			t.Fatalf("missing graphics operator %q", op)
		}
	}
}

// --- images -----------------------------------------------------------------

func TestImagePNGandJPEG(t *testing.T) {
	d := New(Options{})
	before := d.Cursor()
	d.ImageReader(bytes.NewReader(makePNG()), "png", ImageOptions{Width: 60, Height: 45})
	if d.Cursor() >= before {
		t.Fatal("image at cursor should advance the cursor")
	}
	d.ImageReader(bytes.NewReader(makeJPEG()), "jpeg", ImageOptions{AtX: 200, AtY: 400, AtSet: true, FitW: 50, FitH: 50})
	b := renderDoc(t, d)
	mustReader(t, b)
	if !bytes.Contains(b, []byte("/Subtype /Image")) {
		t.Fatal("no image XObject embedded")
	}
}

func TestImageFromFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "pic.png")
	if err := os.WriteFile(p, makePNG(), 0o644); err != nil {
		t.Fatal(err)
	}
	d := New(Options{})
	d.Image(p, ImageOptions{Width: 30})
	b := renderDoc(t, d)
	if !bytes.Contains(b, []byte("/Subtype /Image")) {
		t.Fatal("file image not embedded")
	}

	// JPEG file path (covers imageTypeFromName jpg branch).
	pj := filepath.Join(dir, "pic.jpg")
	_ = os.WriteFile(pj, makeJPEG(), 0o644)
	d2 := New(Options{})
	d2.Image(pj, ImageOptions{Height: 30})
	if _, err := d2.Render(); err != nil {
		t.Fatalf("jpeg file: %v", err)
	}
}

func TestImageErrors(t *testing.T) {
	// Missing file.
	d := New(Options{})
	d.Image(filepath.Join(t.TempDir(), "nope.png"), ImageOptions{})
	if d.Error() == nil {
		t.Fatal("want open error")
	}
	// Unsupported type.
	d2 := New(Options{})
	d2.ImageReader(bytes.NewReader([]byte("gif")), "gif", ImageOptions{})
	if !errors.Is(d2.Error(), ErrUnsupportedImageType) {
		t.Fatalf("err = %v", d2.Error())
	}
	// Unknown extension via file path.
	dir := t.TempDir()
	pg := filepath.Join(dir, "pic.gif")
	_ = os.WriteFile(pg, []byte("gif"), 0o644)
	d3 := New(Options{})
	d3.Image(pg, ImageOptions{})
	if !errors.Is(d3.Error(), ErrUnsupportedImageType) {
		t.Fatalf("ext err = %v", d3.Error())
	}
	// Corrupt image bytes make the generator fail; surfaced via Render.
	d4 := New(Options{})
	d4.ImageReader(bytes.NewReader([]byte("not-a-real-png")), "png", ImageOptions{})
	if _, err := d4.Render(); err == nil {
		t.Fatal("want render error from corrupt image")
	}
	if d4.Error() == nil {
		t.Fatal("want generator error surfaced via Error()")
	}
	// Reader that errors mid-read.
	d5 := New(Options{})
	d5.ImageReader(errReader{}, "png", ImageOptions{})
	if d5.Error() == nil {
		t.Fatal("want read error")
	}
}

func TestResolveImageSizeModes(t *testing.T) {
	cases := []struct {
		o     ImageOptions
		wantW float64
		wantH float64
	}{
		{ImageOptions{}, 100, 50},                       // natural
		{ImageOptions{Width: 200, Height: 20}, 200, 20}, // both
		{ImageOptions{Width: 50}, 50, 25},               // width only
		{ImageOptions{Height: 100}, 200, 100},           // height only
		{ImageOptions{FitW: 40, FitH: 100}, 40, 20},     // fit bound by width
		{ImageOptions{FitW: 400, FitH: 10}, 20, 10},     // fit bound by height
	}
	for i, c := range cases {
		w, h := resolveImageSize(100, 50, c.o)
		if w != c.wantW || h != c.wantH {
			t.Fatalf("case %d: got (%v,%v) want (%v,%v)", i, w, h, c.wantW, c.wantH)
		}
	}
}

// --- table ------------------------------------------------------------------

func TestTableGenerateAndParse(t *testing.T) {
	d := New(Options{})
	data := [][]string{
		{"Name", "Qty", "Price"},
		{"Widget", "3", "9.99"},
		{"Gadget", "1"}, // short row exercises the missing-cell path
	}
	res := d.Table(data, TableOptions{
		ColumnWidths: []float64{120, 60}, // fewer than columns → last width reused
		Header:       true,
		CellPadding:  4,
		BorderWidth:  1,
		FontSize:     10,
	})
	if len(res.ColumnWidths) != 3 || res.ColumnWidths[2] != 60 {
		t.Fatalf("column widths = %v", res.ColumnWidths)
	}
	if res.Height <= 0 || res.Width != 240 {
		t.Fatalf("table geometry = %+v", res)
	}
	b := renderDoc(t, d)
	r := mustReader(t, b)
	txt := pageText(r, 1)
	for _, want := range []string{"Name", "Widget", "9.99", "Gadget"} {
		if !strings.Contains(txt, want) {
			t.Fatalf("table text missing %q in %q", want, txt)
		}
	}
	if len(r.Page(1).Content().Rect) == 0 {
		t.Fatal("no cell borders drawn")
	}
}

func TestTableVariants(t *testing.T) {
	// Auto widths, no header, no borders, positioned via At, forced row height.
	d := New(Options{})
	res := d.Table([][]string{{"a", "b"}, {"c", "d"}}, TableOptions{
		BorderWidth: -1, // no borders
		RowHeight:   30,
		AtX:         50, AtY: 500, AtSet: true,
	})
	if res.RowHeights[0] != 30 {
		t.Fatalf("row height = %v", res.RowHeights[0])
	}
	// Positioned table must not move the cursor.
	if d.Cursor() != d.boundsHeight() {
		t.Fatal("positioned table moved cursor")
	}
	renderDoc(t, d)

	// Default borders (BorderWidth == 0 → 1pt) and auto column widths.
	d.Table([][]string{{"x", "y"}}, TableOptions{})
	renderDoc(t, d)

	// Empty and column-less inputs are no-ops.
	if got := d.Table(nil, TableOptions{}); got.Width != 0 || got.Height != 0 || got.ColumnWidths != nil {
		t.Fatal("nil data should be empty result")
	}
	if got := d.Table([][]string{{}, {}}, TableOptions{}); got.Width != 0 || got.ColumnWidths != nil {
		t.Fatal("zero-column data should be empty result")
	}
}

// --- document / pages / options ---------------------------------------------

func TestPageSizesAndLayout(t *testing.T) {
	d := New(Options{PageSize: "A4", PageLayout: "landscape"})
	if d.PageWidth() <= d.PageHeight() {
		t.Fatalf("landscape A4 should be wider: %v x %v", d.PageWidth(), d.PageHeight())
	}
	custom := New(Options{PageWidth: 300, PageHeight: 400})
	if custom.PageWidth() != 300 || custom.PageHeight() != 400 {
		t.Fatal("custom size")
	}
	if bnd := custom.Bounds(); bnd.Left != 36 || bnd.Bottom != 36 {
		t.Fatalf("bounds = %+v", bnd)
	}
}

func TestInvalidPageLayoutAndSize(t *testing.T) {
	d := New(Options{PageLayout: "diagonal"})
	if !errors.Is(d.Error(), ErrInvalidPageLayout) {
		t.Fatalf("layout err = %v", d.Error())
	}
	d2 := New(Options{PageSize: "NOSUCH"})
	if d2.Error() == nil {
		t.Fatal("want unknown-size error")
	}
}

func TestMargins(t *testing.T) {
	m := 72.0
	d := New(Options{Margin: &m})
	if d.Bounds().Left != 72 {
		t.Fatalf("uniform margin: %+v", d.Bounds())
	}
	d2 := New(Options{Margins: &[4]float64{10, 20, 30, 40}})
	bnd := d2.Bounds()
	if bnd.Left != 40 || bnd.Bottom != 30 {
		t.Fatalf("per-side margins: %+v", bnd)
	}
}

func TestCursorMovement(t *testing.T) {
	d := New(Options{})
	top := d.Cursor()
	d.MoveDown(10)
	d.MoveUp(4)
	if got := top - d.Cursor(); got != 6 {
		t.Fatalf("net move = %v, want 6", got)
	}
	d.MoveCursorTo(100)
	if d.Cursor() != 100 {
		t.Fatal("move_cursor_to")
	}
	var order []string
	d.Pad(5, func() { order = append(order, "pad") })
	d.PadTop(5, func() { order = append(order, "top") })
	d.PadBottom(5, func() { order = append(order, "bottom") })
	if strings.Join(order, ",") != "pad,top,bottom" {
		t.Fatalf("pad order = %v", order)
	}
}

func TestStartNewPage(t *testing.T) {
	d := New(Options{})
	d.Font("Courier", StyleBold)
	d.FillColor("102030")
	d.StrokeColor("405060")
	d.LineWidth(3)
	d.Text("page one", nil)
	d.StartNewPage()
	if d.Cursor() != d.boundsHeight() {
		t.Fatal("cursor not reset on new page")
	}
	d.Text("page two", nil)
	r := mustReader(t, renderDoc(t, d))
	if r.NumPage() != 2 {
		t.Fatalf("pages = %d", r.NumPage())
	}
	if !textHas(r, 1, "page one") || !textHas(r, 2, "page two") {
		t.Fatal("text not on expected pages")
	}
}

func TestGenerateHelpers(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "out.pdf")
	if err := Generate(p, func(pdf *Document) error {
		pdf.Text("generated", nil)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	r := mustReader(t, b)
	if !textHas(r, 1, "generated") {
		t.Fatal("Generate output wrong")
	}

	// GenerateWith options.
	p2 := filepath.Join(dir, "out2.pdf")
	if err := GenerateWith(p2, Options{PageSize: "A5"}, func(pdf *Document) error {
		pdf.Text("a5", nil)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// GenerateTo writer.
	var buf bytes.Buffer
	if err := GenerateTo(&buf, func(pdf *Document) error {
		pdf.Text("to writer", nil)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	mustReader(t, buf.Bytes())
}

func TestGenerateErrorPaths(t *testing.T) {
	boom := errors.New("boom")
	dir := t.TempDir()
	p := filepath.Join(dir, "x.pdf")

	if err := Generate(p, func(*Document) error { return boom }); err != boom {
		t.Fatalf("Generate block err = %v", err)
	}
	if err := GenerateWith(p, Options{}, func(*Document) error { return boom }); err != boom {
		t.Fatalf("GenerateWith block err = %v", err)
	}
	if err := GenerateTo(&bytes.Buffer{}, func(*Document) error { return boom }); err != boom {
		t.Fatalf("GenerateTo block err = %v", err)
	}
	// GenerateTo where Render fails (bad color set in the block).
	if err := GenerateTo(&bytes.Buffer{}, func(pdf *Document) error {
		pdf.FillColor("nope")
		return nil
	}); err == nil {
		t.Fatal("want render error in GenerateTo")
	}
	// GenerateTo where the writer fails.
	if err := GenerateTo(errWriter{}, func(pdf *Document) error {
		pdf.Text("x", nil)
		return nil
	}); err == nil {
		t.Fatal("want writer error")
	}
	// Generate where render fails (block leaves the doc errored).
	if err := Generate(p, func(pdf *Document) error {
		pdf.FillColor("nope")
		return nil
	}); err == nil {
		t.Fatal("want render error in Generate")
	}
}

func TestRenderFileErrorPaths(t *testing.T) {
	// Render error (unknown font) surfaces through RenderFile.
	d := New(Options{})
	d.Font("Nope", StyleNormal)
	if err := d.RenderFile(filepath.Join(t.TempDir(), "x.pdf")); err == nil {
		t.Fatal("want render error")
	}
	// WriteFile error: a path whose parent directory does not exist.
	d2 := New(Options{})
	d2.Text("ok", nil)
	if err := d2.RenderFile("/no/such/dir/here/file.pdf"); err == nil {
		t.Fatal("want write error")
	}
}

func TestErrorAccessorStates(t *testing.T) {
	// nil error.
	if New(Options{}).Error() != nil {
		t.Fatal("fresh doc should have no error")
	}
	// d.err set (bad color).
	d := New(Options{})
	d.FillColor("bad")
	if d.Error() == nil {
		t.Fatal("want d.err")
	}
	// fpdf error, d.err nil (corrupt image).
	d2 := New(Options{})
	d2.ImageReader(bytes.NewReader([]byte("bad")), "png", ImageOptions{})
	if d2.Error() == nil {
		t.Fatal("want fpdf error via Error()")
	}
}

func TestFailKeepsFirstError(t *testing.T) {
	d := New(Options{})
	d.FillColor("firstbad") // sets d.err
	first := d.Error()
	d.StrokeColor("secondbad") // must not overwrite
	if d.Error() != first {
		t.Fatal("fail overwrote the first error")
	}
}

func TestStyleAndErrorStrings(t *testing.T) {
	if StyleBold.fpdfStyle() != "B" || StyleItalic.fpdfStyle() != "I" ||
		StyleBoldItalic.fpdfStyle() != "BI" || StyleNormal.fpdfStyle() != "" {
		t.Fatal("style mapping")
	}
	// prawnError with and without a message, plus Kind.
	e := &prawnError{kind: "CannotFit"}
	if e.Error() != "Prawn::Errors::CannotFit" || e.Kind() != "CannotFit" {
		t.Fatalf("bare error = %q", e.Error())
	}
	e2 := newError("UnknownFont", "missing %s", "Foo")
	if e2.Error() != "Prawn::Errors::UnknownFont: missing Foo" {
		t.Fatalf("msg error = %q", e2.Error())
	}
}

func TestSetClockReset(t *testing.T) {
	prev := SetClock(nil) // reset to time.Now
	// A fresh doc still renders (CreationDate = now); restore the pinned clock.
	d := New(Options{})
	d.Text("now", nil)
	if _, err := d.Render(); err != nil {
		t.Fatal(err)
	}
	SetClock(prev)
}

func TestDeterministicOutput(t *testing.T) {
	gen := func() []byte {
		d := New(Options{})
		d.Text("stable", nil)
		return renderDoc(t, d)
	}
	if !bytes.Equal(gen(), gen()) {
		t.Fatal("output is not deterministic under a pinned clock")
	}
}

// --- test doubles -----------------------------------------------------------

type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }
