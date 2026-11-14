package remote

import (
	"path/filepath"
	"testing"

	"gondesk/internal/config"
	"gondesk/internal/database"
	"gondesk/internal/server"
)

func setupTestManager(t *testing.T) (*Manager, *database.Store) {
	t.Helper()
	tmpDir := t.TempDir()
	cfg := config.Default()
	cfg.DataDir = tmpDir
	cfg.DBPath = filepath.Join(tmpDir, "remote_test.db")
	cfg.ListenHost = "127.0.0.1"
	cfg.ListenPort = 8443

	store, err := database.Open(cfg.DBPath, 8443, "https://127.0.0.1:8443")
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}

	changed := false
	mgr := NewManager(cfg, store, nil, func() {
		changed = true
	})
	_ = changed
	return mgr, store
}

func TestManagerSessionLifecycle(t *testing.T) {
	mgr, _ := setupTestManager(t)

	// 1. Initial snapshot
	online, reachable, mapped, incoming, sessions := mgr.Snapshot()
	if incoming != nil || len(sessions) != 0 {
		t.Fatalf("expected empty sessions initially; got incoming=%v, sessions=%d", incoming, len(sessions))
	}
	_ = online
	_ = reachable
	_ = mapped

	// 2. Simulate incoming session_request
	reqMsg := server.SignalMessage{
		Type:        "session_request",
		SessionUUID: "session-abc-123",
		FromDeskID:  "888 777 666",
		FromAlias:   "Remote-Client",
		ToDeskID:    "123 456 789",
		Transport:   "Single-Port HTTPS/WSS + TURNS",
	}
	mgr.HandleSignal(reqMsg)

	_, _, _, incomingAfterReq, _ := mgr.Snapshot()
	if incomingAfterReq == nil || incomingAfterReq.FromDeskID != "888 777 666" {
		t.Fatalf("expected incoming session prompt; got: %v", incomingAfterReq)
	}

	// 3. Accept incoming session
	mgr.AcceptIncoming()
	_, _, _, incomingAfterAccept, sessionsAfterAccept := mgr.Snapshot()
	if incomingAfterAccept != nil {
		t.Fatalf("expected incoming prompt to be cleared after accept")
	}
	if len(sessionsAfterAccept) != 1 || sessionsAfterAccept[0].SessionUUID != "session-abc-123" {
		t.Fatalf("expected 1 active session; got %v", sessionsAfterAccept)
	}

	// 4. Send chat and remote action
	mgr.SendChat("session-abc-123", "Hello from Host!")
	mgr.SendRemoteAction("session-abc-123", "Ctrl+Alt+Del")

	_, _, _, _, sessionsAfterChat := mgr.Snapshot()
	if len(sessionsAfterChat[0].ChatLog) == 0 {
		t.Fatalf("expected chat log to contain message")
	}

	// 5. Disconnect session
	mgr.DisconnectSession("session-abc-123")

	_, _, _, _, sessionsAfterDisc := mgr.Snapshot()
	if len(sessionsAfterDisc) != 0 {
		t.Fatalf("expected 0 sessions after disconnect; got %d", len(sessionsAfterDisc))
	}
}

func TestManagerRejectIncoming(t *testing.T) {
	mgr, _ := setupTestManager(t)

	reqMsg := server.SignalMessage{
		Type:        "session_request",
		SessionUUID: "session-reject-test",
		FromDeskID:  "999 888 777",
		FromAlias:   "Untrusted-Peer",
	}
	mgr.HandleSignal(reqMsg)

	_, _, _, incoming, _ := mgr.Snapshot()
	if incoming == nil {
		t.Fatalf("expected incoming prompt")
	}

	mgr.RejectIncoming()

	_, _, _, incomingAfterReject, sessions := mgr.Snapshot()
	if incomingAfterReject != nil {
		t.Fatalf("expected incoming prompt to be cleared after reject")
	}
	if len(sessions) != 0 {
		t.Fatalf("expected 0 sessions after reject")
	}
}

func TestProbeSTUNOverTLSInvalid(t *testing.T) {
	// Probing an unreachable / closed port should return an error
	_, err := ProbeSTUNOverTLS("https://127.0.0.1:59999")
	if err == nil {
		t.Fatalf("expected error probing unreachable STUN port")
	}
}
