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

## 2. Alur Kerja Rilis Versi Baru (Step-by-Step)

Ikuti 5 langkah terstruktur ini setiap kali Anda ingin merilis versi baru mesin WhatsApp:

### Langkah 1: Kembangkan & Uji Coba Lokal
1. Lakukan modifikasi kode di folder `engine_goWA/src`.
2. Uji langsung secara lokal menggunakan backend utama (memanfaatkan direktif `replace` di `go.mod`).
3. Pastikan semuanya berjalan lancar dan stabil di laptop Anda.

### Langkah 2: Commit & Push Kode ke GitHub Fork Anda
Buka terminal di folder **`engine_goWA`**:
```bash
# 1. Cek perubahan file
git status

# 2. Tambahkan perubahan ke staging area
git add .

# 3. Lakukan commit dengan pesan deskriptif
git commit -m "feat: add send document endpoint and fix memory leak"

# 4. Push commit tersebut ke branch main fork Anda di GitHub
git push origin main
```

### Langkah 3: Tentukan & Buat Tag Versi Baru
Sebelum membuat tag, cek daftar tag versi yang sudah ada di repositori untuk menentukan nomor versi selanjutnya:
```bash
# 1. Ambil daftar tag terbaru dari GitHub
git fetch --tags

# 2. Tampilkan semua tag yang ada
git tag -l
```
*Misalkan tag terakhir adalah `v1.1.0`, dan Anda menambahkan fitur baru (Minor), maka versi selanjutnya adalah `v1.2.0`.*

Jalankan perintah pembuatan tag di terminal folder **`engine_goWA`**:
```bash
# 3. Buat tag lokal baru
git tag v1.2.0

# 4. Dorong tag tersebut ke GitHub fork Anda
git push origin v1.2.0
```

### Langkah 4: Terapkan Versi Baru di Backend Utama (`masanas_wa_gateway`)
Setelah tag rilis sukses terunggah di GitHub, saatnya memperbarui dependensi di backend utama Anda:

1. Buka berkas `masanas_wa_gateway/go.mod`.
2. **Komentari sementara** direktif `replace` Anda (tambahkan `//` di awal):
   ```go
   // replace github.com/aldinokemal/go-whatsapp-web-multidevice => ../engine_goWA/src
   ```
3. Buka terminal di folder `masanas_wa_gateway`, lalu jalankan perintah `go get` dengan menyertakan tag versi baru Anda secara spesifik:
   ```bash
   go get github.com/viantow/engine-goWA/src@v1.2.0
   ```
   *(Go compiler otomatis akan mengunduh kode rilis `v1.2.0` dari GitHub fork Anda).*
4. Lakukan kompilasi produksi bersih untuk memverifikasi kestabilannya:
   ```bash
   go build -o main.exe ./cmd/app/main.go
   ```

### Langkah 5: Kembali ke Mode Development
Jika kompilasi produksi sukses dan stabil, Anda bisa mengaktifkan kembali direktif `replace` lokal Anda di `go.mod` (hapus tanda `//` komentar) untuk melanjutkan pengembangan harian dengan instan.

---

## 3. Cara Menjaga Sinkronisasi dengan Pengembang Asli (Upstream)

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

# 5. Buat tag PATCH baru untuk menandai update WhatsApp ini (misal v1.2.1)
git tag v1.2.1
git push origin v1.2.1
```
Dengan versi tag `v1.2.1` baru ini, Anda tinggal menjalankan `go get .../src@v1.2.1` di backend utama Anda untuk mendapatkan perbaikan WhatsApp terbaru secara instan.
