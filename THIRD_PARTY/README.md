# Third-party code in the tacet binary

tacet's own source is MIT licensed. The `tacet` binary links Go modules
from other authors. Most are MIT or BSD licensed. The ones below are
released under the Apache License 2.0, which asks that a copy of the
license and each module's NOTICE file travel with any binary built from
it. Release archives carry both under `THIRD_PARTY/notices/`.

| Module | Version | NOTICE file |
|---|---|---|
| github.com/conductor-oss/markitdown | v0.0.1 | none |
| github.com/cyberphone/json-canonicalization | v0.0.0-20241213102144-19d51d7fe467 | none |
| github.com/extrame/ole2 | v0.0.0-20160812065207-d69429661ad7 | none |
| github.com/extrame/xls | v0.0.1 | none |
| github.com/go-logr/logr | v1.4.4 | none |
| github.com/go-logr/stdr | v1.2.2 | none |
| github.com/go-openapi/analysis | v0.25.5 | none |
| github.com/go-openapi/errors | v0.22.8 | none |
| github.com/go-openapi/jsonpointer | v1.0.0 | in `notices/NOTICE` |
| github.com/go-openapi/jsonreference | v1.0.0 | in `notices/NOTICE` |
| github.com/go-openapi/loads | v0.25.0 | none |
| github.com/go-openapi/runtime | v0.33.0 | in `notices/NOTICE` |
| github.com/go-openapi/runtime/server-middleware | v0.30.0 | none |
| github.com/go-openapi/spec | v0.22.9 | none |
| github.com/go-openapi/strfmt | v0.27.0 | none |
| github.com/go-openapi/swag | v0.26.1 | none |
| github.com/go-openapi/swag/* (cmdutils, conv, fileutils, jsonname, jsonutils, loading, mangling, netutils, pools, stringutils, typeutils, yamlutils) | v0.26.1 to v0.27.3 | none |
| github.com/go-openapi/validate | v0.26.1 | none |
| github.com/google/certificate-transparency-go | v1.3.3 | none |
| github.com/google/go-containerregistry | v0.21.7 | none |
| github.com/in-toto/attestation | v1.2.0 | none |
| github.com/in-toto/in-toto-golang | v0.11.0 | none |
| github.com/jolestar/go-commons-pool/v2 | v2.1.2 | none |
| github.com/modern-go/concurrent | v0.0.0-20180306012644-bacd9c7ef1dd | none |
| github.com/modern-go/reflect2 | v1.0.2 | none |
| github.com/oklog/ulid/v2 | v2.1.1 | none |
| github.com/opencontainers/go-digest | v1.0.0 | none |
| github.com/richardlehane/mscfb | v1.0.4 | none |
| github.com/richardlehane/msoleps | v1.0.4 | none |
| github.com/shibumi/go-pathspec | v1.3.0 | none |
| github.com/sigstore/protobuf-specs | v0.5.1 | none |
| github.com/sigstore/rekor | v1.5.3 | none |
| github.com/sigstore/rekor-tiles/v2 | v2.3.0 | none |
| github.com/sigstore/sigstore | v1.10.8 | none |
| github.com/sigstore/sigstore-go | v1.3.0 | none |
| github.com/sigstore/timestamp-authority/v2 | v2.1.3 | none |
| github.com/tetratelabs/wazero | v1.11.0 | in `notices/NOTICE` |
| github.com/theupdateframework/go-tuf/v2 | v2.4.2 | in `notices/NOTICE` |
| github.com/transparency-dev/formats | v0.1.1 | none |
| github.com/transparency-dev/merkle | v0.0.2 | none |
| go.opentelemetry.io/auto/sdk | v1.2.1 | none |
| go.opentelemetry.io/otel | v1.44.0 | none |
| go.opentelemetry.io/otel/metric | v1.44.0 | none |
| go.opentelemetry.io/otel/trace | v1.44.0 | none |
| go.yaml.in/yaml/v3 | v3.0.4 | in `notices/NOTICE` (MIT and Apache 2.0) |
| google.golang.org/genproto/googleapis/api | v0.0.0-20260526163538-3dc84a4a5aaa | none |
| google.golang.org/genproto/googleapis/rpc | v0.0.0-20260523011958-0a33c5d7ca68 | none |
| google.golang.org/grpc | v1.82.1 | in `notices/NOTICE` |
| gopkg.in/yaml.v3 | v3.0.1 | in `notices/NOTICE` (MIT and Apache 2.0) |
| k8s.io/klog/v2 | v2.140.0 | none |

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
