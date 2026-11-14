package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
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
	if ttModal.HasText("Endpoint URL (e.g. https://host:8443)") {
		t.Fatalf("expected 'Endpoint URL' to be renamed to 'URL'")
	}

	// Verify modal URL validation: path/endpoint is rejected and red error text is rendered
	appUI.NewPeerURL = "https://10.20.30.40:8443/api/v1"
	cleaned, errMsg := uiapp.ValidateHostURL(appUI.NewPeerURL)
	appUI.NewPeerURL = cleaned
	appUI.NewPeerURLError = errMsg
	ttModalErr := ui.NewTester(appUI.View, 1120, 720)
	expectedErrMsg := "URL tidak boleh berisi path atau endpoint (hanya IP atau domain, contoh: https://host:8443)"
	if !ttModalErr.HasText(expectedErrMsg) {
		t.Fatalf("expected modal to show red error text %q; got texts: %v", expectedErrMsg, ttModalErr.Texts())
	}
	// Attempting to Save Desk with invalid URL must be blocked
	ttModalErr.Click("Save Desk")
	ttModalErr.Frame()
	if !appUI.AddPeerModalOpen {
		t.Fatalf("expected modal to remain open when Save Desk is clicked with invalid URL")
	}

	// Correct the URL to pure IP:Port
	appUI.NewPeerURL = "10.20.30.40:8443"
	cleaned, errMsg = uiapp.ValidateHostURL(appUI.NewPeerURL)
	appUI.NewPeerURL = cleaned
	appUI.NewPeerURLError = errMsg
	ttModalValid := ui.NewTester(appUI.View, 1120, 720)
	if ttModalValid.HasText(expectedErrMsg) {
		t.Fatalf("expected red error text to disappear after valid URL; got texts: %v", ttModalValid.Texts())
	}

	// Verify GonDesk ID field: must be numeric digits only, styled with gap, zero space character in value
	if !ttModalValid.HasText("GonDesk ID (e.g. 612 840 193)") {
		t.Fatalf("expected modal to show GonDesk ID placeholder; got texts: %v", ttModalValid.Texts())
	}
	// Test FilterOnlyDigits
	if filtered := uiapp.FilterOnlyDigits("612abc840xyz193", 9); filtered != "612840193" {
		t.Fatalf("expected '612840193', got %q", filtered)
	}
	if filtered := uiapp.FilterOnlyDigits("816 692 488", 9); filtered != "816692488" {
		t.Fatalf("expected '816692488', got %q", filtered)
	}
	// Enter ID into appUI
	appUI.NewPeerID = "612840193"
	appUI.NewPeerAlias = "Linux-Workstation"
	ttModalValid.Frame()
	if appUI.NewPeerIDPart1 != "612" || appUI.NewPeerIDPart2 != "840" || appUI.NewPeerIDPart3 != "193" {
		t.Fatalf("expected ID parts '612', '840', '193'; got %q %q %q",
			appUI.NewPeerIDPart1, appUI.NewPeerIDPart2, appUI.NewPeerIDPart3)
	}
	if strings.Contains(appUI.NewPeerID, " ") {
		t.Fatalf("NewPeerID must not contain space characters; got %q", appUI.NewPeerID)
	}
	// Save the desk with the new ID
	ttModalValid.Click("Save Desk")
	ttModalValid.Frame()
	if appUI.AddPeerModalOpen {
		t.Fatalf("expected modal to close after saving valid desk")
	}
	// Verify peer saved in store with normalized ID
	peers, err := store.ListPeers()
	if err != nil {
		t.Fatalf("ListPeers: %v", err)
	}
	found := false
	for _, p := range peers {
		if p.DeskID == "612 840 193" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected peer '612 840 193' in peers list: %v", peers)
	}

	// Verify Edit button ("✏️") is present between Favorite ("☆") and Delete ("🗑") on the peer card
	ttDesks := ui.NewTester(appUI.View, 1120, 720)
	if !ttDesks.HasText("✏️") {
		t.Fatalf("expected peer card to have Edit button '✏️'; got texts: %v", ttDesks.Texts())
	}
	// Click the edit button
	ttDesks.Click("✏️")
	ttDesks.Frame()
	if !appUI.AddPeerModalOpen || !appUI.IsEditingPeer {
		t.Fatalf("expected modal to open in edit mode; AddPeerModalOpen=%v, IsEditingPeer=%v",
			appUI.AddPeerModalOpen, appUI.IsEditingPeer)
	}
	if !ttDesks.HasText("Edit Desk") || !ttDesks.HasText("Save Changes") {
		t.Fatalf("expected modal title 'Edit Desk' and button 'Save Changes'; got texts: %v", ttDesks.Texts())
	}
	if appUI.NewPeerIDPart1 != "612" || appUI.NewPeerIDPart2 != "840" || appUI.NewPeerIDPart3 != "193" {
		t.Fatalf("expected pre-filled ID parts '612', '840', '193'; got %q %q %q",
			appUI.NewPeerIDPart1, appUI.NewPeerIDPart2, appUI.NewPeerIDPart3)
	}
	if appUI.NewPeerAlias != "Linux-Workstation" {
		t.Fatalf("expected pre-filled alias 'Linux-Workstation', got %q", appUI.NewPeerAlias)
	}
	// Modify alias and save
	appUI.NewPeerAlias = "Linux-Workstation-Renamed"
	ttDesks.Click("Save Changes")
	ttDesks.Frame()
	if appUI.AddPeerModalOpen {
		t.Fatalf("expected edit modal to close after saving changes")
	}
	// Verify update in store
	updatedPeer, err := store.GetPeer("612 840 193")
	if err != nil {
		t.Fatalf("GetPeer error: %v", err)
	}
	if updatedPeer.Alias != "Linux-Workstation-Renamed" {
		t.Fatalf("expected alias 'Linux-Workstation-Renamed', got %q", updatedPeer.Alias)
	}

	// Verify remote window pure black screen with "Connecting ..."
	ttRemote := ui.NewTester(uiapp.RenderConnectingView, 1024, 640)
	if !ttRemote.HasText("Connecting ...") {
		t.Fatalf("expected remote window to show 'Connecting ...'; got texts: %v", ttRemote.Texts())
	}

	// Verify Connect button on peer card
	if !ttDesks.HasText("Connect") {
		t.Fatalf("expected peer card to have 'Connect' button; got texts: %v", ttDesks.Texts())
	}
	// Simulate opening remote window for peer
	appUI.OpenRemoteWindow("612 840 193", "Linux-Workstation-Renamed")
	ttDesks.Frame()
	if !appUI.IsDeskConnected("612 840 193") {
		t.Fatalf("expected desk '612 840 193' to be marked as connected")
	}
	// Verify button changes to "Disconnect" and old in-window active session screen is NOT rendered
	if !ttDesks.HasText("Disconnect") {
		t.Fatalf("expected peer card button to become 'Disconnect'; got texts: %v", ttDesks.Texts())
	}
	if ttDesks.HasText("REMOTE DESKTOP STREAM") || ttDesks.HasText("Session Chat & Telemetry") || ttDesks.HasText("Send Ctrl+Alt+Del") {
		t.Fatalf("old in-window session view must NOT be rendered; got texts: %v", ttDesks.Texts())
	}
	// Click "Disconnect"
	ttDesks.Click("Disconnect")
	ttDesks.Frame()
	if appUI.IsDeskConnected("612 840 193") {
		t.Fatalf("expected desk '612 840 193' to be disconnected")
	}
	if !ttDesks.HasText("Connect") {
		t.Fatalf("expected button to revert to 'Connect'; got texts: %v", ttDesks.Texts())
	}

	// Reopen modal to verify Cancel and backdrop behavior
	appUI.AddPeerModalOpen = true
	appUI.NewPeerID = "123123193"
	ttModal.Frame()
	if appUI.NewPeerIDPart1 != "123" || appUI.NewPeerIDPart2 != "123" || appUI.NewPeerIDPart3 != "193" {
		t.Fatalf("expected parts 123 123 193, got %s %s %s", appUI.NewPeerIDPart1, appUI.NewPeerIDPart2, appUI.NewPeerIDPart3)
	}

	// Verify continuous backspace across segments: deleting in part 3 continues into part 2 and 1
	appUI.NewPeerIDPart3 = ""
	appUI.PeerIDReqFocus = 3
	appUI.NewPeerID = appUI.NewPeerIDPart1 + appUI.NewPeerIDPart2 + appUI.NewPeerIDPart3
	ttModal.Frame()
	// Backspace in empty part 3 deletes last digit of part 2
	ttModal.Key(0, ui.KeyBackspace)
	ttModal.Frame()
	if appUI.NewPeerIDPart2 != "12" {
		t.Fatalf("expected backspace in empty part 3 to delete last digit of part 2; got %q", appUI.NewPeerIDPart2)
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

func TestValidateHostURL(t *testing.T) {
	tests := []struct {
		input       string
		expectedURL string
		expectErr   bool
	}{
		// Valid IP and domain formats without endpoint/path
		{"192.168.1.1:8443", "http://192.168.1.1:8443", false},
		{"https://192.168.1.1:8443", "https://192.168.1.1:8443", false},
		{"http://192.168.1.1:8443/", "http://192.168.1.1:8443", false},
		{"https://example.com:8443", "https://example.com:8443", false},
		{"example.com", "http://example.com", false},
		{"localhost:8443", "http://localhost:8443", false},
		{"desk-node.local:8443", "http://desk-node.local:8443", false},
		{"https://[::1]:8443", "https://[::1]:8443", false},
		{"", "", false},

		// Invalid: contains endpoint / path
		{"https://192.168.1.1:8443/api", "https://192.168.1.1:8443/api", true},
		{"http://example.com/remote", "http://example.com/remote", true},
		{"10.0.0.1:8080/path/sub", "http://10.0.0.1:8080/path/sub", true},

		// Invalid: query or fragment
		{"http://192.168.1.1:8443?token=123", "http://192.168.1.1:8443?token=123", true},
		{"http://example.com#section", "http://example.com#section", true},

		// Invalid: malformed IP or domain
		{"http://192.168.1.300:8443", "http://192.168.1.300:8443", true},
		{"http://-bad-.com", "http://-bad-.com", true},
		{"http://invalid..domain", "http://invalid..domain", true},
		{"http://", "http://", true},

		// Invalid: port range out of bounds
		{"http://example.com:99999", "http://example.com:99999", true},
		{"http://example.com:0", "http://example.com:0", true},
	}

	for _, tc := range tests {
		gotCleaned, gotErr := uiapp.ValidateHostURL(tc.input)
		if tc.expectErr && gotErr == "" {
			t.Errorf("ValidateHostURL(%q) expected error, but got none (cleaned=%q)", tc.input, gotCleaned)
		}
		if !tc.expectErr && gotErr != "" {
			t.Errorf("ValidateHostURL(%q) unexpected error: %s (cleaned=%q)", tc.input, gotErr, gotCleaned)
		}
		if gotCleaned != tc.expectedURL {
			t.Errorf("ValidateHostURL(%q) cleaned = %q; expected %q", tc.input, gotCleaned, tc.expectedURL)
		}
	}
}

