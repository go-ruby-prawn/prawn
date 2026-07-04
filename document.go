// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-ruby-prawn/prawn authors

package prawn

import (
	"io"
	"os"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
)

// Style mirrors a prawn font style symbol (:normal, :bold, :italic,
// :bold_italic).
type Style int

const (
	// StyleNormal is the upright roman style (prawn :normal).
	StyleNormal Style = iota
	// StyleBold is the bold style (prawn :bold).
	StyleBold
	// StyleItalic is the italic/oblique style (prawn :italic).
	StyleItalic
	// StyleBoldItalic is the combined bold + italic style (prawn :bold_italic).
	StyleBoldItalic
)

func (s Style) fpdfStyle() string {
	switch s {
	case StyleBold:
		return "B"
	case StyleItalic:
		return "I"
	case StyleBoldItalic:
		return "BI"
	default:
		return ""
	}
}

// Align mirrors a prawn alignment symbol (:left, :center, :right, :justify).
type Align int

const (
	// AlignLeft left-aligns text (prawn :left, the default).
	AlignLeft Align = iota
	// AlignCenter centers text (prawn :center).
	AlignCenter
	// AlignRight right-aligns text (prawn :right).
	AlignRight
	// AlignJustify justifies text (prawn :justify). It is treated as left
	// alignment for the final line, matching prawn's behaviour for a single
	// short line.
	AlignJustify
)

// Options mirror the keyword arguments of Prawn::Document.new
// (:page_size, :page_layout, :margin). A zero Options selects prawn's defaults:
// US Letter, portrait, 36pt (0.5in) margins on every side.
type Options struct {
	// PageSize names a standard page size ("LETTER", "LEGAL", "A4", …). Empty
	// means "LETTER". Ignored when PageWidth and PageHeight are both > 0.
	PageSize string
	// PageWidth and PageHeight give an explicit custom page size in points; when
	// both are > 0 they override PageSize (prawn's page_size: [w, h]).
	PageWidth, PageHeight float64
	// PageLayout is "portrait" (default) or "landscape".
	PageLayout string
	// Margin, when non-nil, sets all four margins to the same value in points.
	// nil selects prawn's 36pt default. Margins overrides it when set.
	Margin *float64
	// Margins, when non-nil, sets the four margins explicitly as
	// [top, right, bottom, left] in points (prawn's margin: [t, r, b, l]).
	Margins *[4]float64
}

// clock is the seam that supplies the PDF /CreationDate. It defaults to the wall
// clock but is replaced by SetClock so tests (and any reproducible build) get
// deterministic, timezone-independent output.
var clock = time.Now

// SetClock overrides the time source used for the PDF /CreationDate and returns
// the previous source, so callers can restore it. Passing nil resets to the
// wall clock (time.Now). This is the determinism seam described in the package
// doc: pin it to make Render byte-for-byte reproducible.
func SetClock(fn func() time.Time) (previous func() time.Time) {
	previous = clock
	if fn == nil {
		clock = time.Now
	} else {
		clock = fn
	}
	return previous
}

// Document mirrors Prawn::Document. It wraps a pure-Go fpdf generator and tracks
// the prawn drawing state (current font, colors, line width, leading and the
// text cursor). Errors are accumulated sticky-style, like fpdf itself: once a
// method fails, later methods are no-ops and Render / Error report the failure.
type Document struct {
	f *fpdf.Fpdf

	pageWidth, pageHeight        float64
	mTop, mRight, mBottom, mLeft float64
	cursor                       float64 // distance from bounds bottom to the top of the next line
	fontFamily                   string
	fontStyle                    Style
	fontSizePts                  float64
	fillColor                    [3]int
	strokeColor                  [3]int
	lineWidth                    float64
	leading                      float64
	path                         []pathOp
	imgSeq                       int
	err                          error
}

// stdFonts is the set of the 14 standard PDF fonts prawn ships without any
// embedded font file. Keys are the prawn family names (case-insensitive).
var stdFonts = map[string]string{
	"helvetica":    "Helvetica",
	"courier":      "Courier",
	"times-roman":  "Times",
	"times":        "Times",
	"symbol":       "Symbol",
	"zapfdingbats": "ZapfDingbats",
}

