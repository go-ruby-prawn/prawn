// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-ruby-prawn/prawn authors

package prawn

// Synthetic in-memory sfnt builders for the go-opentype-backed font backend
// tests. They assemble minimal-but-valid TrueType fonts (optionally variable,
// with fvar + HVAR) so the OTF path — including variable-instance advance
// selection and the FontDescriptor scalar branches — is exercised
// deterministically without any external font file. The table encoders are
// adapted from the go-opentype/opentype test toolkit (BSD-3-Clause).

import (
	"encoding/binary"
	"math"
	"sort"
	"testing"
)

// bw is a tiny big-endian byte writer.
type bw struct{ b []byte }

func (w *bw) u16(v uint16) { w.b = binary.BigEndian.AppendUint16(w.b, v) }
func (w *bw) u32(v uint32) { w.b = binary.BigEndian.AppendUint32(w.b, v) }
func (w *bw) i16(v int16)  { w.u16(uint16(v)) }
func (w *bw) u8(v uint8)   { w.b = append(w.b, v) }

func f2dot14bytes(v float64) int16 { return int16(math.Round(v * 16384.0)) }
func fixedBytes(v float64) uint32  { return uint32(int32(math.Round(v * 65536.0))) }

// synthTables assembles a full sfnt blob (glyf flavour) from a set of tables.
func synthTables(tables map[string][]byte) []byte {
	tags := make([]string, 0, len(tables))
	for t := range tables {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	n := len(tags)
	headerLen := 12 + 16*n

	body := &bw{}
	type rec struct {
		tag    string
		off, l int
	}
	recs := make([]rec, 0, n)
	off := headerLen
	for _, t := range tags {
		data := tables[t]
		recs = append(recs, rec{tag: t, off: off, l: len(data)})
		body.b = append(body.b, data...)
		for len(body.b)%4 != 0 {
			body.b = append(body.b, 0)
		}
		off = headerLen + len(body.b)
	}
	h := &bw{}
	h.u32(0x00010000)
	h.u16(uint16(n))
	h.u16(0)
	h.u16(0)
	h.u16(0)
	for _, r := range recs {
		h.b = append(h.b, []byte(r.tag)...)
		h.u32(0)
		h.u32(uint32(r.off))
		h.u32(uint32(r.l))
	}
	return append(h.b, body.b...)
}

func u16i(v int16) uint16 { return uint16(v) }

func headTable(unitsPerEm int, indexToLocFormat int16) []byte {
	b := make([]byte, 54)
	binary.BigEndian.PutUint16(b[18:], uint16(unitsPerEm))
	// a non-zero bbox so the descriptor FontBBox is meaningful
	binary.BigEndian.PutUint16(b[36:], u16i(-50))  // xMin
	binary.BigEndian.PutUint16(b[38:], u16i(-200)) // yMin
	binary.BigEndian.PutUint16(b[40:], u16i(600))  // xMax
	binary.BigEndian.PutUint16(b[42:], u16i(800))  // yMax
	binary.BigEndian.PutUint16(b[50:], uint16(indexToLocFormat))
	return b
}

func maxpTable(numGlyphs int) []byte {
	w := &bw{}
	w.u32(0x00010000)
	w.u16(uint16(numGlyphs))
	return w.b
}

func hheaTable(ascender, descender, lineGap, numberOfHMetrics int) []byte {
	b := make([]byte, 36)
	binary.BigEndian.PutUint16(b[4:], uint16(int16(ascender)))
	binary.BigEndian.PutUint16(b[6:], uint16(int16(descender)))
	binary.BigEndian.PutUint16(b[8:], uint16(int16(lineGap)))
	binary.BigEndian.PutUint16(b[34:], uint16(numberOfHMetrics))
	return b
}

func hmtxTable(advances, lsbs []int, numberOfHMetrics int) []byte {
	w := &bw{}
	for i := range advances {
		if i < numberOfHMetrics {
			w.u16(uint16(advances[i]))
			w.i16(int16(lsbs[i]))
		} else {
			w.i16(int16(lsbs[i]))
		}
	}
	return w.b
}

// os2Table builds an OS/2 table (version 2, 96 bytes) carrying sCapHeight at
// offset 88.
func os2Table(capHeight int) []byte {
	b := make([]byte, 96)
	binary.BigEndian.PutUint16(b[0:], 2) // version
	binary.BigEndian.PutUint16(b[88:], uint16(int16(capHeight)))
	return b
}

// postTable builds a post table (version 3) with the given italic angle (16.16).
func postTable(italicAngle float64) []byte {
	w := &bw{}
	w.u32(0x00030000) // version 3.0
	w.u32(uint32(int32(math.Round(italicAngle * 65536))))
	for len(w.b) < 32 {
		w.u8(0)
	}
	return w.b
}

// glyfAndLoca packs simple glyph blobs into glyf + a long loca table.
func glyfAndLoca(glyphs [][]byte) (loca, glyf []byte) {
	g := &bw{}
	offsets := []int{0}
	for _, blob := range glyphs {
		g.b = append(g.b, blob...)
		for len(g.b)%2 != 0 {
			g.b = append(g.b, 0)
		}
		offsets = append(offsets, len(g.b))
	}
	l := &bw{}
	for _, o := range offsets {
		l.u32(uint32(o))
	}
	return l.b, g.b
}

// squareGlyph is a simple 1-contour box glyph.
func squareGlyph() []byte {
	w := &bw{}
	w.i16(1) // numberOfContours
	w.i16(0)
	w.i16(0)
	w.i16(500)
	w.i16(500)
	w.u16(3) // endPtsOfContours[0]
	w.u16(0) // instructionLength
	// 4 on-curve points, all short vectors
	for i := 0; i < 4; i++ {
		w.u8(0x03) // on-curve, x-short, y-short (positive)
	}
	w.u8(100)
	w.u8(0)
	w.u8(0)
	w.u8(100)
	w.u8(100)
	w.u8(0)
	w.u8(0)
	w.u8(100)
	return w.b
}

// cmap4Map builds a cmap table with a single format-4 subtable from a rune->gid
// map (idRangeOffset==0 segments plus the mandatory 0xFFFF sentinel).
func cmap4Map(m map[rune]uint16) []byte {
	runes := make([]int, 0, len(m))
	for r := range m {
		runes = append(runes, int(r))
	}
	sort.Ints(runes)
	var ends, starts []uint16
	var deltas []int16
	i := 0
	for i < len(runes) {
		j := i
		for j+1 < len(runes) && runes[j+1] == runes[j]+1 &&
			m[rune(runes[j+1])] == m[rune(runes[j])]+uint16(runes[j+1]-runes[j]) {
			j++
		}
		starts = append(starts, uint16(runes[i]))
		ends = append(ends, uint16(runes[j]))
		deltas = append(deltas, int16(int(m[rune(runes[i])])-runes[i]))
		i = j + 1
	}
	starts = append(starts, 0xFFFF)
	ends = append(ends, 0xFFFF)
	deltas = append(deltas, 1)

	seg := len(ends)
	sub := &bw{}
	sub.u16(4) // format
	sub.u16(uint16(16 + seg*8))
	sub.u16(0) // language
	sub.u16(uint16(seg * 2))
	sr := 2
	es := 0
	for sr*2 <= seg {
		sr *= 2
		es++
	}
	sub.u16(uint16(sr * 2))
	sub.u16(uint16(es))
	sub.u16(uint16(seg*2 - sr*2))
	for _, e := range ends {
		sub.u16(e)
	}
	sub.u16(0) // reservedPad
	for _, s := range starts {
		sub.u16(s)
	}
	for _, d := range deltas {
		sub.i16(d)
	}
	for range ends {
		sub.u16(0) // idRangeOffset
	}

	c := &bw{}
	c.u16(0) // version
	c.u16(1) // numTables
	c.u16(3) // platformID (Windows)
	c.u16(1) // encodingID (BMP)
	c.u32(12)
	c.b = append(c.b, sub.b...)
	return c.b
}

// --- variable-font tables (fvar + HVAR) -------------------------------------

// buildFvar encodes an fvar table with one axis per entry (no named instances).
func buildFvar(tag string, min, def, max float64) []byte {
	w := &bw{}
	w.u16(1) // majorVersion
	w.u16(0) // minorVersion
	w.u16(16)
	w.u16(2) // reserved
	w.u16(1) // axisCount
	w.u16(20)
	w.u16(0) // instanceCount
	w.u16(8) // instanceSize (unused)
	w.b = append(w.b, []byte(tag)...)
	w.u32(fixedBytes(min))
	w.u32(fixedBytes(def))
	w.u32(fixedBytes(max))
	w.u16(0)   // flags
	w.u16(256) // nameID
	return w.b
}

// buildItemVarStore encodes a format-1 ItemVariationStore with a single region
// peaking at axis 0 = +1 and one ItemVariationData carrying 16-bit deltas.
func buildItemVarStore(rows [][]int) []byte {
	rl := &bw{}
	rl.u16(1) // axisCount
	rl.u16(1) // regionCount
	// region 0: {start, peak, end} on axis 0 = {0, 1, 1}
	rl.i16(f2dot14bytes(0))
	rl.i16(f2dot14bytes(1))
	rl.i16(f2dot14bytes(1))

	ivd := &bw{}
	ivd.u16(uint16(len(rows)))
	ivd.u16(1) // wordCount=1, LONG_WORDS off -> 16-bit deltas
	ivd.u16(1) // regionCount
	ivd.u16(0) // regionIndex 0
	for _, row := range rows {
		for _, v := range row {
			ivd.i16(int16(v))
		}
	}

	headerLen := 2 + 4 + 2 + 4*1
	body := &bw{}
	body.b = append(body.b, rl.b...)
	ivdOff := headerLen + len(body.b)
	body.b = append(body.b, ivd.b...)

	h := &bw{}
	h.u16(1)
	h.u32(uint32(headerLen))
	h.u16(1)
	h.u32(uint32(ivdOff))
	return append(h.b, body.b...)
}

// buildDeltaSetMap encodes a format-0 DeltaSetIndexMap (16-bit entries).
func buildDeltaSetMap(entries [][2]int) []byte {
	w := &bw{}
	w.u8(0)                                          // format
	w.u8(uint8((1-1)&0x0F) | uint8(((2-1)&0x03)<<4)) // innerBits=1, entrySize=2
	w.u16(uint16(len(entries)))
	for _, e := range entries {
		entry := uint32(e[0])<<1 | uint32(e[1])
		w.u8(uint8(entry >> 8))
		w.u8(uint8(entry))
	}
	return w.b
}

// buildHVAR encodes an HVAR table from a store and an advance-width map.
func buildHVAR(store, advMap []byte) []byte {
	const headerLen = 20
	w := &bw{}
	w.u16(1)
	w.u16(0)
	w.u32(headerLen)
	w.u32(uint32(headerLen + len(store)))
	w.u32(0)
	w.u32(0)
	w.b = append(w.b, store...)
	w.b = append(w.b, advMap...)
	return w.b
}

// synthGlyfFont assembles a minimal glyf font mapping 'A'+i to glyph i. Glyph 0
// is .notdef (empty); glyphs 1.. are square glyphs. The advances slice sets each
// glyph's base advance. withOS2/withPost toggle those optional tables, and
// italic sets the post italic angle. Extra variation tables may be supplied.
func synthGlyfFont(t *testing.T, n int, advances []int, withOS2, withPost bool, italic float64, extra map[string][]byte) []byte {
	t.Helper()
	glyphs := make([][]byte, n)
	m := map[rune]uint16{}
	lsbs := make([]int, n)
	for i := 0; i < n; i++ {
		if i > 0 {
			glyphs[i] = squareGlyph()
			m[rune('A'+i-1)] = uint16(i)
		}
	}
	loca, glyf := glyfAndLoca(glyphs)
	tables := map[string][]byte{
		"head": headTable(1000, 1),
		"maxp": maxpTable(n),
		"hhea": hheaTable(800, -200, 0, n),
		"hmtx": hmtxTable(advances, lsbs, n),
		"cmap": cmap4Map(m),
		"loca": loca,
		"glyf": glyf,
	}
	if withOS2 {
		tables["OS/2"] = os2Table(700)
	}
	if withPost {
		tables["post"] = postTable(italic)
	}
	for k, v := range extra {
		tables[k] = v
	}
	return synthTables(tables)
}
