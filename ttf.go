// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-ruby-prawn/prawn authors

package prawn

// This file embeds TrueType fonts the way Ruby prawn (via ttfunk) does: it
// parses the sfnt tables, subsets the glyf/loca/hmtx tables down to the glyphs
// actually used (following composite-glyph references), and writes a Type0 /
// CIDFontType2 font with Identity-H encoding, a FontFile2 stream, a per-CID /W
// width array and a /ToUnicode CMap. Everything is pure Go (no cgo).

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sort"
)

// tableRec is one entry of the sfnt table directory.
type tableRec struct {
	offset uint32
	length uint32
}

// ttfFont holds a parsed TrueType font plus the running subset state.
type ttfFont struct {
	name     string
	baseName string
	resName  string
	data     []byte
	tables   map[string]tableRec

	unitsPerEm  float64
	ascent      float64
	descent     float64
	lineGap     float64
	capHeight   float64
	italicAngle float64
	stemV       float64
	bbox        [4]float64
	flags       int
	numGlyphs   int
	longLoca    bool
	numHMetrics int

	cmap map[rune]uint16
	hmtx []uint16

	origToSub map[uint16]uint16
	subToOrig []uint16
	toUnicode map[uint16]rune
}

// RegisterFontTTF parses a TrueType font and registers it under the given family
// name (Prawn::Document#font_families/#font with a TTF file). Subsequent
// Font(name, …) calls select it.
func (d *Document) RegisterFontTTF(name string, data []byte) error {
	tf, err := parseTTF(data)
	if err != nil {
		d.fail(err)
		return err
	}
	tf.name = name
	d.fontOrder++
	fr := &fontRef{resName: fmt.Sprintf("F%d.0", d.fontOrder), ttf: tf}
	tf.resName = fr.resName
	d.fonts["ttf:"+name] = fr
	return nil
}

func be16(b []byte) uint16 { return binary.BigEndian.Uint16(b) }
func be32(b []byte) uint32 { return binary.BigEndian.Uint32(b) }

// parseTTF reads the sfnt directory and the tables needed for measurement,
// encoding and subsetting.
func parseTTF(data []byte) (*ttfFont, error) {
	if len(data) < 12 {
		return nil, fmt.Errorf("%w: truncated font", ErrUnknownFont)
	}
	num := int(be16(data[4:6]))
	tf := &ttfFont{
		data:      data,
		tables:    map[string]tableRec{},
		origToSub: map[uint16]uint16{},
		subToOrig: []uint16{0}, // subset gid 0 is always .notdef
		toUnicode: map[uint16]rune{},
	}
	tf.origToSub[0] = 0
	off := 12
	for i := 0; i < num; i++ {
		if off+16 > len(data) {
			return nil, fmt.Errorf("%w: bad table directory", ErrUnknownFont)
		}
		tag := string(data[off : off+4])
		tf.tables[tag] = tableRec{offset: be32(data[off+8 : off+12]), length: be32(data[off+12 : off+16])}
		off += 16
	}
	for _, req := range []string{"head", "hhea", "maxp", "hmtx", "loca", "glyf", "cmap"} {
		if _, ok := tf.tables[req]; !ok {
			return nil, fmt.Errorf("%w: missing %q table", ErrUnknownFont, req)
		}
	}
	tf.parseHead()
	tf.parseHhea()
	tf.parseMaxp()
	tf.parseOS2Post()
	tf.parseHmtx()
	if err := tf.parseCmap(); err != nil {
		return nil, err
	}
	tf.computeFlags()
	return tf, nil
}

func (tf *ttfFont) tbl(tag string) []byte {
	r := tf.tables[tag]
	return tf.data[r.offset : r.offset+r.length]
}

func (tf *ttfFont) parseHead() {
	h := tf.tbl("head")
	tf.unitsPerEm = float64(be16(h[18:20]))
	tf.bbox = [4]float64{
		float64(int16(be16(h[36:38]))),
		float64(int16(be16(h[38:40]))),
		float64(int16(be16(h[40:42]))),
		float64(int16(be16(h[42:44]))),
	}
	tf.longLoca = int16(be16(h[50:52])) == 1
}

func (tf *ttfFont) parseHhea() {
	h := tf.tbl("hhea")
	tf.ascent = float64(int16(be16(h[4:6])))
	tf.descent = float64(int16(be16(h[6:8])))
	tf.lineGap = float64(int16(be16(h[8:10])))
	tf.numHMetrics = int(be16(h[34:36]))
}

