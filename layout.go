// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-ruby-prawn/prawn authors

package prawn

import "strconv"

// This file implements prawn's layout helpers that create or iterate nested
// coordinate frames: bounding_box, the column/row grid, and repeaters / page
// numbering.

func itoa(n int) string { return strconv.Itoa(n) }

// BoundingBox mirrors Prawn::Document#bounding_box([x, y], width:, height:) { … }.
// (x, y) is the top-left corner in the current bounds' coordinates. Inside the
// block, bounds and the cursor are relative to the new box; afterwards the
// document cursor is left at the box's bottom, as prawn does.
func (d *Document) BoundingBox(x, y, width, height float64, block func()) {
	parent := d.bounds()
	nb := boundingBox{
		absLeft:   parent.absLeft + x,
		absBottom: parent.absBottom + (y - height),
		width:     width,
		height:    height,
	}
	d.boundsS = append(d.boundsS, nb)
	prevCursor := d.cursor
	d.cursor = height
	block()
	d.boundsS = d.boundsS[:len(d.boundsS)-1]
	// prawn: self.y = bounding_box.absolute_bottom → parent-relative (y - height).
	_ = prevCursor
	d.cursor = y - height
}

// Grid mirrors Prawn::Document#define_grid / #grid: a regular columns×rows grid
// with a uniform gutter, laid over the current bounding box.
type Grid struct {
	d       *Document
	Columns int
	Rows    int
	Gutter  float64
}

// DefineGrid mirrors Prawn::Document#define_grid(columns:, rows:, gutter:).
func (d *Document) DefineGrid(columns, rows int, gutter float64) *Grid {
	return &Grid{d: d, Columns: columns, Rows: rows, Gutter: gutter}
}

// ColumnWidth returns the width of a single grid column.
func (g *Grid) ColumnWidth() float64 {
	b := g.d.bounds()
	return (b.width - float64(g.Columns-1)*g.Gutter) / float64(g.Columns)
}

// RowHeight returns the height of a single grid row.
func (g *Grid) RowHeight() float64 {
	b := g.d.bounds()
	return (b.height - float64(g.Rows-1)*g.Gutter) / float64(g.Rows)
}

// Box runs block inside the bounding box of grid cell (row, col), 0-indexed
// from the top-left, mirroring Prawn::Document::Grid::GridBox.
func (g *Grid) Box(row, col int, block func()) {
	cw := g.ColumnWidth()
	rh := g.RowHeight()
	b := g.d.bounds()
	x := float64(col) * (cw + g.Gutter)
	topY := b.height - float64(row)*(rh+g.Gutter)
	g.d.BoundingBox(x, topY, cw, rh, block)
}

// repeater is a deferred draw executed on every page that matches its filter.
type repeater struct {
	filter func(page, total int) bool
	block  func()
}

// Repeat mirrors Prawn::Document#repeat(filter) { … }: the block is executed on
// each page (at render time) for which filter(pageNumber, totalPages) is true.
func (d *Document) Repeat(filter func(page, total int) bool, block func()) {
	d.repeaters = append(d.repeaters, &repeater{filter: filter, block: block})
}

// rptPage / rptTotal expose the page context to a repeater block.
func (d *Document) rptContext() (int, int) { return d.rptPage, d.rptTotal }

// RepeatPageNumber returns the current page number while a repeater/NumberPages
// block runs (1-based); 0 outside a repeater.
func (d *Document) RepeatPageNumber() int { return d.rptPage }

// RepeatPageCount returns the total page count while a repeater block runs.
func (d *Document) RepeatPageCount() int { return d.rptTotal }

// runRepeaters executes every repeater against every matching page, with the
// drawing context (current page, bounds, cursor) pointed at that page.
func (d *Document) runRepeaters() {
	if len(d.repeaters) == 0 {
		return
	}
	total := len(d.pages)
	savedCur, savedBounds, savedCursor := d.cur, d.boundsS, d.cursor
	savedPage, savedTotal := d.rptPage, d.rptTotal
	for i, p := range d.pages {
		d.cur = p
		d.boundsS = []boundingBox{{
			absLeft:   d.mLeft,
			absBottom: d.mBottom,
			width:     p.width - d.mLeft - d.mRight,
			height:    p.height - d.mTop - d.mBottom,
		}}
		d.cursor = d.boundsS[0].height
		d.rptPage, d.rptTotal = i+1, total
		for _, r := range d.repeaters {
			if r.filter(i+1, total) {
				r.block()
			}
		}
	}
	d.cur, d.boundsS, d.cursor = savedCur, savedBounds, savedCursor
	d.rptPage, d.rptTotal = savedPage, savedTotal
}

// NumberPages mirrors Prawn::Document#number_pages(string, options): stamps a
// page number onto every page. In the format string, "<page>" and "<total>" are
// replaced with the current page number and total page count. The text baseline
// is placed at the bounds-relative point (x, y).
func (d *Document) NumberPages(format string, x, y float64, opts *TextOptions) {
	d.Repeat(func(page, total int) bool { return true }, func() {
		page, total := d.rptContext()
		s := replacePlaceholders(format, page, total)
		d.DrawText(s, x, y, opts)
	})
}

func replacePlaceholders(format string, page, total int) string {
	out := make([]byte, 0, len(format))
	for i := 0; i < len(format); {
		if rest := format[i:]; len(rest) >= 6 && rest[:6] == "<page>" {
			out = append(out, itoa(page)...)
			i += 6
		} else if len(rest) >= 7 && rest[:7] == "<total>" {
			out = append(out, itoa(total)...)
			i += 7
		} else {
			out = append(out, format[i])
			i++
		}
	}
	return string(out)
}
