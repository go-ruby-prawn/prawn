// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-ruby-prawn/prawn authors

package prawn

// content is a page content-stream builder. Its methods emit exactly the PDF
// graphics operators Ruby prawn / PDF::Core emit, so a PDF::Inspector-style
// parse of the output yields the same operator tree.

import (
	"bytes"
	"strconv"
)

type content struct {
	buf bytes.Buffer
}

// op writes a line "a b c OP\n", formatting each numeric argument like
// PDF::Core.real and passing strings/names through verbatim.
func (c *content) op(args ...any) {
	for i, a := range args {
		if i > 0 {
			c.buf.WriteByte(' ')
		}
		switch v := a.(type) {
		case float64:
			c.buf.WriteString(formatReal(v))
		case int:
			c.buf.WriteString(strconv.Itoa(v))
		case string:
			c.buf.WriteString(v)
		default:
			panic("prawn: bad content operand")
		}
	}
	c.buf.WriteByte('\n')
}

// raw appends a pre-built line plus a newline.
func (c *content) raw(s string) {
	c.buf.WriteString(s)
	c.buf.WriteByte('\n')
}

// bytes returns the accumulated content stream.
func (c *content) bytes() []byte { return c.buf.Bytes() }

// realParams formats a slice of numbers space-joined like PDF::Core.real_params.
func realParams(nums ...float64) string {
	var b bytes.Buffer
	for i, n := range nums {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(formatReal(n))
	}
	return b.String()
}
