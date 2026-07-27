// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-ruby-prawn/prawn authors

package prawn

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
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

// Align mirrors a prawn alignment symbol (:left, :center, :right, :justify).
type Align int

const (
	// AlignLeft left-aligns text (prawn :left, the default).
	AlignLeft Align = iota
	// AlignCenter centers text (prawn :center).
	AlignCenter
	// AlignRight right-aligns text (prawn :right).
	AlignRight
	// AlignJustify justifies text (prawn :justify).
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
	Margin *float64
	// Margins, when non-nil, sets the four margins explicitly as
	// [top, right, bottom, left] in points (prawn's margin: [t, r, b, l]).
	Margins *[4]float64
	// SkipPageCreation suppresses the initial page (prawn skip_page_creation).
	SkipPageCreation bool
	// CompressStreams enables FlateDecode on content streams (prawn compress).
	CompressStreams bool
}

// clock is the seam that supplies the PDF /CreationDate; SetClock replaces it so
// output is reproducible.
var clock = time.Now

// SetClock overrides the time source used for the PDF /CreationDate and returns
// the previous source. Passing nil resets to the wall clock (time.Now).
func SetClock(fn func() time.Time) (previous func() time.Time) {
	previous = clock
	if fn == nil {
		clock = time.Now
	} else {
		clock = fn
	}
	return previous
}

// boundingBox is prawn's bounds: a rectangle in absolute page coordinates
// (origin bottom-left, y up).
type boundingBox struct {
	absLeft, absBottom, width, height float64
}

// page is one PDF page: its content stream plus the per-page resource usage.
type page struct {
	content   *content
	width     float64
	height    float64
	fontsUsed map[string]bool // set of font resource names used on this page
	xobjsUsed map[string]bool // set of image XObject names used
	gsUsed    map[string]bool // set of ExtGState names used
	fillCS    string          // current fill color space name ("" = unset this page)
	strokeCS  string
	saveDepth int // open q's from save_graphics_state
}

// fontRef is a registered font: its PDF resource name ("F1.0") and either a
// Core-14 AFM font or an embedded TTF font.
type fontRef struct {
	resName string
	afm     *afmFont
	ttf     *ttfFont
}

// Document mirrors Prawn::Document. It owns a native PDF object model (no cgo,
// no third-party PDF writer) and tracks prawn's drawing state so the emitted
// content-stream operators match Ruby prawn's.
type Document struct {
	pages   []*page
	cur     *page
	boundsS []boundingBox // bounding-box stack; boundsS[len-1] is current

	pageWidth, pageHeight        float64
	mTop, mRight, mBottom, mLeft float64
	cursor                       float64

	fonts     map[string]*fontRef
	fontOrder int
	curFont   *fontRef
	fontSize  float64
	leading   float64
	kerning   bool

	fillColor   colorVal
	strokeColor colorVal
	lineWidth   float64
	dash        []float64
	dashPhase   float64
	lineCap     int
	lineJoin    int

	imgSeq            int
	imgOrder          int
	images            map[string]*imageXObject // registration key -> decoded image XObject
	gsSeq             int
	gstates           map[string]*extGState // key -> ExtGState (alpha/transparency)
	repeaters         []*repeater
	rptPage, rptTotal int

	compress  bool
	finalized bool
	err       error
}

// New creates a Document, mirroring Prawn::Document.new(options).
func New(o Options) *Document {
	w, h, err := resolvePageSize(o)
	d := &Document{
		pageWidth:   w,
		pageHeight:  h,
		fontSize:    12,
		kerning:     true,
		fillColor:   rgbColor(0, 0, 0),
		strokeColor: rgbColor(0, 0, 0),
		lineWidth:   1,
		lineCap:     0,
		lineJoin:    0,
		fonts:       map[string]*fontRef{},
		images:      map[string]*imageXObject{},
		gstates:     map[string]*extGState{},
		compress:    o.CompressStreams,
	}
	d.mTop, d.mRight, d.mBottom, d.mLeft = resolveMargins(o)
	d.err = err

	// The default font (Helvetica, normal) is registered eagerly so it takes
	// resource identifier F1.0, exactly as prawn does at construction.
	d.curFont = d.registerAFM("Helvetica", StyleNormal)

	if !o.SkipPageCreation {
		d.startPage()
	}
	return d
}

// startPage appends a fresh page and resets the cursor and margin box.
func (d *Document) startPage() {
	p := &page{
		content:   &content{},
		width:     d.pageWidth,
		height:    d.pageHeight,
		fontsUsed: map[string]bool{},
		xobjsUsed: map[string]bool{},
		gsUsed:    map[string]bool{},
	}
	p.content.raw("q") // prawn wraps each page's content in a save/restore
	d.pages = append(d.pages, p)
	d.cur = p
	d.boundsS = []boundingBox{{
		absLeft:   d.mLeft,
		absBottom: d.mBottom,
		width:     d.pageWidth - d.mLeft - d.mRight,
		height:    d.pageHeight - d.mTop - d.mBottom,
	}}
	d.cursor = d.bounds().height
}

// bounds returns the current bounding box.
func (d *Document) bounds() boundingBox { return d.boundsS[len(d.boundsS)-1] }

// Generate mirrors Prawn::Document.generate(path) { |pdf| … }.
func Generate(path string, block func(*Document) error) error {
	d := New(Options{})
	if err := block(d); err != nil {
		return err
	}
	return d.RenderFile(path)
}

