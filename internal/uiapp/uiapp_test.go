package uiapp

import (
	"path/filepath"
	"testing"

	"gondesk/internal/config"
	"gondesk/internal/database"
	"gondesk/internal/remote"

	"github.com/egoist/mygo/ui"
)

func setupTestAppUI(t *testing.T) (*AppUI, *database.Store) {
	t.Helper()
	tmpDir := t.TempDir()
	cfg := config.Default()
	cfg.DataDir = tmpDir
	cfg.DBPath = filepath.Join(tmpDir, "uiapp_test.db")
	cfg.ListenPort = 8443

	store, err := database.Open(cfg.DBPath, 8443, "https://127.0.0.1:8443")
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}

	mgr := remote.NewManager(cfg, store, nil, nil)
	appUI := NewAppUI(cfg, store, nil, mgr)
	return appUI, store
}

func TestCleanURLUnit(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"   ", ""},
		{"192.168.1.1:8443", "http://192.168.1.1:8443"},
		{"192.168.1.1:8443/", "http://192.168.1.1:8443"},
		{"http://10.20.30.40:8443/", "http://10.20.30.40:8443"},
		{"https://myhost.com/", "https://myhost.com"},
		{"https://myhost.com///", "https://myhost.com"},
	}

	for _, tc := range cases {
		got := CleanURL(tc.input)
		if got != tc.expected {
			t.Fatalf("CleanURL(%q) = %q; expected %q", tc.input, got, tc.expected)
		}
	}
}

func TestValidateHostURLUnit(t *testing.T) {
	// Valid URLs (IP or domain without path/query)
	validCases := []string{
		"192.168.1.100:8443",
		"http://192.168.1.100:8443",
		"https://remote.example.com:8443",
		"localhost:8443",
		"http://127.0.0.1:8443",
	}
	for _, v := range validCases {
		cleaned, errMsg := ValidateHostURL(v)
		if errMsg != "" {
			t.Fatalf("expected %q to be valid, got err: %s (cleaned: %s)", v, errMsg, cleaned)
		}
	}

	// Invalid URLs (path, query, fragment, empty)
	invalidCases := []struct {
		input string
		errSub string
	}{
		{"https://host:8443/api/v1", "path atau endpoint"},
		{"http://10.0.0.1:8443/endpoint", "path atau endpoint"},
		{"https://host:8443?query=1", "query"},
		{"https://host:8443#section", "fragment"},
		{"http://192.168.1.300", "tidak valid"},
	}
	for _, tc := range invalidCases {
		_, errMsg := ValidateHostURL(tc.input)
		if errMsg == "" {
			t.Fatalf("expected %q to be invalid, got no error", tc.input)
		}
	}
}

func TestFilterOnlyDigitsUnit(t *testing.T) {
	if got := FilterOnlyDigits("abc123xyz456", 0); got != "123456" {
		t.Fatalf("expected '123456', got %q", got)
	}
	if got := FilterOnlyDigits("612 840 193", 9); got != "612840193" {
		t.Fatalf("expected '612840193', got %q", got)
	}
	if got := FilterOnlyDigits("1234567890123", 9); got != "123456789" {
		t.Fatalf("expected maxLen 9 truncation '123456789', got %q", got)
	}
}

func TestRenderConnectingViewUnit(t *testing.T) {
	tt := ui.NewTester(RenderConnectingView, 800, 600)
	if !tt.HasText("Connecting ...") {
		t.Fatalf("expected RenderConnectingView to contain 'Connecting ...'; got: %v", tt.Texts())
	}
}

func TestRemoteWindowSessionTracking(t *testing.T) {
	appUI, _ := setupTestAppUI(t)

	peerID := "777 888 999"
	if appUI.IsDeskConnected(peerID) {
		t.Fatalf("expected desk initially not connected")
	}

	// Open remote window in test mode
	appUI.OpenRemoteWindow(peerID, "Office-Workstation")
	if !appUI.IsDeskConnected(peerID) {
		t.Fatalf("expected desk to be marked as connected after OpenRemoteWindow")
	}

	// Disconnect desk
	appUI.DisconnectDesk(nil, peerID)
	if appUI.IsDeskConnected(peerID) {
		t.Fatalf("expected desk to be disconnected after DisconnectDesk")
	}
}
