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

Setiap kolom input URL (terutama pada modal dialog penambahan desk) **WAJIB** menerapkan aturan validasi dan normalisasi berikut:

1. **Penamaan Kolom**:
   - Label atau placeholder kolom URL wajib menggunakan nama **"URL"** (misal: `"URL (e.g. https://host:8443)"`), **DILARANG** menggunakan `"Endpoint URL"`.
2. **Auto-Prepend `http://`**:
   - Jika pengguna hanya mengisi alamat host atau IP beserta port (misalnya: `"192.168.1.1:8443"`), sistem wajib secara otomatis mengubahnya menjadi `"http://192.168.1.1:8443"`.
   - Jika pengguna sudah menuliskan skema protocol (`http://` atau `https://`), protokol tersebut tidak boleh diubah.
3. **Pangkas Trailing Slash (`/`)**:
   - Jika terdapat slash penutup di akhir URL (misalnya `"http://192.168.1.1:8443/"`), karakter slash penutup wajib otomatis dihapus sehingga menjadi `"http://192.168.1.1:8443"`.
4. **Validasi Ketat: Hanya IP atau Domain Tanpa Endpoint / Path**:
   - URL yang dimasukkan wajib murni alamat IP (IPv4 / IPv6) atau nama domain/hostname yang valid, dengan port opsional.
   - **DILARANG KERAS** memuat path / endpoint (misal `/api`, `/endpoint`, `/remote`), query string (`?foo=bar`), atau fragment (`#hash`).
   - Gunakan fungsi utilitas baku `ValidateHostURL(raw string) (cleaned string, errStr string)`.
5. **Eksekusi Validasi pada Event `onblur` & Render Error Merah**:
   - Validasi dijalankan saat kursor berpindah keluar dari kolom input (`FocusLeft()` / `!Focused()`), serta saat tombol aksi submit / `Save Desk` diklik.
   - Jika terdapat error validasi, tampilkan teks pesan error berwarna merah (`#EF443B`) tepat di bawah kolom input URL.
   - Tombol submit wajib memblokir proses simpan selama URL belum valid.

---

## 🔢 3. Aturan Input Kolom GonDesk ID (Wajib Angka & Spacing Style Murni)

Setiap kolom input GonDesk ID (terutama pada modal dialog penambahan desk) **WAJIB** menerapkan aturan berikut:

1. **Wajib Hanya Angka (Numeric Only)**:
   - Nilai input wajib strictly berupa angka (`0`-`9`).
   - Karakter non-angka (huruf, spasi, simbol) wajib otomatis dibuang/difilter seketika (menggunakan utilitas `FilterOnlyDigits(s, maxLen)`).
2. **Spacing Setiap 3 Digit Berupa Style Murni (Bukan Karakter Spasi `" "`)**:
   - Tampilan visual setiap 3 digit harus memiliki jeda/jarak yang menyerupai spasi (misalnya `612  840  193`).
   - Spacing harus pas dan tidak terlalu renggang (gunakan `Width(28)` untuk masing-masing segmen 3 digit monospace dan `Gap(8)` pada layout container `ui.Row`).
   - **DILARANG KERAS** menyisipkan karakter spasi ASCII (`" "`) ke dalam nilai variabel string input!
   - Spasi antar digit harus berupa **style layout murni** (3 sub-input `TextInputBase` tanpa border di dalam container `ui.Row` berjarak `Gap(8)`).
   - Nilai variabel `NewPeerID` yang disimpan wajib hanya angka murni tanpa karakter spasi (misal: `"612840193"`).
3. **Navigasi & Autoflow Input**:
   - Saat pengguna mengetik hingga 3 digit pada segmen 1, fokus kursor otomatis berpindah ke segmen 2, dan dari segmen 2 ke segmen 3 dengan kursor diletakkan di akhir teks menggunakan `.SetTextSelection(len, len)`.
   - **Continuous Backspace**: Menggunakan `HandleInput(ev ui.InputEvent)` pada segmen 3 dan segmen 2 untuk menangkap penekanan `KeyBackspace` saat segmen kosong, sehingga penghapusan terus berlanjut mulus ke segmen sebelumnya (dari digit 9 ke 6 ke 1).
   - Operasi paste (tempel teks) secara otomatis memfilter hanya angka dan memecahnya ke masing-masing segmen secara proporsional.

---

## 🚫 4. Larangan Menampilkan Kembali Elemen yang Dihapus Pengguna
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
     - `header.go`: Top bar navigasi, identitas brand, alamat lokal (Your Address clickable), dan tombol Settings.
     - `banner.go`: Banner notifikasi persetujuan sesi remote masuk (*incoming prompt*).
     - `desks_view.go`: Tampilan utama daftar desk, filter tabs (All Desks, Favorites, Online), pencarian, dan kartu peer remote.
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

## 📐 11. Tata Letak Penuh `desks_view.go` & Alamat Lokal di Header

1. **Peniadaan Split Pane Samping**:
   - `left_pane` telah dihapus karena alamat ID lokal (*Your Address*) sudah dipindahkan secara permanen dan terpusat di top bar / header ([header.go](file:///Users/jefripunza/Documents/Projects/gondesk/internal/uiapp/header.go)).
   - Halaman utama remote desks kini menggunakan tampilan penuh melalui [desks_view.go](file:///Users/jefripunza/Documents/Projects/gondesk/internal/uiapp/desks_view.go) tanpa `ui.Split`.
2. **Larangan Menampilkan Nilai Ukuran Sementara**:
   - Teks angka ukuran debugging/sementara (seperti `%.0f px`) dilarang dimunculkan kembali setelah kebutuhan inspeksi selesai.

---

## 🧪 12. Prosedur Verifikasi Sebelum Commit

Setiap perubahan wajib lolos verifikasi berikut:
```bash
# 1. Analisis statis MyGo UI
go tool mygo vet .

# 2. Pengujian unit UI & integrasi Single-Port
go test -v ./...

# 3. Kompilasi bersih semua paket
go build -v ./...
```

