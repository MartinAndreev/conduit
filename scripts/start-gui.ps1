# Run in Windows PowerShell or PowerShell on Windows.
$ErrorActionPreference = 'Stop'
if ($PSVersionTable.PSVersion -ge [version]'7.3') {
    $PSNativeCommandArgumentPassing = 'Standard'
}
if ($env:OS -ne 'Windows_NT') {
    Write-Error 'Use start-gui.sh on macOS or Linux.'
    exit 1
}
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Error 'Go is required. Install Go and add it to PATH.'
    exit 1
}
$repoDir = Split-Path -Parent $PSScriptRoot
$exitCode = 1
Push-Location $repoDir
try {
    New-Item -ItemType Directory -Force -Path bin | Out-Null
    & go build -tags 'desktop,production' -o bin/conduit-gui.exe ./cmd/conduit-gui
    if ($LASTEXITCODE -eq 0) {
        & "$repoDir/bin/conduit-gui.exe" @args
    }
    $exitCode = $LASTEXITCODE
} finally {
    Pop-Location
}
exit $exitCode