// New creates a Document, mirroring Prawn::Document.new(options). It starts with
// a single blank page, the Helvetica 12pt font, black fill/stroke colors and a
// 1pt line width — prawn's defaults — and the cursor at the top of the margin
// box.
func New(o Options) *Document {
	w, h, err := resolvePageSize(o)
	d := &Document{
		pageWidth:   w,
		pageHeight:  h,
		fontFamily:  "Helvetica",
		fontStyle:   StyleNormal,
		fontSizePts: 12,
		fillColor:   [3]int{0, 0, 0},
		strokeColor: [3]int{0, 0, 0},
		lineWidth:   1,
	}
	d.mTop, d.mRight, d.mBottom, d.mLeft = resolveMargins(o)

	f := fpdf.NewCustom(&fpdf.InitType{
		OrientationStr: "P",
		UnitStr:        "pt",
		Size:           fpdf.SizeType{Wd: w, Ht: h},
	})
	f.SetAutoPageBreak(false, 0)
	f.SetMargins(d.mLeft, d.mTop, d.mRight)
	f.SetCreationDate(clock())
	// Prawn does not compress content streams by default; keep output plainly
	// inspectable and deterministic.
	f.SetCompression(false)
	d.f = f
	d.err = err // an invalid page layout is reported through Render/Error.

	f.AddPage()
	f.SetFont("Helvetica", "", 12)
	f.SetTextColor(0, 0, 0)
	f.SetFillColor(0, 0, 0)
	f.SetDrawColor(0, 0, 0)
	f.SetLineWidth(1)
	d.cursor = d.boundsHeight()
	return d
}

// Generate mirrors Prawn::Document.generate(path) { |pdf| … }: it creates a
// document, runs block against it, and writes the rendered PDF to path. If block
// returns an error, or rendering fails, that error is returned and no file is
// written.
func Generate(path string, block func(*Document) error) error {
	d := New(Options{})
	if err := block(d); err != nil {
		return err
	}
	return d.RenderFile(path)
}

// GenerateTo mirrors Prawn::Document.generate(io) { |pdf| … } for an arbitrary
// writer instead of a file path.
func GenerateTo(w io.Writer, block func(*Document) error) error {
	d := New(Options{})
	if err := block(d); err != nil {
		return err
	}
	b, err := d.Render()
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}

// GenerateWith is Generate with explicit document options.
func GenerateWith(path string, o Options, block func(*Document) error) error {
	d := New(o)
	if err := block(d); err != nil {
		return err
	}
	return d.RenderFile(path)
}

// Render mirrors Prawn::Document#render: it returns the finished PDF as bytes.
// Any error accumulated while building the document (or emitted by the
// underlying generator) is returned here.
func (d *Document) Render() ([]byte, error) {
	if d.err != nil {
		return nil, d.err
	}
	var b strings.Builder
	if err := d.f.Output(writerFunc(func(p []byte) (int, error) {
		b.Write(p)
		return len(p), nil
	})); err != nil {
		return nil, err
	}
	return []byte(b.String()), nil
}

