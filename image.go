// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-ruby-prawn/prawn authors

package prawn

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/go-pdf/fpdf"
)

func imgOpts(tp string) fpdf.ImageOptions {
	if tp == "jpeg" {
		tp = "jpg"
	}
	return fpdf.ImageOptions{ImageType: tp}
}

// ImageOptions mirror the keyword arguments of Prawn::Document#image
// (:at, :width, :height, :fit). Zero-value fields keep prawn's defaults: the
// image is placed at the cursor at its natural pixel-per-point size.
type ImageOptions struct {
	// At, when set (AtSet true), places the image's top-left corner at the
	// bounds-relative point (AtX, AtY) instead of at the cursor.
	AtX, AtY float64
	AtSet    bool
	// Width and Height force the drawn size in points. Setting only one scales
	// the other proportionally (prawn's behaviour).
	Width, Height float64
	// FitW and FitH, when both > 0, scale the image to fit within that box while
	// preserving aspect ratio (prawn :fit => [w, h]).
	FitW, FitH float64
}

// Image mirrors Prawn::Document#image(path, options): it embeds a PNG or JPEG
// image from a file. Unsupported formats record ErrUnsupportedImageType.
func (d *Document) Image(path string, o ImageOptions) {
	f, err := os.Open(path)
	if err != nil {
		d.fail(err)
		return
	}
	defer func() { _ = f.Close() }()
	tp := imageTypeFromName(path)
	d.imageReader(f, tp, path, o)
}

// ImageReader mirrors Prawn::Document#image(io, options): it embeds an image
// from a reader. tp is the image type ("png" or "jpg"/"jpeg"), matching prawn's
// content-type detection.
func (d *Document) ImageReader(r io.Reader, tp string, o ImageOptions) {
	d.imageReader(r, strings.ToLower(tp), "", o)
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

// imageReader is the shared implementation. name, when non-empty, gives the
// image a stable registration key; otherwise a key is derived from the type and
// a counter so repeated readers do not collide.
func (d *Document) imageReader(r io.Reader, tp, name string, o ImageOptions) {
	switch tp {
	case "png", "jpg", "jpeg":
	default:
		d.fail(fmt.Errorf("%w: %q", ErrUnsupportedImageType, tp))
		return
	}
	data, err := io.ReadAll(r)
	if err != nil {
		d.fail(err)
		return
	}
	if name == "" {
		d.imgSeq++
		name = fmt.Sprintf("img-%d.%s", d.imgSeq, tp)
	}

	info := d.f.RegisterImageOptionsReader(name, imgOpts(tp), bytes.NewReader(data))
	if d.f.Error() != nil {
		return
	}

	// Natural size in points: fpdf reports the image size in the current unit.
	natW, natH := info.Extent()
	w, h := resolveImageSize(natW, natH, o)

	// Compute the top-left placement point in fpdf coordinates.
	var x, y float64
	if o.AtSet {
		x, y = d.toFpdfXY(o.AtX, o.AtY)
	} else {
		x, y = d.toFpdfXY(0, d.cursor)
		// Advance the cursor past the image when flowing at the cursor.
		d.cursor -= h
	}
	d.f.ImageOptions(name, x, y, w, h, false, imgOpts(tp), 0, "")
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
