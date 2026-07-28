// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-ruby-prawn/prawn authors

package prawn

// This file is the go-opentype-backed font backend. Where ttf.go hand-rolls a
// TrueType-only parser and glyf subsetter, this backend delegates parsing,
// glyph mapping (cmap), advance metrics, complex-script shaping (GSUB/GPOS) and
// variable-font instancing to the pure-Go github.com/go-opentype/opentype stack
// (plus github.com/go-opentype/shape for Arabic/Indic/CJK shaping). It thereby
// gains three capabilities the ttf.go path cannot offer:
//
//   - CFF/OpenType ("OTTO", a 'CFF ' table) font embedding, as a
//     CIDFontType0 descendant with a FontFile3 (/Subtype /OpenType) program;
//   - an opt-in shaped-text path so Arabic joining, Indic reordering and CJK
//     render with the correct glyphs and positioning;
//   - selecting a variable-font instance (SetVariation) so measured widths and
//     the emitted per-CID /W advances follow a chosen axis coordinate.
//
// glyf-outline fonts registered through this backend embed as a CIDFontType2 /
// FontFile2 instead, so the same shaping and variation features also work for
// TrueType-outline fonts. Encoding is Identity-H with CIDToGIDMap Identity, so
// the whole font program is embedded and the CID is the original glyph id (no
// glyf subsetting is performed here; see the scope note on subsetting below).
//
// Everything is pure Go (CGO disabled) on every supported target.

import (
	"fmt"

	"github.com/go-opentype/opentype"
	"github.com/go-opentype/shape"
)

// OTFOptions mirror the per-font keyword arguments prawn accepts when a TrueType
// or OpenType file is registered through the go-opentype backend. The zero value
// selects the default, Prawn-compatible behaviour: no shaping (each rune maps
// straight through the cmap), no variation (the font's default master).
type OTFOptions struct {
	// Shape enables the complex-text shaper (github.com/go-opentype/shape) for
	// every run drawn with this font: GSUB substitution (ligatures, Arabic
	// joining forms, Indic reordering) and GPOS positioning (kerning, mark
	// attachment). With it off the run is the plain cmap glyph mapping, byte-for
	// -byte the same as the default Prawn text path.
	Shape bool
	// Script forces the shaping script tag ("arab", "deva", "latn", …). Empty
	// auto-detects from the text. Ignored when Shape is false.
	Script string
	// Vertical selects vertical (CJK tategaki) shaping. Ignored when Shape is
	// false.
	Vertical bool
	// Variation, when non-empty, instances a variable font at the given
	// user-space axis coordinates (keyed by axis tag, e.g. {"wght": 700}). Axes
	// absent from the map keep their default. An unknown axis tag is an error.
	Variation map[string]float64
}

// otfFont holds a font parsed by go-opentype plus the running per-CID usage
// state used to emit the PDF Type0 font graph.
type otfFont struct {
	name     string
	baseName string
	resName  string
	data     []byte // the original font program, embedded whole (FontFile2/3)

	font *opentype.Font
	face *opentype.Face // metrics/shaping face at unitsPerEm (scale 1 => font units)

	isCFF bool // CFF/OpenType outlines (FontFile3) vs glyf (FontFile2)

	unitsPerEm  float64
	ascent      float64
	descent     float64
	lineGap     float64
	capHeight   float64
	italicAngle float64
	stemV       float64
	bbox        [4]float64
	flags       int

	shaped   bool
	script   string
	vertical bool

	// Identity-H emission state: CID == original glyph id (CIDToGIDMap Identity).
	usedGID   []opentype.GlyphIndex           // CIDs used, in first-seen order
	seen      map[opentype.GlyphIndex]bool    // membership test for usedGID
	gidAdv    map[opentype.GlyphIndex]float64 // recorded advance (font units) per CID
	toUnicode map[opentype.GlyphIndex]rune    // best-effort CID -> code point
}

// RegisterFontOTF parses a TrueType or OpenType font with the go-opentype stack
// and registers it under family name, gaining CFF/OTF embedding plus optional
// complex-script shaping and variable-font instancing (see OTFOptions).
// Subsequent Font(name, …) calls select it, exactly like a font registered with
// RegisterFontTTF. A nil opts means the default, Prawn-compatible behaviour.
func (d *Document) RegisterFontOTF(name string, data []byte, opts *OTFOptions) error {
	of, err := parseOTF(data, opts)
	if err != nil {
		d.fail(err)
		return err
	}
	of.name = name
	d.fontOrder++
	fr := &fontRef{resName: fmt.Sprintf("F%d.0", d.fontOrder), otf: of}
	of.resName = fr.resName
	d.fonts["ttf:"+name] = fr
	return nil
}

