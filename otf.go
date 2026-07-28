// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-ruby-prawn/prawn authors

package prawn

// This file is the go-opentype-backed font backend. Where ttf.go hand-rolls a
// TrueType-only parser and glyf subsetter, this backend delegates parsing,
// glyph mapping (cmap), advance metrics, complex-script shaping (GSUB/GPOS),
// variable-font instancing and — since go-opentype v0.5 — the FontDescriptor
// scalars and font subsetting to the pure-Go github.com/go-opentype/opentype
// stack (plus github.com/go-opentype/shape for Arabic/Indic/CJK shaping). It
// thereby gains three capabilities the ttf.go path cannot offer:
//
//   - CFF/OpenType ("OTTO", a 'CFF ' table) font embedding, as a
//     CIDFontType0 descendant with a FontFile3 program;
//   - an opt-in shaped-text path so Arabic joining, Indic reordering and CJK
//     render with the correct glyphs and positioning;
//   - selecting a variable-font instance (SetVariation) so measured widths and
//     the emitted per-CID /W advances follow a chosen axis coordinate.
//
// glyf-outline fonts registered through this backend embed as a CIDFontType2 /
// FontFile2 instead, so the same shaping and variation features also work for
// TrueType-outline fonts.
//
// Both outline flavours are now subset before embedding rather than shipped
// whole: a glyf font through opentype.Font.SubsetTrueType (embedding the compact
// subset plus a /CIDToGIDMap built from the old->new glyph remap), a CFF/OpenType
// font through opentype.Font.SubsetCFF (a CIDFontType0C program whose glyph
// numbering is preserved, so an Identity map stays valid). Encoding is Identity-H
// and every CID is the original glyph id, so the drawn content stream is
// unaffected by the remap; only the embedded program and the /CIDToGIDMap change.
//
// Two edges fall back to embedding the whole font program (see embedCFF /
// embedGlyf): a CID-keyed CFF (ROS/FDArray/FDSelect) or CFF2 (variable) font,
// which SubsetCFF deliberately rejects because its preserve-numbering rewrite
// cannot follow their glyph indirection; and the (in practice unreachable)
// failure of SubsetTrueType. The fallback is behaviourally identical to the
// pre-subsetting backend, so no font stops embedding.
//
// The FontDescriptor scalars (units/em, ascent, descent, bounding box, cap
// height, italic angle, StemV estimate and the PDF /Flags bit set) come straight
// from the opentype.Font descriptor accessors; the sfnt header is no longer
// re-parsed here. Variable-instance baking (opentype.Font.InstanceBytes before
// subsetting) is intentionally not performed: PDF viewers render an embedded
// variable font at its default master anyway, so the pre-existing behaviour of
// instanced /W advances over default-master outlines is preserved exactly, and
// a static-instance bake is left as a future step.
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
	data     []byte // the original font program (embedded whole only on a subset fallback)

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

	// Identity-H emission state: CID == original glyph id.
	usedGID   []opentype.GlyphIndex        // CIDs used, in first-seen order
	seen      map[opentype.GlyphIndex]bool // membership test for usedGID
	toUnicode map[opentype.GlyphIndex]rune // best-effort CID -> code point
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
// from the opentype.Font accessors, and applies the requested shaping/variation
// options.
func parseOTF(data []byte, opts *OTFOptions) (*otfFont, error) {
	font, err := opentype.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnknownFont, err)
	}
	of := &otfFont{
		data:      data,
		font:      font,
		isCFF:     !fontHasTable(font, "glyf"),
		seen:      map[opentype.GlyphIndex]bool{},
		toUnicode: map[opentype.GlyphIndex]rune{},
	}
	of.readDescriptor()
	of.face = font.NewFace(int(of.unitsPerEm)) // scale 1: advances come back in font units

	if opts != nil {
		if err := of.applyOptions(opts); err != nil {
			return nil, err
		}
	}
	return of, nil
}

// fontHasTable reports whether the parsed font carries the sfnt table tag. It is
// how the backend tells a glyf font (FontFile2/CIDFontType2) from a CFF or CFF2
// one (FontFile3/CIDFontType0): a CFF flavour is simply the absence of 'glyf'.
func fontHasTable(f *opentype.Font, tag string) bool {
	_, ok := f.Table(tag)
	return ok
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

// readDescriptor fills in the FontDescriptor scalars from the go-opentype
// descriptor accessors, which decode head/hhea/OS-2/post at Parse time. The cap
// height falls back to the ascender when the font carries none (no OS/2, or an
// OS/2 below version 2), matching what a PDF consumer substitutes.
func (of *otfFont) readDescriptor() {
	f := of.font
	of.unitsPerEm = float64(f.UnitsPerEm())
	of.ascent = float64(f.Ascent())
	of.descent = float64(f.Descent())
	of.lineGap = float64(f.LineGap())
	xMin, yMin, xMax, yMax := f.FontBBox()
	of.bbox = [4]float64{float64(xMin), float64(yMin), float64(xMax), float64(yMax)}
	of.capHeight = float64(f.CapHeight())
	if of.capHeight == 0 {
		of.capHeight = of.ascent
	}
	of.italicAngle = f.ItalicAngle()
	of.stemV = float64(f.StemV())
	of.flags = f.Flags()
}

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
// the two never disagree. Base advances come from the face's by-glyph-index
// advance, so they honour the selected variation instance exactly like /W does.
func (of *otfFont) layoutRun(s string) []shapedGlyph {
	if of.shaped {
		return of.shapeRun(s)
	}
	var out []shapedGlyph
	for _, r := range s {
		gid, _ := of.font.GlyphIndex(r)       // unmapped -> 0 (.notdef)
		adv := of.face.AdvanceIndexUnits(gid) // scale 1 => font units, variation-aware
		out = append(out, shapedGlyph{gid: gid, advance: adv, base: adv, text: r})
	}
	return out
}

// shapeRun runs the complex-text shaper. Because the face was built at
// unitsPerEm (scale 1), the shaper's pixel advances are already in font units.
// The /W base advance is the glyph's own context-free advance (from the face, so
// it tracks the instance), so per-run GPOS kerning and ligature spacing ride on
// TJ adjustments rather than a single /W entry that could not capture context.
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
			base:    of.face.AdvanceIndexUnits(g.GID),
			text:    runes[g.Cluster],
		}
	}
	return out
}