func (tf *ttfFont) parseMaxp() {
	tf.numGlyphs = int(be16(tf.tbl("maxp")[4:6]))
}

func (tf *ttfFont) parseOS2Post() {
	if r, ok := tf.tables["OS/2"]; ok && r.length >= 90 {
		o := tf.tbl("OS/2")
		tf.capHeight = float64(int16(be16(o[88:90])))
	}
	if tf.capHeight == 0 {
		tf.capHeight = tf.ascent
	}
	if r, ok := tf.tables["post"]; ok && r.length >= 8 {
		p := tf.tbl("post")
		// italicAngle is a 16.16 fixed-point number.
		tf.italicAngle = float64(int32(be32(p[4:8]))) / 65536.0
	}
	tf.stemV = 80
}

func (tf *ttfFont) parseHmtx() {
	h := tf.tbl("hmtx")
	tf.hmtx = make([]uint16, tf.numGlyphs)
	last := uint16(0)
	for i := 0; i < tf.numGlyphs; i++ {
		if i < tf.numHMetrics {
			last = be16(h[i*4 : i*4+2])
		}
		tf.hmtx[i] = last
	}
}

func (tf *ttfFont) parseCmap() error {
	c := tf.tbl("cmap")
	n := int(be16(c[2:4]))
	var best uint32
	var bestScore int
	for i := 0; i < n; i++ {
		rec := c[4+i*8:]
		plat := be16(rec[0:2])
		enc := be16(rec[2:4])
		sub := be32(rec[4:8])
		score := 0
		switch {
		case plat == 3 && enc == 10:
			score = 5
		case plat == 3 && enc == 1:
			score = 4
		case plat == 0:
			score = 3
		case plat == 3 && enc == 0:
			score = 2
		default:
			score = 1
		}
		if score > bestScore {
			bestScore = score
			best = sub
		}
	}
	tf.cmap = map[rune]uint16{}
	st := c[best:]
	format := be16(st[0:2])
	switch format {
	case 4:
		tf.parseCmap4(st)
	case 12:
		tf.parseCmap12(st)
	case 6:
		tf.parseCmap6(st)
	case 0:
		tf.parseCmap0(st)
	default:
		return fmt.Errorf("%w: unsupported cmap format %d", ErrUnknownFont, format)
	}
	return nil
}

func (tf *ttfFont) parseCmap4(st []byte) {
	segX2 := int(be16(st[6:8]))
	segCount := segX2 / 2
	endO := 14
	startO := endO + segX2 + 2
	deltaO := startO + segX2
	rangeO := deltaO + segX2
	for i := 0; i < segCount; i++ {
		end := be16(st[endO+i*2:])
		start := be16(st[startO+i*2:])
		delta := be16(st[deltaO+i*2:])
		ro := be16(st[rangeO+i*2:])
		for c := int(start); c <= int(end); c++ {
			if c == 0xFFFF {
				continue
			}
			var g uint16
			if ro == 0 {
				g = uint16(c) + delta
			} else {
				idx := rangeO + i*2 + int(ro) + (c-int(start))*2
				if idx+2 > len(st) {
					continue
				}
				g = be16(st[idx:])
				if g != 0 {
					g += delta
				}
			}
			if g != 0 {
				tf.cmap[rune(c)] = g
			}
		}
	}
}

func (tf *ttfFont) parseCmap12(st []byte) {
	ng := be32(st[12:16])
	base := 16
	for i := 0; i < int(ng); i++ {
		g := st[base+i*12:]
		sc := be32(g[0:4])
		ec := be32(g[4:8])
		sg := be32(g[8:12])
		for c := sc; c <= ec; c++ {
			tf.cmap[rune(c)] = uint16(sg + (c - sc))
		}
	}
}

func (tf *ttfFont) parseCmap6(st []byte) {
	first := int(be16(st[6:8]))
	count := int(be16(st[8:10]))
	for i := 0; i < count; i++ {
		tf.cmap[rune(first+i)] = be16(st[10+i*2:])
	}
}

func (tf *ttfFont) parseCmap0(st []byte) {
	for i := 0; i < 256; i++ {
		g := uint16(st[6+i])
		if g != 0 {
			tf.cmap[rune(i)] = g
		}
	}
}

