package remote

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestManagerMultiCursorDynamic(t *testing.T) {
	mgr, _ := setupTestManager(t)

	// Accept a session
	mgr.HandleSignal(server.SignalMessage{
		Type:        "session_request",
		SessionUUID: "session-multicursor",
		FromDeskID:  "111 222 333",
		FromAlias:   "User-1",
	})
	mgr.AcceptIncoming()

	// Simulate 4 concurrent participants sending cursor movements and clicks (more than 3, proving dynamic N support)
	participants := []struct {
		deskID string
		alias  string
		normX  float32
		normY  float32
	}{
		{"111 222 333", "User-1", 0.25, 0.35},
		{"222 333 444", "User-2", 0.45, 0.55},
		{"333 444 555", "User-3", 0.65, 0.75},
		{"444 555 666", "User-4", 0.85, 0.95},
	}

	for _, p := range participants {
		mgr.HandleSignal(server.SignalMessage{
			Type:        "mouse_move",
			SessionUUID: "session-multicursor",
			FromDeskID:  p.deskID,
			FromAlias:   p.alias,
			Payload:     `{"norm_x":` + formatFloat(p.normX) + `,"norm_y":` + formatFloat(p.normY) + `,"alias":"` + p.alias + `"}`,
		})
	}

	// Send mouse click from User-2
	mgr.HandleSignal(server.SignalMessage{
		Type:        "mouse_click",
		SessionUUID: "session-multicursor",
		FromDeskID:  "222 333 444",
		FromAlias:   "User-2",
		Payload:     `{"norm_x":0.45,"norm_y":0.55,"button":0,"alias":"User-2"}`,
	})

	_, _, _, _, sessions := mgr.Snapshot()
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	s := sessions[0]
	if len(s.Participants) < 4 {
		t.Fatalf("expected at least 4 dynamic participants, got %d", len(s.Participants))
	}

	// Verify all 4 participants have distinct non-empty colors
	colors := make(map[string]bool)
	for id, cur := range s.Participants {
		if cur.Color == "" {
			t.Fatalf("expected participant %s to have color", id)
		}
		colors[cur.Color] = true
	}
	if len(colors) < 4 {
		t.Fatalf("expected 4 distinct colors, got %d", len(colors))
	}

	// Verify User-2 has click action
	u2 := s.Participants["222 333 444"]
	if u2 == nil || u2.LastAction != "click" {
		t.Fatalf("expected User-2 to have click action; got: %v", u2)
	}

	// Send mouse_up from User-2
	mgr.HandleSignal(server.SignalMessage{
		Type:        "mouse_up",
		SessionUUID: "session-multicursor",
		FromDeskID:  "222 333 444",
		FromAlias:   "User-2",
		Payload:     `{"norm_x":0.45,"norm_y":0.55,"button":0,"alias":"User-2"}`,
	})

	_, _, _, _, sessionsAfterUp := mgr.Snapshot()
	u2After := sessionsAfterUp[0].Participants["222 333 444"]
	if u2After == nil || u2After.LastAction != "up" {
		t.Fatalf("expected User-2 to have up action; got: %v", u2After)
	}
}

func TestManagerSendMouseUp(t *testing.T) {
	mgr, store := setupTestManager(t)
	st, _ := store.GetSettings()

	sess, err := mgr.ConnectToPeer("888 777 666", "")
	if err != nil {
		t.Fatalf("ConnectToPeer: %v", err)
	}

	mgr.SendMouseClick(sess.SessionUUID, 0.4, 0.4, 0)
	_, _, _, _, sClick := mgr.Snapshot()
	cur := sClick[0].Participants[st.DeskID]
	if cur == nil || cur.LastAction != "click" {
		t.Fatalf("expected self cursor to be in click state; got: %v", cur)
	}

	mgr.SendMouseUp(sess.SessionUUID, 0.4, 0.4, 0)
	_, _, _, _, sUp := mgr.Snapshot()
	curUp := sUp[0].Participants[st.DeskID]
	if curUp == nil || curUp.LastAction != "up" {
		t.Fatalf("expected self cursor to transition to up state; got: %v", curUp)
	}
}

