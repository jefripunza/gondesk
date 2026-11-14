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

	// 1. Verify Desktop Mode renders "Your Address" and "All Desks" (and NOT "Discovered & Saved Desks")
	tt := ui.NewTester(appUI.View, 1120, 720)
	if !tt.HasText("Your Address") || !tt.HasText("All Desks") || !tt.HasText(appUI.DeskID) {
		t.Fatalf("expected Desktop Mode UI to show 'Your Address', 'All Desks', and DeskID %q; got texts: %v", appUI.DeskID, tt.Texts())
	}
	if tt.HasText("Discovered & Saved Desks") {
		t.Fatalf("expected 'Discovered & Saved Desks' header to be completely removed from UI; got texts: %v", tt.Texts())
	}
	if tt.HasText("340 px") || tt.HasText("280 px") || tt.HasText("490 px") || tt.HasText("81 px") {
		t.Fatalf("expected width indicator text to be removed; got texts: %v", tt.Texts())
	}

	// Verify LeftPaneWidth is clamped to minimum 280px and maximum 490px
	appUI.LeftPaneWidth = 81
	ttClamp := ui.NewTester(appUI.View, 1120, 720)
	ttClamp.Frame()
	if appUI.LeftPaneWidth < 280 {
		t.Fatalf("expected LeftPaneWidth to be clamped to at least 280, got %v", appUI.LeftPaneWidth)
	}

	// Verify dragging divider cannot shrink left pane below 280px
	ttClamp.Press(appUI.LeftPaneWidth, 200)
	ttClamp.Move(150, 200)
	ttClamp.Move(50, 200)
	ttClamp.Release(50, 200)
	ttClamp.Frame()
	if appUI.LeftPaneWidth < 280 {
		t.Fatalf("expected LeftPaneWidth after drag to be clamped to >= 280, got %v", appUI.LeftPaneWidth)
	}

	// Verify expanding left pane past 490px is clamped to maximum 490px
	ttClamp.Press(appUI.LeftPaneWidth, 200)
	ttClamp.Move(400, 200)
	ttClamp.Move(600, 200)
	ttClamp.Release(600, 200)
	ttClamp.Frame()
	if appUI.LeftPaneWidth > 490 {
		t.Fatalf("expected LeftPaneWidth to be clamped to at most 490, got %v", appUI.LeftPaneWidth)
	}

	appUI.LeftPaneWidth = 750
	ttClampMax := ui.NewTester(appUI.View, 1120, 720)
	ttClampMax.Frame()
	if appUI.LeftPaneWidth > 490 {
		t.Fatalf("expected LeftPaneWidth to be clamped to 490 on initial frame, got %v", appUI.LeftPaneWidth)
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

	// Verify delete button ("🗑") and confirmation modal
	appUI.PendingDeletePeerID = "482 910 374"
	appUI.PendingDeletePeerAlias = "Workstation-MacPro"
	appUI.DeleteConfirmModalOpen = true
	ttDelModal := ui.NewTester(appUI.View, 1120, 720)
	if !ttDelModal.HasText("Hapus Desk") || !ttDelModal.HasText("Konfirmasi Penghapusan") {
		t.Fatalf("expected delete confirmation modal to be open; got texts: %v", ttDelModal.Texts())
	}
	// Clicking outside does not close delete modal
	ttDelModal.ClickAt(10, 10)
	ttDelModal.Frame()
	if !appUI.DeleteConfirmModalOpen {
		t.Fatalf("delete modal should NOT close when clicking outside; expected DeleteConfirmModalOpen=true")
	}
	// Clicking "Batal" closes modal
	ttDelModal.Click("Batal")
	ttDelModal.Frame()
	if appUI.DeleteConfirmModalOpen {
		t.Fatalf("delete modal should close on 'Batal'; expected DeleteConfirmModalOpen=false")
	}

	// Verify executing delete through Store.DeletePeer
	if err := store.DeletePeer("482 910 374"); err != nil {
		t.Fatalf("failed to delete peer from store: %v", err)
	}
	appUI.ReloadFromDB()
	for _, p := range appUI.Peers {
		if p.DeskID == "482 910 374" {
			t.Fatalf("expected peer 482 910 374 to be deleted from store, but still found")
		}
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

func TestCleanURL(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"192.168.1.1:8443", "http://192.168.1.1:8443"},
		{"192.168.1.1:8443/", "http://192.168.1.1:8443"},
		{"192.168.1.1:8443///", "http://192.168.1.1:8443"},
		{"http://192.168.1.1:8443", "http://192.168.1.1:8443"},
		{"http://192.168.1.1:8443/", "http://192.168.1.1:8443"},
		{"https://192.168.1.1:8443", "https://192.168.1.1:8443"},
		{"https://192.168.1.1:8443/", "https://192.168.1.1:8443"},
		{"https://192.168.1.1:8443///", "https://192.168.1.1:8443"},
		{"  10.0.0.5:8080/path/  ", "http://10.0.0.5:8080/path"},
		{"", ""},
	}

	for _, tc := range tests {
		got := uiapp.CleanURL(tc.input)
		if got != tc.expected {
			t.Errorf("CleanURL(%q) = %q; expected %q", tc.input, got, tc.expected)
		}
	}
}

