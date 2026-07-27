// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-ruby-prawn/prawn authors

package prawn

// This test re-verifies the committed oracle fixtures against a *live* Ruby
// prawn gem. It is skip-gated: when no Ruby runtime with prawn+pdf-reader is
// available (as on CI), it skips, leaving the committed-fixture differential in
// prawn_diff_test.go as the operator oracle.

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// rubyGenScript regenerates one scenario's page-1 content stream via the gem.
const rubyGenScript = `
require 'prawn'; require 'pdf/reader'; require 'stringio'
name = ARGV[0]
pdf = Prawn::Document.new
case name
when 'text_basic'
  pdf.text "Hello World", size: 24, style: :bold
  pdf.text "A quick brown fox."
when 'text_align'
  pdf.text "Centered", align: :center
  pdf.text "Right", align: :right
when 'colors'
  pdf.fill_color "ff0000"; pdf.stroke_color "0000ff"; pdf.line_width 2
  pdf.stroke_rectangle [0, pdf.cursor], 200, 100
  pdf.fill_color 0, 100, 0, 0
  pdf.fill_rectangle [0, pdf.cursor - 120], 150, 60
when 'graphics'
  pdf.stroke_line [0,0],[100,100]
  pdf.stroke_rectangle [10,700],80,40
  pdf.fill_circle [200,400],50
  pdf.stroke_ellipse [300,300],40,20
when 'transform'
  pdf.rotate(30, origin:[100,100]){ pdf.text "rot" }
  pdf.scale(2, origin:[50,50]){ pdf.text "big" }
  pdf.translate(10,20){ pdf.text "moved" }
when 'draw_text'
  pdf.draw_text "At point", at:[72,600], size:14
  pdf.draw_text "Another", at:[72,500]
end
data = pdf.render
r = PDF::Reader.new(StringIO.new(data))
$stdout.binmode; $stdout.write r.pages.first.raw_content
`

func gemAvailable(t *testing.T) bool {
	if _, err := exec.LookPath("ruby"); err != nil {
		return false
	}
	cmd := exec.Command("ruby", "-e", "require 'prawn'; require 'pdf/reader'")
	cmd.Env = os.Environ()
	return cmd.Run() == nil
}

func TestLiveGemFixturesCurrent(t *testing.T) {
	if !gemAvailable(t) {
		t.Skip("ruby prawn+pdf-reader not available; committed fixtures serve as the oracle")
	}
	for name := range oracleScenarios {
		t.Run(name, func(t *testing.T) {
			cmd := exec.Command("ruby", "-e", rubyGenScript, name)
			cmd.Env = os.Environ()
			var out, errb bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &errb
			if err := cmd.Run(); err != nil {
				t.Fatalf("ruby: %v\n%s", err, errb.String())
			}
			golden, err := os.ReadFile(filepath.Join("testdata", "oracle", name+".content"))
			if err != nil {
				t.Fatalf("read golden: %v", err)
			}
			liveOps := tokenizeContent(out.Bytes())
			goldOps := tokenizeContent(golden)
			if ok, why := opsEqual(liveOps, goldOps); !ok {
				t.Errorf("committed fixture %s is stale vs live gem: %s", name, why)
			}
		})
	}
}
