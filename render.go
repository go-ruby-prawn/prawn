// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-ruby-prawn/prawn authors

package prawn

import (
	"bytes"
	"compress/zlib"
	"crypto/md5"
	"fmt"
	"sort"
)

// Render mirrors Prawn::Document#render: it finalizes the document (running any
// repeaters / page numbering, balancing each page's graphics state) and returns
// the finished PDF as bytes. Any accumulated build error is returned here.
func (d *Document) Render() ([]byte, error) {
	if d.err != nil {
		return nil, d.err
	}
	if !d.finalized {
		d.runRepeaters()
		for _, p := range d.pages {
			d.finishPage(p)
		}
		d.finalized = true
	}
	if d.err != nil {
		return nil, d.err
	}
	return d.build(), nil
}

// build serializes the whole PDF object graph.
func (d *Document) build() []byte {
	doc := &pdfDoc{}
	doc.catalog = doc.alloc()
	pagesID := doc.alloc()

	// Which font resource names are used anywhere?
	usedFonts := map[string]bool{}
	usedXObjs := map[string]bool{}
	usedGS := map[string]bool{}
	for _, p := range d.pages {
		for n := range p.fontsUsed {
			usedFonts[n] = true
		}
		for n := range p.xobjsUsed {
			usedXObjs[n] = true
		}
		for n := range p.gsUsed {
			usedGS[n] = true
		}
	}

	// Build font objects (only those actually used).
	fontRefByName := map[string]pdfRef{}
	for _, fr := range d.fonts {
		if !usedFonts[fr.resName] {
			continue
		}
		fontRefByName[fr.resName] = d.buildFontObject(doc, fr)
	}

	// Build image XObjects (only those used).
	imgRefByName := map[string]pdfRef{}
	for _, im := range d.images {
		if !usedXObjs[im.resName] {
			continue
		}
		imgRefByName[im.resName] = d.buildImageObject(doc, im)
	}

	// Build ExtGState objects (only those used).
	gsRefByName := map[string]pdfRef{}
	for _, gs := range d.gstates {
		if !usedGS[gs.resName] {
			continue
		}
		gsRefByName[gs.resName] = doc.add(pdfDict{
			"Type": pdfName("ExtGState"),
			"ca":   gs.fill,
			"CA":   gs.stroke,
		})
	}

	pageRefs := make([]any, 0, len(d.pages))
	for _, p := range d.pages {
		data := p.content.bytes()
		streamDict := pdfDict{}
		if d.compress {
			var zb bytes.Buffer
			zw := zlib.NewWriter(&zb)
			_, _ = zw.Write(data)
			_ = zw.Close()
			data = zb.Bytes()
			streamDict["Filter"] = pdfName("FlateDecode")
		}
		contentRef := doc.add(&pdfStream{dict: streamDict, data: data})

		res := pdfDict{
			"ProcSet": pdfArray{pdfName("PDF"), pdfName("Text"),
				pdfName("ImageB"), pdfName("ImageC"), pdfName("ImageI")},
		}
		if fd := resourceDict(p.fontsUsed, fontRefByName); fd != nil {
			res["Font"] = fd
		}
		if xd := resourceDict(p.xobjsUsed, imgRefByName); xd != nil {
			res["XObject"] = xd
		}
		if gd := resourceDict(p.gsUsed, gsRefByName); gd != nil {
			res["ExtGState"] = gd
		}

		pageID := doc.add(pdfDict{
			"Type":      pdfName("Page"),
			"Parent":    pdfRef{pagesID},
			"MediaBox":  pdfArray{0, 0, p.width, p.height},
			"Contents":  contentRef,
			"Resources": res,
		})
		pageRefs = append(pageRefs, pageID)
	}

	doc.set(pagesID, pdfDict{
		"Type":  pdfName("Pages"),
		"Count": len(d.pages),
		"Kids":  pdfArray(pageRefs),
	})
	doc.set(doc.catalog, pdfDict{
		"Type":  pdfName("Catalog"),
		"Pages": pdfRef{pagesID},
	})

	// Info dictionary with a deterministic creation date.
	created := clock()
	doc.info = doc.add(pdfDict{
		"Producer":     pdfLiteral("go-ruby-prawn/prawn (pure-Go)"),
		"Creator":      pdfLiteral("go-ruby-prawn/prawn"),
		"CreationDate": pdfLiteral(pdfDate(created)),
	}).id

	// A deterministic file identifier derived from the content.
	sum := md5.Sum([]byte(fmt.Sprintf("%d-%d", len(d.pages), created.UnixNano())))
	doc.idBytes = sum[:]

	return doc.bytesOf()
}

// resourceDict builds a name->ref sub-dictionary for the resources used on a
// page, or nil when none are used.
func resourceDict(used map[string]bool, refs map[string]pdfRef) pdfDict {
	if len(used) == 0 {
		return nil
	}
	names := make([]string, 0, len(used))
	for n := range used {
		names = append(names, n)
	}
	sort.Strings(names)
	dict := pdfDict{}
	for _, n := range names {
		if r, ok := refs[n]; ok {
			dict[n] = r
		}
	}
	if len(dict) == 0 {
		return nil
	}
	return dict
}

// buildFontObject creates the PDF font dictionary for a registered font.
func (d *Document) buildFontObject(doc *pdfDoc, fr *fontRef) pdfRef {
	if fr.afm != nil {
		dict := pdfDict{
			"Type":     pdfName("Font"),
			"Subtype":  pdfName("Type1"),
			"BaseFont": pdfName(fr.afm.Name),
		}
		if !fr.afm.Symbolic {
			dict["Encoding"] = pdfName("WinAnsiEncoding")
		}
		return doc.add(dict)
	}
	return fr.ttf.buildObjects(doc)
}

// pdfDate formats a time as a PDF date string, matching prawn's
// "D:%Y%m%d%H%M%S%z" with the trailing offset written as +HH'MM'.
func pdfDate(t interface{ Format(string) string }) string {
	// D:YYYYMMDDHHmmSS+HH'mm'
	s := t.Format("D:20060102150405-0700")
	// turn "-0700" tail into "-07'00'"
	if len(s) >= 5 {
		tz := s[len(s)-5:]
		s = s[:len(s)-5] + tz[:3] + "'" + tz[3:] + "'"
	}
	return s
}