// record marks a glyph as used (assigning it a ToUnicode entry) the first time it
// appears, keeping its source code point.
func (of *otfFont) record(g shapedGlyph) {
	if !of.seen[g.gid] {
		of.seen[g.gid] = true
		of.usedGID = append(of.usedGID, g.gid)
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
// The embedded program is a subset of the original font (see embedCFF/embedGlyf).
func (of *otfFont) buildObjects(doc *pdfDoc) pdfRef {
	base := of.baseName
	if base == "" {
		base = "AAAAAA+" + sanitizeName(of.name)
	}
	scale := 1000.0 / of.unitsPerEm

	var fontFile pdfRef
	var cidSubtype pdfName
	var cidToGIDMap any
	if of.isCFF {
		cidSubtype = pdfName("CIDFontType0")
		fontFile = of.embedCFF(doc)
	} else {
		cidSubtype = pdfName("CIDFontType2")
		fontFile, cidToGIDMap = of.embedGlyf(doc)
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
		cidDict["CIDToGIDMap"] = cidToGIDMap
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

// embedCFF writes the FontFile3 program for a CFF/OpenType font: a subset 'CFF '
// table (FontFile3 /Subtype /CIDFontType0C) when opentype.Font.SubsetCFF can
// subset it — the common name-keyed OTF — or the whole OpenType program
// (/Subtype /OpenType) as a graceful fallback for the fonts SubsetCFF rejects:
// CID-keyed CFF (ROS/FDArray/FDSelect) and CFF2 (variable). Their glyph
// indirection the preserve-numbering subsetter cannot safely rewrite, so shipping
// the whole program keeps them embeddable, exactly as the pre-subsetting backend
// did. SubsetCFF preserves glyph numbering, so the Identity CID->glyph mapping the
// content stream relies on stays valid either way.
func (of *otfFont) embedCFF(doc *pdfDoc) pdfRef {
	if sub, err := of.font.SubsetCFF(of.usedGID); err == nil {
		return doc.add(&pdfStream{
			dict: pdfDict{"Subtype": pdfName("CIDFontType0C")},
			data: sub,
		})
	}
	return doc.add(&pdfStream{
		dict: pdfDict{"Subtype": pdfName("OpenType")},
		data: of.data,
	})
}

// embedGlyf writes the FontFile2 program and /CIDToGIDMap value for a glyf font:
// a SubsetTrueType subset (embedded with a /CIDToGIDMap stream mapping each CID —
// the original glyph id the content stream emits — to its compact id in the
// subset), or the whole program with an Identity /CIDToGIDMap as a graceful
// fallback should subsetting fail (in practice unreachable: the used gids always
// come from the font's own cmap/shaper and so are in range).
func (of *otfFont) embedGlyf(doc *pdfDoc) (pdfRef, any) {
	if sub, oldToNew, err := of.font.SubsetTrueType(of.usedGID); err == nil {
		fontFile := doc.add(&pdfStream{
			dict: pdfDict{"Length1": len(sub)},
			data: sub,
		})
		return fontFile, doc.add(&pdfStream{data: of.cidToGIDMap(oldToNew)})
	}
	fontFile := doc.add(&pdfStream{
		dict: pdfDict{"Length1": len(of.data)},
		data: of.data,
	})
	return fontFile, pdfName("Identity")
}

// cidToGIDMap builds the CIDFontType2 /CIDToGIDMap stream for a subset glyf font:
// a big-endian array indexed by CID (the original glyph id) holding that glyph's
// id in the subset. It is sized to the largest used CID; unused slots stay zero
// (mapping to .notdef), and every emitted CID resolves through oldToNew.
func (of *otfFont) cidToGIDMap(oldToNew map[opentype.GlyphIndex]opentype.GlyphIndex) []byte {
	maxCID := 0
	for _, g := range of.usedGID {
		if int(g) > maxCID {
			maxCID = int(g)
		}
	}
	m := make([]byte, 2*(maxCID+1))
	for _, orig := range of.usedGID {
		sub := oldToNew[orig]
		m[2*int(orig)] = byte(sub >> 8)
		m[2*int(orig)+1] = byte(sub)
	}
	return m
}

// widthArray builds the CIDFont /W array in 1000-em units, one entry per used
// CID (== original glyph id), in first-seen order. The advance comes from the
// face's by-glyph-index advance, so it tracks the selected variation instance.
func (of *otfFont) widthArray(scale float64) pdfArray {
	out := make(pdfArray, 0, len(of.usedGID)*2)
	for _, gid := range of.usedGID {
		out = append(out, pdfArray{int(gid), pdfArray{of.face.AdvanceIndexUnits(gid) * scale}})
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
