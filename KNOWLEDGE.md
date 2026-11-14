# GonDesk — Architecture & Engineering Knowledge Base (`KNOWLEDGE.md`)

Dokumen ini merupakan pusat dokumentasi teknis (**Knowledge Base**) untuk proyek **GonDesk**. Seluruh arsitektur, alur kerja internal, protokol jaringan *Single-Port*, skema database SQLite, hierarki **MyGo Native UI**, hingga *pipeline* CI/CD dituangkan secara menyeluruh di sini.

---

## 📑 Daftar Isi

1. [Gambaran Umum & Filosofi Desain](#1-gambaran-umum--filosofi-desain)
2. [Peta Modul & Struktur Direktori](#2-peta-modul--struktur-direktori)
3. [Arsitektur Single-Port Multiplexer (`internal/netmux`)](#3-arsitektur-single-port-multiplexer-internalnetmux)
4. [Mekanisme Dual-Mode dalam Satu Source Code (`desktop` vs `server`)](#4-mekanisme-dual-mode-dalam-satu-source-code-desktop-vs-server)
5. [Alur Koneksi, Signaling WebSocket & Sesi Remote (`internal/remote` & `internal/server`)](#5-alur-koneksi-signaling-websocket--sesi-remote-internalremote--internalserver)
6. [Skema Database GORM SQLite (`internal/database`)](#6-skema-database-gorm-sqlite-internaldatabase)
7. [Hierarki & Siklus Hidup MyGo Native UI (`internal/uiapp`)](#7-hierarki--siklus-hidup-mygo-native-ui-internaluiapp)
8. [Spesifikasi REST API & Protokol WebSocket](#8-spesifikasi-rest-api--protokol-websocket)
9. [Arsitektur Docker & CI/CD Auto-Versioning (`.github/workflows/release.yml`)](#9-arsitektur-docker--cicd-auto-versioning-githubworkflowsreleaseyml)
10. [Panduan Pengujian & Analisis Statis](#10-panduan-pengujian--analisis-statis)

---

## 1. Gambaran Umum & Filosofi Desain

**GonDesk** dirancang sebagai aplikasi *Remote Desktop* lintas platform (seperti AnyDesk) sekaligus *Global Bridge / TURN Relay Server* yang ditulis sepenuhnya dalam bahasa **Go murni (`CGO_ENABLED=0`)**.

### Empat Pilar Utama GonDesk

1. **Pure-Go Native GPU UI (`github.com/egoist/mygo/ui`)**:
   - Tidak menggunakan HTML, CSS, JavaScript, Node/Bun, ataupun WebView.
   - Antarmuka digambar langsung di atas GPU menggunakan **Metal** (macOS), **Direct3D 11** (Windows), dan **OpenGL / GTK 3** (Linux).
2. **Satu Source Code untuk Dua Bentuk Aplikasi (`desktop` & `server`)**:
   - **Mode `desktop` (Default)**: Berfungsi sebagai klien pengendali (*Me-remote* / *Remote Desk*) sekaligus host yang dapat dikendalikan (*Di-remote* / *Your Address*).
   - **Mode `server` (`desktop-server`)**: Berfungsi sebagai jembatan jaringan global (*Global Bridge & TURN Server*). Menggunakan binary dan kode sumber yang sama persis, tetapi seluruh fitur UI remote desktop disembunyikan (`ShowDesktopFeatures() == false`). Dapat berjalan dengan GUI dashboard maupun `--headless` di Docker/VPS.
3. **Koneksi Antar-Aplikasi Hanya Menggunakan 1 Port (`:8443`) + HTTPS**:
   - Seluruh trafik **HTTPS REST API**, **WebSocket Signaling (`wss://`)**, **STUN Binding**, **TURNS (`turns:` TCP/TLS)**, dan **TURN Relay (`turn:` UDP)** berjalan serentak di **1 nomor port yang sama** (default `:8443`).
4. **Zero External Dependencies (Embedded SQLite + Auto TLS)**:
   - Menggunakan driver SQLite Go murni (`github.com/glebarez/sqlite` + `gorm.io/gorm`) dan otomatis menghasilkan sertifikat TLS X.509 ECDSA P-256 (`certs/server.crt` & `certs/server.key`) saat pertama kali dijalankan.

```mermaid
flowchart TB
    subgraph ClientA["GonDesk App A (Mode: desktop)"]
        UIA["MyGo Native UI\n(Your Address + Quick Connect + Remote Desk)"]
        MgrA["Remote Session Manager\n(internal/remote)"]
        LocalNodeA["Local UnifiedNode\n(:8443 Single-Port)"]
    end

    subgraph BridgeServer["GonDesk Bridge (Mode: server / Docker)"]
        Mux["SinglePortMux (:8443 TCP/TLS + UDP)\n(internal/netmux)"]
        Fiber["GoFiber v2 Server\n(HTTPS REST + /ws/signal WSS)"]
        Turn["Pion TURN v4 Engine\n(TURNS TLS / TURN TCP & UDP)"]
        DB[("SQLite GORM\n(gondesk.db)")]
    end

    subgraph ClientB["GonDesk App B (Mode: desktop)"]
        UIB["MyGo Native UI\n(Incoming Banner + Host Viewport)"]
        MgrB["Remote Session Manager\n(internal/remote)"]
        LocalNodeB["Local UnifiedNode\n(:8443 Single-Port)"]
    end

    UIA --> MgrA
    UIB --> MgrB

    MgrA <-->|"1 Port (:8443)\nHTTPS + WSS + TURNS"| Mux
    Mux <-->|"1 Port (:8443)\nHTTPS + WSS + TURNS"| MgrB

    Mux -->|"HTTP / WebSocket"| Fiber
    Mux -->|"STUN / TURN / ChannelData"| Turn
    Fiber --> DB
    Turn --> DB
```

---

## 2. Peta Modul & Struktur Direktori

```mermaid
classDiagram
    class Main {
        +main()
        -startNodeWithFallback(cfg, store, onSignal)
    }

    class Config {
        +Mode AppMode
        +Headless bool
        +ListenHost string
        +ListenPort int
        +PublicIP string
        +TurnRealm string
        +EnableTLS bool
        +BridgeServerURL string
        +DataDir string
        +DBPath string
        +CertFile string
        +KeyFile string
        +IsServerMode() bool
        +ShowDesktopFeatures() bool
        +ListenAddr() string
        +LocalHTTPSURL() string
        +TurnURI() string
    }

    class Store {
        +DB *gorm.DB
        +GetSettings() AppSetting
        +SaveSettings(*AppSetting) error
        +ListPeers() []PeerRecord
        +UpsertPeer(...) *PeerRecord
        +ToggleFavorite(deskID) error
        +IncrementPeerConnection(deskID, endpointURL)
        +RecordSession(...) error
        +ListSessionLogs(limit) []SessionLog
        +ListTurnCredentials() []TurnCredential
        +LookupTurnPassword(username) (string, bool)
    }

    class SinglePortMux {
        -rawTCP net.Listener
        -udpConn net.PacketConn
        -tlsConfig *tls.Config
        -httpLis *chanListener
        -turnLis *chanListener
        +Port() int
        +HTTPListener() net.Listener
        +TURNListener() net.Listener
        +UDPPacketConn() net.PacketConn
        +Stats() (uint64, uint64, uint64)
        +Serve() error
        +Close() error
    }

    class TurnEngine {
        -server *turn.Server
        -realm string
        -publicIP net.IP
        +AuthRequests() uint64
        +Close() error
    }

    class UnifiedNode {
        +Cfg *Config
        +Store *Store
        +Mux *SinglePortMux
        +TurnEngine *TurnEngine
        +FiberApp *fiber.App
        +Start()
        +Stop() error
        +OnlinePeerCount() int
        +SendSignalToPeer(SignalMessage) bool
    }

    class RemoteManager {
        +Cfg *Config
        +Store *Store
        +Node *UnifiedNode
        +StartBridgeLink()
        +ReconnectBridge()
        +HandleSignal(SignalMessage)
        +ConnectToPeer(target, password) *ActiveSession
        +AcceptIncoming()
        +RejectIncoming()
        +DisconnectSession(uuid)
        +SendChat(uuid, text)
        +SendRemoteAction(uuid, action)
        +Snapshot() (...)
    }

    class AppUI {
        +Cfg *Config
        +Store *Store
        +Node *UnifiedNode
        +Manager *RemoteManager
        +Win *mygo.Window
        +View(*ui.Context)
        +ConnectAndAddURLPeer(*ui.Context)
        +ReloadFromDB()
        +NotifyChanged()
    }

    Main --> Config : parses
    Main --> Store : initializes
    Main --> UnifiedNode : starts
    Main --> RemoteManager : creates
    Main --> AppUI : mounts in mygo.Window
    UnifiedNode *-- SinglePortMux : owns
    UnifiedNode *-- TurnEngine : owns
    UnifiedNode --> Store : queries/persists
    RemoteManager --> UnifiedNode : signals
    RemoteManager --> Store : logs sessions
    AppUI --> RemoteManager : triggers actions
    AppUI --> Store : reads/writes state
```

### Tanggung Jawab Tiap Paket

| Paket / File | Lokasi | Tanggung Jawab Utama |
|---|---|---|
| `main` | [main.go](file:///Users/jefripunza/Documents/Projects/gondesk/main.go) | Entry point aplikasi, *port fallback* (`8443..8452`), inisialisasi `Store`, `UnifiedNode`, `Manager`, mode `--headless`, dan pembuatan window MyGo Native UI. |
| `internal/config` | [config.go](file:///Users/jefripunza/Documents/Projects/gondesk/internal/config/config.go) | Parsing CLI flags & environment variables (`GONDESK_MODE`, `GONDESK_PORT`, dll.) serta penentuan mode `desktop` vs `server`. |
| `internal/database` | [sqlite.go](file:///Users/jefripunza/Documents/Projects/gondesk/internal/database/sqlite.go) | Koneksi GORM SQLite murni (`glebarez/sqlite`), migrasi skema otomatis, data awal (*seed*), generator 9-digit Desk ID, dan operasi CRUD. |
| `internal/netmux` | [singleport.go](file:///Users/jefripunza/Documents/Projects/gondesk/internal/netmux/singleport.go), [tlscert.go](file:///Users/jefripunza/Documents/Projects/gondesk/internal/netmux/tlscert.go) | *Single-Port Multiplexer* (TCP/TLS + UDP pada port yang sama) dan pembuatan sertifikat TLS X.509 ECDSA P-256 otomatis. |
| `internal/turnserver` | [turn.go](file:///Users/jefripunza/Documents/Projects/gondesk/internal/turnserver/turn.go) | Server **Pion TURN v4** yang menerima koneksi TCP/TLS dari `mux.TURNListener()` dan paket UDP dari `mux.UDPPacketConn()`. |
| `internal/server` | [fiber_server.go](file:///Users/jefripunza/Documents/Projects/gondesk/internal/server/fiber_server.go) | Server **GoFiber v2** yang melayani REST API (`/api/v1/*`) dan hub WebSocket Signaling (`/ws/signal`) di atas `mux.HTTPListener()`. |
| `internal/remote` | [manager.go](file:///Users/jefripunza/Documents/Projects/gondesk/internal/remote/manager.go) | Manajemen sesi remote keluar (*outgoing*) dan masuk (*incoming*), *heartbeat* HTTPS, *probe* STUN-over-TLS (`ProbeSTUNOverTLS`), dan komunikasi WebSocket. |
| `internal/uiapp` | [app_ui.go](file:///Users/jefripunza/Documents/Projects/gondesk/internal/uiapp/app_ui.go) | Seluruh tampilan **MyGo Native UI** untuk Mode Desktop (AnyDesk-style) dan Mode Server (Global Bridge Dashboard). |

---

## 3. Arsitektur Single-Port Multiplexer (`internal/netmux`)

Salah satu keunggulan utama GonDesk adalah kemampuan melayani **HTTPS REST API**, **WebSocket (`wss://`)**, **TURNS (`turns:` TCP/TLS)**, dan **TURN/STUN UDP** hanya melalui **1 nomor port** (`:8443`).

### Cara Kerja `SinglePortMux`

```mermaid
flowchart TD
    Start(["Koneksi Masuk pada Port :8443"]) --> ProtoCheck{"Protokol Transport?"}

    ProtoCheck -->|"UDP Datagram (:8443/udp)"| UDPConn["mux.UDPPacketConn()"]
    UDPConn --> PionUDP["Pion TURN v4\n(UDP STUN & TURN Relay)"]

    ProtoCheck -->|"TCP Stream (:8443/tcp)"| PeekTLS["bufferedConn.Peek(1)\nInspeksi Byte Pertama"]
    PeekTLS --> IsTLS{"Byte[0] == 0x16\n(TLS ClientHello)?"}

    IsTLS -->|"Ya (TLS 1.2 / 1.3)"| TLSHandshake["tls.Server(conn, tlsConfig).Handshake()\nTerminasi TLS & Hitung tlsCount++"]
    IsTLS -->|"Tidak (Plain TCP)"| WrapBuf["Gunakan Stream Langsung"]

    TLSHandshake --> PeekProto["innerBC.Peek(4)\nInspeksi 4 Byte Pertama Payload"]
    WrapBuf --> PeekProto

    PeekProto --> IsHTTP{"Apakah Prefix Method HTTP?\n(GET, POST, PUT, DELE,\nHEAD, OPTI, PATC, CONN, PRI)"}

    IsHTTP -->|"Ya (HTTP / WebSocket Upgrade)"| RouteHTTP["Enqueue ke mux.httpLis\n(httpCount++)"]
    RouteHTTP --> GoFiber["GoFiber v2\n(/api/v1/* & /ws/signal)"]

    IsHTTP -->|"Tidak (STUN / TURN / ChannelData)"| RouteTURN["Enqueue ke mux.turnLis\n(turnCount++)"]
    RouteTURN --> PionTCP["Pion TURN v4\n(STUNConn TCP/TLS Packetizer)"]
```

### Detail Implementasi Penting
1. **Non-Destructive Byte Peeking (`bufferedConn`)**:
   - Membungkus `net.Conn` dengan `bufio.Reader` (`4096` bytes) sehingga pemanggilan `Peek(1)` (untuk deteksi TLS `0x16`) dan `Peek(4)` (untuk deteksi method HTTP) tidak mengonsumsi byte dari stream yang nantinya dibaca oleh `crypto/tls`, `GoFiber`, ataupun `pion/turn`.
2. **Sinkronisasi Port TCP & UDP (`NewSinglePortMux`)**:
   - Membuka `net.Listen("tcp", addr)` terlebih dahulu, mengambil nomor port aktual (`tcpLis.Addr().(*net.TCPAddr).Port` — sangat berguna saat testing dengan port `:0`), lalu membuka `net.ListenPacket("udp", ...)` pada nomor port yang persis sama.
3. **Auto Self-Signed Certificate (`LoadOrGenerateTLSConfig`)**:
   - Bila file `data/certs/server.crt` dan `data/certs/server.key` belum ada, aplikasi secara otomatis membangkitkan pasangan kunci **ECDSA P-256** dan sertifikat X.509 yang berlaku selama 365 hari untuk `localhost`, `gondesk.local`, `127.0.0.1`, dan `::1`.

---

## 4. Mekanisme Dual-Mode dalam Satu Source Code (`desktop` vs `server`)

GonDesk dikompilasi menjadi **satu binary tunggal** yang dapat berjalan dalam 2 mode berbeda:

```mermaid
stateDiagram-v2
    [*] --> ParseConfig : Jalankan Binary GonDesk
    ParseConfig --> CheckMode : Baca --mode atau env GONDESK_MODE

    state CheckMode <<choice>>
    CheckMode --> DesktopMode : mode == "desktop" (Default)
    CheckMode --> ServerMode : mode == "server" / "desktop-server"

    state DesktopMode {
        [*] --> StartLocalNode : Jalankan SinglePortMux + GoFiber + Pion TURN
        StartLocalNode --> StartBridgeLink : Hubungkan ke Bridge (HTTPS + STUN + WSS)
        StartBridgeLink --> RenderDesktopUI : Tampilkan UI AnyDesk\n(Your Address, Quick Connect URL,\nRemote Desk, Discovered Desks, Sessions)
    }

    state ServerMode {
        [*] --> StartBridgeNode : Jalankan SinglePortMux + GoFiber + Pion TURN
        StartBridgeNode --> CheckHeadless : Cek --headless / GONDESK_HEADLESS
        state CheckHeadless <<choice>>
        CheckHeadless --> HeadlessDaemon : Headless == true\n(Tunggu SIGINT/SIGTERM di Docker/VPS)
        CheckHeadless --> ServerDashboardUI : Headless == false\n(Tampilkan Dashboard Server,\nSembunyikan Seluruh Fitur Desktop)
    }

    DesktopMode --> ServerMode : Klik "Switch to Server Mode" di TopBar
    ServerMode --> DesktopMode : Klik "Switch to Desktop Mode" di TopBar
```

### Perbandingan Fitur Berdasarkan Mode

| Komponen / Fitur | Mode `desktop` (`ShowDesktopFeatures() == true`) | Mode `server` (`ShowDesktopFeatures() == false`) |
|---|---|---|
| **Single-Port HTTPS + WSS + Pion TURN (`:8443`)** | ✅ Aktif | ✅ Aktif |
| **Database GORM SQLite (`gondesk.db`)** | ✅ Aktif | ✅ Aktif |
| **Panel "Your Address" (9-Digit ID & Di-remote)** | ✅ Ditampilkan | ❌ Disembunyikan (`Hidden`) |
| **Form Quick Connect (`Name` + `http/https URL`)** | ✅ Ditampilkan | ❌ Disembunyikan (`Hidden`) |
| **Daftar "Discovered & Saved Desks"** | ✅ Ditampilkan | ❌ Disembunyikan (`Hidden`) |
| **Incoming Session Approval Banner & Active Viewport** | ✅ Ditampilkan | ❌ Disembunyikan & Ditolak di Manager |
| **Global Bridge & TURN Server Admin Dashboard** | 🔄 Dapat diakses via Switch Mode | ✅ Tampilan Utama Default |

---

## 5. Alur Koneksi, Signaling WebSocket & Sesi Remote (`internal/remote` & `internal/server`)

### 5.1. Alur Quick Connect URL di Panel Kiri (`ConnectAndAddURLPeer`)

Ketika pengguna mengisi **Name (optional)** dan **URL (`http://` atau `https://`)** pada kartu di bawah *Your Address* lalu menekan tombol **Connect**:

```mermaid
sequenceDiagram
    autonumber
    actor User as Pengguna
    participant UI as AppUI (Left Pane)
    participant Store as SQLite Store (GORM)
    participant Mgr as RemoteManager
    participant Bridge as Target URL (:8443)

    User->>UI: Input Name (opsional) & URL (http:// atau https://) + Klik "Connect"
    UI->>UI: Validasi prefix http:// atau https:// & host URL
    alt URL Tidak Valid
        UI-->>User: Toast: "URL wajib menggunakan http:// atau https://"
    else URL Valid
        UI->>UI: Tentukan Nama (gunakan input Name, atau fallback ke Host URL)
        UI->>Store: UpsertPeer(deskID, name, host, os, url, online=true)
        UI->>Store: IncrementPeerConnection(deskID, url)
        UI->>Store: SaveSettings(BridgeServerURL = url)
        UI->>UI: ReloadFromDB() & Reset Filter ke "All Desks"
        UI-->>User: Peer langsung tampil di daftar "Discovered & Saved Desks" (Panel Kanan)
        UI->>Mgr: go ReconnectBridge()
        Mgr->>Bridge: POST /api/v1/peers/heartbeat (HTTPS)
        Mgr->>Bridge: ProbeSTUNOverTLS (STUN Binding Request via TLS :8443)
        Mgr->>Bridge: Dial WSS /ws/signal?desk_id=...
    end
```

### 5.2. Alur Sesi Remote Desktop (*Me-remote* & *Di-remote*)

```mermaid
sequenceDiagram
    autonumber
    actor ClientUser as Pengguna A (Me-remote)
    participant AppA as GonDesk Desktop A
    participant Bridge as Single-Port Bridge (:8443)
    participant AppB as GonDesk Desktop B
    actor HostUser as Pengguna B (Di-remote)

    ClientUser->>AppA: Klik "Connect" pada Desk B (di Remote Desk / Discovered Desks)
    AppA->>AppA: Buat ActiveSession (status="connected") & Catat di SQLite
    AppA->>Bridge: WSS SignalMessage {type: "session_request", from: DeskA, to: DeskB}
    Bridge->>Bridge: Catat SessionLog (status="requested") di SQLite
    Bridge->>AppB: Forward SignalMessage {type: "session_request"} via WSS

    alt Unattended Password Cocok
        AppB->>AppB: Auto-Accept Incoming Session
    else Memerlukan Persetujuan Manual
        AppB-->>HostUser: Tampilkan Banner "INCOMING SESSION"
        HostUser->>AppB: Klik "Accept & Share Screen"
    end

    AppB->>Bridge: WSS SignalMessage {type: "session_accept", session_uuid}
    Bridge->>Bridge: Update SessionLog (status="active")
    Bridge->>AppA: Forward SignalMessage {type: "session_accept"}

    par Interaksi Layar, Input & Chat Real-Time (1 Port :8443)
        ClientUser->>AppA: Klik Viewport / Kirim Ctrl+Alt+Del / Sync Clipboard / Chat
        AppA->>Bridge: WSS SignalMessage {type: "input" | "chat", payload}
        Bridge->>AppB: Forward SignalMessage ke Host B
        AppB-->>HostUser: Perbarui Viewport & Log Sesi
    end

    ClientUser->>AppA: Klik "Disconnect"
    AppA->>Bridge: WSS SignalMessage {type: "disconnect", session_uuid}
    Bridge->>Bridge: Update SessionLog (status="completed", ended_at=now)
```

---

## 6. Skema Database GORM SQLite (`internal/database`)

GonDesk menggunakan `gorm.io/gorm` dengan driver pure-Go `github.com/glebarez/sqlite` dalam mode **WAL (`journal_mode=WAL`)** dan `busy_timeout=5000`. Database disimpan secara default di `data/gondesk.db`.

```mermaid
erDiagram
    APP_SETTINGS {
        uint id PK
        string desk_id UK "9-digit ID (XXX XXX XXX)"
        string alias "Nama perangkat lokal"
        string access_password "Password unattended"
        bool unattended_enabled "Status akses tanpa konfirmasi"
        bool allow_control "Izin kontrol mouse & keyboard"
        bool allow_clipboard "Izin sinkronisasi clipboard"
        string bridge_server_url "URL HTTPS Bridge Server"
        int single_port "Port tunggal (default 8443)"
        bool enable_https "Status TLS/HTTPS"
        datetime updated_at
    }

    PEER_RECORDS {
        uint id PK
        string desk_id UK "9-digit ID peer"
        string alias "Nama tampilan / alias desk"
        string hostname "Hostname perangkat"
        string os "Sistem operasi (darwin/arm64, dll)"
        string endpoint_url "URL HTTP/HTTPS peer/bridge"
        bool online "Status online terakhir"
        bool is_favorite "Ditandai bintang (favorit)"
        datetime last_seen_at "Waktu koneksi/heartbeat terakhir"
        int connection_count "Jumlah sesi koneksi"
    }

    SESSION_LOGS {
        uint id PK
        string session_uuid UK "ID unik sesi (12 karakter)"
        string client_desk_id "Desk ID pengontrol (Me-remote)"
        string host_desk_id "Desk ID target (Di-remote)"
        string transport_mode "Single-Port HTTPS/WSS + TURNS"
        string status "requested | active | rejected | completed"
        datetime started_at "Waktu mulai sesi"
        datetime ended_at "Waktu selesai sesi (nullable)"
        string notes "Catatan audit sesi"
    }

    TURN_CREDENTIALS {
        uint id PK
        string username UK "Username autentikasi TURN"
        string password "Password TURN"
        string realm "Realm TURN (gondesk.local)"
        bool enabled "Status akun aktif"
        datetime created_at
    }

    APP_SETTINGS ||--o{ SESSION_LOGS : "menginisiasi / menerima"
    PEER_RECORDS ||--o{ SESSION_LOGS : "terlibat dalam"
```

---

## 7. Hierarki & Siklus Hidup MyGo Native UI (`internal/uiapp`)

Antarmuka pengguna dibangun menggunakan deklarasi *immediate-mode* `github.com/egoist/mygo/ui`.

```mermaid
flowchart TD
    Root["AppUI.View(c *ui.Context)\nTheme Accent: #EF443B (AnyDesk Coral Red)"] --> TopBar["renderTopBar\n• Logo & Mode Label\n• Navigation Tabs (New Connection, Active Sessions, Settings)\n• Badge 1-Port Status (:%d)\n• Button Switch Mode (Desktop <-> Server)"]

    Root --> ModeBranch{"a.Cfg.IsServerMode()?"}

    ModeBranch -->|"Ya (Mode: server)"| ServerDash["renderServerBridgeDashboard\n(Seluruh Fitur Desktop Di-hide)"]
    ServerDash --> SrvBanner["Header Global Bridge & Refresh SQLite"]
    ServerDash --> SrvMetrics["4 Metric Cards\n(Single Port, Online Desks, HTTPS/WSS Conns, Pion TURN Traffic)"]
    ServerDash --> SrvPeers["Global Peer Registry (GORM SQLite)"]
    ServerDash --> SrvTurnAndLogs["Pion TURN Credentials Form + Global Bridge Relay Logs"]

    ModeBranch -->|"Tidak (Mode: desktop)"| IncomingCheck{"Ada IncomingPrompt?"}
    IncomingCheck -->|"Ya"| IncomingBanner["renderIncomingBanner\n(Accept & Share Screen / Reject)"]
    IncomingCheck -->|"Tidak / Lanjut"| TabBranch{"a.ActiveTab"}

    TabBranch -->|"Tab == 0 (Home)"| MainSplit["renderDesktopMainSplit (ui.Split)"]
    MainSplit --> LeftPane["renderLeftThisDeskPane (Panel Kiri)\n1. Card 'Your Address' (9-Digit ID + Copy Address)\n2. Card Quick Connect:\n   • Input Name (optional)\n   • Input URL (http / https wajib)\n   • Button Connect -> Masuk ke List Kanan"]
    MainSplit --> RightPane["renderRightRemoteDeskPane (Panel Kanan)\n1. Bar 'Discovered & Saved Desks' (Tabs: All / Favorites / Online + Search + Add Desk)\n2. Daftar Kartu Peer (Badge ONLINE/OFFLINE, Star Favorite, Button Connect)"]

    TabBranch -->|"Tab == 1 (Settings)"| SettingsView["renderDesktopSettings\n• Device Alias\n• Global Bridge Server URL\n• Save & Reconnect Bridge"]

    TabBranch -->|"Tab >= 2 (Active Session)"| SessionView["renderActiveSessionView\n• Session Header (Ctrl+Alt+Del, Sync Clipboard, Disconnect)\n• GPU Custom Drawn Remote Screen Canvas (ui.Painter)\n• Live Session Chat & Telemetry Sidebar"]
```

### Aturan Penting MyGo Native UI yang Diterapkan
1. **Aturan Popup / Modal (Tidak Boleh Tertutup Saat Klik di Luar)**:
   - Sesuai aturan di [AGENT.md](AGENT.md), semua dialog popup/modal wajib memanfaatkan `.Dismissed()` dari `ui.Modal` untuk mengembalikan status `*open = true` jika pengguna mengklik backdrop luar atau menekan Escape secara tidak sengaja. Dialog **hanya boleh ditutup** melalui tombol eksplisit di dalam dialog (seperti tombol Cancel atau Save).
2. **Zero `ui.Element` / `*ui.Context` Persistence**:
   - Struct `AppUI` hanya menyimpan *state* data murni (`string`, `bool`, `int`, `float32`, slice model SQLite, dan `*mygo.Window`). Tidak ada `ui.Element` atau `*ui.Context` yang disimpan di dalam struct (diverifikasi dengan `go tool mygo vet .`).
3. **Stable Keys di Dalam Loop (`c.Key(...)`)**:
   - Setiap elemen yang di-render di dalam perulangan (`sessions`, `filteredPeers`, `SessionLogs`, `TurnCreds`, `ChatLog`) selalu diberi kunci unik menggunakan `c.Key(...)` agar state fokus dan klik tetap stabil antar-frame.
4. **Thread-Safe UI Update dari Goroutine Latar Belakang**:
   - Saat *event* WebSocket atau TURN masuk dari goroutine latar belakang, `Manager` memanggil `appUI.NotifyChanged()` yang mengeksekusi `a.Win.Update(func() { a.ReloadFromDB() })` pada *main thread*.

---

## 8. Spesifikasi REST API & Protokol WebSocket

Seluruh endpoint dilayani oleh **GoFiber v2** pada port tunggal yang sama dengan TURN (`https://<host>:<port>`):

### 8.1. Endpoint REST API

| Method | Path | Fungsi | Contoh Respons Utama |
|---|---|---|---|
| `GET` | `/api/v1/info` | Informasi status node, mode aktif, statistik koneksi HTTP/TURN/TLS, serta URI TURN. | `{"app":"gondesk","mode":"desktop","single_port":8443,"turn_uri":"turns:127.0.0.1:8443?transport=tcp",...}` |
| `GET` | `/api/v1/peers` | Mengambil daftar seluruh peer yang tersimpan/terdaftar di SQLite. | `{"peers":[...]}` |
| `POST` | `/api/v1/peers/heartbeat` | Mendaftarkan atau memperbarui status *online* & *endpoint URL* sebuah peer. | `{"status":"ok","peer":{...},"turn_uri":"..."}` |
| `GET` | `/api/v1/sessions` | Mengambil 30 riwayat sesi terakhir dari tabel `session_logs`. | `{"sessions":[...]}` |
| `GET` | `/api/v1/turn/credentials` | Mengambil kredensial aktif untuk otentikasi alokasi relay Pion TURN. | `{"realm":"gondesk.local","username":"gondesk","password":"...","port":8443}` |

### 8.2. Struktur Pesan WebSocket (`/ws/signal?desk_id=...&alias=...`)

```json
{
  "type": "session_request | session_accept | session_reject | chat | input | disconnect",
  "session_uuid": "a1b2c3d4e5f6",
  "from_desk_id": "482 910 374",
  "from_alias": "Workstation-MacPro",
  "to_desk_id": "816 692 488",
  "password": "optional-password",
  "transport": "Single-Port HTTPS/WSS + TURNS (:8443)",
  "payload": "Pesan chat atau aksi input remote",
  "timestamp": "23:35:10"
}
```

---

## 9. Arsitektur Docker & CI/CD Auto-Versioning (`.github/workflows/release.yml`)

Setiap `push` ke branch `master` menjalankan *pipeline* otomatis dari pengujian hingga distribusi lintas OS dan Docker Hub:

```mermaid
flowchart LR
    Push(["git push origin master"]) --> Job1

    subgraph Job1["1. Job: version-and-release"]
        VetTest["go tool mygo vet .\ngo test -v ./..."] --> CalcVer["Hitung Auto SemVer (XX.XX.XX)\n• #major / BREAKING -> X+1.0.0\n• #minor / feat: -> X.Y+1.0\n• Default -> X.Y.Z+1"]
        CalcVer --> CreateTag["Buat & Push Git Tag vXX.XX.XX\n+ Buat GitHub Release"]
    end

    CreateTag --> Job2
    CreateTag --> Job3

    subgraph Job2["2. Job: docker (Docker Hub)"]
        Buildx["Docker Buildx Multi-Arch\n(linux/amd64, linux/arm64)"] --> PushHub["Push ke Docker Hub:\n• <user>/gondesk:XX.XX.XX\n• <user>/gondesk:latest"]
    end

    subgraph Job3["3. Job: build-os-binaries (3 OS Matrix)"]
        Stamp["Update .version di mygo.json\nmenjadi XX.XX.XX"] --> BuildLinux["Linux (amd64, arm64)\n.deb, .rpm, .tar.gz"]
        Stamp --> BuildWin["Windows (amd64, arm64)\nNSIS *-setup-*.exe & .zip"]
        Stamp --> BuildMac["macOS (universal, arm64, amd64)\n.dmg & .app.zip"]
        BuildLinux --> Upload["Generate SHA256SUMS &\ngh release upload ke Tag vXX.XX.XX"]
        BuildWin --> Upload
        BuildMac --> Upload
    end
```

---

## 10. Panduan Pengujian & Analisis Statis

Proyek ini dilengkapi pengujian tanpa jendela (*headless UI test* menggunakan `ui.NewTester`) dan pengujian integrasi jaringan *Single-Port* di [main_test.go](file:///Users/jefripunza/Documents/Projects/gondesk/main_test.go):

1. **`TestDesktopAndServerModeViews`**:
   - Memastikan Mode `desktop` menampilkan **Your Address**, **Remote Desk**, dan Desk ID 9-digit.
   - Memastikan pengisian form **Quick Connect** (`Name` opsional + `URL` `https://...` + klik `Connect`) menambahkan peer baru langsung ke daftar **Discovered & Saved Desks**.
   - Memastikan saat beralih ke Mode `server`, fitur-fitur desktop disembunyikan dan tampilan beralih ke **GonDesk Global Bridge & TURN Server Mode**.
2. **`TestSinglePortHTTPSAndTURNAndWebSocket`**:
   - Memastikan pada **1 port acak yang sama**, request **HTTPS `GET /api/v1/info`**, **STUN Binding over TLS (`ProbeSTUNOverTLS`)**, dan **WebSocket Signaling + pencatatan SQLite** berjalan sukses secara bersamaan.

Perintah verifikasi lengkap:
```bash
# Analisis statis aturan lifetime & goroutine MyGo Native UI
go tool mygo vet .

# Menjalankan seluruh unit & integration test
go test -v ./...

# Memastikan kompilasi seluruh paket bersih
go build -v ./...
```