// parseOTF decodes the font through go-opentype, reads the descriptor scalars
// from the sfnt header tables, and applies the requested shaping/variation
// options.
func parseOTF(data []byte, opts *OTFOptions) (*otfFont, error) {
	font, err := opentype.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnknownFont, err)
	}
	// opentype.Parse has already validated the sfnt container (magic, directory
	// bounds and every table's extent), so the directory re-read here is
	// guaranteed well-formed.
	tables := sfntTables(data)
	of := &otfFont{
		data:      data,
		font:      font,
		isCFF:     hasTable(tables, "CFF "),
		stemV:     80,
		seen:      map[opentype.GlyphIndex]bool{},
		gidAdv:    map[opentype.GlyphIndex]float64{},
		toUnicode: map[opentype.GlyphIndex]rune{},
	}
	of.readDescriptor(data, tables)
	of.face = font.NewFace(int(of.unitsPerEm)) // scale 1: advances come back in font units

	if opts != nil {
		if err := of.applyOptions(opts); err != nil {
			return nil, err
		}
	}
	return of, nil
}

// applyOptions records the shaping flags and validates + applies a variable
// instance.
func (of *otfFont) applyOptions(opts *OTFOptions) error {
	of.shaped = opts.Shape
	of.script = opts.Script
	of.vertical = opts.Vertical
	if len(opts.Variation) == 0 {
		return nil
	}
	axes := map[string]bool{}
	for _, a := range of.font.Axes() {
		axes[a.Tag] = true
	}
	for tag := range opts.Variation {
		if !axes[tag] {
			return fmt.Errorf("%w: font has no variation axis %q", ErrUnknownFont, tag)
		}
	}
	of.face.SetVariation(opts.Variation)
	return nil
}

// readDescriptor fills in the FontDescriptor scalars (units/em, ascent, descent,
// bounding box, cap height, italic angle, flags) from the sfnt head/hhea/OS2/
// post tables. go-opentype validated the container; these header reads recover
// the descriptor metadata the opentype API does not surface.
func (of *otfFont) readDescriptor(data []byte, tables map[string]tableRec) {
	head := tableBytes(data, tables["head"])
	of.unitsPerEm = float64(be16(head[18:20]))
	of.bbox = [4]float64{
		float64(int16(be16(head[36:38]))),
		float64(int16(be16(head[38:40]))),
		float64(int16(be16(head[40:42]))),
		float64(int16(be16(head[42:44]))),
	}
	hhea := tableBytes(data, tables["hhea"])
	of.ascent = float64(int16(be16(hhea[4:6])))
	of.descent = float64(int16(be16(hhea[6:8])))
	of.lineGap = float64(int16(be16(hhea[8:10])))
	if r, ok := tables["OS/2"]; ok && r.length >= 90 {
		of.capHeight = float64(int16(be16(tableBytes(data, r)[88:90])))
	}
	if of.capHeight == 0 {
		of.capHeight = of.ascent
	}
	if r, ok := tables["post"]; ok && r.length >= 8 {
		of.italicAngle = float64(int32(be32(tableBytes(data, r)[4:8]))) / 65536.0
	}
	flags := 0x20 // nonsymbolic
	if of.italicAngle != 0 {
		flags |= 0x40
	}
	of.flags = flags
}

// sfntTables reads an sfnt table directory into a tag->record map (shared record
// type with ttf.go). It assumes a well-formed directory; parseOTF only calls it
// after opentype.Parse has validated the container.
func sfntTables(data []byte) map[string]tableRec {
	num := int(be16(data[4:6]))
	tables := make(map[string]tableRec, num)
	off := 12
	for i := 0; i < num; i++ {
		tag := string(data[off : off+4])
		tables[tag] = tableRec{offset: be32(data[off+8 : off+12]), length: be32(data[off+12 : off+16])}
		off += 16
	}
	return tables
}

func hasTable(tables map[string]tableRec, tag string) bool {
	_, ok := tables[tag]
	return ok
}

func tableBytes(data []byte, r tableRec) []byte { return data[r.offset : r.offset+r.length] }

// shapedGlyph is one glyph of a laid-out run: the CID to emit (== original gid),
// the advance the pen should move by (positioned, in font units), the glyph's
// context-free base advance (what goes in /W), and the source code point (for
// ToUnicode). For the unshaped path advance == base.
type shapedGlyph struct {
	gid     opentype.GlyphIndex
	advance float64 // positioned advance (GPOS-adjusted, or instanced for unshaped)
	base    float64 // context-free advance for the /W array
	text    rune    // source code point (best effort for ligatures)
}

