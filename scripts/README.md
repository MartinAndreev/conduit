# Launch Conduit

From the repository, use either launcher. Every launch builds the current source
into the ignored `bin/` directory, then runs the standalone application.

macOS / Linux:

```sh
./scripts/start-tui.sh
./scripts/start-gui.sh --lang bg --fixture ready
```

Windows PowerShell:

```powershell
.\scripts\start-tui.ps1
.\scripts\start-gui.ps1 --lang bg --fixture ready
```

You can invoke a script by its full path from any directory. Quote paths and
arguments containing spaces. All arguments are forwarded to the application.
Use `--help` or `--version` for safe TUI checks, and `--help` for GUI; these exit
before startup. `--fixture ready` uses synthetic startup data.

Go is required for both launchers. On macOS/Linux, GUI additionally requires a C
compiler and native desktop dependencies: Xcode command line tools on macOS, or
GTK3/WebKitGTK development packages plus pkg-config on Linux. Windows requires
the Microsoft Edge WebView2 runtime. Windows GUI builds do not
require a C compiler; Wails reports unavailable WebView2 at launch. The scripts
check Go and the macOS/Linux build dependencies and do not install anything.

The macOS GUI launcher preserves an explicit `SDKROOT`. Otherwise it prefers an
installed macOS 26.5 SDK to avoid the observed SDK 27/linker TAPI mismatch, then
falls back to the SDK selected by xcrun. Linux automatically selects WebKitGTK
4.1 when installed, otherwise 4.0. Windows builds keep console output so help and
build errors remain visible. The macOS/Linux GUI launcher enables CGO for its build only.

PowerShell execution policy may require allowing local scripts according to your
machine's policy. The scripts do not change execution policy. Both Windows
launchers restore the calling directory and return the native build/application
exit code.

PowerShell 7.3 or newer preserves empty arguments and embedded quotes with native
argument passing. Windows PowerShell uses its legacy native argument handling;
ordinary flags and paths containing spaces are supported.