func (tf *ttfFont) computeFlags() {
	// Bit 3 (0x04): symbolic if the font maps mostly non-Latin; here we mark
	// nonsymbolic (0x20) for standard text fonts, plus italic (0x40) if slanted.
	flags := 0x20
	if tf.italicAngle != 0 {
		flags |= 0x40
	}
	tf.flags = flags
}

// gid returns the original glyph id for a rune (0 = .notdef when unmapped).
func (tf *ttfFont) gid(r rune) uint16 {
	if g, ok := tf.cmap[r]; ok {
		return g
	}
	return 0
}

// subGID assigns (or returns) the subset glyph id for an original gid.
func (tf *ttfFont) subGID(orig uint16) uint16 {
	if s, ok := tf.origToSub[orig]; ok {
		return s
	}
	s := uint16(len(tf.subToOrig))
	tf.origToSub[orig] = s
	tf.subToOrig = append(tf.subToOrig, orig)
	return s
}

// encode maps a UTF-8 string to subset glyph ids, recording per-glyph ToUnicode
// entries, and returns the subset gids for the content stream (Identity-H).
func (tf *ttfFont) encode(s string) []uint16 {
	var out []uint16
	for _, r := range s {
		orig := tf.gid(r)
		sub := tf.subGID(orig)
		if _, ok := tf.toUnicode[sub]; !ok {
			tf.toUnicode[sub] = r
		}
		out = append(out, sub)
	}
	return out
}

func (tf *ttfFont) ascenderScaled(size float64) float64 { return tf.ascent * size / tf.unitsPerEm }

func (tf *ttfFont) height(size float64) float64 {
	return (tf.ascent - tf.descent + tf.lineGap) * size / tf.unitsPerEm
}

// widthOf measures a string in points at the given size.
func (tf *ttfFont) widthOf(s string, size float64) float64 {
	total := 0.0
	for _, r := range s {
		total += float64(tf.hmtx[tf.gid(r)])
	}
	return total * size / tf.unitsPerEm
}

// glyphBytes returns the raw glyf-table bytes for an original gid.
func (tf *ttfFont) glyphBytes(orig uint16) []byte {
	loca := tf.tbl("loca")
	var start, end uint32
	if tf.longLoca {
		start = be32(loca[int(orig)*4:])
		end = be32(loca[int(orig)*4+4:])
	} else {
		start = uint32(be16(loca[int(orig)*2:])) * 2
		end = uint32(be16(loca[int(orig)*2+2:])) * 2
	}
	if end <= start {
		return nil
	}
	g := tf.tbl("glyf")
	return g[start:end]
}

// gidBytes packs subset glyph ids as 2-byte big-endian codes (Identity-H).
func gidBytes(gids []uint16) []byte {
	b := make([]byte, len(gids)*2)
	for i, g := range gids {
		binary.BigEndian.PutUint16(b[i*2:], g)
	}
	return b
}

// composite glyph flags.
const (
	argsAreWords   = 0x0001
	weHaveAScale   = 0x0008
	moreComponents = 0x0020
	weHaveXYScale  = 0x0040
	weHave2x2      = 0x0080
)

// buildSubset expands the subset with composite dependencies and returns the new
// glyf and loca tables plus the ordered original-gid list.
func (tf *ttfFont) buildSubset() (glyf []byte, loca []byte) {
	var glyfBuf bytes.Buffer
	var offsets []uint32
	offsets = append(offsets, 0)
	// subToOrig may grow while iterating (composite deps), so index manually.
	for i := 0; i < len(tf.subToOrig); i++ {
		orig := tf.subToOrig[i]
		gb := tf.glyphBytes(orig)
		if len(gb) >= 10 && int16(be16(gb[0:2])) < 0 {
			gb = tf.remapComposite(gb)
		}
		glyfBuf.Write(gb)
		// pad to even length (glyf tables are word-aligned).
		if glyfBuf.Len()%2 != 0 {
			glyfBuf.WriteByte(0)
		}
		offsets = append(offsets, uint32(glyfBuf.Len()))
	}
	glyf = glyfBuf.Bytes()
	var locaBuf bytes.Buffer
	for _, o := range offsets {
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], o)
		locaBuf.Write(b[:])
	}
	return glyf, locaBuf.Bytes()
}

