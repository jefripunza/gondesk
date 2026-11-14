# GonDesk — Agent & Development Rules (`AGENT.md`)

Dokumen ini berisi aturan baku (**Engineering Rules & Guidelines**) yang **WAJIB** ditaati oleh semua AI Agent maupun pengembang saat memodifikasi codebase **GonDesk**.

---

## 🚨 1. Aturan Dialog Popup / Modal (PENTING)

### 📌 Aturan: Popup Tidak Boleh Tertutup Saat Klik di Luar Dialog
> **Semua dialog popup / modal di aplikasi GonDesk TIDAK BOLEH tertutup jika pengguna mengklik area luar dialog (backdrop / overlay).**
> Dialog hanya boleh tertutup jika pengguna secara eksplisit menekan tombol aksi di dalam dialog (seperti tombol **"Cancel"**, **"Close"**, atau **"Save"**).

### 🛠️ Mengapa Aturan Ini Berlaku?
Secara default pada MyGo (`github.com/egoist/mygo/ui`), fungsi `ui.Modal(c, &open, fn)` akan mengubah `*open = false` apabila pengguna mengklik backdrop atau menekan Escape. Hal ini berbahaya karena pengguna yang sedang mengetik data (misal: ID desk, nama, password) dapat kehilangan input secara tidak sengaja jika kursor mouse terpeleset ke luar batas dialog.

### 💻 Pola Implementasi Baku (Standard Pattern: Dark Backdrop via `ui.DialogBase`)
Gunakan `ui.DialogBase` dengan styling `backdrop.Background(ui.RGBA(0, 0, 0, 0.85))` agar efek overlay gelap pekat (menggantikan default `ui.Modal` yang terlalu terang/transparan), serta method `.Dismissed()` untuk mengembalikan status `*open = true` apabila terjadi penutupan di luar tombol dialog:

```go
func (a *AppUI) renderMyModal(c *ui.Context, t *ui.Theme) {
    if ui.DialogBase(c, &a.MyModalOpen, func(backdrop, panel ui.Element) {
        // 1. Overlay backdrop gelap pekat (opacity ~85%)
        backdrop.Background(ui.RGBA(0, 0, 0, 0.85))

        // 2. Styling panel popup
        panel.Width(390).Padding(20).Gap(14).Radius(12).
            Background(t.Surface).Border(1, t.Border).
            Shadow(0, 16, 40, 0, ui.RGBA(0, 0, 0, 0.7))

        ui.Text(c, "Judul Dialog").FontSize(16).Bold()

        // Form inputs & onblur validation...
        urlIn := ui.TextInput(c.Key("input-field"), &a.Field)
        if urlIn.FocusLeft() || (a.fieldFocused && !urlIn.Focused()) {
            a.Field = CleanURL(a.Field)
        }
        a.fieldFocused = urlIn.Focused()

        ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
            // Tombol Cancel: Menutup modal secara eksplisit
            if ui.Button(c, "Cancel").Clicked() {
                a.MyModalOpen = false
            }

            // Tombol Submit / Save: Menyimpan data & menutup modal
            if ui.PrimaryButton(c, "Save").Clicked() {
                a.saveData()
                a.MyModalOpen = false
            }
        })
    }).Dismissed() {
        // PERTAHANKAN MODAL TETAP TERBUKA JIKA DIKLIK DI LUAR
        // Clicks pada backdrop luar mengaktifkan Dismissed(), kita setel kembali ke true
        // sehingga dialog TIDAK AKAN tertutup kecuali tombol Cancel/Save di dalam diklik.
        a.MyModalOpen = true
    }
}
```

---

## 🔗 2. Aturan Validasi & Normalisasi URL Input (`onblur`)

Setiap kolom input URL (baik pada form koneksi cepat maupun dialog penambahan desk) **WAJIB** menerapkan aturan normalisasi berikut:

1. **Auto-Prepend `http://`**:
   - Jika pengguna hanya mengisi alamat host atau IP beserta port (misalnya: `"192.168.1.1:8443"`), sistem wajib secara otomatis mengubahnya menjadi `"http://192.168.1.1:8443"`.
   - Jika pengguna sudah menuliskan skema protocol (`http://` atau `https://`), protokol tersebut tidak boleh diubah.
2. **Pangkas Trailing Slash (`/`)**:
   - Jika terdapat slash penutup di akhir URL (misalnya `"http://192.168.1.1:8443/"` atau `"192.168.1.1:8443/"`), karakter slash penutup wajib otomatis dihapus sehingga menjadi `"http://192.168.1.1:8443"`.
3. **Eksekusi Normalisasi pada Event `onblur` & Submit**:
   - Normalisasi harus terjadi saat kursor berpindah keluar dari kolom input (`FocusLeft()` / `!Focused()`), serta saat tombol aksi submit / `Connect` diklik.
   - Gunakan fungsi utilitas baku `CleanURL(raw string) string`.

---

