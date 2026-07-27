// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-ruby-prawn/prawn authors

package prawn

import (
	"fmt"
	"strconv"
	"strings"
)

// colorVal is an RGB or CMYK color in the normalized 0..1 range prawn writes to
// the content stream.
type colorVal struct {
	cmyk bool
	v    []float64
}

func rgbColor(r, g, b int) colorVal {
	return colorVal{v: []float64{float64(r) / 255.0, float64(g) / 255.0, float64(b) / 255.0}}
}

func cmykColor(c, m, y, k float64) colorVal {
	return colorVal{cmyk: true, v: []float64{c / 100.0, m / 100.0, y / 100.0, k / 100.0}}
}

// space returns the PDF color-space name for the value.
func (c colorVal) space() string {
	if c.cmyk {
		return "DeviceCMYK"
	}
	return "DeviceRGB"
}

// hex returns the "RRGGBB" form of an RGB color (CMYK is converted first).
func (c colorVal) hex() string {
	r, g, b := c.rgb()
	return fmt.Sprintf("%02X%02X%02X", r, g, b)
}

// rgb returns 0..255 RGB, converting CMYK with the naive formula prawn viewers
// approximate.
func (c colorVal) rgb() (int, int, int) {
	if c.cmyk {
		cc, m, y, k := c.v[0], c.v[1], c.v[2], c.v[3]
		r := 255 * (1 - cc) * (1 - k)
		g := 255 * (1 - m) * (1 - k)
		b := 255 * (1 - y) * (1 - k)
		return int(r + 0.5), int(g + 0.5), int(b + 0.5)
	}
	return int(c.v[0]*255 + 0.5), int(c.v[1]*255 + 0.5), int(c.v[2]*255 + 0.5)
}

// FillColor mirrors Prawn::Document#fill_color = "RRGGBB": it sets the fill color
// (used for shapes and text) and emits the color-space + scn operators.
func (d *Document) FillColor(hex string) {
	rgb, err := parseHexColor(hex)
	if err != nil {
		d.fail(err)
		return
	}
	d.fillColor = colorVal{v: []float64{float64(rgb[0]) / 255, float64(rgb[1]) / 255, float64(rgb[2]) / 255}}
	d.emitFillColor()
}

// FillColorCMYK mirrors Prawn::Document#fill_color(c, m, y, k) with 0..100
// components.
func (d *Document) FillColorCMYK(c, m, y, k float64) {
	d.fillColor = cmykColor(c, m, y, k)
	d.emitFillColor()
}

// StrokeColor mirrors Prawn::Document#stroke_color = "RRGGBB".
func (d *Document) StrokeColor(hex string) {
	rgb, err := parseHexColor(hex)
	if err != nil {
		d.fail(err)
		return
	}
	d.strokeColor = colorVal{v: []float64{float64(rgb[0]) / 255, float64(rgb[1]) / 255, float64(rgb[2]) / 255}}
	d.emitStrokeColor()
}

// StrokeColorCMYK mirrors Prawn::Document#stroke_color(c, m, y, k).
func (d *Document) StrokeColorCMYK(c, m, y, k float64) {
	d.strokeColor = cmykColor(c, m, y, k)
	d.emitStrokeColor()
}

// emitFillColor writes "/Space cs" (deduplicated per page) then "v… scn",
// exactly as prawn's set_color(:fill).
func (d *Document) emitFillColor() {
	sp := d.fillColor.space()
	if d.cur.fillCS != sp {
		d.cur.content.op("/"+sp, "cs")
		d.cur.fillCS = sp
	}
	d.cur.content.raw(realParams(d.fillColor.v...) + " scn")
}

// emitStrokeColor writes "/Space CS" then "v… SCN".
func (d *Document) emitStrokeColor() {
	sp := d.strokeColor.space()
	if d.cur.strokeCS != sp {
		d.cur.content.op("/"+sp, "CS")
		d.cur.strokeCS = sp
	}
	d.cur.content.raw(realParams(d.strokeColor.v...) + " SCN")
}

// FillColorValue and StrokeColorValue return the current colors as hex strings.
func (d *Document) FillColorValue() string   { return d.fillColor.hex() }
func (d *Document) StrokeColorValue() string { return d.strokeColor.hex() }

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
