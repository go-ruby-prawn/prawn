// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-ruby-prawn/prawn authors

package prawn

// This file mirrors prawn's vector-graphics DSL. As in prawn, shape methods
// (Rectangle, Line, Circle, Ellipse) append to a pending path which is then
// painted by Stroke, Fill or FillAndStroke; the Stroke*/Fill* convenience
// methods build and paint in one call.

// pathOp draws one accumulated shape with the given fpdf style string
// ("D" = stroke, "F" = fill, "DF" = both).
type pathOp func(style string)

// LineWidth mirrors Prawn::Document#line_width = n.
func (d *Document) LineWidth(n float64) {
	d.lineWidth = n
	d.f.SetLineWidth(n)
}

// LineWidthValue mirrors Prawn::Document#line_width (reader).
func (d *Document) LineWidthValue() float64 { return d.lineWidth }

// Rectangle mirrors Prawn::Document#rectangle([x, y], width, height): it adds a
// rectangle whose top-left corner is the bounds-relative point (x, y) to the
// pending path.
func (d *Document) Rectangle(x, y, w, h float64) {
	fx, fy := d.toFpdfXY(x, y)
	d.path = append(d.path, func(style string) { d.f.Rect(fx, fy, w, h, style) })
}

// Line mirrors Prawn::Document#line([x1, y1], [x2, y2]): it adds a line segment
// between two bounds-relative points to the pending path.
func (d *Document) Line(x1, y1, x2, y2 float64) {
	fx1, fy1 := d.toFpdfXY(x1, y1)
	fx2, fy2 := d.toFpdfXY(x2, y2)
	d.path = append(d.path, func(style string) { d.f.Line(fx1, fy1, fx2, fy2) })
}

// Circle mirrors Prawn::Document#circle([x, y], radius): it adds a circle
// centered on the bounds-relative point (x, y) to the pending path.
func (d *Document) Circle(x, y, r float64) {
	fx, fy := d.toFpdfXY(x, y)
	d.path = append(d.path, func(style string) { d.f.Circle(fx, fy, r, style) })
}

// Ellipse mirrors Prawn::Document#ellipse([x, y], rx, ry): it adds an ellipse
// centered on (x, y) with horizontal/vertical radii rx/ry to the pending path.
func (d *Document) Ellipse(x, y, rx, ry float64) {
	fx, fy := d.toFpdfXY(x, y)
	d.path = append(d.path, func(style string) { d.f.Ellipse(fx, fy, rx, ry, 0, style) })
}

// paint renders and clears the pending path with the given style.
func (d *Document) paint(style string) {
	for _, op := range d.path {
		op(style)
	}
	d.path = nil
}

// Stroke mirrors Prawn::Document#stroke: outline the pending path.
func (d *Document) Stroke() { d.paint("D") }

// Fill mirrors Prawn::Document#fill: fill the pending path.
func (d *Document) Fill() { d.paint("F") }

// FillAndStroke mirrors Prawn::Document#fill_and_stroke: fill then outline the
// pending path.
func (d *Document) FillAndStroke() { d.paint("DF") }

// StrokeRectangle mirrors Prawn::Document#stroke_rectangle.
func (d *Document) StrokeRectangle(x, y, w, h float64) { d.Rectangle(x, y, w, h); d.Stroke() }

// FillRectangle mirrors Prawn::Document#fill_rectangle.
func (d *Document) FillRectangle(x, y, w, h float64) { d.Rectangle(x, y, w, h); d.Fill() }

// StrokeLine mirrors Prawn::Document#stroke_line([x1, y1], [x2, y2]).
func (d *Document) StrokeLine(x1, y1, x2, y2 float64) { d.Line(x1, y1, x2, y2); d.Stroke() }

// StrokeCircle mirrors Prawn::Document#stroke_circle([x, y], radius).
func (d *Document) StrokeCircle(x, y, r float64) { d.Circle(x, y, r); d.Stroke() }

// FillCircle mirrors Prawn::Document#fill_circle([x, y], radius).
func (d *Document) FillCircle(x, y, r float64) { d.Circle(x, y, r); d.Fill() }

// StrokeEllipse mirrors Prawn::Document#stroke_ellipse([x, y], rx, ry).
func (d *Document) StrokeEllipse(x, y, rx, ry float64) { d.Ellipse(x, y, rx, ry); d.Stroke() }

// FillEllipse mirrors Prawn::Document#fill_ellipse([x, y], rx, ry).
func (d *Document) FillEllipse(x, y, rx, ry float64) { d.Ellipse(x, y, rx, ry); d.Fill() }

// StrokeBounds mirrors Prawn::Document#stroke_bounds: outline the margin box.
func (d *Document) StrokeBounds() {
	d.StrokeRectangle(0, d.boundsHeight(), d.boundsWidth(), d.boundsHeight())
}