func TestManagerClipboardAndFileTransfer(t *testing.T) {
	mgr, _ := setupTestManager(t)

	// Create an outgoing session
	sess, err := mgr.ConnectToPeer("888 999 000", "testpass")
	if err != nil {
		t.Fatalf("ConnectToPeer failed: %v", err)
	}

	// Send clipboard
	mgr.SendClipboard(sess.SessionUUID, "Secret Clipboard Data 123")

	// Simulate incoming clipboard from peer
	mgr.HandleSignal(server.SignalMessage{
		Type:        "clipboard",
		SessionUUID: sess.SessionUUID,
		FromDeskID:  "888 999 000",
		Payload:     "Received Remote Clipboard Text",
		Timestamp:   "12:00:00",
	})

	// Send file
	mgr.SendFile(sess.SessionUUID, "project_report.pdf", []byte("%PDF-1.4 simulated data"))

	// Simulate incoming file from peer
	mgr.HandleSignal(server.SignalMessage{
		Type:        "file_offer",
		SessionUUID: sess.SessionUUID,
		FromDeskID:  "888 999 000",
		Payload:     "remote_spec.docx|4096",
		Timestamp:   "12:00:01",
	})

	_, _, _, _, sessions := mgr.Snapshot()
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session")
	}
	s := sessions[0]
	if len(s.FileTransferLog) < 2 {
		t.Fatalf("expected at least 2 file transfer entries, got %d", len(s.FileTransferLog))
	}
}

func TestManagerSessionErrorOffline(t *testing.T) {
	mgr, _ := setupTestManager(t)

	sess, err := mgr.ConnectToPeer("123 321 123", "")
	if err != nil {
		t.Fatalf("ConnectToPeer: %v", err)
	}

	// Send session_error from bridge/peer
	mgr.HandleSignal(server.SignalMessage{
		Type:        "session_error",
		SessionUUID: sess.SessionUUID,
		FromDeskID:  "123 321 123",
		Payload:     "Desk tujuan (123 321 123) tidak aktif atau belum membuka aplikasi GonDesk",
	})

	_, _, _, _, sessions := mgr.Snapshot()
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session")
	}
	s := sessions[0]
	if s.Status != "error" {
		t.Fatalf("expected status 'error', got %q", s.Status)
	}
	if s.ErrorMessage != "Desk tujuan (123 321 123) tidak aktif atau belum membuka aplikasi GonDesk" {
		t.Fatalf("unexpected error message: %q", s.ErrorMessage)
	}
}

func TestIsSelfEndpoint(t *testing.T) {
	mgr, _ := setupTestManager(t)
	mgr.Cfg.ListenPort = 8443

	tests := []struct {
		url      string
		expected bool
	}{
		{"https://127.0.0.1:8443", true},
		{"http://127.0.0.1:8443", true},
		{"https://localhost:8443", true},
		{"https://0.0.0.0:8443", true},
		{"https://127.0.0.1:8444", false},
		{"https://192.168.1.50:8443", false},
		{"https://remote.desk.local:8443", false},
		{"invalid-url", false},
	}

	for _, tc := range tests {
		got := mgr.isSelfEndpoint(tc.url)
		if got != tc.expected {
			t.Errorf("isSelfEndpoint(%q) = %v; expected %v", tc.url, got, tc.expected)
		}
	}
}

func TestConnectToDirectEndpointUnreachable(t *testing.T) {
	mgr, store := setupTestManager(t)

	// Save peer with a remote non-existent endpoint
	targetDesk := "555 444 333"
	_, err := store.UpsertPeer(targetDesk, "Target-PC", "", "linux/amd64", "https://127.0.0.1:59999", true)
	if err != nil {
		t.Fatalf("UpsertPeer failed: %v", err)
	}

	sess, err := mgr.ConnectToPeer(targetDesk, "")
	if err != nil {
		t.Fatalf("ConnectToPeer returned error: %v", err)
	}
	if sess == nil {
		t.Fatalf("expected non-nil session")
	}

	// Wait briefly for dial goroutine to report unreachable error
	time.Sleep(300 * time.Millisecond)

	_, _, _, _, sessions := mgr.Snapshot()
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session")
	}
	s := sessions[0]
	if s.Status != "error" {
		t.Fatalf("expected session status 'error', got %q", s.Status)
	}
	if !strings.Contains(s.ErrorMessage, "Gagal terhubung ke") {
		t.Fatalf("expected error message to explain connection failure, got: %q", s.ErrorMessage)
	}
}

