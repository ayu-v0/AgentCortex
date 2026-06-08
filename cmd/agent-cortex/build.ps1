$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot = Resolve-Path (Join-Path $scriptDir "..\..")

$binaryName = "agent-cortex"
$targetOS = if ($env:GOOS) { $env:GOOS } else { (& go env GOOS).Trim() }
$targetArch = if ($env:GOARCH) { $env:GOARCH } else { (& go env GOARCH).Trim() }
$targetCGO = if ($env:CGO_ENABLED) { $env:CGO_ENABLED } else { "1" }
$outputDir = if ($env:OUTPUT_DIR) { $env:OUTPUT_DIR } else { Join-Path $repoRoot "dist" }
$goCache = if ($env:GOCACHE) { $env:GOCACHE } else { Join-Path $repoRoot ".gocache\build" }
$goTmp = if ($env:GOTMPDIR) { $env:GOTMPDIR } else { Join-Path $repoRoot ".gocache\tmp" }

$version = $env:VERSION
if (!$version) {
    try {
        $version = (& git -C $repoRoot describe --tags --always --dirty 2>$null).Trim()
    } catch {
        $version = ""
    }
}
if (!$version) {
    $version = "dev"
}
$version = [regex]::Replace($version, "[^0-9A-Za-z._-]", "-")

$executableName = $binaryName
if ($targetOS -eq "windows") {
    $executableName = "$binaryName.exe"
}

$packageName = "${binaryName}_${version}_${targetOS}_${targetArch}"
$packageDir = Join-Path $outputDir $packageName
$archivePath = Join-Path $outputDir "$packageName.tar.gz"
$checksumPath = "$archivePath.sha256"
$binaryPath = Join-Path $packageDir $executableName

if (Test-Path -LiteralPath $packageDir) {
    Remove-Item -Recurse -Force -LiteralPath $packageDir
}
if (Test-Path -LiteralPath $archivePath) {
    Remove-Item -Force -LiteralPath $archivePath
}
if (Test-Path -LiteralPath $checksumPath) {
    Remove-Item -Force -LiteralPath $checksumPath
}
New-Item -ItemType Directory -Force -Path $packageDir | Out-Null
New-Item -ItemType Directory -Force -Path $goCache | Out-Null
New-Item -ItemType Directory -Force -Path $goTmp | Out-Null

Write-Host "Building $binaryName ($targetOS/$targetArch, CGO_ENABLED=$targetCGO)"
$oldGOOS = $env:GOOS
$oldGOARCH = $env:GOARCH
$oldCGO = $env:CGO_ENABLED
$oldGOCACHE = $env:GOCACHE
$oldGOTMPDIR = $env:GOTMPDIR
$oldTMP = $env:TMP
$oldTEMP = $env:TEMP
try {
    $env:GOOS = $targetOS
    $env:GOARCH = $targetArch
    $env:CGO_ENABLED = $targetCGO
    $env:GOCACHE = $goCache
    $env:GOTMPDIR = $goTmp
    $env:TMP = $goTmp
    $env:TEMP = $goTmp

    Push-Location $repoRoot
    try {
        & go build -trimpath "-ldflags=-s -w" -o $binaryPath ".\cmd\$binaryName"
        if ($LASTEXITCODE -ne 0) {
            throw "go build failed with exit code $LASTEXITCODE"
        }
    } finally {
        Pop-Location
    }
} finally {
    $env:GOOS = $oldGOOS
    $env:GOARCH = $oldGOARCH
    $env:CGO_ENABLED = $oldCGO
    $env:GOCACHE = $oldGOCACHE
    $env:GOTMPDIR = $oldGOTMPDIR
    $env:TMP = $oldTMP
    $env:TEMP = $oldTEMP
}

Copy-Item -LiteralPath (Join-Path $repoRoot "README.md") -Destination $packageDir
Copy-Item -LiteralPath (Join-Path $repoRoot "LICENSE") -Destination $packageDir

Push-Location $outputDir
try {
    & tar -czf $archivePath $packageName
    if ($LASTEXITCODE -ne 0) {
        throw "tar failed with exit code $LASTEXITCODE"
    }
} finally {
    Pop-Location
}

$archiveFile = Split-Path -Leaf $archivePath
$hash = (Get-FileHash -LiteralPath $archivePath -Algorithm SHA256).Hash.ToLowerInvariant()
Set-Content -LiteralPath $checksumPath -Value "$hash  $archiveFile"

Write-Host "Package:  $archivePath"
Write-Host "Checksum: $checksumPath"
