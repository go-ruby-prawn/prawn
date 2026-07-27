// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-ruby-prawn/prawn authors

package prawn

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"image/png"
	"io"
	"os"
	"strings"
)

// imageXObject is a decoded image ready to be written as a PDF image XObject.
type imageXObject struct {
	resName       string
	width, height int
	colorSpace    pdfName
	bpc           int
	filter        pdfName
	data          []byte
	smask         *imageXObject // optional soft mask (PNG alpha)
}

// ImageOptions mirror the keyword arguments of Prawn::Document#image
// (:at, :width, :height, :fit).
type ImageOptions struct {
	AtX, AtY   float64
	AtSet      bool
	Width      float64
	Height     float64
	FitW, FitH float64
}

// ImageResult reports the placed geometry of an embedded image.
type ImageResult struct {
	Width, Height float64
}

// Image mirrors Prawn::Document#image(path, options): embeds a PNG or JPEG.
func (d *Document) Image(path string, o ImageOptions) ImageResult {
	f, err := os.Open(path)
	if err != nil {
		d.fail(err)
		return ImageResult{}
	}
	defer func() { _ = f.Close() }()
	return d.imageReader(f, imageTypeFromName(path), path, o)
}

// ImageReader mirrors Prawn::Document#image(io, options): embeds an image from a
// reader. tp is "png", "jpg" or "jpeg".
func (d *Document) ImageReader(r io.Reader, tp string, o ImageOptions) ImageResult {
	return d.imageReader(r, strings.ToLower(tp), "", o)
}

func imageTypeFromName(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".png"):
		return "png"
	case strings.HasSuffix(lower, ".jpg"), strings.HasSuffix(lower, ".jpeg"):
		return "jpg"
	default:
		return ""
	}
}

func (d *Document) imageReader(r io.Reader, tp, name string, o ImageOptions) ImageResult {
	data, err := io.ReadAll(r)
	if err != nil {
		d.fail(err)
		return ImageResult{}
	}
	key := name
	if key == "" {
		key = fmt.Sprintf("%s-%x", tp, len(data))
	}
	im, ok := d.images[key]
	if !ok {
		var derr error
		switch tp {
		case "jpg", "jpeg":
			im, derr = decodeJPEG(data)
		case "png":
			im, derr = decodePNG(data)
		default:
			d.fail(fmt.Errorf("%w: %q", ErrUnsupportedImageType, tp))
			return ImageResult{}
		}
		if derr != nil {
			d.fail(derr)
			return ImageResult{}
		}
		d.imgOrder++
		im.resName = "I" + itoa(d.imgOrder)
		if im.smask != nil {
			d.imgOrder++
			im.smask.resName = "I" + itoa(d.imgOrder)
		}
		d.images[key] = im
	}

	w, h := resolveImageSize(float64(im.width), float64(im.height), o)
	var x, y float64
	if o.AtSet {
		x, y = o.AtX, o.AtY
	} else {
		x, y = 0, d.cursor
		d.cursor -= h
	}
	d.drawImage(im, x, y, w, h)
	return ImageResult{Width: w, Height: h}
}

// drawImage emits "q  w 0 0 h x y  cm  /In Do  Q" placing the image with its
// top-left corner at the bounds-relative point (x, y), as prawn does.
func (d *Document) drawImage(im *imageXObject, x, y, w, h float64) {
	d.cur.xobjsUsed[im.resName] = true
	ax, ay := d.mapToAbs(x, y)
	c := d.cur.content
	c.raw("q")
	c.raw(realParams(w, 0, 0, h, ax, ay-h) + " cm")
	c.raw("/" + im.resName + " Do")
	c.raw("Q")
}

func resolveImageSize(natW, natH float64, o ImageOptions) (w, h float64) {
	switch {
	case o.FitW > 0 && o.FitH > 0:
		scale := o.FitW / natW
		if s := o.FitH / natH; s < scale {
			scale = s
		}
		return natW * scale, natH * scale
	case o.Width > 0 && o.Height > 0:
		return o.Width, o.Height
	case o.Width > 0:
		return o.Width, natH * (o.Width / natW)
	case o.Height > 0:
		return natW * (o.Height / natH), o.Height
	default:
		return natW, natH
	}
}

