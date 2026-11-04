<#
Builds dist\tiny-cam-viewer.exe (64-bit, no console window, no cgo).
Also gets libmpv-2.dll and copies the sample config.
#>
$ErrorActionPreference = 'Stop'
$root = Resolve-Path (Join-Path $PSScriptRoot '..')
Set-Location $root
$dist = Join-Path $root 'dist'
New-Item -ItemType Directory -Force $dist | Out-Null

$env:CGO_ENABLED = '0'
$env:GOOS = 'windows'
$env:GOARCH = 'amd64'
go build -trimpath -ldflags '-s -w -H=windowsgui' -o (Join-Path $dist 'tiny-cam-viewer.exe') .
if ($LASTEXITCODE -ne 0) { throw 'go build failed' }

& (Join-Path $PSScriptRoot 'fetch-libmpv.ps1') -OutDir $dist
Copy-Item cameras.conf.example (Join-Path $dist 'cameras.conf.example') -Force
Copy-Item THIRD-PARTY-NOTICES.md, LICENSE (Join-Path $dist '') -Force
# Local test config (git ignores it). Only copied if the dist folder has none.
if ((Test-Path cameras.conf) -and -not (Test-Path (Join-Path $dist 'cameras.conf'))) {
    Copy-Item cameras.conf (Join-Path $dist 'cameras.conf')
}
Write-Host "Done: $dist"
