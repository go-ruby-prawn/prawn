// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-ruby-prawn/prawn authors

package prawn

import "math"

// This file mirrors prawn's affine-transformation DSL (rotate / scale /
// translate / transformation_matrix) plus the save/restore graphics-state
// primitives. Each transform emits a "cm" operator, wrapped in q/Q when a block
// is supplied, exactly as prawn does.

// SaveGraphicsState mirrors Prawn::Document#save_graphics_state: emits "q".
func (d *Document) SaveGraphicsState() {
	d.cur.content.raw("q")
	d.cur.saveDepth++
}

// RestoreGraphicsState mirrors Prawn::Document#restore_graphics_state: emits
// "Q". Calling it without a matching save records ErrEmptyGraphicStateStack.
func (d *Document) RestoreGraphicsState() {
	if d.cur.saveDepth == 0 {
		d.fail(ErrEmptyGraphicStateStack)
		return
	}
	d.cur.content.raw("Q")
	d.cur.saveDepth--
}

// TransformationMatrix mirrors Prawn::Document#transformation_matrix(a,b,c,d,e,f):
// concatenates the matrix onto the CTM. When block is non-nil it is wrapped in
// q/Q; when nil the caller manages the graphics state.
func (d *Document) TransformationMatrix(a, b, c, e, f, g float64, block func()) {
	if block != nil {
		d.SaveGraphicsState()
	}
	d.cur.content.raw(realParams(a, b, c, e, f, g) + " cm")
	if block != nil {
		block()
		d.RestoreGraphicsState()
	}
}

// Translate mirrors Prawn::Document#translate(x, y) { … }.
func (d *Document) Translate(x, y float64, block func()) {
	d.TransformationMatrix(1, 0, 0, 1, x, y, block)
}

// Scale mirrors Prawn::Document#scale(factor) { … } (uniform scale about the
// coordinate origin).
func (d *Document) Scale(factor float64, block func()) {
	d.TransformationMatrix(factor, 0, 0, factor, 0, 0, block)
}

// ScaleAbout mirrors Prawn::Document#scale(factor, origin: [ox, oy]) { … }.
func (d *Document) ScaleAbout(factor, ox, oy float64, block func()) {
	x, y := d.mapToAbs(ox, oy)
	xp := factor * x
	yp := factor * y
	d.Translate(x-xp, y-yp, func() {
		d.TransformationMatrix(factor, 0, 0, factor, 0, 0, block)
	})
}

// Rotate mirrors Prawn::Document#rotate(angle) { … } (about the origin).
func (d *Document) Rotate(angle float64, block func()) {
	rad := angle * math.Pi / 180
	cos, sin := math.Cos(rad), math.Sin(rad)
	d.TransformationMatrix(cos, sin, -sin, cos, 0, 0, block)
}

// RotateAbout mirrors Prawn::Document#rotate(angle, origin: [ox, oy]) { … }.
func (d *Document) RotateAbout(angle, ox, oy float64, block func()) {
	rad := angle * math.Pi / 180
	cos, sin := math.Cos(rad), math.Sin(rad)
	x, y := d.mapToAbs(ox, oy)
	xp := (x * cos) - (y * sin)
	yp := (x * sin) + (y * cos)
	d.Translate(x-xp, y-yp, func() {
		d.TransformationMatrix(cos, sin, -sin, cos, 0, 0, block)
	})
}

// extGState is a registered transparency graphics state (ca/CA alpha).
type extGState struct {
	resName      string
	fill, stroke float64
}

// Transparency mirrors Prawn::Document#transparency(fill, stroke): sets the
// alpha for fill (and stroke) drawing within block via an ExtGState.
func (d *Document) Transparency(fill, stroke float64, block func()) {
	key := realParams(fill, stroke)
	gs, ok := d.gstates[key]
	if !ok {
		d.gsSeq++
		gs = &extGState{resName: numGSName(d.gsSeq), fill: fill, stroke: stroke}
		d.gstates[key] = gs
	}
	d.SaveGraphicsState()
	d.cur.gsUsed[gs.resName] = true
	d.cur.content.raw("/" + gs.resName + " gs")
	block()
	d.RestoreGraphicsState()
}

func numGSName(n int) string { return "GS" + itoa(n) }
