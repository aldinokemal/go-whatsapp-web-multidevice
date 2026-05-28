# PowerShell Script to Automate Commit, Push, and Version Tagging for engine_goWA
# Save this file as 'release.ps1' in the root folder of engine_goWA

$ErrorActionPreference = "Stop"

Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host "      AUTOMATION RELEASE SCRIPT - ENGINE_GOWA" -ForegroundColor Cyan
Write-Host "==========================================================" -ForegroundColor Cyan

# 1. Check Git status
Write-Host "[INFO] Memeriksa status perubahan file..." -ForegroundColor Yellow
$status = git status --porcelain
if ([string]::IsNullOrEmpty($status)) {
    Write-Host "[SUCCESS] Tidak ada perubahan file lokal yang terdeteksi. Melanjutkan ke menu tagging..." -ForegroundColor Green
} else {
    git status -s
    Write-Host ""
    
    # Prompt for Commit Message
    $commitMsg = Read-Host "Pesan Commit (Commit Message)"
    if ([string]::IsNullOrEmpty($commitMsg)) {
        Write-Host "[ERROR] Pesan commit tidak boleh kosong! Rilis dibatalkan." -ForegroundColor Red
        Exit
    }
    
    # Perform Git Commit & Push
    Write-Host "[INFO] Menambahkan file ke staging..." -ForegroundColor Yellow
    git add .
    
    Write-Host "[INFO] Melakukan commit..." -ForegroundColor Yellow
    git commit -m "$commitMsg"
    
    Write-Host "[INFO] Mengirim commit ke GitHub Fork (origin main)..." -ForegroundColor Yellow
    git push origin main
    Write-Host "[SUCCESS] Commit berhasil terunggah!" -ForegroundColor Green
}

# 2. Ask for Tag Versioning
Write-Host ""
$tagChoice = Read-Host "Apakah Anda ingin membuat tag versi baru? (y/n)"
if ($tagChoice.ToLower() -ne "y") {
    Write-Host "[SUCCESS] Selesai. Perubahan telah terunggah ke GitHub tanpa pembuatan tag baru." -ForegroundColor Green
    Exit
}

Write-Host "[INFO] Mengambil daftar tag terbaru dari GitHub..." -ForegroundColor Yellow
git fetch --tags --quiet

# Find latest tag
$latestTag = git describe --tags --abbrev=0 2>$null
if ($null -eq $latestTag -or $latestTag -eq "") {
    $latestTag = "v1.0.0"
    Write-Host "[INFO] Tidak ditemukan tag sebelumnya di repositori. Memulai versi dari default: $latestTag" -ForegroundColor Cyan
} else {
    Write-Host "[SUCCESS] Tag versi terakhir terdeteksi: $latestTag" -ForegroundColor Green
}

# Parse version numbers (assuming vX.Y.Z)
$versionClean = $latestTag.TrimStart('v')
$parts = $versionClean.Split('.')
if ($parts.Length -lt 3) {
    $parts = @("1", "0", "0")
}
$major = [int]$parts[0]
$minor = [int]$parts[1]
$patch = [int]$parts[2]

# Propose next versions
$nextPatch = "v$major.$minor.$($patch + 1)"
$nextMinor = "v$major.$($minor + 1).0"
$nextMajor = "v$($major + 1).0.0"

Write-Host ""
Write-Host "Pilih jenis pembaruan versi (Semantic Versioning):" -ForegroundColor Cyan
Write-Host "1. PATCH  ($nextPatch) - Untuk perbaikan bug internal / aman."
Write-Host "2. MINOR  ($nextMinor) - Untuk penambahan fitur baru."
Write-Host "3. MAJOR  ($nextMajor) - Untuk perubahan besar (breaking changes)."
Write-Host "4. CUSTOM - Tulis versi custom buatan sendiri."

$choice = Read-Host "Pilihan Anda (1-4)"
$newTag = ""

switch ($choice) {
    "1" { $newTag = $nextPatch }
    "2" { $newTag = $nextMinor }
    "3" { $newTag = $nextMajor }
    "4" { 
        $newTag = Read-Host "Masukkan versi custom Anda (misal v1.2.5)"
        if (-not $newTag.StartsWith("v")) {
            $newTag = "v" + $newTag
        }
    }
    Default {
        Write-Host "[ERROR] Pilihan tidak valid! Rilis dibatalkan." -ForegroundColor Red
        Exit
    }
}

if ([string]::IsNullOrEmpty($newTag)) {
    Write-Host "[ERROR] Versi baru tidak boleh kosong! Rilis dibatalkan." -ForegroundColor Red
    Exit
}

# Verify and Apply Tag
Write-Host ""
$confirm = Read-Host "[WARN] Anda akan merilis engine_goWA dengan tag versi: $newTag . Apakah Anda yakin? (y/n)"
if ($confirm.ToLower() -ne "y") {
    Write-Host "[ERROR] Pembuatan tag versi dibatalkan." -ForegroundColor Red
    Exit
}

Write-Host "[INFO] Membuat tag lokal $newTag ..." -ForegroundColor Yellow
git tag $newTag

Write-Host "[INFO] Mengirim tag $newTag ke GitHub Fork..." -ForegroundColor Yellow
git push origin $newTag

Write-Host "==========================================================" -ForegroundColor Green
Write-Host "🎉 Rilis Sukses! Versi $newTag Telah Terbit." -ForegroundColor Green
Write-Host "==========================================================" -ForegroundColor Green
Write-Host ""
Write-Host "Untuk memperbarui di aplikasi utama (masanas_wa_gateway):" -ForegroundColor Cyan
Write-Host "1. Nonaktifkan (komentari) direktif 'replace' di go.mod."
Write-Host "2. Jalankan perintah berikut di terminal masanas_wa_gateway:" -ForegroundColor Cyan
Write-Host "   go get github.com/viantow/engine-goWA/src@$newTag" -ForegroundColor Yellow
Write-Host "   go mod tidy" -ForegroundColor Yellow
Write-Host "   go build -o main.exe ./cmd/app/main.go" -ForegroundColor Yellow
Write-Host "==========================================================" -ForegroundColor Green
