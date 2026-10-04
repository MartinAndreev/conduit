# Pass all arguments directly to Conduit, including --lang and --fixture.
$ErrorActionPreference = 'Stop'
if ($PSVersionTable.PSVersion -ge [version]'7.3') {
    $PSNativeCommandArgumentPassing = 'Standard'
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
    & go build -o bin/conduit.exe ./cmd/conduit
    if ($LASTEXITCODE -eq 0) {
        & "$repoDir/bin/conduit.exe" @args
    }
    $exitCode = $LASTEXITCODE
} finally {
    Pop-Location
}
exit $exitCode
