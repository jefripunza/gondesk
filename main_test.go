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
	if tt.HasText("New Connection") {
		t.Fatalf("expected 'New Connection' button to be removed from header; got texts: %v", tt.Texts())
	}
	if tt.HasText("1-Port HTTPS + TURN") {
		t.Fatalf("expected '1-Port HTTPS + TURN' badge to be removed from header; got texts: %v", tt.Texts())
	}
	if !tt.HasText("⚙ Settings") {
		t.Fatalf("expected '⚙ Settings' button on the right; got texts: %v", tt.Texts())
	}
	if tt.HasText("340 px") || tt.HasText("280 px") || tt.HasText("490 px") || tt.HasText("81 px") {
		t.Fatalf("expected width indicator text to be removed; got texts: %v", tt.Texts())
	}

	// Verify Desks View has search, filter tabs, and + Add Desk button
	if !tt.HasText("+ Add Desk") || !tt.HasText("Favorites ★") || !tt.HasText("Online") {
		t.Fatalf("expected Desks View to show '+ Add Desk' and filter tabs; got texts: %v", tt.Texts())
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

	// Verify Settings View with Sidebar Menu (General, Network, About)
	appUI.ActiveTab = 1
	appUI.SettingsSection = "general"
	ttSettings := ui.NewTester(appUI.View, 1120, 720)
	if !ttSettings.HasText("General") || !ttSettings.HasText("Network") || !ttSettings.HasText("About") {
		t.Fatalf("expected Settings sidebar to have General, Network, About; got texts: %v", ttSettings.Texts())
	}
	if !ttSettings.HasText("General Preferences") || !ttSettings.HasText("Device Alias") {
		t.Fatalf("expected General settings section to be displayed; got texts: %v", ttSettings.Texts())
	}

	// Switch to Network section
	appUI.SettingsSection = "network"
	ttSettings.Frame()
	if !ttSettings.HasText("GonDesk Network & Single-Port Configuration") || !ttSettings.HasText("Global Bridge Server URL") {
		t.Fatalf("expected Network settings section to be displayed; got texts: %v", ttSettings.Texts())
	}

	// Switch to About section
	appUI.SettingsSection = "about"
	ttSettings.Frame()
	if !ttSettings.HasText("About GonDesk") || !ttSettings.HasText("v0.1.0") || !ttSettings.HasText("MIT License") {
		t.Fatalf("expected About settings section to be displayed; got texts: %v", ttSettings.Texts())
	}

	// Clicking Back to Home returns to main view
	ttSettings.Click("Back to Home")
	ttSettings.Frame()
	if appUI.ActiveTab != 0 {
		t.Fatalf("expected clicking 'Back to Home' to reset ActiveTab to 0, got %d", appUI.ActiveTab)
	}

	// Verify clicking ⚙ Settings button toggles settings view
	ttSettings.Click("⚙ Settings")
	ttSettings.Frame()
	if appUI.ActiveTab != 1 {
		t.Fatalf("expected clicking '⚙ Settings' to set ActiveTab to 1, got %d", appUI.ActiveTab)
	}
	ttSettings.Click("⚙ Settings")
	ttSettings.Frame()
	if appUI.ActiveTab != 0 {
		t.Fatalf("expected clicking '⚙ Settings' again to toggle ActiveTab back to 0, got %d", appUI.ActiveTab)
	}

	// 2. Verify Desktop UI does NOT contain "Switch to Server Mode"
	if tt.HasText("Switch to Server Mode") {
		t.Fatalf("expected 'Switch to Server Mode' button to be removed from Desktop UI; got texts: %v", tt.Texts())
	}

	// 3. Verify Server Mode Config is headless and has desktop features disabled
	serverCfg := config.Default()
	serverCfg.Mode = config.ModeServer
	if serverCfg.ShowDesktopFeatures() {
		t.Fatalf("expected ShowDesktopFeatures to be false in Server Mode")
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

