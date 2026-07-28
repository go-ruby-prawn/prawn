<p align="center"><img src="https://raw.githubusercontent.com/go-ruby-prawn/brand/main/social/go-ruby-prawn-prawn.png" alt="go-ruby-prawn/prawn" width="720"></p>

# prawn — go-ruby-prawn

[![Docs](https://img.shields.io/badge/docs-mkdocs--material-DC2626)](https://go-ruby-prawn.github.io/docs/)
[![License](https://img.shields.io/badge/license-BSD--3--Clause-blue)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.26.4%2B-00ADD8)](https://go.dev/dl/)
[![Coverage](https://img.shields.io/badge/coverage-100%25-1a7f37)](#tests--coverage)

**A pure-Go (no cgo), MRI-faithful reimplementation of Ruby's
[`prawn`](https://github.com/prawnpdf/prawn) PDF-generation gem.** It mirrors
prawn's document DSL — text, fonts (Core-14 **and** embedded/subset TrueType),
colors (RGB **and** CMYK), vector graphics, images, transformations, a grid,
bounding boxes, repeaters/page numbering, a table, plus the `Prawn::Errors`
tree — and writes the PDF with its **own native pure-Go PDF object model and
content-stream writer**. The emitted content-stream operators match Ruby prawn /
`PDF::Core` operator-for-operator, so the output is validated against the real
gem with a `PDF::Inspector`-style differential oracle. No C PDF library, no Ruby
runtime, and no third-party PDF generator: the module has zero non-stdlib runtime
dependencies and builds with **CGO disabled** on every supported 64-bit target
(and to WebAssembly).

It is a PDF backend for
[go-embedded-ruby](https://github.com/go-embedded-ruby/ruby), but is a
**standalone, reusable** module — a sibling of
[go-ruby-rqrcode](https://github.com/go-ruby-rqrcode/rqrcode),
[go-ruby-regexp](https://github.com/go-ruby-regexp/regexp) and
[go-ruby-yaml](https://github.com/go-ruby-yaml/yaml).

> **What it is — and isn't.** Emitting a PDF (page layout, text placement, vector
> graphics, image embedding) is deterministic and needs **no interpreter**, so it
> lives here as pure Go. Binding `Prawn::Document` to live Ruby objects is the
> host's job; this library hands back a plain, well-formed PDF byte stream.

## Ruby → Go

Prawn's block-form generator

```ruby
Prawn::Document.generate("hello.pdf") do |pdf|
  pdf.text "Hello World", size: 24, style: :bold
  pdf.move_down 20
  pdf.stroke_rectangle [0, pdf.cursor], 200, 100
end
```

becomes

```go
prawn.Generate("hello.pdf", func(pdf *prawn.Document) error {
    pdf.Text("Hello World", &prawn.TextOptions{Size: 24, Style: prawn.StyleBold, StyleSet: true})
    pdf.MoveDown(20)
    pdf.StrokeRectangle(0, pdf.Cursor(), 200, 100)
    return nil
})
```

Ruby keyword-argument hashes become option structs (a `nil` selects prawn's
defaults); Ruby symbols become typed Go constants (`Style*`, `Align*`, page-size
and layout strings). Coordinates are PDF points (1/72 inch) with prawn's origin:
the lower-left corner of the margin box (`bounds`).

## Features

Faithful port of the frequently used prawn DSL, verified by generating documents
and parsing them back:

- **Document & pages** — `New` / `Generate` / `GenerateTo` / `GenerateWith`,
  `Render`, `RenderFile`, `StartNewPage`, `Cursor`, `MoveCursorTo`, `Bounds`,
  `MoveDown` / `MoveUp`, `Pad` / `PadTop` / `PadBottom`, `PageCount`. Page sizes
  (LETTER … A0–A10, B0–B6, C0–C6, custom) and portrait / landscape layout, with
  prawn's 36pt default margins (uniform or per-side).
- **Text** — `Text` (flowing, wrapping, auto-paginating), `DrawText` (absolute),
  `TextBox` (positioned box with `:truncate` / `:expand` / `:error` overflow),
  `Font` / `FontSize` / `Leading`, alignment (left / center / right / justify).
- **Fonts** — the 14 standard AFM fonts (`Font`/`FontSize`/`Leading`, kerning),
  **plus TrueType embedding with subsetting** (`RegisterFontTTF`): glyf/loca/hmtx
  subset following composite references, written as a Type0/CIDFontType2 font with
  Identity-H, a `FontFile2` stream, a `/W` width array and a `/ToUnicode` CMap.
- **Formatted text** — `FormattedText` (styled runs) and `TextInline`
  (`<b>`/`<i>`/`<color>` markup).
- **Color** — `FillColor` / `StrokeColor` (hex `"RRGGBB"`) **and CMYK**
  (`FillColorCMYK` / `StrokeColorCMYK`), emitted via prawn's `cs`/`scn`,
  `CS`/`SCN` operators.
- **Graphics** — `MoveTo`/`LineTo`/`CurveTo`, `Rectangle`, `Line`, `Polygon`,
  `Circle`/`Ellipse`, `Stroke`/`Fill`/`FillAndStroke`/`CloseAndStroke`,
  `StrokeBounds`, `LineWidth`, `CapStyle`/`JoinStyle`, `Dash`/`Undash`.
- **Layout** — `BoundingBox`, the column/row `Grid`, `Repeat` / `NumberPages`,
  and affine transforms `Rotate` / `Scale` / `Translate` (with `origin`),
  `TransformationMatrix`, `SaveGraphicsState`/`RestoreGraphicsState`,
  `Transparency`.
- **Images** — `Image` (file) / `ImageReader` (io) for PNG (**alpha → SMask**) and
  JPEG (DCTDecode passthrough), with `at`, `width`, `height` and `fit`.
- **Table** — a basic grid (`Table`): explicit or automatic column widths,
  optional bold header row, cell padding and borders, positioning.
- **Errors** — the `Prawn::Errors` tree (`ErrCannotFit`, `ErrUnknownFont`,
  `ErrInvalidPageLayout`, `ErrUnsupportedImageType`, `ErrEmptyGraphicStateStack`,
  `ErrIncompatibleStringEncoding`, …).

CGO-free, **100% test coverage**, `gofmt` + `go vet` clean, race-clean, and green
across the six 64-bit Go targets (amd64, arm64, riscv64, loong64, ppc64le,
s390x), two WebAssembly targets and three OSes.

## Differential oracle

The output is validated against the real Ruby `prawn` gem the way prawn validates
itself — with a **`PDF::Inspector`-style parse of the content stream**, not raw
byte-equality (PDFs carry timestamps and ids). For each scenario, the PDF
operators this library emits (`BT`/`Td`/`Tf`/kerned `TJ`, `re`/`l`/`m`/`c`,
`S`/`f`, `cs`/`scn`, `CS`/`SCN`, `cm`, `Do`) are compared operator-for-operator
against the gem's for the same script. The gem's operator streams are captured
into `testdata/oracle/*.content` and committed, so the oracle runs in CI without
Ruby; a skip-gated test re-verifies them against a live gem when one is
installed. See `prawn_diff_test.go` / `prawn_gem_test.go`.

## Install

```sh
go get github.com/go-ruby-prawn/prawn
```

## Usage

```go
package main

import (
	"log"

	"github.com/go-ruby-prawn/prawn"
)

func main() {
	err := prawn.Generate("report.pdf", func(pdf *prawn.Document) error {
		pdf.Font("Times", prawn.StyleBold)
		pdf.Text("Quarterly Report", &prawn.TextOptions{Size: 24, Align: prawn.AlignCenter})
		pdf.MoveDown(12)

		pdf.FillColor("333333")
		pdf.Text("Generated with pure-Go prawn.", nil)

		pdf.Table([][]string{
			{"Item", "Qty", "Price"},
			{"Widget", "3", "9.99"},
			{"Gadget", "1", "19.99"},
		}, prawn.TableOptions{Header: true})

		pdf.StrokeColor("CC0000")
		pdf.StrokeRectangle(0, pdf.Cursor(), pdf.Bounds().Width, 40)
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
}
```

## Determinism

The PDF `/CreationDate` comes from a package clock seam, not the wall clock, so
output is reproducible and timezone independent — pin it with `SetClock`:

```go
defer prawn.SetClock(prawn.SetClock(func() time.Time {
	return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
}))
```

PDF is a byte-oriented format and every dependency is pure Go, so the bytes are
identical on little- and big-endian architectures alike.

## Tests & coverage

The suite is **self-contained**: it generates PDFs in memory and parses them back
with the pure-Go [`rsc.io/pdf`](https://pkg.go.dev/rsc.io/pdf) reader, asserting
that each document is a well-formed PDF (header, xref, trailer, `%%EOF`), that the
expected text appears on the right page with the right font and size, that images
are embedded as image XObjects, and that table cells and vector graphics are
drawn. It also runs the committed-fixture **differential operator oracle** (see
above) comparing the emitted operators to the real prawn gem's. No Ruby runtime
and no network are needed in CI; the live-gem re-verification skips when Ruby is
absent. **100% statement coverage** is enforced.

```sh
COVERPKG=$(go list ./... | paste -sd, -)
go test -race -coverpkg="$COVERPKG" -coverprofile=cover.out ./...
go tool cover -func=cover.out | tail -1   # 100.0%
```

## Scope

Covered faithfully: document + page management, text (flowing / absolute / box),
formatted/inline text, Core-14 AFM fonts **and** subset-embedded TrueType, color
(RGB + CMYK), vector graphics, bounding boxes, grid, transformations,
repeaters/page numbering, PNG/JPEG images and a basic table.

Font embedding also has a **go-opentype-backed backend** (`RegisterFontOTF`)
built on the pure-Go [`github.com/go-opentype/opentype`](https://github.com/go-opentype/opentype)
+ [`.../shape`](https://github.com/go-opentype/shape) stack. On top of the
glyf/TrueType path it adds:

- **CFF/OpenType** (`.otf`, "OTTO") embedding as a CIDFontType0 descendant with
  a FontFile3 (`/Subtype /OpenType`) program;
- an **opt-in shaped-text path** (`OTFOptions{Shape: true}`) so Arabic joining,
  Indic reordering, CJK and Latin ligatures/kerning (GSUB + GPOS) render with
  the correct glyphs and spacing, emitted as a positioned `TJ` array;
- **variable-font instance selection** (`OTFOptions{Variation: {"wght": 700}}`),
  so measured widths and the per-CID `/W` advances follow the chosen axis.

The default Prawn text API (`text`, `formatted_text`, `font`, `width_of`) is
unchanged; shaping and variation are opt-in, so default output stays
Prawn-compatible.

Deferred (named in `doc.go`, not implemented): advanced prawn-table features
(row/column spanning, per-cell style callbacks, automatic table pagination),
GPOS mark-attachment vertical placement in the PDF (substitution + horizontal
advances are applied), glyf/CFF subsetting for the OTF backend (the whole font
program is embedded), CJK/soft-hyphen line breaking, and SVG.

## License

BSD-3-Clause — see [LICENSE](LICENSE). Copyright the go-ruby-prawn/prawn authors.

## WebAssembly

Being pure Go (CGO=0), this library also compiles to **WebAssembly** — both
`GOOS=js GOARCH=wasm` (browser / Node.js) and `GOOS=wasip1 GOARCH=wasm` (WASI).
CI builds both targets on every push, alongside the six 64-bit native/qemu arches.

```sh
GOOS=js     GOARCH=wasm go build ./...   # browser / Node
GOOS=wasip1 GOARCH=wasm go build ./...   # WASI (wasmtime, wasmer, wasmedge, …)
```
