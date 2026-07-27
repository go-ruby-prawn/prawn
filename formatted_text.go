// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-ruby-prawn/prawn authors

package prawn

import "strings"

// This file mirrors prawn's formatted-text surface: an array of styled runs
// (Prawn::Document#formatted_text) and the inline-formatting markup
// (text …, inline_format: true) with <b>/<i>/<u> and <font>/<color> tags.

// FormattedFragment is one styled run of a formatted-text array (prawn's
// { text:, styles:, size:, color:, font: } hash).
type FormattedFragment struct {
	Text   string
	Bold   bool
	Italic bool
	Size   float64 // 0 = document size
	Color  string  // "" = current fill color
	Font   string  // "" = current family
}

// styleFor resolves the fragment's font style.
func (f FormattedFragment) style() Style {
	switch {
	case f.Bold && f.Italic:
		return StyleBoldItalic
	case f.Bold:
		return StyleBold
	case f.Italic:
		return StyleItalic
	default:
		return StyleNormal
	}
}

// FormattedText mirrors Prawn::Document#formatted_text(array): it lays a sequence
// of styled runs as flowing, wrapping text starting at the cursor, advancing it
// downward. Runs are laid out inline; a run wraps to the next line when it no
// longer fits the bounding-box width.
func (d *Document) FormattedText(frags []FormattedFragment, opts *TextOptions) {
	restore := d.applyTextOptions(opts)
	defer restore()

	baseFamily := d.FontFamily()
	if d.curFont.afm == nil {
		baseFamily = d.curFont.ttf.name
	}
	width := d.bounds().width

	type word struct {
		text string
		frag FormattedFragment
	}
	var words []word
	for _, f := range frags {
		for i, w := range strings.Fields(f.Text) {
			_ = i
			words = append(words, word{text: w, frag: f})
		}
	}

	x := 0.0
	// Line height uses the tallest fragment size on the document.
	maxSize := d.fontSize
	for _, f := range frags {
		if f.Size > maxSize {
			maxSize = f.Size
		}
	}
	lineHeight := d.curHeight(maxSize) + d.leading

	drawWord := func(w word, atX float64) {
		fam := w.frag.Font
		if fam == "" {
			fam = baseFamily
		}
		size := w.frag.Size
		if size == 0 {
			size = d.fontSize
		}
		prevFont, prevSize, prevFill := d.curFont, d.fontSize, d.fillColor
		d.Font(fam, w.frag.style())
		d.fontSize = size
		if w.frag.Color != "" {
			d.FillColor(w.frag.Color)
		}
		asc := d.curAscender(size)
		d.drawLine(w.text, atX, d.cursor-asc)
		d.curFont, d.fontSize = prevFont, prevSize
		if w.frag.Color != "" {
			d.fillColor = prevFill
			d.emitFillColor()
		}
	}

	for _, w := range words {
		size := w.frag.Size
		if size == 0 {
			size = d.fontSize
		}
		fam := w.frag.Font
		if fam == "" {
			fam = baseFamily
		}
		ww := d.measureWord(w.text, fam, w.frag.style(), size)
		space := d.measureWord(" ", fam, w.frag.style(), size)
		if x > 0 && x+ww > width {
			d.cursor -= lineHeight
			x = 0
		}
		if d.cursor-lineHeight < -1e-9 {
			d.StartNewPage()
			d.cursor = d.bounds().height
		}
		drawWord(w, x)
		x += ww + space
	}
	d.cursor -= lineHeight
}

// measureWord measures one word in an arbitrary family/style/size without
// disturbing the current font.
func (d *Document) measureWord(s, family string, style Style, size float64) float64 {
	prevFont := d.curFont
	d.Font(family, style)
	w := d.widthOfSized(s, size, d.kerning)
	d.curFont = prevFont
	return w
}

// TextInline mirrors Prawn::Document#text(string, inline_format: true): it parses
// <b>/<i>/<u> and <color rgb="…">/<font size="…"> tags into formatted fragments
// and renders them.
func (d *Document) TextInline(s string, opts *TextOptions) {
	frags := parseInline(s)
	d.FormattedText(frags, opts)
}

// parseInline turns a subset of prawn's inline markup into fragments.
func parseInline(s string) []FormattedFragment {
	var frags []FormattedFragment
	bold, italic := 0, 0
	color := ""
	var buf strings.Builder
	flush := func() {
		if buf.Len() == 0 {
			return
		}
		frags = append(frags, FormattedFragment{
			Text:   buf.String(),
			Bold:   bold > 0,
			Italic: italic > 0,
			Color:  color,
		})
		buf.Reset()
	}
	i := 0
	for i < len(s) {
		if s[i] == '<' {
			end := strings.IndexByte(s[i:], '>')
			if end < 0 {
				buf.WriteByte(s[i])
				i++
				continue
			}
			tag := s[i+1 : i+end]
			flush()
			switch {
			case tag == "b" || tag == "strong":
				bold++
			case tag == "/b" || tag == "/strong":
				if bold > 0 {
					bold--
				}
			case tag == "i" || tag == "em":
				italic++
			case tag == "/i" || tag == "/em":
				if italic > 0 {
					italic--
				}
			case strings.HasPrefix(tag, "color"):
				color = parseColorTag(tag)
			case tag == "/color":
				color = ""
			}
			i += end + 1
			continue
		}
		buf.WriteByte(s[i])
		i++
	}
	flush()
	return frags
}

// parseColorTag extracts rgb="RRGGBB" from a <color …> tag.
func parseColorTag(tag string) string {
	if idx := strings.Index(tag, "rgb="); idx >= 0 {
		rest := tag[idx+4:]
		rest = strings.TrimSpace(rest)
		rest = strings.Trim(rest, `"'`)
		if len(rest) >= 6 {
			return rest[:6]
		}
	}
	return ""
}
