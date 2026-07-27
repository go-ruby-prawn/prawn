// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-ruby-prawn/prawn authors

package prawn

// This file is the differential oracle. For each scenario, the same drawing is
// produced by (a) this library and (b) the real Ruby prawn gem, and their PDF
// content-stream operators are compared structurally (PDF::Inspector-style),
// rather than by raw byte equality (PDFs carry timestamps and ids).
//
// The gem's output is captured once into testdata/oracle/<name>.content (the
// page-1 raw content stream) and committed, so the oracle runs in CI without a
// Ruby runtime. prawn_gem_test.go re-verifies those fixtures against a live gem
// when one is installed, and skips otherwise.

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// oracleScenarios builds each named document exactly as the matching Ruby script
// in testdata generation does.
var oracleScenarios = map[string]func(*Document){
	"text_basic": func(d *Document) {
		d.Text("Hello World", &TextOptions{Size: 24, Style: StyleBold, StyleSet: true})
		d.Text("A quick brown fox.", nil)
	},
	"text_align": func(d *Document) {
		d.Text("Centered", &TextOptions{Align: AlignCenter})
		d.Text("Right", &TextOptions{Align: AlignRight})
	},
	"colors": func(d *Document) {
		d.FillColor("ff0000")
		d.StrokeColor("0000ff")
		d.LineWidth(2)
		d.StrokeRectangle(0, d.Cursor(), 200, 100)
		d.FillColorCMYK(0, 100, 0, 0)
		d.FillRectangle(0, d.Cursor()-120, 150, 60)
	},
	"graphics": func(d *Document) {
		d.StrokeLine(0, 0, 100, 100)
		d.StrokeRectangle(10, 700, 80, 40)
		d.FillCircle(200, 400, 50)
		d.StrokeEllipse(300, 300, 40, 20)
	},
	"transform": func(d *Document) {
		d.RotateAbout(30, 100, 100, func() { d.Text("rot", nil) })
		d.ScaleAbout(2, 50, 50, func() { d.Text("big", nil) })
		d.Translate(10, 20, func() { d.Text("moved", nil) })
	},
	"draw_text": func(d *Document) {
		d.DrawText("At point", 72, 600, &TextOptions{Size: 14})
		d.DrawText("Another", 72, 500, nil)
	},
}

func TestOracleDifferential(t *testing.T) {
	for name, build := range oracleScenarios {
		t.Run(name, func(t *testing.T) {
			golden, err := os.ReadFile(filepath.Join("testdata", "oracle", name+".content"))
			if err != nil {
				t.Fatalf("read golden: %v", err)
			}
			gemOps := tokenizeContent(golden)

			d := New(Options{})
			build(d)
			if _, err := d.Render(); err != nil {
				t.Fatalf("render: %v", err)
			}
			ours := d.pageOps(0)

			if ok, why := opsEqual(ours, gemOps); !ok {
				t.Errorf("scenario %s: operators differ from prawn gem: %s\n--- ours ---\n%s\n--- gem ---\n%s",
					name, why, dumpOps(ours), dumpOps(gemOps))
			}
		})
	}
}

func dumpOps(ops []pdfOp) string {
	var s string
	for _, o := range ops {
		s += o.String() + "\n"
	}
	return s
}

// ensure the determinism seam is exercised alongside the oracle.
func init() { _ = time.Now }