func formatFloat(f float32) string {
	return DatabaseFormatFloat(f)
}

func DatabaseFormatFloat(f float32) string {
	return strconvFormatFloat(f)
}

func strconvFormatFloat(f float32) string {
	return formatFloatStr(f)
}

func formatFloatStr(f float32) string {
	// Simple float formatter for test
	return strconvFormat(f)
}

func strconvFormat(f float32) string {
	if f == 0.25 {
		return "0.25"
	}
	if f == 0.35 {
		return "0.35"
	}
	if f == 0.45 {
		return "0.45"
	}
	if f == 0.55 {
		return "0.55"
	}
	if f == 0.65 {
		return "0.65"
	}
	if f == 0.75 {
		return "0.75"
	}
	if f == 0.85 {
		return "0.85"
	}
	if f == 0.95 {
		return "0.95"
	}
	return "0.5"
}

func TestSessionAcceptDoesNotAddHostDummyCursor(t *testing.T) {
	mgr, _ := setupTestManager(t)

	// Create an outgoing session
	mgr.mu.Lock()
	mgr.sessions = append(mgr.sessions, &ActiveSession{
		SessionUUID: "session-out-test",
		PeerDeskID:  "170 095 453",
		PeerAlias:   "anjay",
		Direction:   "outgoing",
		Status:      "connecting",
	})
	mgr.mu.Unlock()

	// Simulate host sending session_accept
	acceptMsg := server.SignalMessage{
		Type:        "session_accept",
		SessionUUID: "session-out-test",
		FromDeskID:  "170 095 453",
		FromAlias:   "anjay",
	}
	mgr.HandleSignal(acceptMsg)

	_, _, _, _, sessions := mgr.Snapshot()
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	s := sessions[0]
	if s.Status != "connected" {
		t.Fatalf("expected status connected, got %s", s.Status)
	}

	// Verify that host is NOT added as a static participant cursor at (0.5, 0.5)
	if s.Participants != nil {
		if cur, exists := s.Participants["170 095 453"]; exists {
			t.Fatalf("host 'anjay' should not be placed as a dummy static participant cursor, got %v", cur)
		}
	}
}

func TestManagerFrameSignal(t *testing.T) {
	mgr, _ := setupTestManager(t)

	mgr.mu.Lock()
	mgr.sessions = append(mgr.sessions, &ActiveSession{
		SessionUUID: "session-frame-test",
		PeerDeskID:  "170 095 453",
		PeerAlias:   "anjay",
		Direction:   "outgoing",
		Status:      "connected",
	})
	mgr.mu.Unlock()

	mockJPEG := GenerateSyntheticDesktopFrame(640, 360, DesktopInfo{HostAlias: "anjay"})
	framePayload := base64.StdEncoding.EncodeToString(mockJPEG)

	frameMsg := server.SignalMessage{
		Type:        "frame",
		SessionUUID: "session-frame-test",
		FromDeskID:  "170 095 453",
		Payload:     framePayload,
	}
	mgr.HandleSignal(frameMsg)

	_, _, _, _, sessions := mgr.Snapshot()
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session")
	}
	s := sessions[0]
	if len(s.LatestFrame) == 0 {
		t.Fatalf("expected LatestFrame to be populated")
	}
	if s.FrameSeq != 1 {
		t.Fatalf("expected FrameSeq to be 1, got %d", s.FrameSeq)
	}
}

