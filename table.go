// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-ruby-prawn/prawn authors

package prawn

// This file implements a basic subset of the prawn-table gem's
// Prawn::Document#table. It covers the common grid: explicit or automatic column
// widths, an optional bold header row, per-cell padding, cell borders, and
// placement at the cursor. Row/column spanning, per-cell style callbacks and
// automatic multi-page splitting are out of scope (see doc.go).

// TableOptions mirror the frequently used options of prawn-table's table
// method.
type TableOptions struct {
	// ColumnWidths sets each column's width in points. When nil, the width is
	// split evenly across the margin box.
	ColumnWidths []float64
	// Header, when true, renders the first data row in bold (prawn header: true).
	Header bool
	// CellPadding is the padding inside every cell in points (prawn
	// cell.padding). Zero selects prawn's 5pt default.
	CellPadding float64
	// BorderWidth is the width of the cell borders in points. Zero selects
	// prawn's 1pt default; a negative value draws no borders.
	BorderWidth float64
	// FontSize is the cell text size in points. Zero keeps the current size.
	FontSize float64
	// RowHeight forces every row's height in points. Zero derives it from the
	// font size and padding.
	RowHeight float64
	// At, when set, places the table's top-left corner at the bounds-relative
	// point (AtX, AtY) instead of at the cursor.
	AtX, AtY float64
	AtSet    bool
}

// TableResult reports the geometry of a rendered table, so callers (and tests)
// can reason about where it landed.
type TableResult struct {
	// Width and Height are the overall table dimensions in points.
	Width, Height float64
	// ColumnWidths and RowHeights are the resolved per-column / per-row sizes.
	ColumnWidths []float64
	RowHeights   []float64
}

// Table mirrors Prawn::Document#table(data, options): it draws a grid of string
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
		fontSize = d.fontSizePts
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
		w := d.boundsWidth() / float64(cols)
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

	// Top-left origin (bounds-relative, y up).
	originX := 0.0
	originY := d.cursor
	if o.AtSet {
		originX, originY = o.AtX, o.AtY
	}

	// Save current text state; the header row temporarily switches to bold.
	prevStyle := d.fontStyle
	prevSize := d.fontSizePts
	d.fontSizePts = fontSize
	d.applyFont()

	y := originY
	if drawBorders {
		d.f.SetLineWidth(border)
	}
	for r, row := range data {
		x := originX
		bold := o.Header && r == 0
		if bold {
			d.fontStyle = StyleBold
			d.applyFont()
		}
		for c := 0; c < cols; c++ {
			cw := colWidths[c]
			if drawBorders {
				fx, fy := d.toFpdfXY(x, y)
				d.f.SetDrawColor(d.strokeColor[0], d.strokeColor[1], d.strokeColor[2])
				d.f.Rect(fx, fy, cw, rowHeights[r], "D")
			}
			var text string
			if c < len(row) {
				text = row[c]
			}
			if text != "" {
				// Baseline: padding down from the cell top.
				baseline := y - padding - fontSize*ascentFactor
				tx, ty := d.toFpdfXY(x+padding, baseline)
				d.f.Text(tx, ty, text)
			}
			x += cw
		}
		if bold {
			d.fontStyle = prevStyle
			d.applyFont()
		}
		y -= rowHeights[r]
	}

	// Restore state and advance the cursor past the table when flowing.
	d.fontStyle = prevStyle
	d.fontSizePts = prevSize
	d.applyFont()
	if drawBorders {
		d.f.SetLineWidth(d.lineWidth)
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