// GenerateTo mirrors Prawn::Document.generate(io) { |pdf| … } for a writer.
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

// RenderFile mirrors Prawn::Document#render_file(path).
func (d *Document) RenderFile(path string) error {
	b, err := d.Render()
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// Error reports the first accumulated build error, or nil.
func (d *Document) Error() error { return d.err }

// fail records the first error; later calls keep the earliest one.
func (d *Document) fail(err error) {
	if d.err == nil {
		d.err = err
	}
}

// StartNewPage mirrors Prawn::Document#start_new_page. Pages are closed
// (their content balanced) only at render time, so repeaters and page
// numbering can still draw onto any page.
func (d *Document) StartNewPage() {
	d.startPage()
	// prawn re-asserts the current colors on each new page (update_colors).
	d.emitFillColor()
	d.emitStrokeColor()
}

// finishPage closes a page's balance: any still-open save_graphics_state, then
// the page-level "q" written at page start.
func (d *Document) finishPage(p *page) {
	for p.saveDepth > 0 {
		p.content.raw("Q")
		p.saveDepth--
	}
	p.content.raw("Q")
}

// PageCount mirrors Prawn::Document#page_count.
func (d *Document) PageCount() int { return len(d.pages) }

// PageNumber returns the current (1-based) page number.
func (d *Document) PageNumber() int { return len(d.pages) }

// PageWidth returns the current page width in points.
func (d *Document) PageWidth() float64 { return d.pageWidth }

// PageHeight returns the current page height in points.
func (d *Document) PageHeight() float64 { return d.pageHeight }

// Bounds exposes the current margin/bounding box.
type Bounds struct {
	Width, Height float64
	Left, Bottom  float64
}

// Bounds returns the current bounding box (Prawn::Document#bounds).
func (d *Document) Bounds() Bounds {
	b := d.bounds()
	return Bounds{Width: b.width, Height: b.height, Left: b.absLeft, Bottom: b.absBottom}
}

// Cursor mirrors Prawn::Document#cursor.
func (d *Document) Cursor() float64 { return d.cursor }

// MoveCursorTo mirrors Prawn::Document#move_cursor_to.
func (d *Document) MoveCursorTo(y float64) { d.cursor = y }

// MoveDown mirrors Prawn::Document#move_down.
func (d *Document) MoveDown(n float64) { d.cursor -= n }

// MoveUp mirrors Prawn::Document#move_up.
func (d *Document) MoveUp(n float64) { d.cursor += n }

// Pad mirrors Prawn::Document#pad.
func (d *Document) Pad(n float64, block func()) {
	d.MoveDown(n)
	block()
	d.MoveDown(n)
}

// PadTop mirrors Prawn::Document#pad_top.
func (d *Document) PadTop(n float64, block func()) {
	d.MoveDown(n)
	block()
}

// PadBottom mirrors Prawn::Document#pad_bottom.
func (d *Document) PadBottom(n float64, block func()) {
	block()
	d.MoveDown(n)
}

// mapToAbs converts a bounds-relative point (prawn origin: current bounds'
// lower-left, y up) to absolute page coordinates.
func (d *Document) mapToAbs(x, y float64) (float64, float64) {
	b := d.bounds()
	return b.absLeft + x, b.absBottom + y
}

// resolvePageSize turns Options into a concrete width/height in points.
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

// registerAFM returns (registering if necessary) the fontRef for a Core-14 font
// family+style, assigning it the next resource identifier.
func (d *Document) registerAFM(family string, style Style) *fontRef {
	base := afmBaseName(family, style)
	key := "afm:" + base
	if fr, ok := d.fonts[key]; ok {
		return fr
	}
	d.fontOrder++
	fr := &fontRef{
		resName: fmt.Sprintf("F%d.0", d.fontOrder),
		afm:     afmFonts[base],
	}
	d.fonts[key] = fr
	return fr
}

// afmBaseName maps a prawn family name + style to a Core-14 PostScript name.
func afmBaseName(family string, style Style) string {
	f := strings.ToLower(family)
	switch f {
	case "courier":
		return pick("Courier", "Courier-Bold", "Courier-Oblique", "Courier-BoldOblique", style)
	case "times-roman", "times":
		return pick("Times-Roman", "Times-Bold", "Times-Italic", "Times-BoldItalic", style)
	case "symbol":
		return "Symbol"
	case "zapfdingbats":
		return "ZapfDingbats"
	default: // helvetica / arial / sans-serif
		return pick("Helvetica", "Helvetica-Bold", "Helvetica-Oblique", "Helvetica-BoldOblique", style)
	}
}

func pick(normal, bold, italic, boldItalic string, s Style) string {
	switch s {
	case StyleBold:
		return bold
	case StyleItalic:
		return italic
	case StyleBoldItalic:
		return boldItalic
	default:
		return normal
	}
}

// isAFMFamily reports whether name is a recognized Core-14 family.
func isAFMFamily(name string) bool {
	switch strings.ToLower(name) {
	case "courier", "times-roman", "times", "symbol", "zapfdingbats",
		"helvetica", "arial", "sans-serif":
		return true
	}
	return false
}

// writerFunc adapts a function to io.Writer.
type writerFunc func([]byte) (int, error)

func (w writerFunc) Write(p []byte) (int, error) { return w(p) }
