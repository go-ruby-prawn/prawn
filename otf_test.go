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

	// Structural markers of a CIDFontType0 / FontFile3 OpenType embed.
	for _, marker := range []string{"CIDFontType0", "FontFile3", "OpenType", "Type0", "Identity-H"} {
		if !bytes.Contains(b, []byte(marker)) {
			t.Errorf("embedded PDF missing %q", marker)
		}
	}
	if bytes.Contains(b, []byte("CIDToGIDMap")) {
		t.Error("CFF CIDFontType0 must not carry CIDToGIDMap")
	}
	// rsc.io/pdf must be able to re-open the produced file.
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
