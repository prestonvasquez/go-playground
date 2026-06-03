# godriver2.6-update

Same program as `../godriver2.6/`, but with a direct dependency bump on
`golang.org/x/crypto` to close the advisories `govulncheck` reports against the
version that driver v2.6.0 pins.

## The point

You do **not** need the driver to publish a new release to pick up newer
versions of its transitive dependencies. Go modules uses
[Minimum Version Selection][mvs] — the build resolves to the **highest**
version any module in the graph requires. So adding a direct `require` for a
newer `golang.org/x/crypto` overrides what the driver asks for, without
forking the driver or waiting on an upstream release.

## The diff vs. `../godriver2.6/`

```diff
 require go.mongodb.org/mongo-driver/v2 v2.6.0

 require (
   github.com/klauspost/compress v1.17.6 // indirect
   ...
-  golang.org/x/crypto v0.33.0 // indirect
-  golang.org/x/sync v0.11.0 // indirect
-  golang.org/x/text v0.22.0 // indirect
+  golang.org/x/crypto v0.52.0 // indirect
+  golang.org/x/sync v0.20.0 // indirect
+  golang.org/x/text v0.37.0 // indirect
 )
```

Only `golang.org/x/crypto` was bumped by hand:

```
go get golang.org/x/crypto@v0.52.0
go mod tidy
```

`go mod tidy` then cascaded `x/sync` and `x/text` to their newer minimums via
crypto's own dependency graph.

## Result

`make vulncheck` here vs. in `../godriver2.6/`:

|                       | `godriver2.6` | `godriver2.6-update` |
| --------------------- | ------------- | -------------------- |
| Module-level findings | 26            | 9                    |
| Third-party advisories| 17            | 0                    |
| `stdlib` advisories   | 9             | 9                    |

The 17 advisories on `golang.org/x/crypto@v0.33.0` (13 `GO-2026-50xx` + 4
SSH-related `GO-2025-3xxx/4xxx`) all clear with the bump. The 9 stdlib
findings are unaffected — those are fixed by upgrading the Go toolchain
(`go1.25.5` → `go1.25.10`), not by anything in `go.mod`.

## Implication for PR #2405

The dependency-bump half of [mongo-go-driver#2405][pr] is not load-bearing for
downstream users — anyone seeing Dependabot/govulncheck warnings against
driver v2.6.0 can clear the third-party advisories today by adding a single
`require` line, without waiting for an upstream release. The driver-side bump
is hygiene (and a nicer default for users who don't override), not the only
remediation path.

[mvs]: https://research.swtch.com/vgo-mvs
[pr]: https://github.com/mongodb/mongo-go-driver/pull/2405