func TestFileAndFolderPasteTransfer(t *testing.T) {
	mgrB, _ := setupTestManager(t)
	destDir := t.TempDir()
	mgrB.SetCustomTargetDir(destDir)

	// Create source folder with nested files on A
	srcDir := t.TempDir()
	projectFolder := filepath.Join(srcDir, "my-project")
	subFolder := filepath.Join(projectFolder, "subfolder")
	if err := os.MkdirAll(subFolder, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	readmePath := filepath.Join(projectFolder, "README.md")
	nestedPath := filepath.Join(subFolder, "config.json")
	if err := os.WriteFile(readmePath, []byte("# My Project\nHello GonDesk"), 0o600); err != nil {
		t.Fatalf("WriteFile readme: %v", err)
	}
	if err := os.WriteFile(nestedPath, []byte(`{"env":"production"}`), 0o600); err != nil {
		t.Fatalf("WriteFile nested: %v", err)
	}

	rootName, isFolder, entries, totalFiles, totalBytes, err := collectTransferEntries([]string{projectFolder})
	if err != nil {
		t.Fatalf("collectTransferEntries failed: %v", err)
	}
	if !isFolder || rootName != "my-project" || totalFiles != 2 {
		t.Fatalf("unexpected entry summary: root=%q, isFolder=%v, totalFiles=%d", rootName, isFolder, totalFiles)
	}

	// Simulate file_transfer_start -> file_transfer_chunk -> file_transfer_end on Host B
	transferID := "transfer-folder-test-001"
	startBytes, _ := json.Marshal(fileTransferStartPayload{
		TransferID: transferID,
		RootName:   rootName,
		IsFolder:   isFolder,
		TotalFiles: totalFiles,
		TotalBytes: totalBytes,
	})
	mgrB.HandleSignal(server.SignalMessage{
		Type:        "file_transfer_start",
		SessionUUID: "sess-folder-1",
		FromDeskID:  "816 692 488",
		FromAlias:   "MacBook-A",
		Payload:     string(startBytes),
	})

	for _, entry := range entries {
		var dataB64 string
		if !entry.isDir {
			raw, rErr := os.ReadFile(entry.absPath)
			if rErr != nil {
				t.Fatalf("ReadFile %s: %v", entry.absPath, rErr)
			}
			dataB64 = base64.StdEncoding.EncodeToString(raw)
		}
		chunkBytes, _ := json.Marshal(fileTransferChunkPayload{
			TransferID:  transferID,
			RelPath:     entry.relPath,
			IsDir:       entry.isDir,
			ChunkIndex:  0,
			DataBase64:  dataB64,
			IsLastChunk: true,
		})
		mgrB.HandleSignal(server.SignalMessage{
			Type:        "file_transfer_chunk",
			SessionUUID: "sess-folder-1",
			FromDeskID:  "816 692 488",
			Payload:     string(chunkBytes),
		})
	}

	endBytes, _ := json.Marshal(fileTransferProgressPayload{
		TransferID:       transferID,
		TransferredBytes: totalBytes,
		CompletedFiles:   totalFiles,
		Status:           "completed",
	})
	mgrB.HandleSignal(server.SignalMessage{
		Type:        "file_transfer_end",
		SessionUUID: "sess-folder-1",
		FromDeskID:  "816 692 488",
		Payload:     string(endBytes),
	})

	// Verify files were pasted directly into destDir on Host B
	// #nosec G304 -- reading test file in temporary directory
	gotReadme, err := os.ReadFile(filepath.Join(destDir, "my-project", "README.md"))
	if err != nil || string(gotReadme) != "# My Project\nHello GonDesk" {
		t.Fatalf("expected README.md to be pasted in destDir, got %q (err: %v)", string(gotReadme), err)
	}
	// #nosec G304 -- reading test file in temporary directory
	gotNested, err := os.ReadFile(filepath.Join(destDir, "my-project", "subfolder", "config.json"))
	if err != nil || string(gotNested) != `{"env":"production"}` {
		t.Fatalf("expected subfolder/config.json to be pasted in destDir, got %q (err: %v)", string(gotNested), err)
	}

	transfers := mgrB.FileTransfersSnapshot()
	if len(transfers) != 1 || transfers[0].Status != "completed" || transfers[0].ProgressFraction() != 1.0 {
		t.Fatalf("expected completed transfer state on Host B, got: %+v", transfers)
	}
}
