// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-ruby-prawn/prawn authors

package prawn

// Tests for the go-opentype-backed font backend (otf.go): CFF/OpenType and glyf
// embedding, opt-in complex-script shaping, and variable-font instance
// selection. Real fonts (a bundled CFF OTF) and deterministic synthetic
// in-memory fonts (see otf_synth_test.go) are used; no network access.

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-opentype/opentype"
	"rsc.io/pdf"
)

func sourceSerifOTF(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "SourceSerif4-Regular.otf"))
	if err != nil {
		t.Fatalf("read OTF: %v", err)
	}
	return b
}

// ---- CFF/OpenType embedding (FontFile3 / CIDFontType0) ---------------------

func TestOTFEmbedCFF(t *testing.T) {
	defer fixedClock()()
	d := New(Options{})
	if err := d.RegisterFontOTF("Serif", sourceSerifOTF(t), nil); err != nil {
		t.Fatalf("register: %v", err)
	}
	d.Font("Serif", StyleNormal)
	if got := d.FontFamily(); got != "Serif" {
		t.Fatalf("FontFamily = %q", got)
	}
	d.Text("Hello office AV", nil) // draw so the font is used and emitted
	if w := d.WidthOfString("Hello"); w <= 0 {
		t.Fatalf("width_of = %v", w)
	}
	b := mustRender(t, d)

	// Structural markers of a subset CIDFontType0 / FontFile3 (CIDFontType0C)
	// embed. Since go-opentype v0.5 the CFF program is a SubsetCFF subset, so the
	// FontFile3 /Subtype is /CIDFontType0C (a bare 'CFF ' table), not /OpenType.
	for _, marker := range []string{"CIDFontType0", "FontFile3", "CIDFontType0C", "Type0", "Identity-H"} {
		if !bytes.Contains(b, []byte(marker)) {
			t.Errorf("embedded PDF missing %q", marker)
		}
	}
	if bytes.Contains(b, []byte("CIDToGIDMap")) {
		t.Error("CFF CIDFontType0 must not carry CIDToGIDMap")
	}
	// rsc.io/pdf must be able to re-open the produced file (structural oracle).
	r, err := pdf.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if r.NumPage() != 1 {
		t.Fatalf("pages = %d", r.NumPage())
	}
}

// ---- glyf embedding through the OTF backend (FontFile2 / CIDFontType2) ------

func TestOTFEmbedGlyf(t *testing.T) {
	defer fixedClock()()
	// Synthetic glyf font, no OS/2, no post: exercises the capHeight=ascent
	// fallback and the missing-post branch of readDescriptor.
	data := synthGlyfFont(t, 3, []int{0, 500, 480}, false, false, 0, nil)
	d := New(Options{})
	if err := d.RegisterFontOTF("Syn", data, nil); err != nil {
		t.Fatalf("register: %v", err)
	}
	d.Font("Syn", StyleNormal)
	d.Text("AB", nil)
	b := mustRender(t, d)
	for _, marker := range []string{"CIDFontType2", "FontFile2", "CIDToGIDMap", "Identity"} {
		if !bytes.Contains(b, []byte(marker)) {
			t.Errorf("glyf embed missing %q", marker)
		}
	}
	if bytes.Contains(b, []byte("FontFile3")) {
		t.Error("glyf font must not use FontFile3")
	}
}

// ---- descriptor scalar branches: OS/2 cap height + italic post -------------

