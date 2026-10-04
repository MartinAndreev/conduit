# Startup desktop adapter

Approved shell: Wails **v2.15.0**, templUI **v2.0.0-beta.10**.
The fixed release declares the module `github.com/axadrn/shadcn-templ/v2`;
tag commit is `2c7fcfe084c33b871d27a38f6a6ee175d39051a2`.
Do not substitute the current templUI module/version.

`cmd/conduit-gui` composes shared startup with native presentation. Native imports
are behind `desktop`; default tests and CLI/TUI builds require no desktop SDK.
Pure bridge/views tests remain part of the default suite.

## Build

```sh
go build -tags desktop,production -o conduit-gui ./cmd/conduit-gui
./conduit-gui --lang en
./conduit-gui --lang bg
```

macOS requires Xcode command line tools. In the tested local toolchain, explicitly
select SDK 26.5 (the default SDK 27 has a TAPI/linker mismatch):

```sh
SDKROOT=/Applications/Xcode.app/Contents/Developer/Platforms/MacOSX.platform/Developer/SDKs/MacOSX26.5.sdk go build -tags desktop,production -o conduit-gui ./cmd/conduit-gui
```

`cmd/conduit-gui/macos.go` explicitly links UniformTypeIdentifiers, required by
Wails 2.15's UTType references. A deprecated toolbar warning remains upstream.
Windows additionally requires WebView2 runtime; use `-ldflags="-H windowsgui"`.
Linux requires GTK3/WebKitGTK and a C compiler; use `webkit2_41` when appropriate.
These platforms require native build/runtime verification; macOS success does
not establish their behavior.

## Resources and reproduction

`views/layout.templ` imports fixed beta.10 button/progress components. Regenerate:

```sh
go run github.com/a-h/templ/cmd/templ@v0.3.1001 generate -path internal/presentation/gui/views
```

`views/assets/progress.js` is copied verbatim from beta.10's
`components/progress/progress.js`. `style.css` is authored presentation CSS using
local theme tokens and beta.10 semantic data slots; no Tailwind runtime or CDN.
`bridge.js` is the local transport renderer. All resources are embedded. Fonts
use the platform's installed system sans-serif; no remote font fetches.
The application does not listen on a localhost port. Wails' asset handler serves
templ plus embedded resources; Go bindings/events carry state and commands.

For native screenshots and keyboard/cancel/retry checks without real harnesses
or configuration access:

```sh
./conduit-gui --fixture ready --lang en
./conduit-gui --fixture warnings --lang bg
./conduit-gui --fixture blocked
./conduit-gui --fixture slow
```

Fixtures run the shared application procedure against synthetic ports only.
The native compatibility spike verified WKWebView, exact beta.10 button/progress,
local CSS/JS and an actual Go binding response. Production captures and bridge
race tests verify the integrated adapter separately.

Focused verification:

```sh
go test -race ./internal/presentation/gui/... ./internal/presentation/i18n
node --test internal/presentation/gui/views/tests/bridge.test.mjs
```

The dependency-free Node tests use a minimal DOM fixture to verify rejected
transport reads, safe feedback, stale events, and cleanup control visibility.
They complement native controls/screenshots, rather than proving native rendering.
