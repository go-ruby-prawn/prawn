// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-ruby-prawn/prawn authors

package prawn

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// TextOptions mirror the per-call keyword arguments of Prawn::Document#text /
// #draw_text (:size, :style, :align, :leading, :color, :kerning, and a per-call
// font family). Zero-value fields keep the document's current settings.
type TextOptions struct {
	Size     float64
	Style    Style
	StyleSet bool
	Font     string
	Align    Align
	Leading  float64
	Color    string
	// Kerning, when KerningSet is true, overrides the document kerning flag.
	Kerning    bool
	KerningSet bool
}

// Font mirrors Prawn::Document#font(name, style:). It selects a Core-14 built-in
// family or a previously embedded TTF family by name and an optional style.
func (d *Document) Font(name string, style Style) {
	if isAFMFamily(name) {
		d.curFont = d.registerAFM(name, style)
		return
	}
	if fr, ok := d.fonts["ttf:"+name]; ok {
		d.curFont = fr
		return
	}
	d.fail(fmt.Errorf("%w: %s", ErrUnknownFont, name))
}

// FontFamily returns the current font's PostScript/base name.
func (d *Document) FontFamily() string {
	if d.curFont.afm != nil {
		return d.curFont.afm.Name
	}
	return d.curFont.ttf.name
}

// FontStyle reports the current style inferred from the base font name.
func (d *Document) FontStyle() Style {
	name := strings.ToLower(d.FontFamily())
	bold := strings.Contains(name, "bold")
	ital := strings.Contains(name, "italic") || strings.Contains(name, "oblique")
	switch {
	case bold && ital:
		return StyleBoldItalic
	case bold:
		return StyleBold
	case ital:
		return StyleItalic
	default:
		return StyleNormal
	}
}

// FontSize sets the current font size in points (Prawn::Document#font_size=).
func (d *Document) FontSize(size float64) { d.fontSize = size }

// FontSizeValue returns the current font size (Prawn::Document#font_size).
func (d *Document) FontSizeValue() float64 { return d.fontSize }

// Leading sets the document-wide extra leading (Prawn::Document#default_leading).
func (d *Document) Leading(n float64) { d.leading = n }

// LeadingValue returns the current default leading.
func (d *Document) LeadingValue() float64 { return d.leading }

// SetKerning enables or disables kerning for subsequent text (prawn default on).
func (d *Document) SetKerning(on bool) { d.kerning = on }

// KerningEnabled reports whether kerning is on.
func (d *Document) KerningEnabled() bool { return d.kerning }

// curAscender returns the current font's ascender at the given size.
func (d *Document) curAscender(size float64) float64 {
	if d.curFont.afm != nil {
		return d.curFont.afm.ascenderScaled(size)
	}
	return d.curFont.ttf.ascenderScaled(size)
}

// curHeight returns the current font's line height at the given size.
func (d *Document) curHeight(size float64) float64 {
	if d.curFont.afm != nil {
		return d.curFont.afm.height(size)
	}
	return d.curFont.ttf.height(size)
}

// lineHeight is one line's vertical advance: font height + document leading +
// any per-call extra leading.
func (d *Document) lineHeight(size, extraLeading float64) float64 {
	return d.curHeight(size) + d.leading + extraLeading
}

// numToken renders a number the way prawn serializes font sizes and matrix
// entries in a content stream: an integer when whole, otherwise real().
func numToken(f float64) string {
	if f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return formatReal(f)
}

// WidthOfString measures a UTF-8 string at the current font and size, applying
// the document kerning flag (Prawn::Document#width_of).
func (d *Document) WidthOfString(s string) float64 {
	return d.widthOfSized(s, d.fontSize, d.kerning)
}

func (d *Document) widthOfSized(s string, size float64, kerning bool) float64 {
	if d.curFont.afm != nil {
		b, err := encodeWinAnsi(s)
		if err != nil {
			return 0
		}
		return d.curFont.afm.widthOf(b, size, kerning)
	}
	return d.curFont.ttf.widthOf(s, size)
}

