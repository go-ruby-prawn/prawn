# Test data

- `goregular.ttf` — the "Go Regular" TrueType font from the Go font family
  (<https://blog.golang.org/go-fonts>), used only by the test suite to exercise
  TrueType parsing, subsetting and embedding. It is redistributed here under its
  own license, reproduced in `GoFont-LICENSE` (BSD-3-Clause, © Google Inc.). It
  is test input only and is not part of the `prawn` package or its build.

- `oracle/*.content` — page-1 PDF content streams captured from the real Ruby
  `prawn` 2.5.0 gem for the differential operator oracle (see
  `prawn_diff_test.go`). Regenerated/verified against a live gem by
  `prawn_gem_test.go` when Ruby + prawn are installed.