## 🚫 3. Larangan Menampilkan Kembali Elemen yang Dihapus Pengguna
- Jika pengguna telah meminta menghapus suatu elemen antarmuka, agen **DILARANG KERAS** memunculkan atau menuliskan kembali elemen tersebut di siklus modifikasi selanjutnya.
- Elemen-elemen yang **WAJIB TETAP DIHAPUS**:
  1. **Judul teks `"Discovered & Saved Desks"`** di atas daftar filter tabs.
  2. **Badge status `"1-Port HTTPS + TURN"`** di pojok kanan top bar.
  3. **Tombol tab navigasi `"New Connection"`** di top bar.
  4. **Tombol mode `"Switch to Server Mode"`** di top bar.
  5. **Teks indikator angka ukuran sementara (seperti `%.0f px`)** di bawah card.
  6. **Data seeder dummy / demo peers** di database SQLite.

---

## 📄 4. Penamaan File Rules: HANYA `AGENT.md` (Dilarang Ada `AGENTS.md`)
> **Aturan Mutlak:** File dokumentasi aturan untuk AI Agent di proyek ini **HANYA BOLEH BERNAMA `AGENT.md`** (singular).
> **DILARANG KERAS** membuat, menduplikasi, atau menyalin file menjadi `AGENTS.md` (plural) di dalam repository ini.
> Jika file `AGENTS.md` ditemukan, file tersebut **wajib segera dihapus**. Seluruh aturan proyek hanya boleh dicatat, dirujuk, dan diperbarui di file `AGENT.md`.

---

## 🧭 5. Aturan Arsitektur Single-Port (`:8443`)

1. **Hanya 1 Nomor Port Jaringan**:
   - Seluruh protokol (**HTTPS REST API**, **WebSocket Signaling `/ws/signal`**, **TURNS TLS/TCP**, dan **STUN/TURN UDP**) harus berjalan di port tunggal yang sama (`cfg.ListenPort`, default `:8443`).
   - Dilarang membuka port tambahan terpisah untuk HTTP atau signaling.
2. **Multiplexing Non-Destruktif (`internal/netmux`)**:
   - Pengecekan TLS (`0x16`) dan prefix HTTP (`GET `, `POST `, dll.) wajib menggunakan `Peek()` pada buffer koneksi tanpa menghilangkan byte dari stream yang akan dikonsumsi oleh `crypto/tls`, `GoFiber`, atau `Pion TURN`.

---

## 🔀 6. Aturan Desktop Peer-to-Peer & Headless Server Bridge

1. **Desktop App Murni Peer-to-Peer (Tanpa Tombol Switch Server di UI)**:
   - Aplikasi desktop GonDesk berfokus murni pada interaksi remote desktop peer-to-peer antar aplikasi desktop.
   - **Dilarang keras menyertakan tombol "Switch to Server Mode"** atau menampilkan dashboard server mode di antarmuka desktop.
2. **Server Mode untuk Headless Bridge (Deployment Docker / VPS)**:
   - Server mode dijalankan di server / Docker secara headless (`GONDESK_MODE=server`, `GONDESK_HEADLESS=true`) sebagai sentral bridge relay (HTTPS, WebSocket signaling, Pion TURN).
   - Pengguna desktop yang terhubung ke alamat IP/domain server bridge yang sama dapat saling terhubung peer-to-peer menggunakan ID GonDesk masing-masing.
3. **Aturan Environment Docker (HANYA `PORT` yang Boleh Dikonfigurasi)**:
   - Nilai default server mode (`GONDESK_MODE=server`, `GONDESK_HEADLESS=true`, `PORT=8443`) ditetapkan langsung di dalam `Dockerfile`.
   - Pada `docker-compose.yml` atau saat deployment container, **kurangi konfigurasi env: HANYA `PORT` yang boleh di-set/diubah**. Dilarang mendefinisikan ulang variabel lingkungan lain yang sudah memiliki nilai default baku di Dockerfile.

---

## 🎨 7. Aturan MyGo Native UI (`github.com/egoist/mygo/ui`)

1. **Larangan Menyimpan `ui.Element` dan `*ui.Context`**:
   - Dilarang menyimpan `ui.Element` atau `*ui.Context` ke dalam struct aplikasi, package variable, atau goroutine. State aplikasi hanya boleh berupa data primitif (`string`, `bool`, `int`), slice model SQLite, dan pointer window `*mygo.Window`.
2. **Kunci Unik pada Perulangan (`c.Key(...)`)**:
   - Setiap elemen interaktif atau kartu di dalam loop wajib memiliki `c.Key(id)` yang unik dan stabil antar-frame render.
3. **Thread Safety untuk Update UI**:
   - Mutasi state UI dari goroutine latar belakang (event WebSocket, heartbeat, dll.) wajib dibungkus dalam `win.Update(func() { ... })` agar dieksekusi dengan aman pada *main thread*.
4. **Pure Go (`CGO_ENABLED=0`)**:
   - Codebase harus bebas dari dependensi `cgo` untuk menjamin portabilitas dan kompilasi silang (*cross-compile*) instan ke macOS, Windows, dan Linux.

---

## 📦 8. Aturan Penamaan & Format Asset Release GitHub

