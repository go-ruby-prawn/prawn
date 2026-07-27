// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-ruby-prawn/prawn authors

package prawn

import "math"

// This file mirrors prawn's vector-graphics DSL. As in prawn, path-construction
// methods (MoveTo, LineTo, CurveTo, Rectangle, …) write their operators to the
// content stream immediately; a subsequent Stroke / Fill / FillAndStroke paints
// the accumulated path (emitting S / f / b). The Stroke*/Fill* convenience
// methods build a shape and paint it in one call.

// kappa approximates a quarter arc with a cubic Bézier (prawn's KAPPA).
var kappa = 4.0 * ((math.Sqrt2 - 1.0) / 3.0)

// LineWidth mirrors Prawn::Document#line_width = n: sets and writes "n w".
func (d *Document) LineWidth(n float64) {
	d.lineWidth = n
	d.cur.content.op(n, "w")
}

// LineWidthValue mirrors Prawn::Document#line_width (reader).
func (d *Document) LineWidthValue() float64 { return d.lineWidth }

// CapStyle mirrors Prawn::Document#cap_style: 0 butt, 1 round, 2 projecting.
func (d *Document) CapStyle(style int) {
	d.lineCap = style
	d.cur.content.op(style, "J")
}

// JoinStyle mirrors Prawn::Document#join_style: 0 miter, 1 round, 2 bevel.
func (d *Document) JoinStyle(style int) {
	d.lineJoin = style
	d.cur.content.op(style, "j")
}

// Dash mirrors Prawn::Document#dash(length, space:, phase:): sets a dash of
// alternating on/off lengths and writes the "[on off] phase d" operator.
func (d *Document) Dash(on, off, phase float64) {
	d.dash = []float64{on, off}
	d.dashPhase = phase
	d.cur.content.raw("[" + realParams(on, off) + "] " + realParams(phase) + " d")
}

// Undash mirrors Prawn::Document#undash: clears the dash pattern ("[] 0 d").
func (d *Document) Undash() {
	d.dash = nil
	d.dashPhase = 0
	d.cur.content.raw("[] 0 d")
}

// MoveTo mirrors Prawn::Document#move_to([x, y]): emits "x y m".
func (d *Document) MoveTo(x, y float64) {
	ax, ay := d.mapToAbs(x, y)
	d.cur.content.op(ax, ay, "m")
}

// LineTo mirrors Prawn::Document#line_to([x, y]): emits "x y l".
func (d *Document) LineTo(x, y float64) {
	ax, ay := d.mapToAbs(x, y)
	d.cur.content.op(ax, ay, "l")
}

// CurveTo mirrors Prawn::Document#curve_to(dest, bounds: [c1, c2]): emits the
// two control points and the destination followed by "c".
func (d *Document) CurveTo(destX, destY, c1x, c1y, c2x, c2y float64) {
	ax1, ay1 := d.mapToAbs(c1x, c1y)
	ax2, ay2 := d.mapToAbs(c2x, c2y)
	adx, ady := d.mapToAbs(destX, destY)
	d.cur.content.raw(realParams(ax1, ay1, ax2, ay2, adx, ady) + " c")
}

// ClosePath mirrors Prawn::Document#close_path: emits "h".
func (d *Document) ClosePath() { d.cur.content.raw("h") }

// Line mirrors Prawn::Document#line([x1, y1], [x2, y2]): MoveTo then LineTo.
func (d *Document) Line(x1, y1, x2, y2 float64) {
	d.MoveTo(x1, y1)
	d.LineTo(x2, y2)
}

// Rectangle mirrors Prawn::Document#rectangle([x, y], w, h): (x, y) is the
// upper-left corner; emits "x (y-h) w h re".
func (d *Document) Rectangle(x, y, w, h float64) {
	ax, ay := d.mapToAbs(x, y)
	d.cur.content.raw(realParams(ax, ay-h, w, h) + " re")
}