// remapComposite rewrites the component glyph indices of a composite glyph to
// their subset gids (assigning new subset ids for any newly referenced glyph).
func (tf *ttfFont) remapComposite(gb []byte) []byte {
	out := make([]byte, len(gb))
	copy(out, gb)
	p := 10 // skip numberOfContours + bbox
	for {
		if p+4 > len(out) {
			break
		}
		flags := be16(out[p:])
		compOrig := be16(out[p+2:])
		sub := tf.subGID(compOrig)
		binary.BigEndian.PutUint16(out[p+2:], sub)
		p += 4
		if flags&argsAreWords != 0 {
			p += 4
		} else {
			p += 2
		}
		switch {
		case flags&weHaveAScale != 0:
			p += 2
		case flags&weHaveXYScale != 0:
			p += 4
		case flags&weHave2x2 != 0:
			p += 8
		}
		if flags&moreComponents == 0 {
			break
		}
	}
	return out
}

// subsetSFNT assembles the embedded subset TrueType font from the rebuilt
// glyf/loca plus copies of the metric/program tables.
func (tf *ttfFont) subsetSFNT() []byte {
	glyf, loca := tf.buildSubset()
	nGlyphs := len(tf.subToOrig)

	// new hmtx: one advance per subset glyph.
	var hmtx bytes.Buffer
	for _, orig := range tf.subToOrig {
		var b [4]byte
		binary.BigEndian.PutUint16(b[0:], tf.hmtx[orig])
		hmtx.Write(b[:])
	}

	// head: copy, force long loca and zero the checksum adjustment.
	head := append([]byte(nil), tf.tbl("head")...)
	binary.BigEndian.PutUint32(head[8:], 0)
	binary.BigEndian.PutUint16(head[50:], 1)

	// hhea: copy, set numberOfHMetrics = nGlyphs.
	hhea := append([]byte(nil), tf.tbl("hhea")...)
	binary.BigEndian.PutUint16(hhea[34:], uint16(nGlyphs))

	// maxp: copy, set numGlyphs.
	maxp := append([]byte(nil), tf.tbl("maxp")...)
	binary.BigEndian.PutUint16(maxp[4:], uint16(nGlyphs))

	tables := map[string][]byte{
		"head": head,
		"hhea": hhea,
		"maxp": maxp,
		"hmtx": hmtx.Bytes(),
		"loca": loca,
		"glyf": glyf,
	}
	for _, opt := range []string{"cvt ", "fpgm", "prep"} {
		if _, ok := tf.tables[opt]; ok {
			tables[opt] = append([]byte(nil), tf.tbl(opt)...)
		}
	}
	return writeSFNT(tables)
}

