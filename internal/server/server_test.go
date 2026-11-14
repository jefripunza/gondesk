package server

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gondesk/internal/config"
	"gondesk/internal/database"
)

func setupTestNode(t *testing.T) (*UnifiedNode, string) {
	t.Helper()
	tmpDir := t.TempDir()
	cfg := config.Default()
	cfg.Mode = config.ModeServer
	cfg.DataDir = tmpDir
	cfg.DBPath = filepath.Join(tmpDir, "server_test.db")
	cfg.CertFile = filepath.Join(tmpDir, "certs", "server.crt")
	cfg.KeyFile = filepath.Join(tmpDir, "certs", "server.key")
	cfg.ListenHost = "127.0.0.1"
	cfg.ListenPort = 0
	cfg.PublicIP = "127.0.0.1"
	cfg.EnableTLS = true

	store, err := database.Open(cfg.DBPath, 0, "")
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}

	node, err := NewUnifiedNode(cfg, store, nil)
	if err != nil {
		t.Fatalf("NewUnifiedNode: %v", err)
	}
	node.Start()

	httpsURL := fmt.Sprintf("https://127.0.0.1:%d", node.Mux.Port())
	return node, httpsURL
}

func TestUnifiedNodeEndpoints(t *testing.T) {
	node, httpsURL := setupTestNode(t)
	defer func() { _ = node.Stop() }()

	client := &http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{
			// #nosec G402 -- self-signed cert for local test node
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	// 1. GET /api/v1/info
	resp, err := client.Get(httpsURL + "/api/v1/info")
	if err != nil {
		t.Fatalf("GET /api/v1/info: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var info map[string]any
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &info); err != nil {
		t.Fatalf("unmarshal info: %v", err)
	}
	if info["app"] != "gondesk" {
		t.Fatalf("expected app gondesk, got %v", info["app"])
	}

	// 2. GET /api/v1/turn/credentials
	respTurn, err := client.Get(httpsURL + "/api/v1/turn/credentials")
	if err != nil {
		t.Fatalf("GET /api/v1/turn/credentials: %v", err)
	}
	defer func() { _ = respTurn.Body.Close() }()
	if respTurn.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", respTurn.StatusCode)
	}

	// 3. GET /api/v1/peers
	respPeers, err := client.Get(httpsURL + "/api/v1/peers")
	if err != nil {
		t.Fatalf("GET /api/v1/peers: %v", err)
	}
	defer func() { _ = respPeers.Body.Close() }()
	if respPeers.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", respPeers.StatusCode)
	}

	// 4. GET /api/v1/sessions
	respSessions, err := client.Get(httpsURL + "/api/v1/sessions")
	if err != nil {
		t.Fatalf("GET /api/v1/sessions: %v", err)
	}
	defer func() { _ = respSessions.Body.Close() }()
	if respSessions.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", respSessions.StatusCode)
	}
}

func TestSignalMessageEnvelope(t *testing.T) {
	msg := SignalMessage{
		Type:        "session_request",
		SessionUUID: "test-uuid-999",
		FromDeskID:  "111 222 333",
		ToDeskID:    "444 555 666",
		Transport:   "Single-Port HTTPS/WSS + TURNS",
		Timestamp:   "2026-11-15T00:00:00Z",
	}

	// #nosec G117 -- test serialization of signaling envelope
	bytes, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal SignalMessage: %v", err)
	}

	var decoded SignalMessage
	if err := json.Unmarshal(bytes, &decoded); err != nil {
		t.Fatalf("unmarshal SignalMessage: %v", err)
	}
	if decoded.Type != "session_request" || decoded.SessionUUID != "test-uuid-999" {
		t.Fatalf("unexpected decoded SignalMessage: %+v", decoded)
	}
}

func TestBroadcastSignalAndPeerSend(t *testing.T) {
	node, _ := setupTestNode(t)
	defer func() { _ = node.Stop() }()

	msg := SignalMessage{
		Type:        "cursor_broadcast",
		SessionUUID: "session-broad-test",
		FromDeskID:  "111 222 333",
		ToDeskID:    "999 999 999",
		Payload:     `{"norm_x":0.5,"norm_y":0.5}`,
	}

	// Sending to non-existent peer should return false safely
	if node.SendSignalToPeer(msg) {
		t.Fatalf("expected SendSignalToPeer to return false for non-connected peer")
	}

	// BroadcastSignal to empty client map should not panic
	node.BroadcastSignal(msg, "111 222 333")
	if count := node.OnlinePeerCount(); count != 0 {
		t.Fatalf("expected 0 online peers, got %d", count)
	}
}

func TestDesktopModeDoesNotUpsertIncomingPeers(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.Default()
	cfg.Mode = config.ModeDesktop
	cfg.DataDir = tmpDir
	cfg.DBPath = filepath.Join(tmpDir, "desktop_test.db")
	cfg.CertFile = filepath.Join(tmpDir, "certs", "server.crt")
	cfg.KeyFile = filepath.Join(tmpDir, "certs", "server.key")
	cfg.ListenHost = "127.0.0.1"
	cfg.ListenPort = 0
	cfg.PublicIP = "127.0.0.1"
	cfg.EnableTLS = true

	store, err := database.Open(cfg.DBPath, 0, "")
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}

	node, err := NewUnifiedNode(cfg, store, nil)
	if err != nil {
		t.Fatalf("NewUnifiedNode: %v", err)
	}
	node.Start()
	defer func() { _ = node.Stop() }()

	client := &http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{
			// #nosec G402 -- self-signed cert for local test node
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	httpsURL := fmt.Sprintf("https://127.0.0.1:%d", node.Mux.Port())
	reqBody := `{"desk_id":"816692488","alias":"Jefris-MacBook-Pro.local"}`
	resp, err := client.Post(httpsURL+"/api/v1/peers/heartbeat", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST /peers/heartbeat: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	// Verify that in desktop mode, the caller (A) was NOT saved to the SQLite peers address book of B!
	peers, err := store.ListPeers()
	if err != nil {
		t.Fatalf("ListPeers error: %v", err)
	}
	for _, p := range peers {
		if database.NormalizeDeskID(p.DeskID) == "816692488" {
			t.Fatalf("incoming peer 816 692 488 was inadvertently saved to host address book in desktop mode")
		}
	}
}
