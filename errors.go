// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-ruby-prawn/prawn authors

package prawn

import "fmt"

// This file mirrors Ruby prawn's Prawn::Errors::* exception tree. Each Ruby
// error class becomes a Go error value (for the parameterless ones) or a typed
// error (for the ones that carry a message), so callers can match them with
// errors.Is / errors.As exactly as a Ruby program would rescue the matching
// class.

// Error is the root of the error tree, mirroring the fact that every
// Prawn::Errors::* class is a StandardError. Every error produced by this
// package satisfies this interface, and the concrete values below embed it so
// errors.As(err, &prawn.Error(...)) style checks work through the sentinels.
type prawnError struct {
	kind string // stable machine-readable tag, e.g. "UnknownFont"
	msg  string
}

func (e *prawnError) Error() string {
	if e.msg == "" {
		return "Prawn::Errors::" + e.kind
	}
	return "Prawn::Errors::" + e.kind + ": " + e.msg
}

// Kind returns the stable Prawn::Errors class name (without the module prefix),
// e.g. "UnknownFont". It lets callers switch on the error category without
// string-matching the full message.
func (e *prawnError) Kind() string { return e.kind }

func newError(kind, format string, args ...any) *prawnError {
	return &prawnError{kind: kind, msg: fmt.Sprintf(format, args...)}
}

// Sentinel errors mirror the parameterless Prawn::Errors classes. They are
// returned directly (or wrapped) so callers can use errors.Is.
var (
	// ErrCannotFit mirrors Prawn::Errors::CannotFit — content is too large for
	// the space it must occupy (e.g. a word wider than the available width).
	ErrCannotFit = &prawnError{kind: "CannotFit"}

	// ErrUnknownFont mirrors Prawn::Errors::UnknownFont — a font was requested
	// by a name that has not been registered.
	ErrUnknownFont = &prawnError{kind: "UnknownFont"}

	// ErrInvalidPageLayout mirrors Prawn::Errors::InvalidPageLayout — a page
	// layout other than :portrait or :landscape was requested.
	ErrInvalidPageLayout = &prawnError{kind: "InvalidPageLayout"}

	// ErrUnsupportedImageType mirrors Prawn::Errors::UnsupportedImageType — an
	// image whose format is neither PNG nor JPEG was supplied.
	ErrUnsupportedImageType = &prawnError{kind: "UnsupportedImageType"}

	// ErrEmptyGraphicStateStack mirrors Prawn::Errors::EmptyGraphicStateStack —
	// restore_graphics_state was called with no matching save_graphics_state.
	ErrEmptyGraphicStateStack = &prawnError{kind: "EmptyGraphicStateStack"}

	// ErrIncompatibleStringEncoding mirrors
	// Prawn::Errors::IncompatibleStringEncoding — a string cannot be rendered
	// with the current font (here: invalid UTF-8).
	ErrIncompatibleStringEncoding = &prawnError{kind: "IncompatibleStringEncoding"}
)
