# mlow (vendored)

Pure-Go implementation of the WhatsApp **MLow** audio codec, vendored into this
repository so the build is self-contained (no external module, no cgo, no DLL).

- **Upstream:** [`github.com/purpshell/meowcaller`](https://github.com/purpshell/meowcaller) (`mlow` package), MIT licensed.
- **Reference implementation:** ported from [`github.com/oxidezap/whatsapp-rust`](https://github.com/oxidezap/whatsapp-rust) (cited in per-file source-of-truth comments).
- **License:** see [`LICENSE`](./LICENSE) — MIT, © 2026 Rajeh Taher. Retained per the MIT attribution requirement.

`testdata/` holds the upstream reference vectors (libopus reference output, real
WhatsApp frame captures, and per-module ground-truth) used by the package tests.
