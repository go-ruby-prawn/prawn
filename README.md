<p align="center"><img src="https://raw.githubusercontent.com/go-ruby-prawn/brand/main/social/go-ruby-prawn-prawn.png" alt="go-ruby-prawn/prawn" width="720"></p>

# prawn — go-ruby-prawn

[![Docs](https://img.shields.io/badge/docs-mkdocs--material-DC2626)](https://go-ruby-prawn.github.io/docs/)
[![License](https://img.shields.io/badge/license-BSD--3--Clause-blue)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.26.4%2B-00ADD8)](https://go.dev/dl/)
[![Coverage](https://img.shields.io/badge/coverage-100%25-1a7f37)](#tests--coverage)

**A pure-Go (no cgo), MRI-faithful reimplementation of Ruby's
[`prawn`](https://github.com/prawnpdf/prawn) PDF-generation gem.** It mirrors
prawn's document DSL — text, fonts, colors, vector graphics, images, a basic
table and full page management, plus the `Prawn::Errors` tree — while writing the
actual PDF with the maintained pure-Go
[`github.com/go-pdf/fpdf`](https://github.com/go-pdf/fpdf) generator. No C PDF
library, no Ruby runtime: the whole module builds and runs with **CGO disabled**
on every supported 64-bit target.

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
- **Color** — `FillColor` / `StrokeColor` (hex `"RRGGBB"`), applied to text and
  shapes.
- **Graphics** — `Rectangle` / `FillRectangle` / `StrokeRectangle`,
  `Line` / `StrokeLine`, `Circle` / `FillCircle` / `StrokeCircle`,
  `Ellipse` / `FillEllipse` / `StrokeEllipse`, `Stroke` / `Fill` /
  `FillAndStroke`, `StrokeBounds`, `LineWidth`.
- **Images** — `Image` (file) / `ImageReader` (io) for PNG and JPEG, with
  `at`, `width`, `height` and aspect-preserving `fit`.
- **Table** — a basic grid (`Table`): explicit or automatic column widths,
  optional bold header row, cell padding and borders, positioning.
- **Errors** — the `Prawn::Errors` tree (`ErrCannotFit`, `ErrUnknownFont`,
  `ErrInvalidPageLayout`, `ErrUnsupportedImageType`,
  `ErrIncompatibleStringEncoding`, …).

CGO-free, **100% test coverage**, `gofmt` + `go vet` clean, race-clean, and green
across the six 64-bit Go targets (amd64, arm64, riscv64, loong64, ppc64le,
s390x) and three OSes.

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
drawn. No Ruby runtime and no network are needed.

```sh
COVERPKG=$(go list ./... | paste -sd, -)
go test -race -coverpkg="$COVERPKG" -coverprofile=cover.out ./...
go tool cover -func=cover.out | tail -1   # 100.0%
```

## Scope

Covered faithfully: document + page management, text (flowing / absolute / box),
fonts (the 14 standard PDF fonts), color, vector graphics, PNG/JPEG images and a
basic table.

Out of scope (documented in `doc.go`, not implemented): prawn's inline
formatted-text array and `<b>/<i>` inline tags, full multi-column flowing text,
advanced prawn-table features (row/column spanning, per-cell style callbacks,
automatic table pagination), embedded TTF/OTF fonts, and SVG.

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