// applyTextOptions applies per-call overrides and returns a restore function.
func (d *Document) applyTextOptions(opts *TextOptions) func() {
	prevFont, prevSize, prevKern := d.curFont, d.fontSize, d.kerning
	if opts == nil {
		return func() { d.curFont, d.fontSize, d.kerning = prevFont, prevSize, prevKern }
	}
	if opts.Font != "" || opts.StyleSet {
		fam := opts.Font
		style := d.FontStyle()
		if opts.StyleSet {
			style = opts.Style
		}
		if fam == "" {
			fam = d.FontFamily()
		}
		d.Font(fam, style)
	}
	if opts.Size > 0 {
		d.fontSize = opts.Size
	}
	if opts.KerningSet {
		d.kerning = opts.Kerning
	}
	changedColor := false
	var prevFill colorVal
	if opts.Color != "" {
		prevFill = d.fillColor
		d.FillColor(opts.Color)
		changedColor = true
	}
	return func() {
		d.curFont, d.fontSize, d.kerning = prevFont, prevSize, prevKern
		if changedColor {
			d.fillColor = prevFill
			d.emitFillColor()
		}
	}
}

// drawLine emits the BT/Td/Tf/(TJ|Tj)/ET operator group for a single line of
// UTF-8 text whose baseline sits at the bounds-relative point (x, y), using the
// current font, size and kerning flag. It records the font as used on the page.
func (d *Document) drawLine(s string, x, y float64) {
	d.cur.fontsUsed[d.curFont.resName] = true
	ax, ay := d.mapToAbs(x, y)
	c := d.cur.content
	c.raw("BT")
	c.op(ax, ay, "Td")
	c.raw("/" + d.curFont.resName + " " + numToken(d.fontSize) + " Tf")
	if d.curFont.afm != nil {
		b, _ := encodeWinAnsi(s)
		if d.kerning {
			c.raw(tjOperand(b, d.curFont.afm, true) + " TJ")
		} else {
			c.raw("<" + hexEncode(b) + "> Tj")
		}
	} else {
		gids := d.curFont.ttf.encode(s)
		c.raw("<" + hexEncode(gidBytes(gids)) + "> Tj")
	}
	c.raw("ET")
}

// alignOffset returns the x offset within a box of boxWidth for a line of
// lineWidth under the alignment.
func alignOffset(a Align, boxWidth, lineWidth float64) float64 {
	switch a {
	case AlignCenter:
		return (boxWidth - lineWidth) / 2
	case AlignRight:
		return boxWidth - lineWidth
	default:
		return 0
	}
}

// wrapText splits s into lines that each fit within width at the current font
// and size, honouring explicit newlines.
func (d *Document) wrapText(s string, width, size float64, kerning bool) []string {
	var lines []string
	for _, para := range strings.Split(s, "\n") {
		lines = append(lines, d.wrapParagraph(para, width, size, kerning)...)
	}
	return lines
}

func (d *Document) wrapParagraph(para string, width, size float64, kerning bool) []string {
	if para == "" {
		return []string{""}
	}
	words := strings.Split(para, " ")
	var out []string
	cur := ""
	for _, w := range words {
		trial := w
		if cur != "" {
			trial = cur + " " + w
		}
		if d.widthOfSized(trial, size, kerning) <= width || cur == "" {
			cur = trial
		} else {
			out = append(out, cur)
			cur = w
		}
	}
	out = append(out, cur)
	return out
}