// Polygon mirrors Prawn::Document#polygon(points…): MoveTo the first point,
// LineTo the rest and back to the first, then "h".
func (d *Document) Polygon(points ...[2]float64) {
	if len(points) == 0 {
		return
	}
	d.MoveTo(points[0][0], points[0][1])
	for _, p := range points[1:] {
		d.LineTo(p[0], p[1])
	}
	d.LineTo(points[0][0], points[0][1])
	d.ClosePath()
}

// Ellipse mirrors Prawn::Document#ellipse([x, y], rx, ry): four Bézier arcs
// around the centre, ending with a MoveTo back to the centre (prawn's exact
// construction).
func (d *Document) Ellipse(x, y, rx, ry float64) {
	l1 := rx * kappa
	l2 := ry * kappa
	d.MoveTo(x+rx, y)
	d.CurveTo(x, y+ry, x+rx, y+l2, x+l1, y+ry)
	d.CurveTo(x-rx, y, x-l1, y+ry, x-rx, y+l2)
	d.CurveTo(x, y-ry, x-rx, y-l2, x-l1, y-ry)
	d.CurveTo(x+rx, y, x+l1, y-ry, x+rx, y-l2)
	d.MoveTo(x, y)
}

// Circle mirrors Prawn::Document#circle([x, y], r).
func (d *Document) Circle(x, y, r float64) { d.Ellipse(x, y, r, r) }

// Stroke mirrors Prawn::Document#stroke: paints the current path with "S".
func (d *Document) Stroke() { d.cur.content.raw("S") }

// Fill mirrors Prawn::Document#fill: paints the current path with "f".
func (d *Document) Fill() { d.cur.content.raw("f") }

// FillAndStroke mirrors Prawn::Document#fill_and_stroke: paints with "b".
func (d *Document) FillAndStroke() { d.cur.content.raw("b") }

// CloseAndStroke mirrors Prawn::Document#close_and_stroke: paints with "s".
func (d *Document) CloseAndStroke() { d.cur.content.raw("s") }

// StrokeRectangle mirrors Prawn::Document#stroke_rectangle.
func (d *Document) StrokeRectangle(x, y, w, h float64) { d.Rectangle(x, y, w, h); d.Stroke() }

// FillRectangle mirrors Prawn::Document#fill_rectangle.
func (d *Document) FillRectangle(x, y, w, h float64) { d.Rectangle(x, y, w, h); d.Fill() }

// StrokeLine mirrors Prawn::Document#stroke_line([x1, y1], [x2, y2]).
func (d *Document) StrokeLine(x1, y1, x2, y2 float64) { d.Line(x1, y1, x2, y2); d.Stroke() }

// StrokeCircle mirrors Prawn::Document#stroke_circle([x, y], r).
func (d *Document) StrokeCircle(x, y, r float64) { d.Circle(x, y, r); d.Stroke() }

// FillCircle mirrors Prawn::Document#fill_circle([x, y], r).
func (d *Document) FillCircle(x, y, r float64) { d.Circle(x, y, r); d.Fill() }

// StrokeEllipse mirrors Prawn::Document#stroke_ellipse([x, y], rx, ry).
func (d *Document) StrokeEllipse(x, y, rx, ry float64) { d.Ellipse(x, y, rx, ry); d.Stroke() }

// FillEllipse mirrors Prawn::Document#fill_ellipse([x, y], rx, ry).
func (d *Document) FillEllipse(x, y, rx, ry float64) { d.Ellipse(x, y, rx, ry); d.Fill() }

// FillPolygon mirrors Prawn::Document#fill_polygon.
func (d *Document) FillPolygon(points ...[2]float64) { d.Polygon(points...); d.Fill() }

// StrokePolygon mirrors Prawn::Document#stroke_polygon.
func (d *Document) StrokePolygon(points ...[2]float64) { d.Polygon(points...); d.Stroke() }

// StrokeBounds mirrors Prawn::Document#stroke_bounds: outline the bounding box.
func (d *Document) StrokeBounds() {
	b := d.bounds()
	d.StrokeRectangle(0, b.height, b.width, b.height)
}
