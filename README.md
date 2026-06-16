# Generator Kartu Otomatis

Aplikasi web untuk generate kartu identitas (KTP-size) secara massal dari template SVG dan data CSV, menggunakan Inkscape sebagai renderer.

## Fitur

- Upload template desain SVG (Inkscape)
- Upload data massal via CSV
- Inject teks dan gambar (logo, foto, QR code) ke template SVG
- Output PNG 300 DPI ukuran KTP — Portrait (638×1012 px) atau Landscape (1012×638 px)
- Unduh satu per satu, dipilih, atau semua dalam ZIP
- Auto-generate config YAML dari elemen SVG

## Struktur Proyek

```
kartu-madrasah/
├── main.go
├── go.mod
├── go.sum
├── handler/
│   ├── handler.go
│   ├── template.go
│   ├── generate.go
│   └── download.go
├── job/
│   └── store.go
├── svg/
│   ├── extract.go
│   ├── inject.go
│   └── inkscape.go
├── csvutil/
│   └── parse.go
├── static/
│   ├── index.html
│   ├── style.css
│   ├── script.js
│   └── favicon.ico
├── template/
│   ├── desain.svg
│   └── data.csv
└── scripts/
    ├── build.sh
    ├── install.sh
    ├── uninstall.sh
    └── generate.sh
```

## Prasyarat

- Linux (Debian/Ubuntu atau RHEL/CentOS)
- Go 1.24+
- Inkscape (CLI)

## Instalasi

```bash
wget -O install.sh https://raw.githubusercontent.com/zulfikriyahya/kartu-madrasah/main/scripts/install.sh
chmod +x install.sh
sudo ./install.sh
```

Script otomatis mendeteksi package manager (apt/dnf/yum) dan arsitektur CPU (amd64/arm64/armv7), menginstall dependensi, build binary, dan mendaftarkan systemd service.

Port default: `8090`. Ganti dengan environment variable:

```bash
sudo PORT=9000 ./install.sh
```

## Uninstall

```bash
wget -O uninstall.sh https://raw.githubusercontent.com/zulfikriyahya/kartu-madrasah/main/scripts/uninstall.sh
chmod +x uninstall.sh
sudo ./uninstall.sh
```

Menghapus service systemd dan semua file di `/opt/kartu-madrasah`.

## Build Manual (Multi-Arch)

```bash
chmod +x scripts/build.sh
./scripts/build.sh
```

Output binary tersimpan di folder `dist/`:

| File | Target |
|------|--------|
| `kartu-madrasah_linux_amd64` | x86_64 |
| `kartu-madrasah_linux_arm64` | ARM 64-bit |
| `kartu-madrasah_linux_armv7` | ARM 32-bit |

## Manajemen Service

```bash
systemctl status kartu-madrasah
systemctl restart kartu-madrasah
systemctl stop kartu-madrasah
journalctl -u kartu-madrasah -f
```

## Template Data CSV

| Kolom | Keterangan |
|-------|------------|
| `nama_instansi` | Nama instansi/organisasi |
| `nama_kartu` | Jenis kartu |
| `uid_kartu` | ID unik kartu |
| `nama_user` | Nama lengkap pengguna |
| `username` | Username (digunakan untuk mencocokkan file gambar) |
| `hp_instansi` | Nomor telepon instansi |
| `ig_instansi` | Instagram instansi |
| `email_instansi` | Email instansi |
| `web_instansi` | Website instansi |
| `alamat_instansi` | Alamat instansi |
| `foto_user` | Nama file foto user |
| `qr_user` | Nama file QR code user |

## Placeholder SVG

Tambahkan placeholder berikut pada elemen teks di file SVG:

```
%VAR_nama_instansi%
%VAR_nama_kartu%
%VAR_uid_kartu%
%VAR_nama_user%
%VAR_username%
%VAR_hp_instansi%
%VAR_ig_instansi%
%VAR_email_instansi%
%VAR_web_instansi%
%VAR_alamat_instansi%
```

## Elemen Gambar SVG

Beri `id` berikut pada elemen `<rect>` atau `<image>` di SVG:

| ID | Gambar |
|----|--------|
| `logo_instansi` | Logo instansi (satu file, berlaku global) |
| `foto_user` | Foto per user (nama file = username) |
| `qr_user` | QR code per user (nama file = username) |

File gambar bisa diupload satu per satu atau dalam satu file ZIP.

## API Endpoint

Base URL: `http://localhost:8090`

| Method | Endpoint | Keterangan |
|--------|----------|------------|
| GET | `/api/template-csv` | Unduh template CSV |
| GET | `/api/template-svg` | Unduh template SVG |
| POST | `/api/extract-config` | Generate config YAML dari SVG |
| POST | `/api/generate` | Mulai generate kartu |
| GET | `/api/status?job_id=` | Cek status job |
| GET | `/api/list?job_id=` | Daftar file hasil |
| GET | `/api/download?job_id=&file=` | Unduh satu file |
| POST | `/api/download-zip` | Unduh file terpilih sebagai ZIP |

## Lisensi

MIT
