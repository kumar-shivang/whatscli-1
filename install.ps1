# SPDX-License-Identifier: AGPL-3.0-only
#
# Install the latest whatscli-extended release on Windows (amd64):
#   irm https://raw.githubusercontent.com/kumar-shivang/whatscli-extended/master/install.ps1 | iex
#
# Environment:
#   WHATSCLI_VERSION  install this tag (for example v1.7.3) instead of the latest
#   INSTALL_DIR       target directory (default: %LOCALAPPDATA%\whatscli)
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$repo = 'kumar-shivang/whatscli-extended'

if ($env:PROCESSOR_ARCHITECTURE -ne 'AMD64') { throw "unsupported architecture $env:PROCESSOR_ARCHITECTURE (only amd64 is built)" }

$tag = $env:WHATSCLI_VERSION
if (-not $tag) {
    $rel = Invoke-RestMethod "https://api.github.com/repos/$repo/releases/latest"
    $tag = $rel.tag_name
}
if ($tag -notmatch '^v\d+\.\d+\.\d+([-.][0-9A-Za-z.]+)?$') { throw "could not determine the latest release (got '$tag')" }

$dir = if ($env:INSTALL_DIR) { $env:INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'whatscli' }
New-Item -ItemType Directory -Force -Path $dir | Out-Null

$asset = "whatscli-$tag-windows-amd64.zip"
$base = "https://github.com/$repo/releases/download/$tag"
$tmp = Join-Path ([IO.Path]::GetTempPath()) ("whatscli-" + [Guid]::NewGuid())
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
    Write-Host "Installing whatscli $tag (windows/amd64) to $dir"
    Invoke-WebRequest "$base/$asset" -OutFile "$tmp\$asset"
    Invoke-WebRequest "$base/whatscli-$tag-checksums.txt" -OutFile "$tmp\checksums.txt"

    $line = Get-Content "$tmp\checksums.txt" | Where-Object { ($_ -split '\s+')[1] -eq $asset } | Select-Object -First 1
    if (-not $line) { throw "no checksum listed for $asset" }
    $want = ($line -split '\s+')[0].ToLower()
    $got = (Get-FileHash "$tmp\$asset" -Algorithm SHA256).Hash.ToLower()
    if ($got -ne $want) { throw "checksum mismatch (got $got, want $want)" }

    Expand-Archive "$tmp\$asset" -DestinationPath $tmp -Force
    $exe = Join-Path $dir 'whatscli.exe'
    if (Test-Path $exe) {
        # a running exe can be renamed but not overwritten
        Remove-Item "$exe.old" -Force -ErrorAction SilentlyContinue
        Move-Item $exe "$exe.old" -Force
    }
    Move-Item "$tmp\whatscli.exe" $exe -Force
}
finally {
    Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
}

$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (($userPath -split ';') -notcontains $dir) {
    [Environment]::SetEnvironmentVariable('Path', "$userPath;$dir", 'User')
    Write-Host "Added $dir to your user PATH (open a new terminal to pick it up)."
}
Write-Host "Installed $exe"
Write-Host "Run: whatscli    (update later from inside the app with /update)"
