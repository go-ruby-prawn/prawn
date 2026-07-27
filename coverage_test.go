// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-ruby-prawn/prawn authors

package prawn

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// ---- formatReal / DrawText / resolveImageSize edge cases -------------------

func TestFormatRealNegZero(t *testing.T) {
	if got := formatReal(math.Copysign(0.0000001, -1)); got != "0" {
		t.Fatalf("neg tiny -> %q", got)
	}
}

func TestDrawTextUnmappable(t *testing.T) {
	d := New(Options{})
	d.DrawText("\U0001F600", 0, 0, nil)
	if !errors.Is(d.Error(), ErrIncompatibleStringEncoding) {
		t.Fatal("want incompatible encoding")
	}
}

func TestResolveImageSizeBoth(t *testing.T) {
	if w, h := resolveImageSize(100, 50, ImageOptions{Width: 30, Height: 70}); w != 30 || h != 70 {
		t.Fatalf("both %v %v", w, h)
	}
}

// ---- JPEG colorspaces + marker walking -------------------------------------

func grayJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 8, 6))
	for y := 0; y < 6; y++ {
		for x := 0; x < 8; x++ {
			img.SetGray(x, y, color.Gray{Y: uint8(x * 20)})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// craftJPEG builds a minimal JPEG stream: SOI, an APP0 segment (to exercise the
// segment-skip path), an optional RST run, a SOF0 frame header with the given
// component count, and EOI.
func craftJPEG(comps byte) []byte {
	var b bytes.Buffer
	b.Write([]byte{0xFF, 0xD8}) // SOI
	// APP0 segment length 4 (2 length bytes + 2 payload)
	b.Write([]byte{0xFF, 0xE0, 0x00, 0x04, 0x00, 0x00})
	// A restart marker (should be skipped)
	b.Write([]byte{0xFF, 0xD0})
	// SOF0: marker, length(17-ish placeholder), precision, height(4), width(6), comps
	b.Write([]byte{0xFF, 0xC0, 0x00, 0x11, 0x08, 0x00, 0x04, 0x00, 0x06, comps})
	for i := byte(0); i < comps*3; i++ {
		b.WriteByte(0)
	}
	b.Write([]byte{0xFF, 0xD9}) // EOI
	return b.Bytes()
}

func TestJPEGColorSpaces(t *testing.T) {
	defer fixedClock()()
	d := New(Options{})
	d.ImageReader(bytes.NewReader(grayJPEG(t)), "jpg", ImageOptions{})
	mustRender(t, d)

	gray, err := decodeJPEG(craftJPEG(1))
	if err != nil || gray.colorSpace != "DeviceGray" {
		t.Fatalf("gray cs %v %v", err, gray)
	}
	cmyk, err := decodeJPEG(craftJPEG(4))
	if err != nil || cmyk.colorSpace != "DeviceCMYK" {
		t.Fatalf("cmyk cs %v", err)
	}
	rgb, err := decodeJPEG(craftJPEG(3))
	if err != nil || rgb.colorSpace != "DeviceRGB" {
		t.Fatalf("rgb cs %v", err)
	}
}

// ---- Render error via repeater ---------------------------------------------

func TestRenderErrorInRepeater(t *testing.T) {
	d := New(Options{})
	d.Text("x", nil)
	d.Repeat(func(p, total int) bool { return true }, func() {
		d.DrawText("\xff\xfe", 10, 10, nil) // invalid utf8 -> sticky error
	})
	if _, err := d.Render(); err == nil {
		t.Fatal("want error surfaced from repeater")
	}
}

// ---- TTF white-box: gid, glyphBytes, cmap4 ranges, composites, parse errors -

func parsedGoFont(t *testing.T) *ttfFont {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "goregular.ttf"))
	if err != nil {
		t.Skip("no test TTF")
	}
	tf, err := parseTTF(b)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return tf
}

func TestTTFGidAndGlyphBytes(t *testing.T) {
	tf := parsedGoFont(t)
	if tf.gid('A') == 0 {
		t.Fatal("A should map")
	}
	if tf.gid('￾') != 0 {
		t.Fatal("unmapped -> 0")
	}
	// space is typically an empty glyph -> nil bytes
	sp := tf.gid(' ')
	_ = tf.glyphBytes(sp)
	// a real glyph -> non-nil
	if tf.glyphBytes(tf.gid('A')) == nil {
		t.Fatal("A glyph bytes")
	}
	// exercise widthOf and subset build with spaces (empty glyph path)
	tf.encode("A A")
	glyf, loca := tf.buildSubset()
	if len(glyf) == 0 || len(loca) == 0 {
		t.Fatal("subset build")
	}
}

