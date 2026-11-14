package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"gondesk/internal/config"
	"gondesk/internal/database"
	"gondesk/internal/remote"
	"gondesk/internal/server"
	"gondesk/internal/uiapp"

	"github.com/egoist/mygo/ui"
)

func TestDesktopAndServerModeViews(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.Default()
	cfg.DataDir = tmpDir
	cfg.DBPath = filepath.Join(tmpDir, "test.db")
	cfg.CertFile = filepath.Join(tmpDir, "certs", "server.crt")
	cfg.KeyFile = filepath.Join(tmpDir, "certs", "server.key")
	cfg.ListenHost = "127.0.0.1"
	cfg.ListenPort = 0

	store, err := database.Open(cfg.DBPath, 8443, "https://127.0.0.1:8443")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	mgr := remote.NewManager(cfg, store, nil, nil)
	appUI := uiapp.NewAppUI(cfg, store, nil, mgr)

	// 1. Verify Desktop Mode renders "Your Address" and "Discovered & Saved Desks"
	tt := ui.NewTester(appUI.View, 1120, 720)
	if !tt.HasText("Your Address") || !tt.HasText("Discovered & Saved Desks") || !tt.HasText(appUI.DeskID) {
		t.Fatalf("expected Desktop Mode UI to show 'Your Address', 'Discovered & Saved Desks', and DeskID %q; got texts: %v", appUI.DeskID, tt.Texts())
	}

	// Verify connecting via Name (optional) + URL (http/https required) adds to Discovered & Saved Desks list
	appUI.QuickConnectName = "Kantor-Cabang-Server"
	appUI.QuickConnectURL = "https://10.20.30.40:8443"
	ttConnect := ui.NewTester(func(c *ui.Context) {
		appUI.ConnectAndAddURLPeer(c)
		appUI.View(c)
	}, 1120, 720)
	if !ttConnect.HasText("Kantor-Cabang-Server") {
		t.Fatalf("expected connected URL peer 'Kantor-Cabang-Server' to appear in Discovered & Saved Desks list; got texts: %v", ttConnect.Texts())
	}

	// Verify modal dialog does not close when clicking outside (backdrop)
	appUI.AddPeerModalOpen = true
	ttModal := ui.NewTester(appUI.View, 1120, 720)
	if !ttModal.HasText("Add Desk to Address Book") {
		t.Fatalf("expected modal to be open; got texts: %v", ttModal.Texts())
	}
	// Click outside the modal dialog (at top-left backdrop coordinate)
	ttModal.ClickAt(10, 10)
	ttModal.Frame()
	if !appUI.AddPeerModalOpen {
		t.Fatalf("modal should NOT close when clicking outside; expected AddPeerModalOpen=true")
	}
	// Verify Cancel button explicitly closes the modal
	ttModal.Click("Cancel")
	ttModal.Frame()
	if appUI.AddPeerModalOpen {
		t.Fatalf("modal should close when clicking Cancel button; expected AddPeerModalOpen=false")
	}

	// 2. Switch to Server Mode and verify Desktop features are hidden
	cfg.Mode = config.ModeServer
	ttServer := ui.NewTester(appUI.View, 1120, 720)
	if !ttServer.HasText("GonDesk Global Bridge & TURN Server Mode") {
		t.Fatalf("expected Server Mode UI to show Global Bridge header; got texts: %v", ttServer.Texts())
	}
	if ttServer.HasText("Your GonDesk Address") || ttServer.HasText("Host Ready (Di-remote)") {
		t.Fatalf("expected Desktop features to be hidden in Server Mode, but found them in texts: %v", ttServer.Texts())
	}
}

func TestSinglePortHTTPSAndTURNAndWebSocket(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.Default()
	cfg.Mode = config.ModeServer
	cfg.DataDir = tmpDir
	cfg.DBPath = filepath.Join(tmpDir, "bridge.db")
	cfg.CertFile = filepath.Join(tmpDir, "certs", "server.crt")
	cfg.KeyFile = filepath.Join(tmpDir, "certs", "server.key")
	cfg.ListenHost = "127.0.0.1"
	cfg.ListenPort = 0
	cfg.PublicIP = "127.0.0.1"
	cfg.EnableTLS = true

	store, err := database.Open(cfg.DBPath, 0, "")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	node, err := server.NewUnifiedNode(cfg, store, nil)
	if err != nil {
		t.Fatalf("new unified node: %v", err)
	}
	node.Start()
	defer node.Stop()

	port := node.Mux.Port()
	httpsURL := fmt.Sprintf("https://127.0.0.1:%d", port)

	// 1. Test HTTPS (GoFiber v2) on the single port
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	resp, err := client.Get(httpsURL + "/api/v1/info")
	if err != nil {
		t.Fatalf("HTTPS GET /api/v1/info on single port %d failed: %v", port, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	var info map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		t.Fatalf("decode info json: %v", err)
	}
	if info["app"] != "gondesk" {
		t.Fatalf("unexpected app info: %v", info)
	}

	// 2. Test STUN/TURN over TLS on the EXACT SAME PORT
	mappedAddr, err := remote.ProbeSTUNOverTLS(httpsURL)
	if err != nil {
		t.Fatalf("STUN/TURN over TLS on single port %d failed: %v", port, err)
	}
	if mappedAddr == "" {
		t.Fatalf("expected non-empty STUN XOR-MAPPED-ADDRESS on single port %d", port)
	}

	// 3. Test Inter-App Connection via Single-Port HTTPS/WSS Signaling
	desktopCfg := *cfg
	desktopCfg.Mode = config.ModeDesktop
	desktopCfg.BridgeServerURL = httpsURL

	st, _ := store.GetSettings()
	st.BridgeServerURL = httpsURL
	_ = store.SaveSettings(&st)

	mgr := remote.NewManager(&desktopCfg, store, node, nil)
	mgr.ReconnectBridge()

	sess, err := mgr.ConnectToPeer("482 910 374", "")
	if err != nil {
		t.Fatalf("ConnectToPeer failed: %v", err)
	}
	if sess.PeerDeskID != "482 910 374" {
		t.Fatalf("unexpected session peer ID: %s", sess.PeerDeskID)
	}

	logs, err := store.ListSessionLogs(10)
	if err != nil || len(logs) == 0 {
		t.Fatalf("expected session log persisted in SQLite, got %d (err=%v)", len(logs), err)
	}
}

