# Test data

- `goregular.ttf` — the "Go Regular" TrueType font from the Go font family
  (<https://blog.golang.org/go-fonts>), used only by the test suite to exercise
  TrueType parsing, subsetting and embedding. It is redistributed here under its
  own license, reproduced in `GoFont-LICENSE` (BSD-3-Clause, © Google Inc.). It
  is test input only and is not part of the `prawn` package or its build.

- `SourceSerif4-Regular.otf` — "Source Serif 4" (Adobe), a CFF/OpenType
  ("OTTO") font used by the test suite to exercise the go-opentype-backed font
  backend: CFF/OTF embedding (FontFile3 / CIDFontType0) and opt-in complex-text
  shaping (GSUB ligatures, GPOS kerning). Redistributed under the SIL Open Font
  License 1.1, reproduced in `SourceSerif4-OFL.txt`. Test input only; not part
  of the `prawn` package or its build. Variable-font instancing is tested with
  deterministic synthetic in-memory fonts (see `otf_synth_test.go`), so no
  variable font file is bundled.

- `oracle/*.content` — page-1 PDF content streams captured from the real Ruby
  `prawn` 2.5.0 gem for the differential operator oracle (see
  `prawn_diff_test.go`). Regenerated/verified against a live gem by
  `prawn_gem_test.go` when Ruby + prawn are installed.
