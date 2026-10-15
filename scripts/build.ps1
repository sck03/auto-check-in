param(
    [string]$Go = '',
    [ValidatePattern('^[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9.]+)?$')]
    [string]$Version = '1.0.0',
    [switch]$SkipTests
)
$ErrorActionPreference = 'Stop'
$projectRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
if (-not $Go) {
    $goCommand = Get-Command go -ErrorAction SilentlyContinue
    if ($goCommand) { $Go = $goCommand.Source }
    else { $Go = Join-Path $projectRoot '.tools/go/bin/go.exe' }
}
if (-not (Test-Path -LiteralPath $Go)) { throw 'Install Go 1.27.2 or supply -Go with its full path.' }
function Invoke-Checked([string]$Program, [string[]]$Arguments) {
    & $Program @Arguments
    if ($LASTEXITCODE -ne 0) { throw "Command failed: $Program (exit $LASTEXITCODE)" }
}
$previousGOOS = $env:GOOS
$previousGOARCH = $env:GOARCH
$previousCGO = $env:CGO_ENABLED
Push-Location $projectRoot
try {
    Push-Location (Join-Path $projectRoot 'web')
    try {
        Invoke-Checked 'npm.cmd' @('ci')
        Invoke-Checked 'npm.cmd' @('run', 'build')
    } finally { Pop-Location }
    if (-not $SkipTests) {
        Invoke-Checked $Go @('test', './...')
        Invoke-Checked $Go @('vet', './...')
    }
    $env:GOOS = 'windows'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '0'
    Invoke-Checked $Go @('run', 'github.com/josephspurrier/goversioninfo/cmd/goversioninfo@v1.7.0',
        '-64', '-icon=build/windows/app.ico', '-manifest=build/windows/app.manifest',
        "-file-version=$Version", "-product-version=$Version",
        '-o=cmd/desktop/resources_windows_amd64.syso', 'build/windows/versioninfo.json')
    $windowsOutput = Join-Path $projectRoot 'dist/windows-amd64'
    $linuxOutput = Join-Path $projectRoot 'dist/linux-amd64'
    New-Item -ItemType Directory -Path $windowsOutput, $linuxOutput -Force | Out-Null
    Invoke-Checked $Go @('build', '-trimpath', '-ldflags', "-H=windowsgui -s -w -X auto-check-in/internal/buildinfo.Version=$Version", '-o', (Join-Path $windowsOutput 'AutoCheckIn.exe'), './cmd/desktop')
    Invoke-Checked $Go @('build', '-trimpath', '-ldflags', "-s -w -X auto-check-in/internal/buildinfo.Version=$Version", '-o', (Join-Path $windowsOutput 'checkin-server.exe'), './cmd/server')
    Copy-Item -LiteralPath (Join-Path $windowsOutput 'AutoCheckIn.exe'), (Join-Path $windowsOutput 'checkin-server.exe') -Destination (Join-Path $projectRoot 'dist') -Force
    Invoke-Checked 'node' @('scripts/licenses.mjs', '--go', $Go, '--out', (Join-Path $windowsOutput 'licenses'))
    $env:GOOS = 'linux'
    Invoke-Checked $Go @('build', '-trimpath', '-ldflags', "-s -w -X auto-check-in/internal/buildinfo.Version=$Version", '-o', (Join-Path $linuxOutput 'checkin-server'), './cmd/server')
    Copy-Item -LiteralPath (Join-Path $windowsOutput 'licenses') -Destination $linuxOutput -Recurse -Force
    foreach ($destination in @($windowsOutput, $linuxOutput)) {
        Copy-Item -LiteralPath 'README.md', 'THIRD_PARTY_NOTICES.md' -Destination $destination -Force
        Copy-Item -LiteralPath 'docs' -Destination $destination -Recurse -Force
    }
    $windowsArchive = Join-Path $projectRoot "dist/AutoCheckIn-$Version-windows-amd64.zip"
    $linuxArchive = Join-Path $projectRoot "dist/auto-check-in-$Version-linux-amd64.tar.gz"
    Compress-Archive -Path (Join-Path $windowsOutput '*') -DestinationPath $windowsArchive -Force
    Invoke-Checked 'tar.exe' @('-czf', $linuxArchive, '-C', $linuxOutput, '.')
    Get-FileHash -Algorithm SHA256 -LiteralPath $windowsArchive, $linuxArchive |
        ForEach-Object { $_.Hash.ToLowerInvariant() + '  ' + [IO.Path]::GetFileName($_.Path) } |
        Set-Content -LiteralPath (Join-Path $projectRoot 'dist/SHA256SUMS.txt') -Encoding ascii
    Write-Output "Release files are in $projectRoot/dist"
} finally {
    $env:GOOS = $previousGOOS
    $env:GOARCH = $previousGOARCH
    $env:CGO_ENABLED = $previousCGO
    Pop-Location
}