func TestParseCmap4Ranges(t *testing.T) {
	// Build a format-4 cmap with two segments: one delta segment and one that
	// uses rangeOffset (idFangeOffset != 0), plus the required 0xFFFF terminator.
	// Layout: format(2) length(2) language(2) segX2(2) searchRange(2)
	//         entrySelector(2) rangeShift(2) endCodes[segX2] pad(2)
	//         startCodes[segX2] idDelta[segX2] idRangeOffset[segX2] glyphIds…
	segCount := 3
	segX2 := segCount * 2
	// segment 0: 0x0041..0x0041, delta so 'A'->70 (ro=0)
	// segment 1: 0x0061..0x0062 via rangeOffset -> glyph ids table
	// segment 2: 0xFFFF terminator
	end := []uint16{0x0041, 0x0062, 0xFFFF}
	start := []uint16{0x0041, 0x0061, 0xFFFF}
	delta := []uint16{70 - 0x41, 0, 1}
	// rangeOffset for seg1 points into glyphIdArray right after the ro array.
	// bytes from ro[1] to glyph entry: (segCount-1)*2 + 0*2 = for c=0x61 first.
	ro := []uint16{0, uint16((segCount - 1) * 2), 0}
	glyphIds := []uint16{200, 201} // for 0x61, 0x62

	var b bytes.Buffer
	put := func(v uint16) { b.Write([]byte{byte(v >> 8), byte(v)}) }
	put(4)                  // format
	put(0)                  // length (unused by parser)
	put(0)                  // language
	put(uint16(segX2))      // segCountX2
	put(0)                  // searchRange
	put(0)                  // entrySelector
	put(0)                  // rangeShift
	for _, v := range end { // endCodes
		put(v)
	}
	put(0) // reservedPad
	for _, v := range start {
		put(v)
	}
	for _, v := range delta {
		put(v)
	}
	for _, v := range ro {
		put(v)
	}
	for _, v := range glyphIds {
		put(v)
	}

	tf := &ttfFont{cmap: map[rune]uint16{}}
	tf.parseCmap4(b.Bytes())
	if tf.cmap['A'] != 70 {
		t.Fatalf("delta seg: A=%d", tf.cmap['A'])
	}
	if tf.cmap['a'] != 200 || tf.cmap['b'] != 201 {
		t.Fatalf("rangeOffset seg: a=%d b=%d", tf.cmap['a'], tf.cmap['b'])
	}
}

func TestParseCmapSelectionAndError(t *testing.T) {
	// Craft a cmap table with two subtables; one unsupported format so the
	// selected best is unsupported -> error. Header: version(2) numTables(2)
	// then records: platform(2) enc(2) offset(4).
	var b bytes.Buffer
	put16 := func(v uint16) { b.Write([]byte{byte(v >> 8), byte(v)}) }
	put32 := func(v uint32) { b.Write([]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}) }
	put16(0) // version
	put16(1) // numTables
	// one record (3,1) pointing at offset 12
	put16(3)
	put16(1)
	put32(12)
	// subtable format 99 (unsupported)
	put16(99)

	tf := &ttfFont{data: b.Bytes(), tables: map[string]tableRec{
		"cmap": {offset: 0, length: uint32(b.Len())},
	}}
	if err := tf.parseCmap(); err == nil {
		t.Fatal("want unsupported cmap error")
	}
}

func TestComputeFlagsItalic(t *testing.T) {
	tf := &ttfFont{italicAngle: -12}
	tf.computeFlags()
	if tf.flags&0x40 == 0 {
		t.Fatal("italic flag")
	}
}

func TestParseOS2PostAbsent(t *testing.T) {
	// A tf whose tables map lacks OS/2 and post: capHeight falls back to ascent,
	// stemV defaults.
	tf := &ttfFont{ascent: 750, tables: map[string]tableRec{}}
	tf.parseOS2Post()
	if tf.capHeight != 750 || tf.stemV != 80 {
		t.Fatalf("fallbacks cap=%v stem=%v", tf.capHeight, tf.stemV)
	}
}

func TestParseTTFBadDirectory(t *testing.T) {
	// Header claims 5 tables but the data is too short for the directory.
	hdr := []byte{0, 1, 0, 0, 0, 5, 0, 0, 0, 0, 0, 0, 0, 0}
	if _, err := parseTTF(hdr); err == nil {
		t.Fatal("want bad directory error")
	}
}

func TestRemapComposite(t *testing.T) {
	// Build a synthetic composite glyph referencing original gids 5 and 9, with
	// the three argument/scale variants, and verify they get remapped to subset
	// ids and the flag walk terminates.
	tf := &ttfFont{origToSub: map[uint16]uint16{0: 0}, subToOrig: []uint16{0}}
	var g bytes.Buffer
	put16 := func(v uint16) { g.Write([]byte{byte(v >> 8), byte(v)}) }
	putSigned := func(v int16) { put16(uint16(v)) }
	// numberOfContours = -1 (composite), bbox 4 words
	putSigned(-1)
	put16(0)
	put16(0)
	put16(0)
	put16(0)
	// component 1: flags = ARGS_ARE_WORDS | WE_HAVE_A_SCALE | MORE_COMPONENTS
	put16(argsAreWords | weHaveAScale | moreComponents)
	put16(5) // glyphIndex
	put16(0) // arg1 (word)
	put16(0) // arg2 (word)
	put16(0) // scale
	// component 2: flags = WE_HAVE_XY_SCALE (no MORE_COMPONENTS -> terminate),
	// byte args (no ARGS_ARE_WORDS)
	put16(weHaveXYScale)
	put16(9) // glyphIndex
	put16(0) // arg1+arg2 (2 bytes)
	put16(0) // xy scale (4 bytes)
	put16(0)

	out := tf.remapComposite(g.Bytes())
	// after remap, component glyph indices should be subset ids (1 and 2)
	if be16(out[10+2:]) != 1 {
		t.Fatalf("comp1 remapped to %d", be16(out[10+2:]))
	}
	// exercise the 2x2 branch as well
	tf2 := &ttfFont{origToSub: map[uint16]uint16{0: 0}, subToOrig: []uint16{0}}
	var g2 bytes.Buffer
	p16 := func(v uint16) { g2.Write([]byte{byte(v >> 8), byte(v)}) }
	p16(uint16(0xFFFF)) // numberOfContours = -1 (composite)
	p16(0)
	p16(0)
	p16(0)
	p16(0)
	p16(weHave2x2) // byte args, 2x2 scale, no more
	p16(3)
	p16(0) // args (2 bytes)
	p16(0) // 2x2 (8 bytes)
	p16(0)
	p16(0)
	p16(0)
	tf2.remapComposite(g2.Bytes())
}