// buildImageObject writes the image XObject (and its soft mask, if any).
func (d *Document) buildImageObject(doc *pdfDoc, im *imageXObject) pdfRef {
	dict := pdfDict{
		"Type":             pdfName("XObject"),
		"Subtype":          pdfName("Image"),
		"Width":            im.width,
		"Height":           im.height,
		"ColorSpace":       im.colorSpace,
		"BitsPerComponent": im.bpc,
		"Filter":           im.filter,
	}
	if im.smask != nil {
		dict["SMask"] = d.buildImageObject(doc, im.smask)
	}
	return doc.add(&pdfStream{dict: dict, data: im.data})
}

// decodeJPEG parses a JPEG's frame header for dimensions and component count and
// embeds the raw JPEG bytes with the DCTDecode filter (prawn's approach).
func decodeJPEG(data []byte) (*imageXObject, error) {
	w, h, comps, err := jpegInfo(data)
	if err != nil {
		return nil, err
	}
	cs := pdfName("DeviceRGB")
	switch comps {
	case 1:
		cs = "DeviceGray"
	case 4:
		cs = "DeviceCMYK"
	}
	return &imageXObject{
		width: w, height: h, colorSpace: cs, bpc: 8,
		filter: "DCTDecode", data: data,
	}, nil
}

// jpegInfo scans JPEG markers for the SOF (start-of-frame) segment.
func jpegInfo(data []byte) (w, h, comps int, err error) {
	if len(data) < 2 || data[0] != 0xFF || data[1] != 0xD8 {
		return 0, 0, 0, fmt.Errorf("%w: not a JPEG", ErrUnsupportedImageType)
	}
	i := 2
	for i+9 < len(data) {
		if data[i] != 0xFF {
			i++
			continue
		}
		marker := data[i+1]
		// SOF0..SOF15 except DHT(C4), DAC(CC), RSTn hold the frame header.
		if (marker >= 0xC0 && marker <= 0xCF) && marker != 0xC4 && marker != 0xC8 && marker != 0xCC {
			h = int(binary.BigEndian.Uint16(data[i+5 : i+7]))
			w = int(binary.BigEndian.Uint16(data[i+7 : i+9]))
			comps = int(data[i+9])
			return w, h, comps, nil
		}
		if marker == 0xD8 || marker == 0xD9 || (marker >= 0xD0 && marker <= 0xD7) {
			i += 2
			continue
		}
		seg := int(binary.BigEndian.Uint16(data[i+2 : i+4]))
		i += 2 + seg
	}
	return 0, 0, 0, fmt.Errorf("%w: no JPEG frame header", ErrUnsupportedImageType)
}

// decodePNG decodes a PNG and re-encodes it as a FlateDecode RGB image, moving
// any alpha channel into a DeviceGray soft mask (PDF /SMask), matching how
// prawn embeds an RGBA PNG.
func decodePNG(data []byte) (*imageXObject, error) {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	rgb := make([]byte, 0, w*h*3)
	alpha := make([]byte, 0, w*h)
	hasAlpha := false
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := img.At(x, y).RGBA()
			rgb = append(rgb, byte(r>>8), byte(g>>8), byte(bl>>8))
			av := byte(a >> 8)
			alpha = append(alpha, av)
			if av != 0xFF {
				hasAlpha = true
			}
		}
	}
	im := &imageXObject{
		width: w, height: h, colorSpace: "DeviceRGB", bpc: 8,
		filter: "FlateDecode", data: deflate(rgb),
	}
	if hasAlpha {
		im.smask = &imageXObject{
			width: w, height: h, colorSpace: "DeviceGray", bpc: 8,
			filter: "FlateDecode", data: deflate(alpha),
		}
	}
	return im, nil
}

// deflate zlib-compresses b (the FlateDecode filter prawn uses for PNG data).
func deflate(b []byte) []byte {
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	_, _ = zw.Write(b)
	_ = zw.Close()
	return buf.Bytes()
}
