$ErrorActionPreference = "Stop"

$toolchainBin = "L:\Development\mingw64\bin"
$sqliteInclude = "L:\Development\sqlite-headers"

if (!(Test-Path (Join-Path $toolchainBin "gcc.exe"))) {
    throw "gcc.exe not found at $toolchainBin"
}

if (!(Test-Path (Join-Path $sqliteInclude "sqlite3.h"))) {
    throw "sqlite3.h not found at $sqliteInclude"
}

$env:CGO_ENABLED = "1"
$env:CC = Join-Path $toolchainBin "gcc.exe"
$env:CXX = Join-Path $toolchainBin "g++.exe"
$env:CGO_CFLAGS = "-I$sqliteInclude"
$env:Path = "$toolchainBin;$env:Path"

if ($args.Count -eq 0) {
    throw "usage: .\\scripts\\run-with-cgo.ps1 <go arguments>"
}

& go @args