// layoutRun turns a UTF-8 string into its glyph run: the plain cmap mapping when
// shaping is off, or the shape.Shape result (GSUB + GPOS in font units) when it
// is on. It is the single source of truth for both measurement and emission so
// the two never disagree.
func (of *otfFont) layoutRun(s string) []shapedGlyph {
	if of.shaped {
		return of.shapeRun(s)
	}
	var out []shapedGlyph
	for _, r := range s {
		gid, _ := of.font.GlyphIndex(r)    // unmapped -> 0 (.notdef)
		adv := float64(of.face.Advance(r)) // scale 1 => font units, variation-aware
		out = append(out, shapedGlyph{gid: gid, advance: adv, base: adv, text: r})
	}
	return out
}

// shapeRun runs the complex-text shaper. Because the face was built at
// unitsPerEm (scale 1), the shaper's pixel advances are already in font units.
// The /W base advance is the glyph's own context-free advance, so per-run GPOS
// kerning and ligature spacing ride on TJ adjustments rather than a single /W
// entry that could not capture context.
func (of *otfFont) shapeRun(s string) []shapedGlyph {
	runes := []rune(s)
	glyphs := shape.Shape(of.face, s, shape.Options{
		Script:   of.script,
		Vertical: of.vertical,
	})
	out := make([]shapedGlyph, len(glyphs))
	for i, g := range glyphs {
		out[i] = shapedGlyph{
			gid:     g.GID,
			advance: float64(g.XAdvance),
			base:    float64(of.font.GlyphAdvance(g.GID)),
			text:    runes[g.Cluster],
		}
	}
	return out
}

// record marks a glyph as used (assigning it a /W and ToUnicode entry) the first
// time it appears, keeping its base advance and source code point.
func (of *otfFont) record(g shapedGlyph) {
	if !of.seen[g.gid] {
		of.seen[g.gid] = true
		of.usedGID = append(of.usedGID, g.gid)
		of.gidAdv[g.gid] = g.base
		of.toUnicode[g.gid] = g.text
	}
}

// operand lays out s, records per-CID usage, and returns the content-stream text
// operator: a simple Tj of concatenated 2-byte CIDs when every glyph's pen
// advance equals its /W base advance, or a TJ array carrying the per-glyph
// horizontal adjustments (GPOS kerning / ligature spacing) otherwise. An
// adjustment is (base − advance) in thousandths of an em, per the TJ sign
// convention (a positive number moves the pen left).
func (of *otfFont) operand(s string) string {
	run := of.layoutRun(s)
	for _, g := range run {
		of.record(g)
	}
	if !needsTJ(run) {
		gids := make([]opentype.GlyphIndex, len(run))
		for i, g := range run {
			gids[i] = g.gid
		}
		return "<" + hexEncode(gidBytesOTF(gids)) + "> Tj"
	}
	scale := 1000.0 / of.unitsPerEm
	var b []byte
	b = append(b, '[')
	for _, g := range run {
		b = append(b, '<')
		b = append(b, hexEncode(gidBytesOTF([]opentype.GlyphIndex{g.gid}))...)
		b = append(b, '>')
		if adj := (g.base - g.advance) * scale; adj != 0 {
			b = appendNum(b, adj)
		}
	}
	b = append(b, ']')
	return string(b) + " TJ"
}

// needsTJ reports whether any glyph's positioned advance differs from its /W
// base advance, so the run must be emitted as a positioned TJ array.
func needsTJ(run []shapedGlyph) bool {
	for _, g := range run {
		if g.base != g.advance {
			return true
		}
	}
	return false
}

// appendNum appends a space-separated normalized number to a TJ operand buffer.
func appendNum(b []byte, f float64) []byte {
	b = append(b, ' ')
	b = append(b, formatReal(f)...)
	return b
}

// gidBytesOTF packs opentype glyph ids as 2-byte big-endian codes (Identity-H).
func gidBytesOTF(gids []opentype.GlyphIndex) []byte {
	b := make([]byte, len(gids)*2)
	for i, g := range gids {
		b[i*2] = byte(g >> 8)
		b[i*2+1] = byte(g)
	}
	return b
}

// widthOf measures s in points at the given size, honouring shaping and the
// selected variation instance.
func (of *otfFont) widthOf(s string, size float64) float64 {
	total := 0.0
	for _, g := range of.layoutRun(s) {
		total += g.advance
	}
	return total * size / of.unitsPerEm
}

