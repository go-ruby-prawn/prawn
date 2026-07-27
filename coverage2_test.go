// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-ruby-prawn/prawn authors

package prawn

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// ---- resolveImageSize: fit constrained by height ---------------------------

func TestResolveImageSizeFitHeight(t *testing.T) {
	if w, h := resolveImageSize(100, 100, ImageOptions{FitW: 80, FitH: 40}); w != 40 || h != 40 {
		t.Fatalf("fit-by-height %v %v", w, h)
	}
}

// ---- jpegInfo: non-0xFF scan byte ------------------------------------------

func TestJPEGInfoPadding(t *testing.T) {
	// A stray non-marker byte between SOI and SOF0 exercises the scan-advance
	// branch.
	data := []byte{0xFF, 0xD8, 0x00, 0xFF, 0xC0, 0x00, 0x11, 0x08, 0x00, 0x04, 0x00, 0x06, 0x03, 0, 0, 0, 0xFF, 0xD9}
	w, h, comps, err := jpegInfo(data)
	if err != nil || w != 6 || h != 4 || comps != 3 {
		t.Fatalf("jpegInfo padded: %v %d %d %d", err, w, h, comps)
	}
}

// ---- FormattedText: TTF base family + pagination ---------------------------

func TestFormattedTextTTFAndPaginate(t *testing.T) {
	defer fixedClock()()
	d := New(Options{})
	ttf, err := os.ReadFile(filepath.Join("testdata", "goregular.ttf"))
	if err != nil {
		t.Skip("no ttf")
	}
	if err := d.RegisterFontTTF("Go", ttf); err != nil {
		t.Fatal(err)
	}
	d.Font("Go", StyleNormal)
	// enough words to overflow a page and hit the pagination branch
	var frags []FormattedFragment
	for i := 0; i < 4000; i++ {
		frags = append(frags, FormattedFragment{Text: "word "})
	}
	d.FormattedText(frags, nil)
	if d.PageCount() < 2 {
		t.Fatalf("expected pagination, pages=%d", d.PageCount())
	}
	mustRender(t, d)
}

// ---- render: registered-but-unused image and gstate ------------------------

func TestRenderUnusedResources(t *testing.T) {
	defer fixedClock()()
	d := New(Options{})
	d.Text("x", nil)
	// inject an image and a gstate that no page references
	d.images["ghost"] = &imageXObject{
		resName: "I99", width: 1, height: 1, colorSpace: "DeviceRGB", bpc: 8,
		filter: "FlateDecode", data: deflate([]byte{0, 0, 0}),
	}
	d.gstates["ghost"] = &extGState{resName: "GS99", fill: 0.5, stroke: 0.5}
	b := mustRender(t, d)
	if bytes.Contains(b, []byte("/I99")) || bytes.Contains(b, []byte("/GS99")) {
		t.Fatal("unused resources should not be referenced")
	}
}

// ---- TTF subsetting: synthetic font with long loca, composite, odd padding -

func buildSyntheticFont() *ttfFont {
	put16 := func(b *bytes.Buffer, v uint16) { b.Write([]byte{byte(v >> 8), byte(v)}) }
	put32 := func(b *bytes.Buffer, v uint32) {
		b.Write([]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
	}

	// glyph 1: simple glyph, 17 bytes (odd length -> exercises word padding)
	var g1 bytes.Buffer
	put16(&g1, 1)      // numberOfContours = 1 (simple)
	put16(&g1, 0)      // xMin
	put16(&g1, 0)      // yMin
	put16(&g1, 10)     // xMax
	put16(&g1, 10)     // yMax
	put16(&g1, 0)      // endPtsOfContours[0]
	put16(&g1, 0)      // instructionLength
	g1.WriteByte(0x01) // flags: on-curve
	g1.WriteByte(10)   // x
	g1.WriteByte(10)   // y

	// glyph 2: composite referencing glyph 1
	var g2 bytes.Buffer
	put16(&g2, 0xFFFF) // numberOfContours = -1 (composite)
	put16(&g2, 0)      // bbox
	put16(&g2, 0)
	put16(&g2, 0)
	put16(&g2, 0)
	put16(&g2, weHaveAScale) // flags (byte args, a scale, no MORE)
	put16(&g2, 1)            // component glyphIndex = 1
	put16(&g2, 0)            // arg1/arg2 (2 bytes)
	put16(&g2, 0)            // scale (2 bytes)

	glyf := append(g1.Bytes(), g2.Bytes()...)

	// long loca: offsets for 3 glyphs (0=empty, 1=g1, 2=g2)
	var loca bytes.Buffer
	put32(&loca, 0)                         // glyph 0 start (empty)
	put32(&loca, 0)                         // glyph 0 end / glyph 1 start
	put32(&loca, uint32(g1.Len()))          // glyph 1 end / glyph 2 start
	put32(&loca, uint32(g1.Len()+g2.Len())) // glyph 2 end

	data := append(loca.Bytes(), glyf...)
	tf := &ttfFont{
		data:       data,
		longLoca:   true,
		numGlyphs:  3,
		hmtx:       []uint16{100, 200, 300},
		unitsPerEm: 1000,
		tables: map[string]tableRec{
			"loca": {offset: 0, length: uint32(loca.Len())},
			"glyf": {offset: uint32(loca.Len()), length: uint32(len(glyf))},
		},
		origToSub: map[uint16]uint16{0: 0, 1: 1, 2: 2},
		subToOrig: []uint16{0, 1, 2},
		toUnicode: map[uint16]rune{},
	}
	return tf
}

func TestBuildSubsetSynthetic(t *testing.T) {
	tf := buildSyntheticFont()
	glyf, loca := tf.buildSubset()
	if len(glyf) == 0 || len(loca) != 4*4 {
		t.Fatalf("subset glyf=%d loca=%d", len(glyf), len(loca))
	}
	// glyf length must be even (padding applied)
	if len(glyf)%2 != 0 {
		t.Fatal("glyf not word-aligned")
	}
}

func TestGlyphBytesLongLoca(t *testing.T) {
	tf := buildSyntheticFont()
	if tf.glyphBytes(0) != nil {
		t.Fatal("empty glyph should be nil")
	}
	if tf.glyphBytes(1) == nil {
		t.Fatal("glyph 1 bytes")
	}
}

func TestRemapCompositeTruncated(t *testing.T) {
	// A composite whose final component keeps MORE_COMPONENTS set but has no
	// following data: the next loop iteration must break on the length guard.
	tf := &ttfFont{origToSub: map[uint16]uint16{0: 0}, subToOrig: []uint16{0}}
	var g bytes.Buffer
	p16 := func(v uint16) { g.Write([]byte{byte(v >> 8), byte(v)}) }
	p16(0xFFFF) // composite
	p16(0)
	p16(0)
	p16(0)
	p16(0)
	p16(moreComponents) // says "more" but nothing follows -> guard break
	p16(7)              // glyphIndex
	p16(0)              // byte args
	tf.remapComposite(g.Bytes())
}

// ---- cmap: full-table selection and every format ---------------------------

type cmapRec struct {
	plat, enc uint16
	sub       []byte
}

func buildCmapTable(recs []cmapRec) []byte {
	var b bytes.Buffer
	put16 := func(v uint16) { b.Write([]byte{byte(v >> 8), byte(v)}) }
	put32 := func(v uint32) { b.Write([]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}) }
	put16(0)                 // version
	put16(uint16(len(recs))) // numTables
	headerLen := 4 + len(recs)*8
	offset := headerLen
	for _, r := range recs {
		put16(r.plat)
		put16(r.enc)
		put32(uint32(offset))
		offset += len(r.sub)
	}
	for _, r := range recs {
		b.Write(r.sub)
	}
	return b.Bytes()
}

