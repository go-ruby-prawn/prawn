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

// ascentFactor approximates, as a fraction of the font size, the distance from
// the top of a line box down to the text baseline. It positions glyphs within
// the notional line box; it does not affect line advance.
const ascentFactor = 0.75

// TextOptions mirror the per-call keyword arguments of Prawn::Document#text /
// #draw_text (:size, :style, :align, :leading, :color, and a per-call font
// family). Zero-value fields keep the document's current settings; the
// overrides apply only for the duration of the call, exactly as in prawn.
type TextOptions struct {
	// Size overrides the font size in points for this call (0 = current size).
	Size float64
	// Style overrides the font style for this call.
	Style Style
	// StyleSet must be true to apply Style (so StyleNormal can be requested
	// explicitly rather than being read as "unset").
	StyleSet bool
	// Font overrides the font family for this call ("" = current family).
	Font string
	// Align sets horizontal alignment (Text only; ignored by DrawText).
	Align Align
	// Leading adds extra leading (line spacing) in points for this call.
	Leading float64
	// Color overrides the fill (text) color for this call as a hex string
	// "RRGGBB" or "#RRGGBB" ("" = current fill color).
	Color string
}

// Font mirrors Prawn::Document#font(name, style:). It selects one of the 14
// standard PDF fonts by prawn family name (case-insensitive) and an optional
// style. An unregistered family records ErrUnknownFont.
func (d *Document) Font(name string, style Style) {
	if _, ok := stdFonts[strings.ToLower(name)]; !ok {
		d.fail(fmt.Errorf("%w: %s", ErrUnknownFont, name))
		return
	}
	d.fontFamily = name
	d.fontStyle = style
	d.applyFont()
}

// FontFamily returns the current font family name.
func (d *Document) FontFamily() string { return d.fontFamily }

// FontStyle returns the current font style.
func (d *Document) FontStyle() Style { return d.fontStyle }

// FontSize mirrors Prawn::Document#font_size = n (the setter form): it sets the
// current font size in points.
func (d *Document) FontSize(size float64) {
	d.fontSizePts = size
	d.applyFont()
}

// FontSizeValue mirrors Prawn::Document#font_size (the reader form).
func (d *Document) FontSizeValue() float64 { return d.fontSizePts }

// Leading sets the document-wide extra leading (Prawn::Document#default_leading).
func (d *Document) Leading(n float64) { d.leading = n }

// FillColor mirrors Prawn::Document#fill_color = "RRGGBB": the color used to
// fill shapes and paint text.
func (d *Document) FillColor(hex string) {
	rgb, err := parseHexColor(hex)
	if err != nil {
		d.fail(err)
		return
	}
	d.fillColor = rgb
	d.applyFillColor()
}

// StrokeColor mirrors Prawn::Document#stroke_color = "RRGGBB": the color used to
// stroke lines and shape outlines.
func (d *Document) StrokeColor(hex string) {
	rgb, err := parseHexColor(hex)
	if err != nil {
		d.fail(err)
		return
	}
	d.strokeColor = rgb
	d.applyStrokeColor()
}

// FillColorValue and StrokeColorValue return the current colors as hex strings.
func (d *Document) FillColorValue() string   { return hexOf(d.fillColor) }
func (d *Document) StrokeColorValue() string { return hexOf(d.strokeColor) }

func hexOf(c [3]int) string { return fmt.Sprintf("%02X%02X%02X", c[0], c[1], c[2]) }

func parseHexColor(hex string) ([3]int, error) {
	s := strings.TrimPrefix(hex, "#")
	if len(s) != 6 {
		return [3]int{}, fmt.Errorf("prawn: invalid color %q", hex)
	}
	var out [3]int
	for i := 0; i < 3; i++ {
		v, err := strconv.ParseUint(s[i*2:i*2+2], 16, 8)
		if err != nil {
			return [3]int{}, fmt.Errorf("prawn: invalid color %q", hex)
		}
		out[i] = int(v)
	}
	return out, nil
}

// lineHeight is the vertical advance for one line: the font size plus the
// document leading plus any per-call leading. Prawn derives this from the font's
// AFM ascender/descender/line-gap; here it is approximated as font_size +
// leading, documented as such in doc.go.
func (d *Document) lineHeight(size, extraLeading float64) float64 {
	return size + d.leading + extraLeading
}

// applyTextOptions applies per-call overrides and returns a function that
// restores the previous state. A nil opts is a no-op.
func (d *Document) applyTextOptions(opts *TextOptions) func() {
	if opts == nil {
		return func() {}
	}
	prevFamily, prevStyle, prevSize := d.fontFamily, d.fontStyle, d.fontSizePts
	prevFill := d.fillColor
	changedFont := false
	if opts.Font != "" {
		if _, ok := stdFonts[strings.ToLower(opts.Font)]; !ok {
			d.fail(fmt.Errorf("%w: %s", ErrUnknownFont, opts.Font))
		} else {
			d.fontFamily = opts.Font
			changedFont = true
		}
	}
	if opts.StyleSet {
		d.fontStyle = opts.Style
		changedFont = true
	}
	if opts.Size > 0 {
		d.fontSizePts = opts.Size
		changedFont = true
	}
	if changedFont {
		d.applyFont()
	}
	changedColor := false
	if opts.Color != "" {
		if rgb, err := parseHexColor(opts.Color); err != nil {
			d.fail(err)
		} else {
			d.fillColor = rgb
			d.applyFillColor()
			changedColor = true
		}
	}
	return func() {
		d.fontFamily, d.fontStyle, d.fontSizePts = prevFamily, prevStyle, prevSize
		if changedFont {
			d.applyFont()
		}
		if changedColor {
			d.fillColor = prevFill
			d.applyFillColor()
		}
	}
}

