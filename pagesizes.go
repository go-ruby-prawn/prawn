// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-ruby-prawn/prawn authors

package prawn

// pageSizes mirrors the portion of PDF::Core::PageGeometry::SIZES that prawn
// exposes, in PDF points (1/72 inch), as [width, height] in portrait. Landscape
// is derived by swapping the two. The values match prawn's table exactly.
var pageSizes = map[string][2]float64{
	"LETTER":    {612.00, 792.00},
	"LEGAL":     {612.00, 1008.00},
	"TABLOID":   {792.00, 1224.00},
	"LEDGER":    {1224.00, 792.00},
	"EXECUTIVE": {521.86, 756.00},
	"FOLIO":     {595.00, 936.00},

	"A0":  {2383.94, 3370.39},
	"A1":  {1683.78, 2383.94},
	"A2":  {1190.55, 1683.78},
	"A3":  {841.89, 1190.55},
	"A4":  {595.28, 841.89},
	"A5":  {419.53, 595.28},
	"A6":  {297.64, 419.53},
	"A7":  {209.76, 297.64},
	"A8":  {147.40, 209.76},
	"A9":  {104.88, 147.40},
	"A10": {73.70, 104.88},

	"B0": {2834.65, 4008.19},
	"B1": {2004.09, 2834.65},
	"B2": {1417.32, 2004.09},
	"B3": {1000.63, 1417.32},
	"B4": {708.66, 1000.63},
	"B5": {498.90, 708.66},
	"B6": {354.33, 498.90},

	"C0": {2599.37, 3676.54},
	"C1": {1836.85, 2599.37},
	"C2": {1298.27, 1836.85},
	"C3": {918.43, 1298.27},
	"C4": {649.13, 918.43},
	"C5": {459.21, 649.13},
	"C6": {323.15, 459.21},
}