1. **Dilarang Menyertakan Format `.tar.gz` dan `.zip`**:
   - Release assets pada GitHub Release **TIDAK BOLEH** menyertakan arsip `.tar.gz` maupun `.zip`.
2. **Format Baku Penamaan File Asset**:
   - Semua file asset rilis **WAJIB** mengikuti format seragam:
     ```text
     gondesk-[version]-[os]-[arch].[ext]
     ```
   - Rincian penamaan baku untuk seluruh OS:
     - **Linux DEB**: `gondesk-[version]-linux-amd64.deb`, `gondesk-[version]-linux-arm64.deb`
     - **Linux RPM**: `gondesk-[version]-linux-amd64.rpm`, `gondesk-[version]-linux-arm64.rpm`
     - **Windows EXE**: `gondesk-[version]-windows-amd64.exe`, `gondesk-[version]-windows-arm64.exe`
     - **macOS DMG**: `gondesk-[version]-macos-universal.dmg`, `gondesk-[version]-macos-arm64.dmg`, `gondesk-[version]-macos-amd64.dmg`

---

## 🧩 9. Modularitas Arsitektur UI (Larangan File Bloated)

1. **Pemisahan File Berdasarkan Konteks Antarmuka**:
   - Dilarang keras menumpuk seluruh fungsi antarmuka dalam satu file besar (*bloated file*).
   - Setiap komponen UI yang memiliki konteks atau tanggung jawab berbeda **WAJIB** dipisahkan ke dalam file `.go` tersendiri di dalam package `internal/uiapp/`:
     - `app_ui.go`: Definisi struct `AppUI`, inisialisasi state, lifecycle, frame builder utama `View`, dan helper `CleanURL`.
     - `header.go`: Top bar navigasi, identitas brand, tab navigasi, dan status single-port.
     - `banner.go`: Banner notifikasi persetujuan sesi remote masuk (*incoming prompt*).
     - `left_pane.go`: Panel kiri / sidebar (Your Address 9-digit, form Quick Connect).
     - `right_pane.go`: Panel kanan / content (Buku alamat peer, filter tabs, pencarian, kartu peer).
     - `modals.go`: Dialog modal popup (dialog tambah desk dan dialog konfirmasi hapus).
     - `active_session.go`: Viewport remote desktop canvas GPU dan sidebar chat/telemetri.
     - `settings.go`: Pengaturan desktop dengan navigasi sidebar menu (`General`, `Network`, `About`).
2. **Kaidah Penambahan Fitur UI Baru**:
   - Setiap kali membuat komponen antarmuka baru dengan konteks berbeda, **wajib dibuatkan file `.go` baru** di package `internal/uiapp/` dengan nama yang deskriptif.

---

## 🚫 10. Larangan Auto-Seeder Dummy / Demo Peers

1. **Daftar Buku Alamat Murni Milik Pengguna**:
   - Sistem **DILARANG KERAS** melakukan auto-seeding data dummy / demo peer ke dalam database SQLite (`peer_records`).
   - Jika belum ada data atau pengguna telah menghapus semua desk, biarkan daftar buku alamat kosong (*empty state*: *"No matching desks found in SQLite Address Book"*).
2. **Pengecualian Desk Lokal Sendiri ("This Desk")**:
   - Desk lokal milik perangkat sendiri (`a.DeskID`) dilarang dimasukkan atau dimunculkan pada daftar `Remote Desks` / buku alamat.

---

## 📐 12. Batasan Rentang Lebar Split Pane (`ui.Split`)

1. **Batas Rentang Lebar Left Pane (Min 280px, Max 490px)**:
   - Panel kiri (`left_pane`) yang dapat di-resize menggunakan `ui.Split` **WAJIB dibatasi dengan rentang 280px s.d. 490px** (`mentok minimal 280px, maksimal 490px`).
   - Lebar panel kiri dilarang mengecil di bawah 280px agar konten (kartu *Your Address*, 9 digit ID, tombol) tidak terhimpit menjadi teks vertikal per karakter.
   - Lebar panel kiri dilarang membesar melebihi 490px agar tidak mendominasi layout desktop remote desks.
   - Terapkan pembatasan clamping pada nilai split (`if a.LeftPaneWidth < 280 { a.LeftPaneWidth = 280 } else if a.LeftPaneWidth > 490 { a.LeftPaneWidth = 490 }`) serta atribut `.MinWidth(280).MaxWidth(490)` pada elemen root scroll panel kiri.
2. **Larangan Menampilkan Nilai Ukuran Sementara**:
   - Teks angka ukuran debugging/sementara (seperti `%.0f px`) di bawah card dilarang dimunculkan kembali setelah kebutuhan inspeksi selesai.

---

## 🧪 13. Prosedur Verifikasi Sebelum Commit

Setiap perubahan wajib lolos verifikasi berikut:
```bash
# 1. Analisis statis MyGo UI
go tool mygo vet .

# 2. Pengujian unit UI & integrasi Single-Port
go test -v ./...

# 3. Kompilasi bersih semua paket
go build -v ./...
```

