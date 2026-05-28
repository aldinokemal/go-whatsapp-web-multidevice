# Panduan Versi & Rilis Git: Manajemen Tagging engine_goWA

Panduan ini menjelaskan alur kerja profesional untuk mengedit, melakukan commit, melakukan push, serta mengelola **tagging versi (versioning)** pada repositori **`engine_goWA`** (fork Anda: `github.com/viantow/engine-goWA`). 

Dengan menerapkan panduan ini, Anda dapat meminimalkan risiko kerusakan kode (*breaking changes*) pada aplikasi **`masanas_wa_gateway`** ketika melakukan pembaruan mesin WhatsApp di masa depan.

---

## 1. Aturan Versi: Semantic Versioning (SemVer)

Modul `engine_goWA` menggunakan format penamaan versi standar industri: **`vMAJOR.MINOR.PATCH`** (contoh: `v1.2.1`).

| Tipe Rilis | Cara Penamaan | Kapan Digunakan? | Dampak pada Backend Gateway |
| :--- | :--- | :--- | :--- |
| **PATCH** | Naikkan angka ketiga<br>(e.g. `v1.1.0` ➡️ `v1.1.1`) | Perbaikan bug internal, optimasi kode, atau pembaruan minor dependency (`whatsmeow`) **tanpa** mengubah rute API atau struktur JSON data. | **Sangat Aman.** Backend hanya perlu di-build ulang tanpa perubahan kode. |
| **MINOR** | Naikkan angka kedua, reset angka ketiga<br>(e.g. `v1.1.2` ➡️ `v1.2.0`) | Penambahan fitur atau rute API baru (misal: menambahkan API `/send/document`) yang bersifat **backwards-compatible** (tidak merusak fitur lama). | **Aman.** Backend bisa mulai menulis kode untuk memakai fitur baru tersebut jika diperlukan. |
| **MAJOR** | Naikkan angka pertama, reset lainnya<br>(e.g. `v1.2.5` ➡️ `v2.0.0`) | Perubahan besar yang merusak kompatibilitas (*breaking changes*), seperti merombak total sistem autentikasi, mengubah skema database secara masif, atau mengubah nama payload JSON. | **Perlu Refactoring.** Kode di backend utama harus diperiksa dan disesuaikan secara hati-hati agar tidak error. |

---

## 2. Aturan Penting Go Modules: Subdirectory Tagging (`src/vX.Y.Z`)

> [!IMPORTANT]
> **Aturan Subdirektori Go Modules:**
> Karena file `go.mod` di repositori `engine_goWA` berada di dalam subdirektori **`src/`** (bukan di root folder), sistem kompilasi Go mewajibkan seluruh Git tag untuk **diberikan awalan folder tersebut**.
> * **SALAH**: `v8.5.2` (Go compiler tidak akan mendeteksi modul Anda).
> * **BENAR**: **`src/v8.5.2`** (Go compiler mengenali modul di dalam subdirektori `/src` secara sempurna).
> 
> *Script otomatis `./release.ps1` yang telah kami sediakan secara cerdas akan langsung menangani format awalan `src/` ini di balik layar.*

---

## 3. Alur Kerja Rilis Versi Baru (Step-by-Step)

Ikuti 5 langkah terstruktur ini setiap kali Anda ingin merilis versi baru mesin WhatsApp:

### Langkah 1: Kembangkan & Uji Coba Lokal
1. Lakukan modifikasi kode di folder `engine_goWA/src`.
2. Uji langsung secara lokal menggunakan backend utama (memanfaatkan direktif `replace` lokal di `go.mod`).

### Langkah 2: Jalankan Script Otomatis `release.ps1`
Buka terminal PowerShell di folder **`engine_goWA`** dan jalankan:
```powershell
./release.ps1
```
*Script akan memandu Anda secara interaktif untuk melakukan Git commit, push ke GitHub, menghitung versi kenaikan (SemVer), melakukan tagging berawalan `src/`, dan mempublikasikannya ke repositori GitHub fork Anda.*

### Langkah 3: Terapkan Versi Baru di Backend Utama (`masanas_wa_gateway`)
Setelah tag rilis sukses terunggah (misal versi `v8.5.2` dengan tag `src/v8.5.2`):

1. Buka berkas `masanas_wa_gateway/go.mod`.
2. Ubah baris `replace` yang mengarah ke lokal Anda menjadi **mengarah ke modul GitHub fork Anda** dengan tag versi yang baru:
   ```go
   replace github.com/aldinokemal/go-whatsapp-web-multidevice => github.com/viantow/engine-goWA/src v8.5.2
   ```
3. Buka terminal di folder `masanas_wa_gateway` dan jalankan:
   ```bash
   go mod tidy
   go build -o main.exe ./cmd/app/main.go
   ```
   *(Go compiler otomatis akan mengunduh kode rilis `v8.5.2` dari GitHub fork Anda dan mengkompilasi file rilis `main.exe` yang bersih).*

---

## 4. Cara Menjaga Sinkronisasi dengan Pengembang Asli (Upstream)

Sesekali, developer asli (`aldinokemal`) akan merilis update penting untuk mengatasi perubahan protokol WhatsApp API (Meta). Anda harus menarik update tersebut ke fork Anda agar mesin WhatsApp Anda tetap berfungsi normal.

Jalankan rangkaian perintah berikut di terminal folder **`engine_goWA`**:

```bash
# 1. Ambil commit terbaru dari developer asli
git fetch upstream

# 2. Gabungkan update tersebut ke dalam branch main Anda
git merge upstream/main

# 3. Jika ada konflik (conflict), selesaikan konflik tersebut lalu commit
# 4. Push hasil penggabungan ke GitHub fork Anda
git push origin main

# 5. Buat tag PATCH baru untuk menandai update WhatsApp ini (misal v8.5.3 dengan tag src/v8.5.3)
# (Sangat direkomendasikan menjalankan ./release.ps1 untuk kalkulasi tag otomatis ini)
```
Dengan versi tag `v8.5.3` baru ini, Anda tinggal memperbarui baris `replace` di `masanas_wa_gateway/go.mod` ke `v8.5.3` dan menjalankan `go mod tidy`!
