#Requires -Version 5.1
$ErrorActionPreference = 'Stop'

$Repo   = 'jaimegago/skillhub'
$Binary = 'skillhub'
$GoOs   = 'windows'

# ── Architecture detection ────────────────────────────────────────────────────
# Only windows/amd64 is shipped for v0.1.0.
$Arch = $env:PROCESSOR_ARCHITECTURE
switch ($Arch) {
    'AMD64'  { $GoArch = 'amd64' }
    default  {
        Write-Error ("Architecture '$Arch' is not supported. " +
            "Only windows/amd64 is shipped for v0.1.0. " +
            "Download manually: https://github.com/$Repo/releases")
        exit 1
    }
}

# ── Version resolution ────────────────────────────────────────────────────────
# Override: $env:SKILLHUB_VERSION = 'v0.1.2' before running this script.
if ($env:SKILLHUB_VERSION) {
    $Tag = $env:SKILLHUB_VERSION
} else {
    Write-Host 'Fetching latest release version...' -ForegroundColor Cyan
    $Resp = Invoke-RestMethod `
        -Uri "https://api.github.com/repos/$Repo/releases/latest" `
        -UseBasicParsing
    $Tag = $Resp.tag_name
}

# Normalize: Tag always has the v prefix (for release URLs);
# Version strips it to match goreleaser archive filenames (e.g. skillhub_0.1.0_windows_amd64.zip).
$Tag     = 'v' + $Tag.TrimStart('v')
$Version = $Tag.TrimStart('v')

Write-Host "Installing $Binary $Tag ($GoOs/$GoArch)..." -ForegroundColor Cyan

# ── Build URLs ────────────────────────────────────────────────────────────────
$Archive     = "${Binary}_${Version}_${GoOs}_${GoArch}.zip"
$Checksums   = "${Binary}_${Version}_checksums.txt"
$BaseUrl     = "https://github.com/$Repo/releases/download/$Tag"
$ArchiveUrl  = "$BaseUrl/$Archive"
$ChecksumUrl = "$BaseUrl/$Checksums"

# ── Temp directory ────────────────────────────────────────────────────────────
$TempDir = [System.IO.Path]::Combine(
    [System.IO.Path]::GetTempPath(),
    [System.IO.Path]::GetRandomFileName()
)
New-Item -ItemType Directory -Path $TempDir | Out-Null

try {
    $ArchivePath  = Join-Path $TempDir $Archive
    $ChecksumPath = Join-Path $TempDir $Checksums

    # ── Download ──────────────────────────────────────────────────────────────
    Write-Host "Downloading $Archive..." -ForegroundColor Cyan
    Invoke-WebRequest -Uri $ArchiveUrl  -OutFile $ArchivePath  -UseBasicParsing
    Invoke-WebRequest -Uri $ChecksumUrl -OutFile $ChecksumPath -UseBasicParsing

    # ── Checksum verification ─────────────────────────────────────────────────
    # goreleaser checksums file format: "<hash>  <filename>" (two spaces, sha256sum convention).
    Write-Host 'Verifying checksum...' -ForegroundColor Cyan
    $Pattern  = "\s+$([regex]::Escape($Archive))$"
    $Match    = Select-String -Path $ChecksumPath -Pattern $Pattern | Select-Object -First 1
    if (-not $Match) {
        Write-Error "No checksum entry found for $Archive in $Checksums"
        exit 1
    }
    $Expected = ($Match.Line -split '\s+' | Select-Object -First 1)
    $Actual   = (Get-FileHash -Path $ArchivePath -Algorithm SHA256).Hash.ToLower()
    if ($Actual -ne $Expected) {
        Write-Error ("Checksum mismatch for ${Archive}:`n" +
            "  expected: $Expected`n  got:      $Actual")
        exit 1
    }

    # ── Extract ───────────────────────────────────────────────────────────────
    Write-Host 'Extracting archive...' -ForegroundColor Cyan
    Expand-Archive -Path $ArchivePath -DestinationPath $TempDir -Force

    # ── Install ───────────────────────────────────────────────────────────────
    # Override install directory with $env:SKILLHUB_INSTALL_DIR;
    # default is %LOCALAPPDATA%\Programs\skillhub (user-scoped, no elevation needed).
    $InstallDir = if ($env:SKILLHUB_INSTALL_DIR) {
        $env:SKILLHUB_INSTALL_DIR
    } else {
        Join-Path $env:LOCALAPPDATA 'Programs\skillhub'
    }

    if (-not (Test-Path $InstallDir)) {
        New-Item -ItemType Directory -Path $InstallDir | Out-Null
    }

    $SrcBin = Join-Path $TempDir "$Binary.exe"
    $DstBin = Join-Path $InstallDir "$Binary.exe"
    Copy-Item -Path $SrcBin -Destination $DstBin -Force

    # Final install line goes to the success output stream (stream 1).
    # All other progress uses Write-Host (Information stream 6) so iwr | iex is unpolluted.
    Write-Output "Installed $Binary to $DstBin"

    # ── PATH hint ─────────────────────────────────────────────────────────────
    $UserPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if ($UserPath -notlike "*$InstallDir*") {
        $PathCmd = "[Environment]::SetEnvironmentVariable('Path', '$InstallDir;' + " +
                   "[Environment]::GetEnvironmentVariable('Path', 'User'), 'User')"
        Write-Host "Hint: add $InstallDir to your PATH (current user, no admin required):" `
            -ForegroundColor Yellow
        Write-Host "  $PathCmd" -ForegroundColor Yellow
    }
} finally {
    Remove-Item -Recurse -Force $TempDir -ErrorAction SilentlyContinue
}
