package database

import (
	"path/filepath"
	"strings"
	"testing"
)

func setupTestStore(t *testing.T) *Store {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_store.db")
	store, err := Open(dbPath, 8443, "https://127.0.0.1:8443")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	return store
}

func TestStoreSettings(t *testing.T) {
	store := setupTestStore(t)

	st, err := store.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings failed: %v", err)
	}
	if st.DeskID == "" {
		t.Fatalf("expected auto-generated DeskID in settings")
	}

	st.Alias = "Custom-Workstation"
	st.AllowControl = true
	st.AllowClipboard = true
	st.UnattendedEnabled = true
	st.AccessPassword = "securepassword123"

	if err := store.SaveSettings(&st); err != nil {
		t.Fatalf("SaveSettings failed: %v", err)
	}

	updated, err := store.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings after save failed: %v", err)
	}
	if updated.Alias != "Custom-Workstation" {
		t.Fatalf("expected alias 'Custom-Workstation', got %s", updated.Alias)
	}
	if !updated.AllowControl || !updated.AllowClipboard || !updated.UnattendedEnabled {
		t.Fatalf("expected updated permissions to be true")
	}
}

func TestStorePeersAndFavorites(t *testing.T) {
	store := setupTestStore(t)

	// Upsert new peer
	p1, err := store.UpsertPeer("111 222 333", "Office-PC", "office.local", "linux/amd64", "https://192.168.1.50:8443", true)
	if err != nil {
		t.Fatalf("UpsertPeer failed: %v", err)
	}
	if p1.Alias != "Office-PC" {
		t.Fatalf("expected alias Office-PC, got %s", p1.Alias)
	}

	// Retrieve by DeskID
	got, err := store.GetPeer("111222333") // Test without spaces to verify NormalizeDeskID
	if err != nil {
		t.Fatalf("GetPeer failed: %v", err)
	}
	if got.DeskID != "111 222 333" {
		t.Fatalf("expected normalized DeskID '111 222 333', got %s", got.DeskID)
	}

	// Update existing peer alias
	_, err = store.UpsertPeer("111 222 333", "Office-PC-Updated", "office.local", "linux/amd64", "https://192.168.1.50:8443", true)
	if err != nil {
		t.Fatalf("Update UpsertPeer failed: %v", err)
	}
	gotUpdated, _ := store.GetPeer("111 222 333")
	if gotUpdated.Alias != "Office-PC-Updated" {
		t.Fatalf("expected updated alias, got %s", gotUpdated.Alias)
	}

	// Toggle favorite
	if err := store.ToggleFavorite("111 222 333"); err != nil {
		t.Fatalf("ToggleFavorite failed: %v", err)
	}
	favPeer, _ := store.GetPeer("111 222 333")
	if !favPeer.IsFavorite {
		t.Fatalf("expected IsFavorite to be true")
	}

	// Increment connection count
	store.IncrementPeerConnection("111 222 333", "https://192.168.1.50:8443")
	incPeer, _ := store.GetPeer("111 222 333")
	if incPeer.ConnectionCount < 1 {
		t.Fatalf("expected connection count >= 1, got %d", incPeer.ConnectionCount)
	}

	// Verify that calling IncrementPeerConnection with a different URL does NOT overwrite the peer's custom saved URL
	store.IncrementPeerConnection("111 222 333", "https://127.0.0.1:8443")
	incPeerAfter, _ := store.GetPeer("111 222 333")
	if incPeerAfter.EndpointURL != "https://192.168.1.50:8443" {
		t.Fatalf("expected EndpointURL to remain 'https://192.168.1.50:8443', got '%s'", incPeerAfter.EndpointURL)
	}

	// Update peer details directly
	_, err = store.UpdatePeerDetails("111 222 333", "Renamed-PC", "https://custom.desk.io:8443")
	if err != nil {
		t.Fatalf("UpdatePeerDetails failed: %v", err)
	}
	updatedPeer, _ := store.GetPeer("111 222 333")
	if updatedPeer.Alias != "Renamed-PC" || updatedPeer.EndpointURL != "https://custom.desk.io:8443" {
		t.Fatalf("unexpected peer after UpdatePeerDetails: %+v", updatedPeer)
	}

	// List peers
	peers, err := store.ListPeers()
	if err != nil || len(peers) == 0 {
		t.Fatalf("expected peers list, got: %v (err: %v)", peers, err)
	}

	// Delete peer
	if err := store.DeletePeer("111 222 333"); err != nil {
		t.Fatalf("DeletePeer failed: %v", err)
	}
	if _, err := store.GetPeer("111 222 333"); err == nil {
		t.Fatalf("expected peer to be deleted")
	}
}

func TestStoreSessionsAndTurnCredentials(t *testing.T) {
	store := setupTestStore(t)

	sessionUUID := "sess-12345-abcde"
	err := store.RecordSession(sessionUUID, "111 222 333", "444 555 666", "Single-Port HTTPS/WSS", "active", "initiating test session")
	if err != nil {
		t.Fatalf("RecordSession failed: %v", err)
	}

	logs, err := store.ListSessionLogs(10)
	if err != nil || len(logs) == 0 {
		t.Fatalf("ListSessionLogs failed: %v", err)
	}
	if logs[0].SessionUUID != sessionUUID {
		t.Fatalf("expected session UUID %s, got %s", sessionUUID, logs[0].SessionUUID)
	}

	// Update session to completed
	err = store.RecordSession(sessionUUID, "111 222 333", "444 555 666", "Single-Port HTTPS/WSS", "completed", "closed normally")
	if err != nil {
		t.Fatalf("RecordSession completed update failed: %v", err)
	}

	// Test TURN credentials
	creds, err := store.ListTurnCredentials()
	if err != nil || len(creds) == 0 {
		t.Fatalf("expected default TURN credentials seeded: %v", err)
	}

	pwd, ok := store.LookupTurnPassword("gondesk")
	// #nosec G101 -- test assertion for default dev TURN password
	if !ok || pwd != "gondesk-turn-secret" {
		t.Fatalf("LookupTurnPassword failed; ok=%v, pwd=%s", ok, pwd)
	}

	_, ok = store.LookupTurnPassword("nonexistent-user")
	if ok {
		t.Fatalf("expected nonexistent-user to fail lookup")
	}
}

func TestDeskIDGenerationAndNormalization(t *testing.T) {
	id := GenerateDeskID()
	parts := strings.Split(id, " ")
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts in generated DeskID, got %s", id)
	}
	for _, p := range parts {
		if len(p) != 3 {
			t.Fatalf("expected each part to be 3 digits, got %s in %s", p, id)
		}
	}

	// Test NormalizeDeskID
	if norm := NormalizeDeskID("123456789"); norm != "123 456 789" {
		t.Fatalf("expected '123 456 789', got %s", norm)
	}
	if norm := NormalizeDeskID(" 123-456-789 "); norm != "123 456 789" {
		t.Fatalf("expected '123 456 789', got %s", norm)
	}
	if norm := NormalizeDeskID("short"); norm != "short" {
		t.Fatalf("expected raw string returned if not 9 digits, got %s", norm)
	}
}
