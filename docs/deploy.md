# Auto-deploy ke server (GitHub Actions)

Panduan ini untuk server produksi `wa.ruangsanad.com`. Setelah setup selesai,
setiap push/merge ke branch `main` akan otomatis di-build, dites, lalu di-deploy
ke server. Deploy juga bisa dijalankan manual dari tab **Actions** di GitHub,
termasuk untuk rollback ke versi lama.

Setup cukup dilakukan **sekali**. Bagian [Pemakaian sehari-hari](#pemakaian-sehari-hari)
menjelaskan apa yang dilakukan setelahnya.

## Daftar isi

1. [Cara kerja](#cara-kerja)
2. [Susunan server](#susunan-server)
3. [Setup sekali jalan](#setup-sekali-jalan)
4. [Pemakaian sehari-hari](#pemakaian-sehari-hari)
5. [Rollback dan restore backup](#rollback-dan-restore-backup)
6. [Troubleshooting](#troubleshooting)
7. [Risiko dan batasan](#risiko-dan-batasan)

## Cara kerja

```
push ke main  ─┐
               ├─> GitHub Actions: job "Build & test" (go build + go test)
Run workflow ──┘          │  gagal → berhenti, server tidak disentuh
                          ▼
                 job "Deploy": SSH ke server sebagai user "deploy"
                          │  key ini HANYA bisa menjalankan deploy.sh
                          ▼
     /opt/gowa-deploy/deploy.sh <commit-sha>   (di server, sebagai root via sudo)
       1. git fetch, cek commit memang bagian dari main
       2. checkout commit tersebut di /opt/gowa
       3. docker compose build  (container lama masih melayani)
       4. docker compose stop
       5. backup /opt/gowa/storages ke /var/backups/gowa (simpan 7 terakhir)
       6. docker compose up -d
       7. health check http://127.0.0.1:3000/
       8. gagal → otomatis kembali ke commit sebelumnya
```

File yang terlibat:

| File | Lokasi | Fungsi |
|---|---|---|
| `.github/workflows/deploy.yml` | repo | Workflow GitHub Actions |
| `deploy/deploy.sh` | repo (sumber) | Script deploy |
| `/opt/gowa-deploy/deploy.sh` | server | Salinan script yang benar-benar dijalankan |
| `/opt/gowa-deploy/compose.yml` | server saja | Konfigurasi Docker khusus server (port, path) |
| `/opt/gowa/src/.env` | server saja | Konfigurasi aplikasi, tidak pernah ada di Git |

Script di server sengaja **tidak** di-update otomatis dari repo. Alasannya:
script itu berjalan sebagai root. Kalau ikut di-update dari Git, siapa pun yang
bisa push ke `main` bisa menjalankan perintah root di server. Jika
`deploy/deploy.sh` di repo berubah, salin ulang manual (lihat langkah 2).

Downtime per deploy: hanya saat stop → backup → start, biasanya 5–30 detik
tergantung ukuran `storages/`. Build dilakukan sebelum container dihentikan.

## Susunan server

Hasil pengecekan server saat ini:

```
/opt/gowa                  clone repo ruangsanadindonesia-art/go-whatsapp-web-multidevice (branch main)
├── src/.env               konfigurasi aplikasi (di-.gitignore)
├── storages/              database sesi WhatsApp + chat (di-.gitignore)
└── statics/               media, QR (di-.gitignore)
/opt/gowa-deploy
└── compose.yml            build dari /opt/gowa, port 127.0.0.1:3000
```

Container: `gowa-deploy-whatsapp_go-1`. Reverse proxy (Nginx/Caddy) meneruskan
`wa.ruangsanad.com` ke `127.0.0.1:3000`, dan tidak perlu diubah.

## Setup sekali jalan

Jalankan semua perintah di server sebagai **root**, kecuali yang ditandai
"di GitHub". Kerjakan berurutan. **Jangan merge ke `main` sebelum langkah 8.**

### Langkah 1 — Cek prasyarat

```bash
command -v git docker curl flock sudo tar
docker compose version
sshd -T 2>/dev/null | grep -Ei '^(port|allowusers|allowgroups|pubkeyauthentication)'
```

- Semua perintah di baris pertama harus menampilkan path. Kalau ada yang tidak
  muncul: `apt-get update && apt-get install -y git curl util-linux sudo tar`.
- Catat nilai `port` (biasanya `22`). Nilai ini dipakai di langkah 7.
- Kalau muncul `allowusers ...`, user `deploy` harus ditambahkan ke daftar itu
  (lihat [Troubleshooting](#troubleshooting)).
- `pubkeyauthentication` harus `yes`.

Cek firewall:

```bash
ufw status 2>/dev/null
```

GitHub Actions masuk dari IP yang berubah-ubah. Port SSH harus terbuka untuk
umum (`22/tcp ALLOW Anywhere`). Kalau SSH dibatasi hanya untuk IP tertentu,
auto-deploy tidak bisa jalan. Buka dulu port-nya, atau lihat
[Risiko dan batasan](#risiko-dan-batasan).

### Langkah 2 — Pasang script deploy

Sebelum di-merge, script masih ada di branch kerja. Ambil dari sana:

```bash
curl -fsSL -o /tmp/deploy.sh \
  https://raw.githubusercontent.com/ruangsanadindonesia-art/go-whatsapp-web-multidevice/claude/project-deployment-updates-2hxkil/deploy/deploy.sh
less /tmp/deploy.sh            # baca dulu isinya, tekan q untuk keluar
install -o root -g root -m 0755 /tmp/deploy.sh /opt/gowa-deploy/deploy.sh
rm /tmp/deploy.sh
chmod 711 /opt/gowa-deploy     # user lain bisa melewati folder, tapi tidak bisa melihat isinya
```

Setelah branch ini di-merge, salinan resminya ada di `/opt/gowa/deploy/deploy.sh`.
Kalau nanti script itu diperbarui:

```bash
install -o root -g root -m 0755 /opt/gowa/deploy/deploy.sh /opt/gowa-deploy/deploy.sh
```

### Langkah 3 — Buat user `deploy`

```bash
adduser --disabled-password --gecos "" deploy
install -o deploy -g deploy -m 700 -d /home/deploy/.ssh
```

User ini tidak punya password dan **tidak** dimasukkan ke grup `docker`
(anggota grup docker setara root).

### Langkah 4 — Izinkan `deploy` menjalankan script, dan hanya script itu

```bash
echo 'deploy ALL=(root) NOPASSWD: /opt/gowa-deploy/deploy.sh' > /etc/sudoers.d/gowa-deploy
chmod 440 /etc/sudoers.d/gowa-deploy
visudo -cf /etc/sudoers.d/gowa-deploy
```

Perintah terakhir harus menampilkan `parsed OK`. Kalau error, hapus filenya
(`rm /etc/sudoers.d/gowa-deploy`) lalu ulangi. File sudoers yang rusak bisa
membuat `sudo` tidak bisa dipakai sama sekali.

### Langkah 5 — Buat SSH key khusus deploy

```bash
ssh-keygen -t ed25519 -N "" -C "github-actions-deploy" -f /root/gowa-deploy-key
```

Pasang public key-nya, dikunci supaya hanya bisa menjalankan `deploy.sh`:

```bash
printf 'command="/usr/bin/sudo -n /opt/gowa-deploy/deploy.sh \\"$SSH_ORIGINAL_COMMAND\\"",restrict %s\n' \
  "$(cat /root/gowa-deploy-key.pub)" > /home/deploy/.ssh/authorized_keys
chown deploy:deploy /home/deploy/.ssh/authorized_keys
chmod 600 /home/deploy/.ssh/authorized_keys
cat /home/deploy/.ssh/authorized_keys
```

Hasil `cat` harus berbentuk satu baris seperti ini:

```
command="/usr/bin/sudo -n /opt/gowa-deploy/deploy.sh \"$SSH_ORIGINAL_COMMAND\"",restrict ssh-ed25519 AAAA... github-actions-deploy
```

Artinya:
- `command=...`: apa pun perintah yang dikirim lewat key ini, yang dijalankan
  tetap `deploy.sh`. Teks yang dikirim hanya menjadi argumen, dan `deploy.sh`
  hanya menerima hash commit (huruf `0-9a-f`).
- `restrict`: mematikan port forwarding, terminal interaktif, dan sejenisnya.

### Langkah 6 — Uji dari server sendiri

Uji 1. Pastikan key terkunci. Perintah ini **tidak** me-restart apa pun:

```bash
ssh -i /root/gowa-deploy-key -o StrictHostKeyChecking=accept-new deploy@127.0.0.1 'ls /'
```

Hasil yang benar:

```
... ERROR: invalid commit sha: 'ls/' (only hex commit hashes are accepted)
```

Kalau yang muncul daftar folder, atau ada pertanyaan password sudo, berarti ada
yang salah. Ulangi langkah 4–5.

Uji 2. Deploy sungguhan (versi `main` terbaru). Container akan restart beberapa
detik:

```bash
ssh -i /root/gowa-deploy-key deploy@127.0.0.1
```

Hasil yang benar diakhiri `deploy OK: <hash> (<judul commit>)`. Cek juga:

```bash
ls -lh /var/backups/gowa/          # ada file storages-....tar.gz
docker ps | grep whatsapp_go       # status "Up ... seconds"
```

Buka `https://wa.ruangsanad.com/` dan pastikan device masih terhubung tanpa
scan QR ulang.

### Langkah 7 — Kumpulkan data untuk GitHub

```bash
# 1) IP publik server
curl -4 -s https://ifconfig.me; echo

# 2) Fingerprint server (ganti 22 jika port SSH berbeda, IP dari hasil di atas)
ssh-keyscan -p 22 IP_SERVER 2>/dev/null

# 3) Private key (disalin ke GitHub, lalu dihapus dari server di langkah 9)
cat /root/gowa-deploy-key
```

`ssh-keyscan` akan menampilkan 1–3 baris (`IP ssh-ed25519 ...`, `IP ssh-rsa ...`,
dst.). Salin **semua baris**.

### Langkah 8 — Isi Secrets di GitHub (di GitHub)

Buka repo → **Settings** → **Secrets and variables** → **Actions** →
**New repository secret**. Buat satu per satu:

| Nama secret | Isi | Wajib |
|---|---|---|
| `DEPLOY_HOST` | IP publik server dari langkah 7 | Ya |
| `DEPLOY_SSH_KEY` | Seluruh isi `/root/gowa-deploy-key`, dari `-----BEGIN OPENSSH PRIVATE KEY-----` sampai `-----END OPENSSH PRIVATE KEY-----` | Ya |
| `DEPLOY_KNOWN_HOSTS` | Semua baris hasil `ssh-keyscan` | Ya |
| `DEPLOY_PORT` | Port SSH, hanya jika bukan `22` | Tidak |
| `DEPLOY_USER` | Hanya jika user bukan `deploy` | Tidak |

Gunakan IP, bukan `wa.ruangsanad.com`, karena domain itu bisa saja melewati
proxy/CDN yang tidak meneruskan SSH.

Opsional: di **Settings → Environments → production** Anda bisa menambahkan
*Required reviewers*. Dengan itu setiap deploy menunggu tombol "Approve" dulu.
Environment `production` mungkin sudah ada dari workflow bawaan upstream.

### Langkah 9 — Hapus private key dari server

Setelah `DEPLOY_SSH_KEY` tersimpan di GitHub:

```bash
shred -u /root/gowa-deploy-key
```

Public key (`/root/gowa-deploy-key.pub`) boleh tetap ada. Kalau key ini nanti
perlu diganti, buat ulang dari langkah 5.

### Langkah 10 — Merge ke `main` (di GitHub)

Buat Pull Request dari branch `claude/project-deployment-updates-2hxkil` ke
`main`, lalu merge. Merge ini sendiri sudah menjadi deploy pertama:

1. Buka tab **Actions** → workflow **Deploy to Server**.
2. Job **Build & test** berjalan sekitar 2–4 menit, lalu **Deploy**.
3. Hijau = berhasil. Buka log job Deploy untuk melihat output `deploy.sh`.

Setup selesai.

## Pemakaian sehari-hari

**Deploy otomatis.** Merge atau push ke `main`. Tidak perlu melakukan apa-apa
lagi di server.

**Deploy manual / deploy ulang.** Tab **Actions** → **Deploy to Server** →
**Run workflow** → biarkan kolom SHA kosong → **Run workflow**.

**Lihat status dan log di server:**

```bash
tail -50 /var/log/gowa-deploy.log                         # riwayat deploy
docker compose -f /opt/gowa-deploy/compose.yml logs -f --tail=100   # log aplikasi
git -C /opt/gowa log --oneline -5                          # versi yang sedang jalan
```

**Deploy dari server tanpa GitHub** (misalnya saat GitHub Actions sedang down):

```bash
/opt/gowa-deploy/deploy.sh            # versi main terbaru
/opt/gowa-deploy/deploy.sh <sha>      # versi tertentu
```

Aturan yang berlaku:
- Hanya commit yang ada di riwayat `main` yang bisa di-deploy. Commit dari branch
  lain ditolak.
- Jangan edit file yang ter-track Git langsung di `/opt/gowa`. Deploy akan
  berhenti dengan pesan `local changes`. Semua perubahan kode lewat GitHub.
- `src/.env` boleh diedit langsung di server. Setelah edit, jalankan
  `docker compose -f /opt/gowa-deploy/compose.yml up -d`.

## Rollback dan restore backup

**Otomatis.** Kalau health check gagal setelah deploy, script kembali sendiri ke
commit sebelumnya dan workflow ditandai merah dengan pesan `rolled back to ...`.

**Manual ke versi tertentu.**
1. Cari SHA versi yang ingin dipakai: tab **Code** → **Commits**, atau
   `git -C /opt/gowa log --oneline -10` di server.
2. **Actions** → **Deploy to Server** → **Run workflow** → isi kolom SHA →
   **Run workflow**.

Rollback kode **tidak** mengembalikan database. Kalau versi baru mengubah
struktur database dan versi lama tidak bisa membacanya, restore juga backup
`storages`:

```bash
ls -lt /var/backups/gowa/                       # pilih backup sebelum deploy bermasalah
docker compose -f /opt/gowa-deploy/compose.yml stop
mv /opt/gowa/storages /opt/gowa/storages.broken-$(date +%Y%m%d-%H%M%S)
tar -xzf /var/backups/gowa/storages-YYYYMMDD-HHMMSS-xxxxxxx.tar.gz -C /opt/gowa
docker compose -f /opt/gowa-deploy/compose.yml up -d
```

Nama file backup berisi tanggal, jam, dan 7 karakter SHA versi yang berjalan
saat backup diambil. Restore backup juga mengembalikan semua pesan/chat yang
tersimpan ke kondisi saat backup. Data yang masuk setelah itu hilang dari
database lokal.

## Troubleshooting

| Gejala (di log Actions / server) | Penyebab | Solusi |
|---|---|---|
| `Missing secrets: ...` | Secret belum diisi | Langkah 8 |
| `Host key verification failed` | `DEPLOY_KNOWN_HOSTS` salah/kosong, atau server di-reinstall | Ulangi `ssh-keyscan` (langkah 7), perbarui secret |
| `Permission denied (publickey)` | Isi `DEPLOY_SSH_KEY` terpotong, atau `authorized_keys` salah | Pastikan baris BEGIN/END ikut tersalin. Cek langkah 5. Cek `AllowUsers` di `/etc/ssh/sshd_config` (tambahkan `deploy`, lalu `systemctl reload ssh`) |
| `Connection timed out` | Firewall / port salah | Langkah 1 (ufw), cek `DEPLOY_PORT` |
| `sudo: a password is required` | File sudoers belum benar | Langkah 4 |
| `another deploy is still running` | Dua deploy bersamaan (jarang terjadi, workflow sudah antre) | Tunggu, lalu jalankan ulang |
| `local changes in /opt/gowa` | Ada file ter-track yang diedit langsung di server | `git -C /opt/gowa status`. Pindahkan perubahan ke GitHub, atau buang dengan `git -C /opt/gowa checkout -- <file>` |
| `is not part of main history` | SHA dari branch lain | Merge dulu ke `main` |
| `build failed` | Error saat `docker compose build` (sering karena disk penuh) | `df -h`, `docker system df`, lalu `docker builder prune -f`. Container lama tetap jalan |
| `backup failed (disk full?)` | Disk penuh saat backup | Bersihkan disk. Versi lama sudah dijalankan ulang |
| `rolled back to ...` | Versi baru tidak merespons | Lihat log aplikasi di output Actions, perbaiki kode, push lagi |
| Job "Build & test" merah | Kode tidak bisa di-build atau test gagal | Perbaiki kode. Server tidak disentuh |

## Risiko dan batasan

- **Semua yang masuk `main` langsung live.** Termasuk tombol **Sync fork**
  di GitHub. Repo ini fork dari `aldinokemal/go-whatsapp-web-multidevice`, dan
  menekan Sync fork akan men-deploy perubahan upstream saat itu juga. Kalau ingin
  ada jeda, aktifkan *Required reviewers* (langkah 8, opsional).
- **Test bukan jaminan.** `go test` menangkap error kode, tapi tidak menguji
  koneksi ke WhatsApp sungguhan. Health check hanya memastikan server HTTP
  merespons, bukan bahwa device sudah login. Setelah deploy penting, buka
  dashboard dan cek status device.
- **Port SSH terbuka untuk umum.** Ini konsekuensi memakai runner GitHub.
  Mitigasinya: login password dimatikan untuk user `deploy`, key-nya dikunci ke
  satu script, dan disarankan memasang `fail2ban`. Kalau SSH harus dibatasi IP,
  alternatifnya adalah self-hosted runner di server (di luar cakupan panduan ini).
- **Key GitHub bocor = orang lain bisa memicu deploy.** Dampaknya terbatas pada
  me-redeploy commit yang sudah ada di `main`, tanpa akses shell. Kalau
  dicurigai bocor, hapus baris di `/home/deploy/.ssh/authorized_keys`, lalu
  ulangi langkah 5–9.
- **Backup ada di server yang sama.** Kalau server rusak total, backup ikut
  hilang. Salin `/var/backups/gowa` ke tempat lain secara berkala jika data chat
  penting.
