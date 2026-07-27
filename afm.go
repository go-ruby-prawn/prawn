// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-ruby-prawn/prawn authors

package prawn

// This file is the runtime for the Core-14 AFM (built-in) fonts: WinAnsi
// (cp1252) encoding, advance-width measurement and prawn's kerning algorithm
// (which turns a byte run into the [<str> kern <str>] operand of a TJ operator).
// The metric tables live in afm_data_gen.go.

import (
	"bytes"
	"strconv"
)

// cp1252Extra maps the 0x80–0x9F WinAnsi/cp1252 slots (which differ from
// Latin-1) to their Unicode code points. Undefined slots are absent.
var cp1252Extra = map[rune]byte{
	0x20AC: 0x80, 0x201A: 0x82, 0x0192: 0x83, 0x201E: 0x84, 0x2026: 0x85,
	0x2020: 0x86, 0x2021: 0x87, 0x02C6: 0x88, 0x2030: 0x89, 0x0160: 0x8A,
	0x2039: 0x8B, 0x0152: 0x8C, 0x017D: 0x8E, 0x2018: 0x91, 0x2019: 0x92,
	0x201C: 0x93, 0x201D: 0x94, 0x2022: 0x95, 0x2013: 0x96, 0x2014: 0x97,
	0x02DC: 0x98, 0x2122: 0x99, 0x0161: 0x9A, 0x203A: 0x9B, 0x0153: 0x9C,
	0x017E: 0x9E, 0x0178: 0x9F,
}

// encodeWinAnsi converts a UTF-8 string to WinAnsi (cp1252) bytes, mirroring
// prawn's normalize_encoding (String#encode("windows-1252")). A rune with no
// cp1252 representation yields ErrIncompatibleStringEncoding, exactly as prawn
// raises Prawn::Errors::IncompatibleStringEncoding.
func encodeWinAnsi(s string) ([]byte, error) {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		switch {
		case r < 0x80:
			out = append(out, byte(r))
		case r >= 0xA0 && r <= 0xFF:
			out = append(out, byte(r))
		default:
			if b, ok := cp1252Extra[r]; ok {
				out = append(out, b)
			} else {
				return nil, ErrIncompatibleStringEncoding
			}
		}
	}
	return out, nil
}

// tjElem is one element of a TJ operand: a run of bytes, or a kerning number.
type tjElem struct {
	str  []byte // non-nil for a string run
	kern int    // used when str == nil
}

// kernRuns splits WinAnsi bytes into TJ elements the way prawn's AFM#kern does:
// wherever a kern pair exists between two consecutive bytes, the run is broken
// and the kerning adjustment (-KPX) is inserted between the two runs.
func kernRuns(b []byte, f *afmFont) []tjElem {
	if len(b) == 0 {
		return []tjElem{{str: []byte{}}}
	}
	runs := []tjElem{{str: []byte{b[0]}}}
	for i := 1; i < len(b); i++ {
		if k, ok := f.kern[[2]byte{b[i-1], b[i]}]; ok {
			runs = append(runs, tjElem{kern: -k}, tjElem{str: []byte{b[i]}})
		} else {
			last := &runs[len(runs)-1]
			last.str = append(last.str, b[i])
		}
	}
	return runs
}

// tjOperand renders the [<hex> num …] operand for a TJ operator from a WinAnsi
// byte slice, applying kerning when kerning is true.
func tjOperand(b []byte, f *afmFont, kerning bool) string {
	var buf bytes.Buffer
	buf.WriteByte('[')
	if kerning {
		for i, e := range kernRuns(b, f) {
			if i > 0 {
				buf.WriteByte(' ')
			}
			if e.str != nil {
				buf.WriteByte('<')
				buf.WriteString(hexEncode(e.str))
				buf.WriteByte('>')
			} else {
				buf.WriteString(strconv.Itoa(e.kern))
			}
		}
	} else {
		buf.WriteByte('<')
		buf.WriteString(hexEncode(b))
		buf.WriteByte('>')
	}
	buf.WriteByte(']')
	return buf.String()
}

// unscaledWidth sums the per-byte advance widths (1000-em units), like prawn's
// unscaled_width_of.
func (f *afmFont) unscaledWidth(b []byte) int {
	total := 0
	for _, c := range b {
		total += f.widths[c]
	}
	return total
}

// totalKern returns the sum of KPX kern values for the byte run. prawn's kern
// array inserts -KPX between glyphs, so its total_kerning_offset equals
// -totalKern; the kerned width is therefore unscaled + totalKern (a negative
// KPX, the common case, tightens the run).
func (f *afmFont) totalKern(b []byte) int {
	sum := 0
	for i := 1; i < len(b); i++ {
		sum += f.kern[[2]byte{b[i-1], b[i]}]
	}
	return sum
}

// widthOf computes the rendered width of a WinAnsi byte run at the given size,
// with or without kerning, mirroring prawn's compute_width_of.
func (f *afmFont) widthOf(b []byte, size float64, kerning bool) float64 {
	scale := size / 1000.0
	if kerning {
		return float64(f.unscaledWidth(b)+f.totalKern(b)) * scale
	}
	return float64(f.unscaledWidth(b)) * scale
}

// height returns the line height at the given size (ascender − descender +
// line_gap, scaled), matching prawn's AFM font height.
func (f *afmFont) height(size float64) float64 {
	lineGap := (f.BBox[3] - f.BBox[1]) - (f.Ascender - f.Descender)
	return (f.Ascender - f.Descender + lineGap) * size / 1000.0
}

// ascenderScaled returns the ascender at the given size (used to place the first
// baseline: top − ascender·size/1000, prawn's convention).
func (f *afmFont) ascenderScaled(size float64) float64 { return f.Ascender * size / 1000.0 }
