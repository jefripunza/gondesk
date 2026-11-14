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
- Jika pengguna telah meminta menghapus suatu elemen antarmuka (misalnya judul teks `"Discovered & Saved Desks"` di atas daftar tab), agen **DILARANG KERAS** memunculkan atau menuliskan kembali elemen tersebut di siklus modifikasi selanjutnya.

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

## 🔀 6. Aturan Dual-Mode (`desktop` vs `server`)

1. **Satu Kode Sumber (Single Codebase)**:
   - Mode `desktop` dan `server` dikompilasi dari kode sumber yang sama.
2. **Isolasi Fitur Desktop**:
   - Semua fungsi atau antarmuka yang terkait dengan fitur remote desktop klien (*Me-remote* / *Di-remote*) wajib dibatasi oleh `cfg.ShowDesktopFeatures()`.
   - Pada mode server (`--mode=server` / `GONDESK_MODE=server`), seluruh fitur remote desktop harus disembunyikan (`hidden`) dan request incoming harus ditolak.

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

## 🧪 8. Prosedur Verifikasi Sebelum Commit

Setiap perubahan wajib lolos verifikasi berikut:
```bash
# 1. Analisis statis MyGo UI
go tool mygo vet .

# 2. Pengujian unit UI & integrasi Single-Port
go test -v ./...

# 3. Kompilasi bersih semua paket
go build -v ./...
```