// wrapText splits s into lines that each fit within width, honouring explicit
// newlines. It relies on fpdf's font metrics (GetStringWidth) so wrapping
// matches the emitted glyph widths.
func (d *Document) wrapText(s string, width float64) []string {
	var lines []string
	for _, para := range strings.Split(s, "\n") {
		wrapped := d.f.SplitText(para, width)
		if len(wrapped) == 0 {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, wrapped...)
	}
	return lines
}

// alignOffset returns the x offset within a box of the given width for a line of
// the given rendered width under the alignment.
func alignOffset(a Align, boxWidth, lineWidth float64) float64 {
	switch a {
	case AlignCenter:
		return (boxWidth - lineWidth) / 2
	case AlignRight:
		return boxWidth - lineWidth
	default: // AlignLeft, AlignJustify (treated as left for the last/short line)
		return 0
	}
}

// Text mirrors Prawn::Document#text: it draws a string as flowing text that
// wraps to the width of the margin box, starting at the current cursor and
// advancing it downward. When the text reaches the bottom of the page it
// continues on a new page (prawn's default flowing behaviour).
func (d *Document) Text(s string, opts *TextOptions) {
	if !utf8.ValidString(s) {
		d.fail(ErrIncompatibleStringEncoding)
		return
	}
	restore := d.applyTextOptions(opts)
	defer restore()

	align := AlignLeft
	extraLeading := 0.0
	if opts != nil {
		align = opts.Align
		extraLeading = opts.Leading
	}
	width := d.boundsWidth()
	lh := d.lineHeight(d.fontSizePts, extraLeading)
	for _, line := range d.wrapText(s, width) {
		if d.cursor-lh < 0 {
			d.StartNewPage()
		}
		lw := d.f.GetStringWidth(line)
		x := alignOffset(align, width, lw)
		baseline := d.cursor - d.fontSizePts*ascentFactor
		fx, fy := d.toFpdfXY(x, baseline)
		d.f.Text(fx, fy, line)
		d.cursor -= lh
	}
}

// DrawText mirrors Prawn::Document#draw_text(string, at: [x, y]): it draws the
// string once at an absolute point (bounds-relative, y measured up from the
// bottom of the margin box) without any wrapping and without moving the cursor.
func (d *Document) DrawText(s string, x, y float64, opts *TextOptions) {
	if !utf8.ValidString(s) {
		d.fail(ErrIncompatibleStringEncoding)
		return
	}
	restore := d.applyTextOptions(opts)
	defer restore()
	fx, fy := d.toFpdfXY(x, y)
	d.f.Text(fx, fy, s)
}

// Overflow mirrors prawn's text_box :overflow modes.
type Overflow int

const (
	// OverflowTruncate stops at the box boundary and returns the unprinted text
	// (prawn :truncate, the default).
	OverflowTruncate Overflow = iota
	// OverflowExpand draws every line even past the box height (prawn :expand).
	OverflowExpand
	// OverflowError records ErrCannotFit if the text does not fit (prawn
	// :error).
	OverflowError
)

// TextBoxOptions mirror the keyword arguments of Prawn::Document#text_box.
type TextBoxOptions struct {
	// X, Y are the bounds-relative coordinates of the box's top-left corner
	// (Y measured up from the bottom of the margin box).
	X, Y float64
	// Width is the box width in points (required, must be > 0).
	Width float64
	// Height, when > 0, bounds the box vertically and drives Overflow.
	Height float64
	// Align sets horizontal alignment within the box.
	Align Align
	// Overflow selects the behaviour when the text exceeds Height.
	Overflow Overflow
	// Size, Style, StyleSet, Font, Leading, Color mirror TextOptions for the box.
	Size     float64
	Style    Style
	StyleSet bool
	Font     string
	Leading  float64
	Color    string
}

// TextBox mirrors Prawn::Document#text_box: it lays flowing text into a
// positioned rectangle and returns the portion that did not fit (empty when all
// of it was drawn). It does not move the document cursor.
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

	lines := d.wrapText(s, o.Width)
	lh := d.lineHeight(d.fontSizePts, o.Leading)
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
		lw := d.f.GetStringWidth(line)
		x := o.X + alignOffset(o.Align, o.Width, lw)
		top := o.Y - used
		baseline := top - d.fontSizePts*ascentFactor
		fx, fy := d.toFpdfXY(x, baseline)
		d.f.Text(fx, fy, line)
		used += lh
		printed++
	}
	if printed >= len(lines) {
		return ""
	}
	return strings.Join(lines[printed:], "\n")
}