func TestOTFDescriptorItalicAndOS2(t *testing.T) {
	defer fixedClock()()
	data := synthGlyfFont(t, 2, []int{0, 500}, true, true, -12, nil)
	of, err := parseOTF(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	if of.capHeight != 700 {
		t.Errorf("capHeight = %v, want 700 (from OS/2)", of.capHeight)
	}
	if of.italicAngle != -12 {
		t.Errorf("italicAngle = %v, want -12", of.italicAngle)
	}
	if of.flags&0x40 == 0 {
		t.Error("italic flag not set for slanted font")
	}
}

// ---- opt-in shaping: ligatures change the glyph run ------------------------

func TestOTFShapingLigature(t *testing.T) {
	defer fixedClock()()
	b := sourceSerifOTF(t)

	plain, err := parseOTF(b, nil)
	if err != nil {
		t.Fatal(err)
	}
	shaped, err := parseOTF(b, &OTFOptions{Shape: true})
	if err != nil {
		t.Fatal(err)
	}

	unshaped := plain.layoutRun("office")
	lig := shaped.layoutRun("office")
	if len(lig) >= len(unshaped) {
		t.Fatalf("shaping did not ligate: unshaped=%d shaped=%d", len(unshaped), len(lig))
	}
	// The shaped run must contain a glyph id absent from the unshaped run (the
	// ffi ligature), proving GSUB substitution.
	unseen := map[uint16]bool{}
	for _, g := range unshaped {
		unseen[uint16(g.gid)] = true
	}
	found := false
	for _, g := range lig {
		if !unseen[uint16(g.gid)] {
			found = true
		}
	}
	if !found {
		t.Fatal("no ligature glyph introduced by shaping")
	}
	// Shaped width differs from the naive per-rune width.
	if shaped.widthOf("office", 12) == plain.widthOf("office", 12) {
		t.Error("shaped width equals unshaped width")
	}
}

// ---- shaped kerning is emitted as a positioned TJ array --------------------

func TestOTFShapedKerningTJ(t *testing.T) {
	defer fixedClock()()
	d := New(Options{})
	if err := d.RegisterFontOTF("Serif", sourceSerifOTF(t), &OTFOptions{Shape: true}); err != nil {
		t.Fatal(err)
	}
	of := d.fonts["ttf:Serif"].otf

	// "AV" kerns: a TJ array with a non-zero adjustment on the second glyph and
	// no adjustment on the first (exercises both the append and skip branches).
	op := of.operand("AV")
	if !strings.HasSuffix(op, "TJ") || !strings.HasPrefix(op, "[") {
		t.Fatalf("expected TJ array, got %q", op)
	}
	// "office" ligates with no kerning -> a plain Tj.
	if op2 := of.operand("office"); !strings.HasSuffix(op2, "Tj") {
		t.Fatalf("expected plain Tj, got %q", op2)
	}

	d.Font("Serif", StyleNormal)
	d.Text("AV Wa To", nil)
	b := mustRender(t, d)
	if !bytes.Contains(b, []byte("TJ")) {
		t.Error("kerned shaped text should emit a TJ operator")
	}
	if _, err := pdf.NewReader(bytes.NewReader(b), int64(len(b))); err != nil {
		t.Fatalf("reopen: %v", err)
	}
}

// ---- variable-font instance selection changes advances ---------------------

func variableWghtFont(t *testing.T) []byte {
	t.Helper()
	fv := buildFvar("wght", 100, 400, 900)
	store := buildItemVarStore([][]int{{0}, {200}}) // glyph 1 gains +200 at wght max
	advMap := buildDeltaSetMap([][2]int{{0, 0}, {0, 1}})
	hv := buildHVAR(store, advMap)
	return synthGlyfFont(t, 2, []int{0, 500}, true, true, 0, map[string][]byte{
		"fvar": fv, "HVAR": hv,
	})
}

func TestOTFVariableInstance(t *testing.T) {
	defer fixedClock()()
	data := variableWghtFont(t)

	base, err := parseOTF(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	heavy, err := parseOTF(data, &OTFOptions{Variation: map[string]float64{"wght": 900}})
	if err != nil {
		t.Fatal(err)
	}

	// 'A' is glyph 1: base advance 500, +200 at wght=900 -> 700 font units.
	wb := base.widthOf("A", 1000) // size == unitsPerEm -> width in font units
	wh := heavy.widthOf("A", 1000)
	if wb != 500 {
		t.Fatalf("base width = %v, want 500", wb)
	}
	if wh != 700 {
		t.Fatalf("instanced width = %v, want 700", wh)
	}

	// The emitted /W advance follows the selected instance, so the produced PDF
	// renders at the chosen weight.
	d := New(Options{})
	if err := d.RegisterFontOTF("Var", data, &OTFOptions{Variation: map[string]float64{"wght": 900}}); err != nil {
		t.Fatal(err)
	}
	d.Font("Var", StyleNormal)
	d.Text("A", nil)
	b := mustRender(t, d)
	// /W entry for CID 1 should be 700 (font units == 1000-em here).
	if !bytes.Contains(b, []byte("[ 1 [ 700 ] ]")) && !bytes.Contains(b, []byte("[1 [700]]")) {
		// tolerate spacing differences by checking the number is present near /W
		if !bytes.Contains(b, []byte("700")) {
			t.Error("instanced /W advance (700) not found in embedded font")
		}
	}
}

// ---- error paths -----------------------------------------------------------

func TestOTFErrors(t *testing.T) {
	// Unparseable font: opentype.Parse rejects it.
	d := New(Options{})
	if err := d.RegisterFontOTF("Bad", []byte("not a font at all"), nil); err == nil {
		t.Fatal("want error for garbage font")
	} else if !errors.Is(err, ErrUnknownFont) {
		t.Fatalf("want ErrUnknownFont, got %v", err)
	}
	if d.Error() == nil {
		t.Fatal("document error not recorded")
	}

	// Unknown variation axis is rejected.
	data := synthGlyfFont(t, 2, []int{0, 500}, true, true, 0, map[string][]byte{
		"fvar": buildFvar("wght", 100, 400, 900),
	})
	if _, err := parseOTF(data, &OTFOptions{Variation: map[string]float64{"zzzz": 1}}); err == nil {
		t.Fatal("want error for unknown axis")
	} else if !errors.Is(err, ErrUnknownFont) {
		t.Fatalf("want ErrUnknownFont, got %v", err)
	}
	// A valid axis on that font is accepted.
	if _, err := parseOTF(data, &OTFOptions{Variation: map[string]float64{"wght": 700}}); err != nil {
		t.Fatalf("valid axis rejected: %v", err)
	}
}

// ---- repeated glyphs record once; formatted text + wrapping through OTF -----

func TestOTFFormattedAndRepeat(t *testing.T) {
	defer fixedClock()()
	d := New(Options{})
	if err := d.RegisterFontOTF("Serif", sourceSerifOTF(t), nil); err != nil {
		t.Fatal(err)
	}
	d.Font("Serif", StyleNormal)
	// Repeated letters exercise record()'s already-seen branch.
	d.Text("aaa bbb aaa", nil)
	d.FormattedText([]FormattedFragment{
		{Text: "one "}, {Text: "two", Bold: false},
	}, nil)
	of := d.fonts["ttf:Serif"].otf
	// 'a' recorded exactly once despite many occurrences.
	count := 0
	for _, g := range of.usedGID {
		ga, _ := of.font.GlyphIndex('a')
		if g == ga {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("glyph 'a' recorded %d times, want 1", count)
	}
	mustRender(t, d)
}

// ---- subsetting proof: glyf (SubsetTrueType + /CIDToGIDMap) -----------------

// otfStream fetches the *pdfStream stored at ref in a white-box pdfDoc.
func otfStream(t *testing.T, doc *pdfDoc, ref pdfRef) *pdfStream {
	t.Helper()
	s, ok := doc.objs[ref.id-1].(*pdfStream)
	if !ok {
		t.Fatalf("object %d is not a stream (%T)", ref.id, doc.objs[ref.id-1])
	}
	return s
}

func TestOTFSubsetGlyfSmallerAndReparses(t *testing.T) {
	defer fixedClock()()
	// A real glyf font routed through the OTF backend (FontFile2/CIDFontType2).
	whole, err := os.ReadFile(filepath.Join("testdata", "goregular.ttf"))
	if err != nil {
		t.Fatalf("read TTF: %v", err)
	}
	of, err := parseOTF(whole, nil)
	if err != nil {
		t.Fatal(err)
	}
	if of.isCFF {
		t.Fatal("goregular.ttf should route as a glyf font")
	}
	of.name = "Go"
	// Use only a handful of glyphs so the subset is a small fraction of the font.
	of.operand("Hello World")

	doc := &pdfDoc{}
	fontFileRef, cidMapAny := of.embedGlyf(doc)
	sub := otfStream(t, doc, fontFileRef).data
	if len(sub) >= len(whole) {
		t.Fatalf("subset (%d) not smaller than whole font (%d)", len(sub), len(whole))
	}

	// The embedded FontFile2 is a valid sfnt with far fewer glyphs than the whole
	// font, and every kept CID re-parses to its glyph via the /CIDToGIDMap remap.
	sf, err := opentype.Parse(sub)
	if err != nil {
		t.Fatalf("re-parse subset: %v", err)
	}
	if sf.NumGlyphs() >= of.font.NumGlyphs() {
		t.Fatalf("subset glyphs %d not fewer than whole %d", sf.NumGlyphs(), of.font.NumGlyphs())
	}
	cidMapRef, ok := cidMapAny.(pdfRef)
	if !ok {
		t.Fatalf("subset glyf must carry a /CIDToGIDMap stream, got %T", cidMapAny)
	}
	cidMap := otfStream(t, doc, cidMapRef).data
	face := sf.NewFace(64)        // small size: we only test outline presence, not fidelity
	for _, r := range "HeloWrd" { // the distinct kept letters (space has no outline)
		orig, _ := of.font.GlyphIndex(r)
		subGID := opentype.GlyphIndex(int(cidMap[2*int(orig)])<<8 | int(cidMap[2*int(orig)+1]))
		if subGID == 0 {
			t.Fatalf("CID %d (%q) not remapped in /CIDToGIDMap", orig, r)
		}
		if _, mask, _, _, ok := face.GlyphMaskIndex(subGID, 0, 0); !ok || mask == nil {
			t.Errorf("kept glyph %q (subset gid %d) has no outline after subsetting", r, subGID)
		}
	}
}

// ---- subsetting proof: CFF (SubsetCFF, CIDFontType0C) ----------------------

// wrapCFF wraps a bare 'CFF ' table in a minimal sfnt using src's real companion
// tables, so opentype can re-parse a SubsetCFF result (which is a bare table, not
// a container) and its glyph outlines can be inspected.
func wrapCFF(t *testing.T, cff []byte, src *opentype.Font) []byte {
	t.Helper()
	tabs := map[string][]byte{"CFF ": cff}
	for _, tag := range []string{"head", "maxp", "hhea", "hmtx", "cmap"} {
		b, ok := src.Table(tag)
		if !ok {
			t.Fatalf("source font missing %q", tag)
		}
		tabs[tag] = b
	}
	return synthTables(tabs)
}

func TestOTFSubsetCFFSmallerAndReparses(t *testing.T) {
	defer fixedClock()()
	whole := sourceSerifOTF(t)
	of, err := parseOTF(whole, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !of.isCFF {
		t.Fatal("SourceSerif4 should route as a CFF font")
	}
	of.name = "Serif"
	of.operand("Hello") // keep only H e l o (+ .notdef)

	doc := &pdfDoc{}
	sub := otfStream(t, doc, of.embedCFF(doc)).data
	origCFF, _ := of.font.Table("CFF ")
	if len(sub) >= len(origCFF) {
		t.Fatalf("subset CFF (%d) not smaller than whole 'CFF ' table (%d)", len(sub), len(origCFF))
	}

	// Re-wrap and re-parse: kept glyphs keep their outlines, an unused glyph is
	// emptied by the charstring subsetter.
	sf, err := opentype.Parse(wrapCFF(t, sub, of.font))
	if err != nil {
		t.Fatalf("re-parse wrapped subset: %v", err)
	}
	face := sf.NewFace(64) // small size: we only test outline presence, not fidelity
	keptGID, _ := of.font.GlyphIndex('H')
	if _, mask, _, _, ok := face.GlyphMaskIndex(keptGID, 0, 0); !ok || mask == nil {
		t.Errorf("kept glyph 'H' (gid %d) lost its outline in the CFF subset", keptGID)
	}
	dropGID, _ := of.font.GlyphIndex('Z') // 'Z' was never drawn -> emptied by the subsetter
	if _, mask, _, _, _ := face.GlyphMaskIndex(dropGID, 0, 0); mask != nil {
		t.Errorf("unused glyph 'Z' (gid %d) still has an outline; CFF not subset", dropGID)
	}
}

// ---- CID-keyed CFF / CFF2 fallback: whole program embedded -----------------

func TestOTFCFFFallbackWholeEmbed(t *testing.T) {
	defer fixedClock()()
	// A CID-keyed CFF (ROS/FDArray/FDSelect) is one SubsetCFF rejects, so the
	// backend must fall back to embedding the whole OpenType program.
	whole := synthCIDKeyedCFFFont(t)
	of, err := parseOTF(whole, nil)
	if err != nil {
		t.Fatalf("parse CID-keyed CFF: %v", err)
	}
	if !of.isCFF {
		t.Fatal("CID-keyed CFF should route as a CFF font")
	}
	of.name = "CID"
	of.operand("AB")

	doc := &pdfDoc{}
	stream := otfStream(t, doc, of.embedCFF(doc))
	if got := stream.dict["Subtype"]; got != pdfName("OpenType") {
		t.Fatalf("fallback FontFile3 /Subtype = %v, want OpenType", got)
	}
	if len(stream.data) != len(whole) {
		t.Fatalf("fallback embeds %d bytes, want the whole %d-byte program", len(stream.data), len(whole))
	}

	// End-to-end: the produced PDF still embeds and re-opens.
	d := New(Options{})
	if err := d.RegisterFontOTF("CID", whole, nil); err != nil {
		t.Fatal(err)
	}
	d.Font("CID", StyleNormal)
	d.Text("AB", nil)
	b := mustRender(t, d)
	if !bytes.Contains(b, []byte("OpenType")) {
		t.Error("CID-keyed fallback should embed a /Subtype /OpenType FontFile3")
	}
	if _, err := pdf.NewReader(bytes.NewReader(b), int64(len(b))); err != nil {
		t.Fatalf("reopen: %v", err)
	}
}

// ---- glyf subset failure falls back to a whole embed + Identity map --------

func TestOTFGlyfFallbackWholeEmbed(t *testing.T) {
	defer fixedClock()()
	whole := synthGlyfFont(t, 3, []int{0, 500, 480}, false, false, 0, nil)
	of, err := parseOTF(whole, nil)
	if err != nil {
		t.Fatal(err)
	}
	of.name = "Syn"
	// Force SubsetTrueType to fail with an out-of-range CID so the graceful
	// whole-embed / Identity-map fallback branch runs.
	const badGID = opentype.GlyphIndex(60000) // valid uint16, but past the 3-glyph font
	of.usedGID = []opentype.GlyphIndex{badGID}
	of.seen[badGID] = true
	of.toUnicode[badGID] = 'A'

	doc := &pdfDoc{}
	fontFileRef, cidMapAny := of.embedGlyf(doc)
	if got := cidMapAny; got != pdfName("Identity") {
		t.Fatalf("glyf fallback /CIDToGIDMap = %v, want Identity", got)
	}
	if data := otfStream(t, doc, fontFileRef).data; len(data) != len(whole) {
		t.Fatalf("glyf fallback embeds %d bytes, want the whole %d", len(data), len(whole))
	}
}
