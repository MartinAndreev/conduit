param(
  [string]$Repository = "__CONDUIT_REPOSITORY__",
  [string]$Version = "latest",
  [string]$InstallDir = "$env:LOCALAPPDATA\Conduit\bin"
)

if ($Repository -eq "__CONDUIT_REPOSITORY__") {
  throw "Set -Repository owner/repository before running this installer."
}
if (-not [Environment]::Is64BitOperatingSystem) {
  throw "Only Windows x64 is currently supported."
}

$asset = "conduit-windows-x64.exe"
$base = if ($Version -eq "latest") {
  "https://github.com/$Repository/releases/latest/download"
} else {
  "https://github.com/$Repository/releases/download/$Version"
}
$temp = Join-Path ([IO.Path]::GetTempPath()) ("conduit-" + [guid]::NewGuid())
New-Item -ItemType Directory -Path $temp | Out-Null
try {
  Invoke-WebRequest "$base/$asset" -OutFile (Join-Path $temp $asset)
  Invoke-WebRequest "$base/SHA256SUMS" -OutFile (Join-Path $temp "SHA256SUMS")
  $expected = ((Get-Content (Join-Path $temp "SHA256SUMS")) | Where-Object { $_ -match "\s$asset$" }) -replace "\s+.*$", ""
  $actual = (Get-FileHash (Join-Path $temp $asset) -Algorithm SHA256).Hash.ToLower()
  if ($expected.ToLower() -ne $actual) { throw "Checksum verification failed for $asset." }
  New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
  Copy-Item (Join-Path $temp $asset) (Join-Path $InstallDir "conduit.exe") -Force
  $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
  if (($userPath -split ";") -notcontains $InstallDir) {
    [Environment]::SetEnvironmentVariable("Path", "$userPath;$InstallDir", "User")
  }
  Write-Host "Installed Conduit to $InstallDir\conduit.exe. Open a new terminal to use it."
} finally {
  Remove-Item -Recurse -Force $temp -ErrorAction SilentlyContinue
}
