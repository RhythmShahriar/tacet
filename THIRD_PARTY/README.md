# Third-party code in the tacet binary

tacet's own source is MIT licensed. The `tacet` binary links Go modules
from other authors. Most are MIT or BSD licensed. The ones below are
released under the Apache License 2.0, which asks that a copy of the
license and each module's NOTICE file travel with any binary built from
it. Release archives carry both under `THIRD_PARTY/notices/`.

| Module | Version | NOTICE file |
|---|---|---|
| github.com/conductor-oss/markitdown | v0.0.1 | none |
| github.com/extrame/ole2 | v0.0.0-20160812065207-d69429661ad7 | none |
| github.com/extrame/xls | v0.0.1 | none |
| github.com/jolestar/go-commons-pool/v2 | v2.1.2 | none |
| github.com/modern-go/concurrent | v0.0.0-20180306012644-bacd9c7ef1dd | none |
| github.com/modern-go/reflect2 | v1.0.2 | none |
| github.com/richardlehane/mscfb | v1.0.4 | none |
| github.com/richardlehane/msoleps | v1.0.4 | none |
| github.com/tetratelabs/wazero | v1.11.0 | in `notices/NOTICE` |
| gopkg.in/yaml.v3 | v3.0.1 | in `notices/NOTICE` (MIT and Apache 2.0) |

The binary also embeds a WebAssembly build of [PDFium](https://pdfium.googlesource.com/pdfium/)
through `github.com/klippa-app/go-pdfium` (MIT), which reads PDF files.
PDFium is released under the Apache License 2.0 with parts under a
BSD-style license, as its own LICENSE file states.

## Files

- `notices/LICENSE-Apache-2.0`: the Apache License 2.0, unchanged.
- `notices/NOTICE`: the NOTICE file of each module above that has one,
  unchanged, under its module path and version.

## Keeping it current

After a dependency update, list what the binary links and check each
module's license in the module cache:

```sh
make build
go version -m bin/tacet
```

Add any new Apache-2.0 module to the table and copy its NOTICE file, when
it has one, into `notices/NOTICE`.
