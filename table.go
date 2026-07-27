// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-ruby-prawn/prawn authors

package prawn

// This file implements a common subset of the prawn-table gem's
// Prawn::Document#table: explicit or automatic column widths, an optional bold
// header row, per-cell padding, cell borders and placement at the cursor.
// Row/column spanning, per-cell style callbacks and automatic multi-page
// splitting are out of scope (named in doc.go).

// TableOptions mirror the frequently used options of prawn-table's table method.
type TableOptions struct {
	ColumnWidths []float64
	Header       bool
	CellPadding  float64
	BorderWidth  float64
	FontSize     float64
	RowHeight    float64
	AtX, AtY     float64
	AtSet        bool
}

// TableResult reports the geometry of a rendered table.
type TableResult struct {
	Width, Height float64
	ColumnWidths  []float64
	RowHeights    []float64
}

// Table mirrors Prawn::Document#table(data, options): draws a grid of string
// cells and advances the cursor past it. An empty data set is a no-op.
func (d *Document) Table(data [][]string, o TableOptions) TableResult {
	if len(data) == 0 {
		return TableResult{}
	}
	cols := 0
	for _, row := range data {
		if len(row) > cols {
			cols = len(row)
		}
	}
	if cols == 0 {
		return TableResult{}
	}

	padding := o.CellPadding
	if padding == 0 {
		padding = 5
	}
	border := o.BorderWidth
	drawBorders := border >= 0
	if border == 0 {
		border = 1
	}
	fontSize := o.FontSize
	if fontSize == 0 {
		fontSize = d.fontSize
	}
	rowHeight := o.RowHeight
	if rowHeight == 0 {
		rowHeight = fontSize + 2*padding
	}

	colWidths := make([]float64, cols)
	if o.ColumnWidths != nil {
		for i := range colWidths {
			if i < len(o.ColumnWidths) {
				colWidths[i] = o.ColumnWidths[i]
			} else {
				colWidths[i] = o.ColumnWidths[len(o.ColumnWidths)-1]
			}
		}
	} else {
		w := d.bounds().width / float64(cols)
		for i := range colWidths {
			colWidths[i] = w
		}
	}

	totalWidth := 0.0
	for _, w := range colWidths {
		totalWidth += w
	}
	rowHeights := make([]float64, len(data))
	for i := range rowHeights {
		rowHeights[i] = rowHeight
	}
	totalHeight := rowHeight * float64(len(data))

	originX, originY := 0.0, d.cursor
	if o.AtSet {
		originX, originY = o.AtX, o.AtY
	}

	prevFont, prevSize := d.curFont, d.fontSize
	d.fontSize = fontSize
	if drawBorders {
		d.LineWidth(border)
	}
	asc := d.curAscender(fontSize)

	y := originY
	for r, row := range data {
		x := originX
		bold := o.Header && r == 0
		if bold {
			d.curFont = d.registerAFM(d.afmFamilyName(), StyleBold)
			asc = d.curAscender(fontSize)
		}
		for c := 0; c < cols; c++ {
			cw := colWidths[c]
			if drawBorders {
				d.StrokeRectangle(x, y, cw, rowHeights[r])
			}
			var text string
			if c < len(row) {
				text = row[c]
			}
			if text != "" {
				baseline := y - padding - asc
				d.drawLine(text, x+padding, baseline)
			}
			x += cw
		}
		if bold {
			d.curFont = prevFont
			asc = d.curAscender(fontSize)
		}
		y -= rowHeights[r]
	}

	d.curFont, d.fontSize = prevFont, prevSize
	if drawBorders {
		d.LineWidth(d.lineWidth)
	}
	if !o.AtSet {
		d.cursor = originY - totalHeight
	}

	return TableResult{
		Width:        totalWidth,
		Height:       totalHeight,
		ColumnWidths: colWidths,
		RowHeights:   rowHeights,
	}
}

// afmFamilyName returns a Core-14 family name for the current font (falling back
// to Helvetica for embedded fonts), used to derive the bold header face.
func (d *Document) afmFamilyName() string {
	if d.curFont.afm != nil {
		switch {
		case containsFold(d.curFont.afm.Name, "Courier"):
			return "Courier"
		case containsFold(d.curFont.afm.Name, "Times"):
			return "Times-Roman"
		}
	}
	return "Helvetica"
}

func containsFold(s, sub string) bool { return indexFold(s, sub) >= 0 }

func indexFold(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if equalFold(s[i:i+len(sub)], sub) {
			return i
		}
	}
	return -1
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
