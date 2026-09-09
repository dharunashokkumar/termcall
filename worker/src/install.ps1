# termcall installer for Windows.
#
#   irm https://__HOST__/install.ps1 | iex
#
# Everything the client needs is inside the one .exe, ffmpeg included.
$ErrorActionPreference = 'Stop'

$repo = 'dharunashokkumar/termcall'

$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    'AMD64' { 'amd64' }
    'ARM64' { 'arm64' }
    default { throw "termcall has no build for $($env:PROCESSOR_ARCHITECTURE)" }
}

# A per-user location, so this never needs an administrator.
$target = Join-Path $env:LOCALAPPDATA 'Programs\termcall'
New-Item -ItemType Directory -Force -Path $target | Out-Null

$url = "https://github.com/$repo/releases/latest/download/tc_windows_$arch.exe"
$exe = Join-Path $target 'tc.exe'

Write-Host "fetching termcall for windows/$arch..."
Invoke-WebRequest -Uri $url -OutFile $exe -UseBasicParsing

# Put it on PATH for future shells. The current one keeps the old PATH, so
# say so rather than letting `tc` mysteriously not be found.
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if ($userPath -notlike "*$target*") {
    [Environment]::SetEnvironmentVariable('Path', "$userPath;$target", 'User')
    Write-Host "installed $exe"
    Write-Host "added it to your PATH - open a new terminal, then run: tc"
} else {
    Write-Host "installed $exe"
    Write-Host "run: tc"
}