// RenderFile mirrors Prawn::Document#render_file(path): it renders and writes
// the PDF to path.
func (d *Document) RenderFile(path string) error {
	b, err := d.Render()
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// Error reports the first error accumulated while building the document, or nil.
func (d *Document) Error() error {
	if d.err != nil {
		return d.err
	}
	return d.f.Error()
}

// writerFunc adapts a function to io.Writer so Render can collect output without
// a bytes.Buffer import cycle in callers.
type writerFunc func([]byte) (int, error)

func (w writerFunc) Write(p []byte) (int, error) { return w(p) }

// resolvePageSize turns Options into a concrete width/height in points, applying
// the page layout. An unknown layout yields ErrInvalidPageLayout; an unknown
// named size falls back to LETTER (prawn raises, but here the error surfaces via
// the layout/known-size checks and we keep a sane default).
func resolvePageSize(o Options) (w, h float64, err error) {
	if o.PageWidth > 0 && o.PageHeight > 0 {
		w, h = o.PageWidth, o.PageHeight
	} else {
		name := strings.ToUpper(o.PageSize)
		if name == "" {
			name = "LETTER"
		}
		dims, ok := pageSizes[name]
		if !ok {
			return 0, 0, newError("InvalidPageLayout", "unknown page size %q", o.PageSize)
		}
		w, h = dims[0], dims[1]
	}
	switch strings.ToLower(o.PageLayout) {
	case "", "portrait":
		// keep as-is
	case "landscape":
		w, h = h, w
	default:
		return 0, 0, ErrInvalidPageLayout
	}
	return w, h, nil
}

func resolveMargins(o Options) (top, right, bottom, left float64) {
	if o.Margins != nil {
		return o.Margins[0], o.Margins[1], o.Margins[2], o.Margins[3]
	}
	if o.Margin != nil {
		m := *o.Margin
		return m, m, m, m
	}
	return 36, 36, 36, 36
}

// boundsHeight is the height of the margin box (prawn bounds.height).
func (d *Document) boundsHeight() float64 { return d.pageHeight - d.mTop - d.mBottom }

// boundsWidth is the width of the margin box (prawn bounds.width).
func (d *Document) boundsWidth() float64 { return d.pageWidth - d.mLeft - d.mRight }

// Bounds mirrors Prawn::Document#bounds enough to expose the margin box: its
// width, height and the absolute (page) coordinates of its lower-left corner.
type Bounds struct {
	// Width and Height are the margin box dimensions in points.
	Width, Height float64
	// Left and Bottom are the page-absolute coordinates (from the page's
	// lower-left corner) of the margin box's lower-left corner.
	Left, Bottom float64
}

// Bounds returns the current margin box (Prawn::Document#bounds).
func (d *Document) Bounds() Bounds {
	return Bounds{
		Width:  d.boundsWidth(),
		Height: d.boundsHeight(),
		Left:   d.mLeft,
		Bottom: d.mBottom,
	}
}

// Cursor mirrors Prawn::Document#cursor: the vertical position of the drawing
// cursor measured from the bottom of the margin box (bounds.height at the top of
// a fresh page, 0 at the bottom).
func (d *Document) Cursor() float64 { return d.cursor }

// MoveCursorTo mirrors Prawn::Document#move_cursor_to: place the cursor at an
// absolute height within the margin box.
func (d *Document) MoveCursorTo(y float64) { d.cursor = y }

// MoveDown mirrors Prawn::Document#move_down: lower the cursor by n points.
func (d *Document) MoveDown(n float64) { d.cursor -= n }

// MoveUp mirrors Prawn::Document#move_up: raise the cursor by n points.
func (d *Document) MoveUp(n float64) { d.cursor += n }

// Pad mirrors Prawn::Document#pad: move the cursor down by n, run block, then
// move down by n again.
func (d *Document) Pad(n float64, block func()) {
	d.MoveDown(n)
	block()
	d.MoveDown(n)
}

// PadTop mirrors Prawn::Document#pad_top: move down by n, then run block.
func (d *Document) PadTop(n float64, block func()) {
	d.MoveDown(n)
	block()
}

// PadBottom mirrors Prawn::Document#pad_bottom: run block, then move down by n.
func (d *Document) PadBottom(n float64, block func()) {
	block()
	d.MoveDown(n)
}

// StartNewPage mirrors Prawn::Document#start_new_page: begin a fresh page and
// reset the cursor to the top of the margin box.
func (d *Document) StartNewPage() {
	d.f.AddPage()
	// fpdf resets font/colors per page via its state; re-assert prawn state.
	d.applyFont()
	d.applyFillColor()
	d.applyStrokeColor()
	d.f.SetLineWidth(d.lineWidth)
	d.cursor = d.boundsHeight()
}

// PageCount mirrors Prawn::Document#page_count.
func (d *Document) PageCount() int { return d.f.PageCount() }

// PageWidth returns the current page width in points.
func (d *Document) PageWidth() float64 { return d.pageWidth }

// PageHeight returns the current page height in points.
func (d *Document) PageHeight() float64 { return d.pageHeight }

// fail records the first error; later calls keep the earliest one.
func (d *Document) fail(err error) {
	if d.err == nil {
		d.err = err
	}
}

// toFpdfXY converts a point given in prawn bounds-relative coordinates (origin
// at the lower-left of the margin box, y increasing upward) to fpdf's top-left
// page coordinates (origin at the upper-left of the page, y increasing
// downward).
func (d *Document) toFpdfXY(x, y float64) (float64, float64) {
	absX := d.mLeft + x
	absYfromTop := d.pageHeight - (d.mBottom + y)
	return absX, absYfromTop
}

func (d *Document) applyFont() {
	d.f.SetFont(stdFonts[strings.ToLower(d.fontFamily)], d.fontStyle.fpdfStyle(), d.fontSizePts)
}

func (d *Document) applyFillColor() {
	d.f.SetFillColor(d.fillColor[0], d.fillColor[1], d.fillColor[2])
	d.f.SetTextColor(d.fillColor[0], d.fillColor[1], d.fillColor[2])
}

func (d *Document) applyStrokeColor() {
	d.f.SetDrawColor(d.strokeColor[0], d.strokeColor[1], d.strokeColor[2])
}
