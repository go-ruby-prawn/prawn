// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-ruby-prawn/prawn authors

package prawn

// This file is the native, pure-Go PDF object model and file serializer that
// backs the package. It replaces the previous third-party PDF generator so the
// library emits the *same* content-stream operators as Ruby prawn / PDF::Core
// (BT/ET, Td, Tf, TJ, re/l/m/c, S/f, cs/scn, CS/SCN, cm, Do, gs …), which is
// what the differential PDF::Inspector-style oracle compares. Nothing here uses
// cgo; the format is byte-oriented so output is identical on every architecture.

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
)

// pdfRef is an indirect reference to object number id (generation 0).
type pdfRef struct{ id int }

// pdfName is a PDF name object (serialized as /Name).
type pdfName string

// pdfLiteral is a PDF literal string, serialized as (…) with escaping.
type pdfLiteral string

// pdfHex is a PDF hexadecimal string, serialized as <…>.
type pdfHex []byte

// pdfDict is a PDF dictionary. Keys are serialized as names, sorted for
// determinism (mirroring PDF::Core, which sorts dictionary keys).
type pdfDict map[string]any

// pdfArray is a PDF array.
type pdfArray []any

// pdfStream is an indirect stream object: a dictionary plus raw bytes. The
// /Length entry is filled in automatically at write time.
type pdfStream struct {
	dict pdfDict
	data []byte
}

// pdfDoc collects indirect objects and serializes the whole file with a
// cross-reference table and trailer.
type pdfDoc struct {
	objs    []any  // 1-based: objs[id-1] is object id
	catalog int    // object id of the /Catalog
	info    int    // object id of the /Info dictionary
	idBytes []byte // the /ID file identifier (deterministic)
}

// alloc reserves the next object id without storing a value yet.
func (d *pdfDoc) alloc() int {
	d.objs = append(d.objs, nil)
	return len(d.objs)
}

// set stores v as the body of object id (id must come from alloc).
func (d *pdfDoc) set(id int, v any) { d.objs[id-1] = v }

// add stores v as a fresh indirect object and returns its reference.
func (d *pdfDoc) add(v any) pdfRef {
	id := d.alloc()
	d.set(id, v)
	return pdfRef{id}
}

// serialize writes a single (possibly nested) object value to buf, in content
// stream / object syntax. in_content is unused for the container types here but
// kept for parity with PDF::Core's pdf_object signature intent.
func serializeObject(buf *bytes.Buffer, v any) {
	switch x := v.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		if x {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case int:
		buf.WriteString(strconv.Itoa(x))
	case float64:
		buf.WriteString(formatReal(x))
	case pdfName:
		writeName(buf, string(x))
	case pdfRef:
		buf.WriteString(strconv.Itoa(x.id))
		buf.WriteString(" 0 R")
	case pdfLiteral:
		buf.WriteByte('(')
		buf.WriteString(escapeLiteral(string(x)))
		buf.WriteByte(')')
	case pdfHex:
		buf.WriteByte('<')
		buf.WriteString(hexEncode(x))
		buf.WriteByte('>')
	case pdfArray:
		buf.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				buf.WriteByte(' ')
			}
			serializeObject(buf, e)
		}
		buf.WriteByte(']')
	case pdfDict:
		serializeDict(buf, x)
	default:
		panic(fmt.Sprintf("prawn: cannot serialize %T", v))
	}
}

// serializeDict writes a dictionary with keys sorted alphabetically.
func serializeDict(buf *bytes.Buffer, d pdfDict) {
	keys := make([]string, 0, len(d))
	for k := range d {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	buf.WriteString("<< ")
	for _, k := range keys {
		writeName(buf, k)
		buf.WriteByte(' ')
		serializeObject(buf, d[k])
		buf.WriteByte('\n')
	}
	buf.WriteString(">>")
}

// writeName serializes a PDF name, escaping characters PDF requires as #xx.
func writeName(buf *bytes.Buffer, s string) {
	buf.WriteByte('/')
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 33 || c > 126 || c == '#' || c == '(' || c == ')' || c == '/' ||
			c == '<' || c == '>' || c == '[' || c == ']' || c == '{' || c == '}' || c == '%' {
			buf.WriteByte('#')
			buf.WriteString(fmt.Sprintf("%02X", c))
		} else {
			buf.WriteByte(c)
		}
	}
}

// escapeLiteral escapes a PDF literal string body.
func escapeLiteral(s string) string {
	var b bytes.Buffer
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '\\':
			b.WriteString(`\\`)
		case '(':
			b.WriteString(`\(`)
		case ')':
			b.WriteString(`\)`)
		case '\r':
			b.WriteString(`\r`)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

const hexDigits = "0123456789abcdef"

// hexEncode returns the lowercase hex representation of b.
func hexEncode(b []byte) string {
	out := make([]byte, len(b)*2)
	for i, c := range b {
		out[i*2] = hexDigits[c>>4]
		out[i*2+1] = hexDigits[c&0x0f]
	}
	return string(out)
}

// formatReal serializes a float like PDF::Core.real: "%.5f" with trailing
// zeros stripped (keeping at least one digit after the point removed entirely
// when whole, e.g. 2.0 -> "2"). The oracle parses numbers back, so exact string
// form is not load-bearing; this keeps output compact and valid.
func formatReal(f float64) string {
	s := strconv.FormatFloat(f, 'f', 5, 64)
	// strip trailing zeros
	if bytes.ContainsRune([]byte(s), '.') {
		i := len(s)
		for i > 0 && s[i-1] == '0' {
			i--
		}
		if i > 0 && s[i-1] == '.' {
			i--
		}
		s = s[:i]
	}
	if s == "-0" {
		s = "0"
	}
	return s
}

// bytesOf returns the fully serialized PDF file.
func (d *pdfDoc) bytesOf() []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	// binary marker comment so tools treat the file as binary
	buf.WriteString("%\xe2\xe3\xcf\xd3\n")

	offsets := make([]int, len(d.objs)+1)
	for i, o := range d.objs {
		id := i + 1
		offsets[id] = buf.Len()
		buf.WriteString(strconv.Itoa(id))
		buf.WriteString(" 0 obj\n")
		switch s := o.(type) {
		case *pdfStream:
			dict := pdfDict{}
			for k, v := range s.dict {
				dict[k] = v
			}
			dict["Length"] = len(s.data)
			serializeDict(&buf, dict)
			buf.WriteString("\nstream\n")
			buf.Write(s.data)
			buf.WriteString("\nendstream")
		default:
			serializeObject(&buf, o)
		}
		buf.WriteString("\nendobj\n")
	}

	xrefOff := buf.Len()
	n := len(d.objs) + 1
	buf.WriteString("xref\n")
	buf.WriteString("0 ")
	buf.WriteString(strconv.Itoa(n))
	buf.WriteByte('\n')
	buf.WriteString("0000000000 65535 f \n")
	for id := 1; id < n; id++ {
		buf.WriteString(fmt.Sprintf("%010d 00000 n \n", offsets[id]))
	}
	buf.WriteString("trailer\n")
	trailer := pdfDict{
		"Size": n,
		"Root": pdfRef{d.catalog},
	}
	if d.info != 0 {
		trailer["Info"] = pdfRef{d.info}
	}
	if d.idBytes != nil {
		trailer["ID"] = pdfArray{pdfHex(d.idBytes), pdfHex(d.idBytes)}
	}
	serializeDict(&buf, trailer)
	buf.WriteString("\nstartxref\n")
	buf.WriteString(strconv.Itoa(xrefOff))
	buf.WriteString("\n%%EOF\n")
	return buf.Bytes()
}
