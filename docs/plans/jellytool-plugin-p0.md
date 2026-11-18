# Jellytool plugin P0 verification

Date: 2026-11-03 (Asia/Shanghai). Branch: `codex/jellytool-plugin`.

The user approved the three product decisions in
[the plan](jellytool-plugin-plan.md) and P0 using mock interfaces.

## Implemented contract

- A private, dependency-free Jellytoken OpenCode plugin is embedded in Go.
- `plugin.PackJellytoolPlugins(ctx, bun, resources)` packages the local Bun
  at the host's pinned version and platform, plugin files and runtime checksum.
  The output directory must be empty. There is no npm or GitHub install path.
- `plugin.StartJellytoolPlugins(ctx, resources, data)` verifies the bundle,
  registers its files under the explicit private data directory and starts an
  independently owned Bun host. Its public surface contains no plugin selector
  or install/update method. `Close` stops and waits for the child process.
- The host uses the same RPC and stream implementation as magpie. The ordinary
  magpie startup, auth cache, plugin registry and catalog behavior are preserved.
- API key sign-in appears in the `plugin.Provider` contract that the supplier
  page consumes. Key replacement keeps one sign-in in the private auth file.
  Saving a key is separate from the model listing that tests authorization.
- Model listing retains literal IDs and TEXT/IMAGE/VIDEO outputs; unsupported
  types and invalid/business-error envelopes fail instead of appearing as a
  successful empty list. Error messages do not copy upstream text or keys.
- Chat, Responses and Anthropic POSTs use their proper authentication headers
  and stream the upstream reply unchanged. Redirects are not followed.
- Third-party entries in plugins.json and ambient Bun preloads never enter
  this host. Resource, runtime and staged-file edits fail startup.
- A generation includes the plugin, package manifest and host script, so a
  host-only application update registers a new generation rather than rejecting
  its new host script as tampering with the old one.

The application packager must protect the resource directory and runtime lock
together. P0's checks detect mismatched resources and mutable staged files;
they do not replace an app signature. The data directory is a caller-owned
Jellytool directory, not a migration of magpie's state.

## Reproduce

Install Go matching go.mod and put **Bun 1.4.2** on PATH (the tests skip with
an older Bun; a skip is not acceptance). Run from the repository root:

```sh
go test -race ./internal/plugin \
  -run '^(TestJellytoolBundle.*|TestFakePlugin|TestPluginAccounts)$' -count=1
```

The package's TestMain isolates HOME and blocks external Go HTTP requests.
The new tests run the actual Bun host with a stripped environment, private
resource/data directories and an HTTP server bound to loopback. The public
launcher has no test-origin override. Tests supply synthetic keys only.

## Verification results

Checks used Go 1.26.8 and Bun 1.4.2 on macOS arm64, with pinned Go caches and
temporary test homes. The development-only Bun was obtained from its npm
platform package and checked against the registry's SHA-512 integrity value;
that download is not part of Jellytool startup or the test protocol.

| Check | Result |
| --- | --- |
| Focused command above, including `-race` | All four tests passed on the final implementation |
| Negative auth check (Go build overlay, no source edits) | Replacing the Bearer key made `TestJellytoolBundleContract` fail with `OpenAI auth` |
| Negative integrity check (Go build overlay, no source edits) | Removing staged-byte equality made `TestJellytoolBundleIntegrity` fail with `edited private plugin was executed` |
| Negative generation check (Go build overlay, no source edits) | Leaving the host script out of the generation hash made the host-only upgrade test fail |
| `gofmt -l` on changed Go files; `git diff --check` | No output |
| `go vet ./...` | Passed |
| `GOOS=linux CGO_ENABLED=0 go vet -tags nogui ./...` | Passed |
| `GOOS=windows CGO_ENABLED=0 go vet ./...` | Passed |
| Native, Linux (`nogui`), Windows builds to temporary output paths | Passed |
| `go test -p 2 -tags nogui ./...` | Failed: 75 packages passed, 2 packages had the four failures below |

The focused suite also covers a catalog larger than 8 MiB, single-key
replacement, both catalog auth forms, HTTP-200 business failures and malformed
catalogs. macOS SDK deprecation/enum warnings in unchanged native code were
printed during native checks; they were not check failures.

The full run occurred before the final host-only generation correction; the
final implementation then passed the focused race suite, all three vet/build
targets and all three negative overlays again. The full suite was not rerun
after that correction; its four failures remain unresolved. P0's mock-contract
checks passed, and the repository merge/release gate remains red.

| Full-suite failure | Investigation / unresolved limit |
| --- | --- |
| `gui.TestCoreScriptBudget` | Unchanged GUI scripts total 1,531,531 bytes against 1,530,861. Reproduced standalone; no GUI assets or budget were changed. |
| `gui.TestLaneLimitRoutes` | Expected an empty queue map, received a live `jellytoken#...` lane. The unchanged route calls `gateway.OwnerLanes` when no test-owned gateway exists, reading the running local gateway. No service was changed or stopped. |
| `gui.TestProxyCheckSaysAProxyPassedBy` | Expected the fake direct trace IP, got HTTP 502 with zero calls to the fake proxy. Reproduced standalone in the same machine environment; proxy behavior is unresolved. |
| `plugin.TestHostToldTheGatewayAddress` | Expected port 3592 after a settings edit, got 3591. Reproduced with `host.go` from HEAD `1223856f` and the new bundle source/tests excluded via a build overlay. The test clears only the no-proxy polling delay, while a system proxy can select the longer delay. No change to the ordinary host polling policy was made. |

These failures remain recorded and unresolved; this work has not been merged,
published or deployed. The GUI browser suite, real provider/agent checks and
native Windows execution were not run because this change exposes no new GUI
route and P0 is explicitly mock-only.

## Still outside P0

- Desktop supplier-page rendering and GUI/API/gateway composition. The tested
  provider object matches the page's contract; this is not a GUI acceptance run.
- Jellytool app identity, bundle ID, single-instance behavior, port, menus,
  CLI/API admission and background-job closure (P1).
- Recommendations and selection-set UI (P2).
- Per-model protocol compatibility, image/video generation, real Jellytoken
  authorization, paid requests, real agent configuration and platform-native
  Windows acceptance (P3). A TEXT type does not prove all three APIs accept it.
- Signatures, installers, publishing and production deployment.