// writeSFNT serializes a set of tables into a valid sfnt (TrueType) file.
func writeSFNT(tables map[string][]byte) []byte {
	tags := make([]string, 0, len(tables))
	for t := range tables {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	n := len(tags)

	// binary search header values.
	entrySelector := 0
	searchRange := 1
	for searchRange*2 <= n {
		searchRange *= 2
		entrySelector++
	}
	searchRange *= 16
	rangeShift := n*16 - searchRange

	var buf bytes.Buffer
	var hdr [12]byte
	binary.BigEndian.PutUint32(hdr[0:], 0x00010000)
	binary.BigEndian.PutUint16(hdr[4:], uint16(n))
	binary.BigEndian.PutUint16(hdr[6:], uint16(searchRange))
	binary.BigEndian.PutUint16(hdr[8:], uint16(entrySelector))
	binary.BigEndian.PutUint16(hdr[10:], uint16(rangeShift))
	buf.Write(hdr[:])

	offset := 12 + n*16
	dir := make([]byte, 0, n*16)
	body := &bytes.Buffer{}
	for _, tag := range tags {
		data := tables[tag]
		padded := len(data)
		for padded%4 != 0 {
			padded++
		}
		var e [16]byte
		copy(e[0:4], tag)
		binary.BigEndian.PutUint32(e[4:], tableChecksum(data))
		binary.BigEndian.PutUint32(e[8:], uint32(offset))
		binary.BigEndian.PutUint32(e[12:], uint32(len(data)))
		dir = append(dir, e[:]...)
		body.Write(data)
		for i := len(data); i < padded; i++ {
			body.WriteByte(0)
		}
		offset += padded
	}
	buf.Write(dir)
	buf.Write(body.Bytes())
	return buf.Bytes()
}

func tableChecksum(b []byte) uint32 {
	var sum uint32
	for i := 0; i+4 <= len(b); i += 4 {
		sum += be32(b[i:])
	}
	if r := len(b) % 4; r != 0 {
		var tail [4]byte
		copy(tail[:], b[len(b)-r:])
		sum += be32(tail[:])
	}
	return sum
}

// buildObjects writes the Type0/CIDFontType2 font graph and returns the Type0
// font-dictionary reference.
func (tf *ttfFont) buildObjects(doc *pdfDoc) pdfRef {
	subset := tf.subsetSFNT()
	fontFile := doc.add(&pdfStream{
		dict: pdfDict{"Length1": len(subset)},
		data: subset,
	})

	base := tf.baseName
	if base == "" {
		base = "AAAAAA+" + sanitizeName(tf.name)
	}

	scale := 1000.0 / tf.unitsPerEm
	descriptor := doc.add(pdfDict{
		"Type":        pdfName("FontDescriptor"),
		"FontName":    pdfName(base),
		"Flags":       tf.flags,
		"FontBBox":    pdfArray{tf.bbox[0] * scale, tf.bbox[1] * scale, tf.bbox[2] * scale, tf.bbox[3] * scale},
		"ItalicAngle": tf.italicAngle,
		"Ascent":      tf.ascent * scale,
		"Descent":     tf.descent * scale,
		"CapHeight":   tf.capHeight * scale,
		"StemV":       tf.stemV,
		"FontFile2":   fontFile,
	})

	cidFont := doc.add(pdfDict{
		"Type":     pdfName("Font"),
		"Subtype":  pdfName("CIDFontType2"),
		"BaseFont": pdfName(base),
		"CIDSystemInfo": pdfDict{
			"Registry":   pdfLiteral("Adobe"),
			"Ordering":   pdfLiteral("Identity"),
			"Supplement": 0,
		},
		"FontDescriptor": descriptor,
		"CIDToGIDMap":    pdfName("Identity"),
		"DW":             1000,
		"W":              tf.widthArray(scale),
	})

	toUni := doc.add(&pdfStream{data: []byte(tf.toUnicodeCMap())})

	return doc.add(pdfDict{
		"Type":            pdfName("Font"),
		"Subtype":         pdfName("Type0"),
		"BaseFont":        pdfName(base),
		"Encoding":        pdfName("Identity-H"),
		"DescendantFonts": pdfArray{cidFont},
		"ToUnicode":       toUni,
	})
}

// widthArray builds the CIDFont /W array in 1000-em units (CID = subset gid).
func (tf *ttfFont) widthArray(scale float64) pdfArray {
	widths := make(pdfArray, 0, len(tf.subToOrig))
	for _, orig := range tf.subToOrig {
		widths = append(widths, float64(tf.hmtx[orig])*scale)
	}
	return pdfArray{0, widths}
}

// toUnicodeCMap builds a minimal ToUnicode CMap mapping subset gids to code
// points, so text is copy/paste-able from the produced PDF.
func (tf *ttfFont) toUnicodeCMap() string {
	subs := make([]int, 0, len(tf.toUnicode))
	for s := range tf.toUnicode {
		subs = append(subs, int(s))
	}
	sort.Ints(subs)
	var b bytes.Buffer
	b.WriteString("/CIDInit /ProcSet findresource begin\n")
	b.WriteString("12 dict begin\nbegincmap\n")
	b.WriteString("/CMapName /Adobe-Identity-UCS def\n")
	b.WriteString("/CMapType 2 def\n")
	b.WriteString("1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n")
	fmt.Fprintf(&b, "%d beginbfchar\n", len(subs))
	for _, s := range subs {
		fmt.Fprintf(&b, "<%04X> <%04X>\n", s, tf.toUnicode[uint16(s)])
	}
	b.WriteString("endbfchar\nendcmap\n")
	b.WriteString("CMapName currentdict /CMap defineresource pop\nend\nend\n")
	return b.String()
}

// sanitizeName strips characters not allowed in a PDF BaseFont name.
func sanitizeName(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c > 32 && c < 127 && c != '/' && c != '(' && c != ')' && c != '<' && c != '>' {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return "Embedded"
	}
	return string(out)
}
