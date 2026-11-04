<#
Downloads libmpv-2.dll (LGPL build, 64-bit) and puts it in the dist folder.
Source: https://github.com/zhongfly/mpv-winbuild (automatic builds of mpv).
Uses the Windows tar.exe (built into Windows 10/11) to unpack the .7z file.
#>
param(
    [string]$OutDir = (Join-Path $PSScriptRoot '..\dist'),
    [string]$Tag = ''   # release tag, empty = latest
)
$ErrorActionPreference = 'Stop'
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
New-Item -ItemType Directory -Force $OutDir | Out-Null
$OutDir = (Resolve-Path $OutDir).Path
$target = Join-Path $OutDir 'libmpv-2.dll'
if (Test-Path $target) { Write-Host "libmpv-2.dll already exists: $target"; return }

$api = 'https://api.github.com/repos/zhongfly/mpv-winbuild/releases/' + $(if ($Tag) { "tags/$Tag" } else { 'latest' })
$rel = Invoke-RestMethod $api -Headers @{ 'User-Agent' = 'tiny-cam-viewer' }
$asset = $rel.assets | Where-Object { $_.name -match '^mpv-dev-lgpl-x86_64-\d' -and $_.name -notmatch 'v3' } | Select-Object -First 1
if (-not $asset) { throw 'Cannot find the mpv-dev-lgpl-x86_64 file in the release.' }
Write-Host "Downloading $($asset.name) ..."
$tmp = Join-Path ([IO.Path]::GetTempPath()) ('tcv-' + [guid]::NewGuid())
New-Item -ItemType Directory $tmp | Out-Null
try {
    $archive = Join-Path $tmp $asset.name
    Invoke-WebRequest $asset.browser_download_url -OutFile $archive -UseBasicParsing
    # Use the Windows tar.exe (bsdtar, reads .7z). Git's GNU tar can not.
    & (Join-Path $env:SystemRoot 'System32/tar.exe') -xf $archive -C $tmp libmpv-2.dll
    if ($LASTEXITCODE -ne 0) { throw 'tar could not unpack the archive. Unpack it with 7-Zip and copy libmpv-2.dll yourself.' }
    Copy-Item (Join-Path $tmp 'libmpv-2.dll') $target
    Write-Host "Saved $target"
} finally {
    Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
}