func (of *otfFont) ascenderScaled(size float64) float64 { return of.ascent * size / of.unitsPerEm }

func (of *otfFont) height(size float64) float64 {
	return (of.ascent - of.descent + of.lineGap) * size / of.unitsPerEm
}

// buildObjects writes the Type0 font graph (CIDFontType0/FontFile3 for CFF, or
// CIDFontType2/FontFile2 for glyf) and returns the Type0 dictionary reference.
func (of *otfFont) buildObjects(doc *pdfDoc) pdfRef {
	base := of.baseName
	if base == "" {
		base = "AAAAAA+" + sanitizeName(of.name)
	}
	scale := 1000.0 / of.unitsPerEm

	var fontFile pdfRef
	var cidSubtype pdfName
	if of.isCFF {
		fontFile = doc.add(&pdfStream{
			dict: pdfDict{"Subtype": pdfName("OpenType")},
			data: of.data,
		})
		cidSubtype = pdfName("CIDFontType0")
	} else {
		fontFile = doc.add(&pdfStream{
			dict: pdfDict{"Length1": len(of.data)},
			data: of.data,
		})
		cidSubtype = pdfName("CIDFontType2")
	}

	descriptorDict := pdfDict{
		"Type":        pdfName("FontDescriptor"),
		"FontName":    pdfName(base),
		"Flags":       of.flags,
		"FontBBox":    pdfArray{of.bbox[0] * scale, of.bbox[1] * scale, of.bbox[2] * scale, of.bbox[3] * scale},
		"ItalicAngle": of.italicAngle,
		"Ascent":      of.ascent * scale,
		"Descent":     of.descent * scale,
		"CapHeight":   of.capHeight * scale,
		"StemV":       of.stemV,
	}
	if of.isCFF {
		descriptorDict["FontFile3"] = fontFile
	} else {
		descriptorDict["FontFile2"] = fontFile
	}
	descriptor := doc.add(descriptorDict)

	cidDict := pdfDict{
		"Type":     pdfName("Font"),
		"Subtype":  cidSubtype,
		"BaseFont": pdfName(base),
		"CIDSystemInfo": pdfDict{
			"Registry":   pdfLiteral("Adobe"),
			"Ordering":   pdfLiteral("Identity"),
			"Supplement": 0,
		},
		"FontDescriptor": descriptor,
		"DW":             1000,
		"W":              of.widthArray(scale),
	}
	if !of.isCFF {
		cidDict["CIDToGIDMap"] = pdfName("Identity")
	}
	cidFont := doc.add(cidDict)

	toUni := doc.add(&pdfStream{data: []byte(of.toUnicodeCMap())})

	return doc.add(pdfDict{
		"Type":            pdfName("Font"),
		"Subtype":         pdfName("Type0"),
		"BaseFont":        pdfName(base),
		"Encoding":        pdfName("Identity-H"),
		"DescendantFonts": pdfArray{cidFont},
		"ToUnicode":       toUni,
	})
}

// widthArray builds the CIDFont /W array in 1000-em units, one entry per used
// CID (== original glyph id), in first-seen order.
func (of *otfFont) widthArray(scale float64) pdfArray {
	out := make(pdfArray, 0, len(of.usedGID)*2)
	for _, gid := range of.usedGID {
		out = append(out, pdfArray{int(gid), pdfArray{of.gidAdv[gid] * scale}})
	}
	return out
}

// toUnicodeCMap builds a ToUnicode CMap mapping each used CID to a code point so
// text stays copy/paste-able. Ligature and reordered glyphs map to their source
// cluster's code point (best effort).
func (of *otfFont) toUnicodeCMap() string {
	var b []byte
	b = append(b, "/CIDInit /ProcSet findresource begin\n"...)
	b = append(b, "12 dict begin\nbegincmap\n"...)
	b = append(b, "/CMapName /Adobe-Identity-UCS def\n"...)
	b = append(b, "/CMapType 2 def\n"...)
	b = append(b, "1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n"...)
	b = append(b, fmt.Sprintf("%d beginbfchar\n", len(of.usedGID))...)
	for _, gid := range of.usedGID {
		b = append(b, fmt.Sprintf("<%04X> <%04X>\n", int(gid), of.toUnicode[gid])...)
	}
	b = append(b, "endbfchar\nendcmap\n"...)
	b = append(b, "CMapName currentdict /CMap defineresource pop\nend\nend\n"...)
	return string(b)
}