// Text mirrors Prawn::Document#text: flowing, wrapping, auto-paginating text
// that starts at the cursor and advances it downward.
func (d *Document) Text(s string, opts *TextOptions) {
	if !utf8.ValidString(s) {
		d.fail(ErrIncompatibleStringEncoding)
		return
	}
	restore := d.applyTextOptions(opts)
	defer restore()
	if d.curFont.afm != nil {
		if _, err := encodeWinAnsi(s); err != nil {
			d.fail(err)
			return
		}
	}
	align := AlignLeft
	extraLeading := 0.0
	if opts != nil {
		align = opts.Align
		extraLeading = opts.Leading
	}
	width := d.bounds().width
	lh := d.lineHeight(d.fontSize, extraLeading)
	asc := d.curAscender(d.fontSize)
	for _, line := range d.wrapText(s, width, d.fontSize, d.kerning) {
		if d.cursor-lh < 0 {
			d.StartNewPage()
		}
		lw := d.widthOfSized(line, d.fontSize, d.kerning)
		x := alignOffset(align, width, lw)
		d.drawLine(line, x, d.cursor-asc)
		d.cursor -= lh
	}
}

// DrawText mirrors Prawn::Document#draw_text(string, at: [x, y]): one line at an
// absolute (bounds-relative) point, no wrapping, cursor unchanged. The point is
// the text baseline, as in prawn.
func (d *Document) DrawText(s string, x, y float64, opts *TextOptions) {
	if !utf8.ValidString(s) {
		d.fail(ErrIncompatibleStringEncoding)
		return
	}
	restore := d.applyTextOptions(opts)
	defer restore()
	if d.curFont.afm != nil {
		if _, err := encodeWinAnsi(s); err != nil {
			d.fail(err)
			return
		}
	}
	d.drawLine(s, x, y)
}

// Overflow mirrors prawn's text_box :overflow modes.
type Overflow int

const (
	// OverflowTruncate stops at the box boundary, returning unprinted text.
	OverflowTruncate Overflow = iota
	// OverflowExpand draws every line even past the box height.
	OverflowExpand
	// OverflowError records ErrCannotFit if the text does not fit.
	OverflowError
)

// TextBoxOptions mirror the keyword arguments of Prawn::Document#text_box.
type TextBoxOptions struct {
	X, Y     float64
	Width    float64
	Height   float64
	Align    Align
	Overflow Overflow
	Size     float64
	Style    Style
	StyleSet bool
	Font     string
	Leading  float64
	Color    string
}

// TextBox mirrors Prawn::Document#text_box: lays flowing text into a positioned
// rectangle and returns the portion that did not fit. It leaves the cursor
// unchanged.
func (d *Document) TextBox(s string, o TextBoxOptions) string {
	if !utf8.ValidString(s) {
		d.fail(ErrIncompatibleStringEncoding)
		return s
	}
	if o.Width <= 0 {
		d.fail(fmt.Errorf("%w: text_box width must be positive", ErrCannotFit))
		return s
	}
	restore := d.applyTextOptions(&TextOptions{
		Size: o.Size, Style: o.Style, StyleSet: o.StyleSet, Font: o.Font, Color: o.Color,
	})
	defer restore()
	if d.curFont.afm != nil {
		if _, err := encodeWinAnsi(s); err != nil {
			d.fail(err)
			return s
		}
	}

	lines := d.wrapText(s, o.Width, d.fontSize, d.kerning)
	lh := d.lineHeight(d.fontSize, o.Leading)
	asc := d.curAscender(d.fontSize)
	used := 0.0
	printed := 0
	for _, line := range lines {
		if o.Height > 0 && used+lh > o.Height+1e-9 {
			if o.Overflow == OverflowError {
				d.fail(fmt.Errorf("%w: text does not fit in text_box", ErrCannotFit))
			}
			if o.Overflow != OverflowExpand {
				break
			}
		}
		lw := d.widthOfSized(line, d.fontSize, d.kerning)
		x := o.X + alignOffset(o.Align, o.Width, lw)
		baseline := o.Y - used - asc
		d.drawLine(line, x, baseline)
		used += lh
		printed++
	}
	if printed >= len(lines) {
		return ""
	}
	return strings.Join(lines[printed:], "\n")
}
