// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-ruby-prawn/prawn authors

package prawn

import (
	"bytes"
	"testing"
)

// TestParseTTFCmapError assembles a minimal-but-complete sfnt whose cmap uses an
// unsupported subtable format, so parseTTF surfaces the parseCmap error.
func TestParseTTFCmapError(t *testing.T) {
	head := make([]byte, 54)
	be16put(head[18:], 1000) // unitsPerEm
	// indexToLocFormat (short) left as 0
	hhea := make([]byte, 36)
	be16put(hhea[34:], 1) // numberOfHMetrics
	maxp := make([]byte, 32)
	be16put(maxp[4:], 1) // numGlyphs = 1
	hmtx := make([]byte, 4)
	loca := make([]byte, 4) // (numGlyphs+1) short entries
	glyf := make([]byte, 4)

	var cmap bytes.Buffer
	put16 := func(v uint16) { cmap.Write([]byte{byte(v >> 8), byte(v)}) }
	put32 := func(v uint32) { cmap.Write([]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}) }
	put16(0) // version
	put16(1) // numTables
	put16(3)
	put16(1)
	put32(12)
	put16(99) // unsupported format

	font := writeSFNT(map[string][]byte{
		"head": head, "hhea": hhea, "maxp": maxp, "hmtx": hmtx,
		"loca": loca, "glyf": glyf, "cmap": cmap.Bytes(),
	})
	if _, err := parseTTF(font); err == nil {
		t.Fatal("want cmap error propagated from parseTTF")
	}
}

// TestParseCmap4OutOfRange covers the bounds guard in the rangeOffset path: a
// segment whose idRangeOffset points past the end of the subtable.
func TestParseCmap4OutOfRange(t *testing.T) {
	var b bytes.Buffer
	put := func(v uint16) { b.Write([]byte{byte(v >> 8), byte(v)}) }
	segCount := 2
	segX2 := segCount * 2
	put(4)             // format
	put(0)             // length
	put(0)             // language
	put(uint16(segX2)) // segCountX2
	put(0)             // searchRange
	put(0)             // entrySelector
	put(0)             // rangeShift
	// endCodes
	put(0x0041)
	put(0xFFFF)
	put(0) // reservedPad
	// startCodes
	put(0x0041)
	put(0xFFFF)
	// idDelta
	put(0)
	put(1)
	// idRangeOffset: seg0 has a huge offset so idx overflows the table
	put(60000)
	put(0)

	tf := &ttfFont{cmap: map[rune]uint16{}}
	tf.parseCmap4(b.Bytes())
	if _, ok := tf.cmap['A']; ok {
		t.Fatal("out-of-range segment should map nothing")
	}
}
