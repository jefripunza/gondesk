# GonDesk — Agent & Development Rules (`AGENT.md`)

Dokumen ini berisi aturan baku (**Engineering Rules & Guidelines**) yang **WAJIB** ditaati oleh semua AI Agent maupun pengembang saat memodifikasi codebase **GonDesk**.

---

## 🚨 1. Aturan Dialog Popup / Modal (PENTING)

### 📌 Aturan: Popup Tidak Boleh Tertutup Saat Klik di Luar Dialog
> **Semua dialog popup / modal di aplikasi GonDesk TIDAK BOLEH tertutup jika pengguna mengklik area luar dialog (backdrop / overlay).**
> Dialog hanya boleh tertutup jika pengguna secara eksplisit menekan tombol aksi di dalam dialog (seperti tombol **"Cancel"**, **"Close"**, atau **"Save"**).

### 🛠️ Mengapa Aturan Ini Berlaku?
Secara default pada MyGo (`github.com/egoist/mygo/ui`), fungsi `ui.Modal(c, &open, fn)` akan mengubah `*open = false` apabila pengguna mengklik backdrop atau menekan Escape. Hal ini berbahaya karena pengguna yang sedang mengetik data (misal: ID desk, nama, password) dapat kehilangan input secara tidak sengaja jika kursor mouse terpeleset ke luar batas dialog.

### 💻 Pola Implementasi Baku (Standard Pattern)
Gunakan method `.Dismissed()` dari `ui.Modal` untuk mengembalikan status `*open = true` apabila terjadi penutupan di luar tombol dialog:

```go
func (a *AppUI) renderMyModal(c *ui.Context, t *ui.Theme) {
    if ui.Modal(c, &a.MyModalOpen, func() {
        ui.Column(c).Width(380).Gap(12).Children(func() {
            ui.Text(c, "Judul Dialog").FontSize(16).Bold()

            // Form inputs ...
            ui.TextInput(c.Key("input-field"), &a.Field)

            ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
                // 1. Tombol Cancel: Menutup modal secara eksplisit
                if ui.Button(c, "Cancel").Clicked() {
                    a.MyModalOpen = false
                }

                // 2. Tombol Submit / Save: Menyimpan data & menutup modal
                if ui.PrimaryButton(c, "Save").Clicked() {
                    a.saveData()
                    a.MyModalOpen = false
                }
            })
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

## 🧭 2. Aturan Arsitektur Single-Port (`:8443`)

1. **Hanya 1 Nomor Port Jaringan**:
   - Seluruh protokol (**HTTPS REST API**, **WebSocket Signaling `/ws/signal`**, **TURNS TLS/TCP**, dan **STUN/TURN UDP**) harus berjalan di port tunggal yang sama (`cfg.ListenPort`, default `:8443`).
   - Dilarang membuka port tambahan terpisah untuk HTTP atau signaling.
2. **Multiplexing Non-Destruktif (`internal/netmux`)**:
   - Pengecekan TLS (`0x16`) dan prefix HTTP (`GET `, `POST `, dll.) wajib menggunakan `Peek()` pada buffer koneksi tanpa menghilangkan byte dari stream yang akan dikonsumsi oleh `crypto/tls`, `GoFiber`, atau `Pion TURN`.

---

## 🔀 3. Aturan Dual-Mode (`desktop` vs `server`)

1. **Satu Kode Sumber (Single Codebase)**:
   - Mode `desktop` dan `server` dikompilasi dari kode sumber yang sama.
2. **Isolasi Fitur Desktop**:
   - Semua fungsi atau antarmuka yang terkait dengan fitur remote desktop klien (*Me-remote* / *Di-remote*) wajib dibatasi oleh `cfg.ShowDesktopFeatures()`.
   - Pada mode server (`--mode=server` / `GONDESK_MODE=server`), seluruh fitur remote desktop harus disembunyikan (`hidden`) dan request incoming harus ditolak.

---

## 🎨 4. Aturan MyGo Native UI (`github.com/egoist/mygo/ui`)

1. **Larangan Menyimpan `ui.Element` dan `*ui.Context`**:
   - Dilarang menyimpan `ui.Element` atau `*ui.Context` ke dalam struct aplikasi, package variable, atau goroutine. State aplikasi hanya boleh berupa data primitif (`string`, `bool`, `int`), slice model SQLite, dan pointer window `*mygo.Window`.
2. **Kunci Unik pada Perulangan (`c.Key(...)`)**:
   - Setiap elemen interaktif atau kartu di dalam loop wajib memiliki `c.Key(id)` yang unik dan stabil antar-frame render.
3. **Thread Safety untuk Update UI**:
   - Mutasi state UI dari goroutine latar belakang (event WebSocket, heartbeat, dll.) wajib dibungkus dalam `win.Update(func() { ... })` agar dieksekusi dengan aman pada *main thread*.
4. **Pure Go (`CGO_ENABLED=0`)**:
   - Codebase harus bebas dari dependensi `cgo` untuk menjamin portabilitas dan kompilasi silang (*cross-compile*) instan ke macOS, Windows, dan Linux.

---

## 🧪 5. Prosedur Verifikasi Sebelum Commit

Setiap perubahan wajib lolos verifikasi berikut:
```bash
# 1. Analisis statis MyGo UI
go tool mygo vet .

# 2. Pengujian unit UI & integrasi Single-Port
go test -v ./...

# 3. Kompilasi bersih semua paket
go build -v ./...
```