func format4Sub() []byte {
	var b bytes.Buffer
	put := func(v uint16) { b.Write([]byte{byte(v >> 8), byte(v)}) }
	put(4)
	put(0)
	put(0)
	put(4) // segCountX2 = 4 (2 segments)
	put(0)
	put(0)
	put(0)
	put(0x0041) // endCode seg0
	put(0xFFFF) // endCode seg1
	put(0)      // reservedPad
	put(0x0041) // startCode seg0
	put(0xFFFF) // startCode seg1
	put(70 - 0x41)
	put(1)
	put(0) // rangeOffset seg0
	put(0) // rangeOffset seg1
	return b.Bytes()
}

func format12Sub() []byte {
	var b bytes.Buffer
	put16 := func(v uint16) { b.Write([]byte{byte(v >> 8), byte(v)}) }
	put32 := func(v uint32) { b.Write([]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}) }
	put16(12)
	put16(0)
	put32(0)
	put32(0)
	put32(1) // nGroups
	put32(0x41)
	put32(0x41)
	put32(300)
	return b.Bytes()
}

func format6Sub() []byte {
	b := make([]byte, 12)
	be16put(b[0:], 6)
	be16put(b[6:], 0x41)
	be16put(b[8:], 1)
	be16put(b[10:], 55)
	return b
}

func format0Sub() []byte {
	b := make([]byte, 262)
	be16put(b[0:], 0)
	b[6+0x41] = 44
	return b
}

func parseCmapWith(t *testing.T, sub []byte, plat, enc uint16) *ttfFont {
	t.Helper()
	table := buildCmapTable([]cmapRec{{plat, enc, sub}})
	tf := &ttfFont{data: table, tables: map[string]tableRec{"cmap": {0, uint32(len(table))}}}
	if err := tf.parseCmap(); err != nil {
		t.Fatalf("parseCmap: %v", err)
	}
	return tf
}

func TestParseCmapAllFormats(t *testing.T) {
	if parseCmapWith(t, format4Sub(), 3, 1).cmap['A'] != 70 {
		t.Fatal("format4")
	}
	if parseCmapWith(t, format12Sub(), 3, 10).cmap['A'] != 300 {
		t.Fatal("format12")
	}
	if parseCmapWith(t, format6Sub(), 0, 3).cmap['A'] != 55 {
		t.Fatal("format6")
	}
	if parseCmapWith(t, format0Sub(), 3, 0).cmap['A'] != 44 {
		t.Fatal("format0")
	}
	// scoring: multiple records, best (3,10) selected among (1,0) and (3,0)
	recs := []cmapRec{
		{1, 0, format0Sub()},
		{3, 0, format0Sub()},
		{3, 10, format12Sub()},
	}
	table := buildCmapTable(recs)
	tf := &ttfFont{data: table, tables: map[string]tableRec{"cmap": {0, uint32(len(table))}}}
	if err := tf.parseCmap(); err != nil {
		t.Fatal(err)
	}
	if tf.cmap['A'] != 300 {
		t.Fatalf("scoring picked wrong subtable: A=%d", tf.cmap['A'])
	}
}
