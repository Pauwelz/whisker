# QR library for the Daemon

Research date: 2026-10-24. All facts come from the repos and docs listed in Sources.

## Answer

Use `rsc.io/qr`.

It has zero dependencies. It is pure Go. `Code.PNG()` returns PNG bytes, ready for base64 into a `data:image/png;base64,...` URI.

The Daemon passes each whatsmeow QR string to `qr.Encode(code, qr.L)`, sets `Scale`, and encodes the result.

- Level `L` is enough for linking. The QR code is shown on a screen, not printed.
- The standard library has no QR encoder. A tiny encoder from scratch is not smaller than `rsc.io/qr` (see "Standard library only").
- whatsmeow's own old `mdtest` already pulled `rsc.io/qr` (indirectly, through `qrterminal`). The Daemon would use the same code.
- Risk: the last commit is from 2022 and the last tag is from 2018. The code is small and finished. If it breaks, vendor it (BSD-3-Clause allows this).
- Fallback: `skip2/go-qrcode` (no dependencies, but no release since 2020).

## Comparison

| | rsc.io/qr | skip2/go-qrcode | yeqown/go-qrcode/v2 |
|---|---|---|---|
| Licence | BSD-3-Clause | MIT | MIT |
| Last commit | 2022-04-17 | 2020-06-17 | 2026-08-05 (core) |
| Last release | v0.2.0, 2018-06-05 | none (no tags) | v2.2.5 on GitHub Releases (2025-02-14); latest tag v2.3.0 |
| Transitive deps (core) | none | none | `yeqown/reedsolomon`, `golang.org/x/text` (+ test-only `testify`) |
| Transitive deps for PNG | none | none | The `writer/standard` module adds `fogleman/gg`, `golang/freetype`, `golang.org/x/image`, `pkg/errors` |
| cgo-free | yes | yes | yes |
| PNG output API | `(*Code).PNG() []byte`; also `Image()` for `image/png` | `qrcode.Encode(content, level, size) ([]byte, error)`; `(*QRCode).PNG(size)` | No direct API. Core builds a `Matrix`. A `Writer` renders it (`writer/standard` for PNG, or your own) |
| Error-correction control | `qr.L`, `M`, `Q`, `H` | `qrcode.Low`, `Medium`, `High`, `Highest` | `WithErrorCorrectionLevel(...)`; default is Quart |
| Size of code (non-test Go lines, approx.) | about 1,500 plus 520 in `gf256` | about 7,900 (3,050 are the `version.go` tables) | core about 8,300 lines in total (includes tests); `writer/standard` about 1,600 |

Notes:

- `rsc.io/qr` `PNG()` uses a custom encoder. Its output is about 2x larger than optimal, but it runs about 20x faster than `png.Encode`. The size is irrelevant for a QR code of a few hundred bytes.
- `rsc.io/qr` is `go 1.18` in `go.mod`. whatsmeow needs `go 1.26.0`, so there is no conflict.
- `skip2/go-qrcode` has a `qrcode` command-line tool in the same module. The Daemon does not build it.
- `yeqown/go-qrcode/v2` has the most features (styles, logos) and the most dependencies. The Daemon needs none of these.
- Line counts are approximate (`wc -l` on a shallow clone). Treat them as an order of magnitude.

## Standard library only

No. Go's standard library has no QR encoder. `image/png` only encodes pixels.

You still need a QR encoder: Reed-Solomon, mask selection and version tables. That is the part `rsc.io/qr` supplies, in about 2,000 lines. Writing a smaller one means owning a QR implementation.

The option with no extra dependencies is `rsc.io/qr` or `skip2/go-qrcode`. Both add no transitive modules. Both use `image/png` or an internal encoder only.

## What whatsmeow recommends

- The package example only says "Render the QR code here". It suggests `qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)` or `qrencode -t ansiutf8`. It prints the raw code string.
- The repo no longer has `mdtest` (deleted 2024-08-11). Its last `go.mod` required `github.com/mdp/qrterminal/v3 v3.2.0` and `rsc.io/qr v0.2.0` (indirect).
- whatsmeow does not require any QR library in its own `go.mod`. It only emits the string.
- `qrterminal` writes to a terminal. It does not produce PNG. It is not fit for the Daemon.
- whatsmeow does not recommend an encoder for PNG output. This is the project's choice.

## Sources

- whisker architecture spec: `docs/architecture.md` (sections 2, 6, 9)
- [rsc/qr repository](https://github.com/rsc/qr) ([go.mod](https://github.com/rsc/qr/blob/master/go.mod), [qr.go](https://github.com/rsc/qr/blob/master/qr.go), [png.go](https://github.com/rsc/qr/blob/master/png.go), [LICENSE](https://github.com/rsc/qr/blob/master/LICENSE), [tags](https://github.com/rsc/qr/tags))
- [skip2/go-qrcode repository](https://github.com/skip2/go-qrcode) ([go.mod](https://github.com/skip2/go-qrcode/blob/master/go.mod), [qrcode.go](https://github.com/skip2/go-qrcode/blob/master/qrcode.go), [LICENSE](https://github.com/skip2/go-qrcode/blob/master/LICENSE))
- [yeqown/go-qrcode repository](https://github.com/yeqown/go-qrcode) ([core go.mod](https://github.com/yeqown/go-qrcode/blob/main/go.mod), [writer/standard go.mod](https://github.com/yeqown/go-qrcode/blob/main/writer/standard/go.mod), [encoder_option.go](https://github.com/yeqown/go-qrcode/blob/main/encoder_option.go), [writer.go](https://github.com/yeqown/go-qrcode/blob/main/writer.go), [releases](https://github.com/yeqown/go-qrcode/releases))
- [whatsmeow client_test.go (QR example)](https://github.com/tulir/whatsmeow/blob/main/client_test.go)
- [whatsmeow qrchan.go (GetQRChannel)](https://github.com/tulir/whatsmeow/blob/main/qrchan.go)
- [whatsmeow package docs](https://pkg.go.dev/go.mau.fi/whatsmeow)
- [whatsmeow commit 9bd3fa0 "mdtest: delete"](https://github.com/tulir/whatsmeow/commit/9bd3fa0cc1c1a56c4e1ea324883b876c2c008c84) (its parent holds the old `mdtest/go.mod`)
