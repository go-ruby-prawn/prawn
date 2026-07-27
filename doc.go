// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-ruby-prawn/prawn authors

// Package prawn is a pure-Go (CGO=0), MRI-faithful reimplementation of Ruby's
// prawn PDF-generation gem.
//
// It mirrors prawn's public DSL — Prawn::Document and its text / font / color /
// graphics / image / table / transformation / grid / page-management methods,
// plus the Prawn::Errors tree — and writes the PDF with its own native,
// pure-Go PDF object model and content-stream writer (see pdfcore.go /
// content.go). The emitted content-stream operators match Ruby prawn / PDF::Core
// operator-for-operator (BT/ET, Td, Tf, kerned TJ, re/l/m/c, S/f/b, cs/scn,
// CS/SCN, cm, Do, gs), so a PDF::Inspector-style parse of the output yields the
// same operator tree. Nothing here uses cgo or a Ruby runtime, so the whole
// module builds and runs with CGO disabled on every supported 64-bit target and
// compiles to WebAssembly.
//
// # Mapping from Ruby to Go
//
// Ruby's block-form generator
//
//	Prawn::Document.generate("hello.pdf") do |pdf|
//	  pdf.text "Hello World", size: 24, style: :bold
//	  pdf.move_down 20
//	  pdf.stroke_rectangle [0, pdf.cursor], 200, 100
//	end
//
// becomes
//
//	prawn.Generate("hello.pdf", func(pdf *prawn.Document) error {
//	    pdf.Text("Hello World", &prawn.TextOptions{Size: 24, Style: prawn.StyleBold, StyleSet: true})
//	    pdf.MoveDown(20)
//	    pdf.StrokeRectangle(0, pdf.Cursor(), 200, 100)
//	    return nil
//	})
//
// Ruby keyword-argument hashes map to option structs (nil selects prawn's
// defaults); Ruby symbols map to typed Go constants (Style*, Align*, page-size
// and layout string constants). Coordinates use PDF points (1/72 inch) with
// prawn's native origin: the lower-left corner of the current bounding box, y up.
//
// # Fonts
//
// The 14 standard PDF (AFM/Core-14) fonts are supported with prawn's exact
// glyph-advance and kerning metrics (WinAnsi/cp1252 encoding, kerned TJ output).
// TrueType fonts are embedded the way prawn/ttfunk does: parsed, subset down to
// the used glyphs (following composite-glyph references), and written as a Type0
// / CIDFontType2 font with Identity-H encoding, a FontFile2 stream, a per-CID /W
// width array and a /ToUnicode CMap (see RegisterFontTTF).
//
// # Verification
//
// Every PDF is a well-formed PDF that round-trips through the pure-Go rsc.io/pdf
// reader. The library is validated against the real Ruby prawn gem with a
// PDF::Inspector-style differential oracle: for a set of scenarios, the
// content-stream operators this package emits are compared, operator-for-operator
// (numbers normalized), against the gem's output for the same script. The gem's
// output is captured into testdata/oracle/*.content and committed so the oracle
// runs in CI without Ruby; a skip-gated test re-verifies those fixtures against a
// live gem when one is installed. See prawn_diff_test.go and prawn_gem_test.go.
//
// # Determinism
//
// The PDF /CreationDate and file /ID are derived from a package clock seam (see
// SetClock) rather than the wall clock, so output is reproducible and timezone
// independent. The bytes are big-endian/little-endian independent (the format is
// byte-oriented and all binary parsing is explicitly big-endian), so the s390x
// lane exercises the same code path as the little-endian ones.
//
// # Scope
//
// Implemented: document + page management (Generate/New, Render, RenderFile,
// StartNewPage, Cursor, Bounds, MoveDown/Up, Pad, page sizes and layout,
// SkipPageCreation, stream compression); text (Text, DrawText, TextBox with
// truncate/expand/error overflow, Font/FontSize/Leading, alignment, kerning,
// WidthOfString); formatted text (FormattedText runs and TextInline <b>/<i>/
// <color> markup); Core-14 AFM fonts and TrueType embedding with subsetting;
// color (RGB and CMYK fill/stroke via cs/scn, CS/SCN); vector graphics
// (MoveTo/LineTo/CurveTo, Rectangle, Line, Polygon, Circle/Ellipse, Stroke/Fill/
// FillAndStroke/CloseAndStroke, LineWidth, Cap/Join style, Dash); bounding boxes;
// the column/row grid; affine transformations (Rotate/Scale/Translate, with
// origin, and TransformationMatrix) plus save/restore graphics state and
// transparency; PNG (with alpha → SMask) and JPEG images; repeaters and page
// numbering; and the Prawn::Errors tree.
//
// Deferred (named): prawn-table's advanced features (row/column spanning,
// per-cell style callbacks, automatic table pagination); TrueType 'kern'-table
// and GPOS kerning for embedded fonts (AFM kerning is complete); OpenType/CFF
// (glyf-based TrueType only); soft-hyphen/CJK line breaking; and SVG.
package prawn
