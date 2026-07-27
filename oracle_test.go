// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-ruby-prawn/prawn authors

package prawn

// This file holds the PDF::Inspector-style operator oracle: a content-stream
// tokenizer that turns a page's operators into a normalized, comparable form,
// used both by structural assertions and by the differential tests against the
// real Ruby prawn gem (see prawn_diff_test.go).

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// pdfOp is one content-stream operator with its operands rendered as normalized
// tokens (numbers rounded, strings decoded to their bytes as hex, arrays kept).
type pdfOp struct {
	Name string
	Args []string
}

func (o pdfOp) String() string { return strings.Join(append(o.Args, o.Name), " ") }

// tokenizeContent parses a PDF content stream into a slice of operators. It
// understands numbers, names, literal/hex strings and arrays — the token classes
// prawn emits — which is all the oracle needs.
func tokenizeContent(data []byte) []pdfOp {
	var ops []pdfOp
	var stack []string
	i := 0
	n := len(data)
	for i < n {
		c := data[i]
		switch {
		case c == ' ' || c == '\n' || c == '\r' || c == '\t':
			i++
		case c == '[':
			// read a raw array token verbatim (normalized numbers/hex inside).
			depth := 1
			j := i + 1
			for j < n && depth > 0 {
				if data[j] == '[' {
					depth++
				} else if data[j] == ']' {
					depth--
				}
				j++
			}
			stack = append(stack, normalizeArray(string(data[i:j])))
			i = j
		case c == '<':
			j := i + 1
			for j < n && data[j] != '>' {
				j++
			}
			stack = append(stack, "<"+strings.ToLower(strings.TrimSpace(string(data[i+1:j])))+">")
			i = j + 1
		case c == '(':
			depth := 1
			j := i + 1
			var b strings.Builder
			for j < n && depth > 0 {
				if data[j] == '\\' && j+1 < n {
					b.WriteByte(data[j+1])
					j += 2
					continue
				}
				if data[j] == '(' {
					depth++
				} else if data[j] == ')' {
					depth--
					if depth == 0 {
						break
					}
				}
				b.WriteByte(data[j])
				j++
			}
			stack = append(stack, "("+b.String()+")")
			i = j + 1
		case c == '/':
			j := i + 1
			for j < n && !isDelim(data[j]) {
				j++
			}
			stack = append(stack, string(data[i:j]))
			i = j
		case (c >= '0' && c <= '9') || c == '-' || c == '+' || c == '.':
			j := i
			for j < n && !isDelim(data[j]) {
				j++
			}
			stack = append(stack, normalizeNumber(string(data[i:j])))
			i = j
		default:
			j := i
			for j < n && !isDelim(data[j]) {
				j++
			}
			name := string(data[i:j])
			ops = append(ops, pdfOp{Name: name, Args: stack})
			stack = nil
			i = j
		}
	}
	return ops
}

func isDelim(c byte) bool {
	switch c {
	case ' ', '\n', '\r', '\t', '[', ']', '<', '>', '(', ')', '/', '%':
		return true
	}
	return false
}

// normalizeNumber rounds a numeric token to 3 decimals so tiny float differences
// between our output and the gem's do not cause spurious mismatches.
func normalizeNumber(s string) string {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return s
	}
	return strconv.FormatFloat(round3(f), 'f', -1, 64)
}

func round3(f float64) float64 { return math.Round(f*1000) / 1000 }

// normalizeArray re-tokenizes a "[ … ]" array, normalizing numbers and hex
// strings inside so the TJ operand compares cleanly.
func normalizeArray(s string) string {
	inner := strings.TrimSpace(s[1 : len(s)-1])
	var out []string
	i := 0
	for i < len(inner) {
		c := inner[i]
		switch {
		case c == ' ' || c == '\n' || c == '\r' || c == '\t':
			i++
		case c == '<':
			j := i + 1
			for j < len(inner) && inner[j] != '>' {
				j++
			}
			out = append(out, "<"+strings.ToLower(strings.TrimSpace(inner[i+1:j]))+">")
			i = j + 1
		default:
			j := i
			for j < len(inner) && inner[j] != ' ' && inner[j] != '<' {
				j++
			}
			out = append(out, normalizeNumber(inner[i:j]))
			i = j
		}
	}
	return "[" + strings.Join(out, " ") + "]"
}

// pageOps tokenizes a page's content stream (white-box helper for tests).
func (d *Document) pageOps(i int) []pdfOp {
	return tokenizeContent(d.pages[i].content.bytes())
}

// opNames returns just the operator names in order (structural signature).
func opNames(ops []pdfOp) []string {
	names := make([]string, len(ops))
	for i, o := range ops {
		names[i] = o.Name
	}
	return names
}

// opsEqual compares two operator slices for equality (name + normalized args).
func opsEqual(a, b []pdfOp) (bool, string) {
	if len(a) != len(b) {
		return false, fmt.Sprintf("operator count %d != %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Name != b[i].Name {
			return false, fmt.Sprintf("op %d: name %q != %q", i, a[i].Name, b[i].Name)
		}
		if len(a[i].Args) != len(b[i].Args) {
			return false, fmt.Sprintf("op %d (%s): arg count %d != %d", i, a[i].Name, len(a[i].Args), len(b[i].Args))
		}
		for k := range a[i].Args {
			if a[i].Args[k] != b[i].Args[k] {
				return false, fmt.Sprintf("op %d (%s) arg %d: %q != %q", i, a[i].Name, k, a[i].Args[k], b[i].Args[k])
			}
		}
	}
	return true, ""
}
