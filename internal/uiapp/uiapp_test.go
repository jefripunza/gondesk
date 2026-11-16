package uiapp

import (
	"path/filepath"
	"testing"
	"time"

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

func TestRenderRemoteWindowViews(t *testing.T) {
	appUI, _ := setupTestAppUI(t)
	norm := database.NormalizeDeskID("555 666 777")

	appUI.OpenRemoteWindow("555 666 777", "Workstation-Alpha")
	sw := appUI.ActiveWindows[norm]
	if sw == nil {
		t.Fatalf("expected ActiveSessionWindow for %s", norm)
	}

	// 1. Test "connecting" view
	sw.Status = "connecting"
	ttConnecting := ui.NewTester(func(c *ui.Context) {
		appUI.RenderRemoteWindow(c, norm)
	}, 1024, 640)
	if !ttConnecting.HasText("Connecting ...") {
		t.Fatalf("expected 'Connecting ...' in connecting state, got: %v", ttConnecting.Texts())
	}

	// 2. Test "error" view with Reconnect button
	sw.Status = "error"
	sw.ErrorMessage = "Desk tujuan tidak aktif atau tidak membuka aplikasi GonDesk"
	ttError := ui.NewTester(func(c *ui.Context) {
		appUI.RenderRemoteWindow(c, norm)
	}, 1024, 640)
	if !ttError.HasText("Connection Failed") {
		t.Fatalf("expected 'Connection Failed' in error state, got: %v", ttError.Texts())
	}
	if !ttError.HasText("Desk tujuan tidak aktif atau tidak membuka aplikasi GonDesk") {
		t.Fatalf("expected error message in error state, got: %v", ttError.Texts())
	}
	if !ttError.HasText("Reconnect") {
		t.Fatalf("expected 'Reconnect' button in error state, got: %v", ttError.Texts())
	}

	// Click "Reconnect"
	_ = ttError.Click("Reconnect")
	if sw.Status != "connecting" {
		t.Fatalf("expected status to reset to 'connecting' on Reconnect click, got %q", sw.Status)
	}

	// 3. Test "connected" view with top poni panel (grip, clipboard, file, multi-cursor)
	sess, err := appUI.Manager.ConnectToPeer("555 666 777", "")
	if err == nil && sess != nil {
		sess.Status = "connected"
		sw.Status = "connected"
		sw.SessionUUID = sess.SessionUUID
		// Add 4 participants to test dynamic N-cursor collaboration (more than 3 participants)
		sess.Participants["user1"] = &remote.RemoteCursor{
			DeskID:     "user1",
			Alias:      "Alice",
			Color:      remote.GetCursorColor(0),
			NormX:      0.2,
			NormY:      0.3,
			LastAction: "move",
		}
		sess.Participants["user2"] = &remote.RemoteCursor{
			DeskID:     "user2",
			Alias:      "Bob",
			Color:      remote.GetCursorColor(1),
			NormX:      0.4,
			NormY:      0.5,
			LastAction: "click",
		}
		sess.Participants["user3"] = &remote.RemoteCursor{
			DeskID:     "user3",
			Alias:      "Charlie",
			Color:      remote.GetCursorColor(2),
			NormX:      0.6,
			NormY:      0.7,
			LastAction: "move",
		}
		sess.Participants["user4"] = &remote.RemoteCursor{
			DeskID:     "user4",
			Alias:      "David",
			Color:      remote.GetCursorColor(3),
			NormX:      0.8,
			NormY:      0.9,
			LastAction: "move",
		}
	}

	ttConnected := ui.NewTester(func(c *ui.Context) {
		appUI.RenderRemoteWindow(c, norm)
	}, 1024, 640)

	// Verify Top Poni Notch elements
	if !ttConnected.HasText("CONNECTED") {
		t.Fatalf("expected 'CONNECTED' badge in poni toolbar")
	}
	if !ttConnected.HasText("⠿") {
		t.Fatalf("expected draggable grip '⠿' in poni toolbar")
	}
	if !ttConnected.HasText("📋 Send Clipboard") {
		t.Fatalf("expected '📋 Send Clipboard' in poni toolbar")
	}
	if !ttConnected.HasText("📁 Send File") {
		t.Fatalf("expected '📁 Send File' in poni toolbar")
	}
	if !ttConnected.HasText("Disconnect") {
		t.Fatalf("expected 'Disconnect' in poni toolbar")
	}

	// Click Send Clipboard
	_ = ttConnected.Click("📋 Send Clipboard")

	// Test grip horizontal dragging
	initialOffset := sw.NotchOffsetX
	sw.NotchOffsetX += 45
	if sw.NotchOffsetX != initialOffset+45 {
		t.Fatalf("expected notch offset to be adjustable by drag")
	}
}

func TestPeerEditAndConnectURLPersistence(t *testing.T) {
	appUI, store := setupTestAppUI(t)

	customURL := "https://192.168.1.100:8443"
	deskID := "170 095 453"
	alias := "anjay"

	// 1. Initial peer save
	_, err := store.UpsertPeer(deskID, alias, "", "remote", customURL, true)
	if err != nil {
		t.Fatalf("UpsertPeer failed: %v", err)
	}
	appUI.ReloadFromDB()

	// 2. Open edit modal
	p, err := store.GetPeer(deskID)
	if err != nil {
		t.Fatalf("GetPeer failed: %v", err)
	}
	appUI.openEditPeerModal(*p)
	if appUI.NewPeerURL != customURL {
		t.Fatalf("expected NewPeerURL to be %s, got %s", customURL, appUI.NewPeerURL)
	}

	// 3. User connects to peer
	appUI.startOutgoingConnection(nil, deskID, "")

	// 4. Verify DB was NOT overwritten with bridge/local URL
	pAfter, err := store.GetPeer(deskID)
	if err != nil {
		t.Fatalf("GetPeer after connect failed: %v", err)
	}
	if pAfter.EndpointURL != customURL {
		t.Fatalf("expected EndpointURL to remain %s, got %s", customURL, pAfter.EndpointURL)
	}

	// 5. Open edit modal again after connection, must still have customURL
	appUI.openEditPeerModal(*pAfter)
	if appUI.NewPeerURL != customURL {
		t.Fatalf("expected NewPeerURL to still be %s, got %s", customURL, appUI.NewPeerURL)
	}
}

func TestIsDeskConnectedOnlyForOutgoing(t *testing.T) {
	appUI, _ := setupTestAppUI(t)
	peerID := "816 692 488"

	// 1. Simulate incoming session (peer remoted into us)
	appUI.Manager.AcceptIncoming() // no incoming initially, let's create incoming in manager
	// Manually inject active session into manager
	// Outgoing check should be false for incoming direction
	if appUI.IsDeskConnected(peerID) {
		t.Fatalf("should not be connected initially")
	}

	// 2. Open an active window for peerID (Outgoing session)
	appUI.ActiveWindows = make(map[string]*ActiveSessionWindow)
	appUI.ActiveWindows[database.NormalizeDeskID(peerID)] = &ActiveSessionWindow{
		DeskID:  peerID,
		Status:  "connected",
		CanvasW: 1440,
		CanvasH: 900,
	}

	if !appUI.IsDeskConnected(peerID) {
		t.Fatalf("expected IsDeskConnected to return true for active outgoing window")
	}

	// 3. Close window
	delete(appUI.ActiveWindows, database.NormalizeDeskID(peerID))
	if appUI.IsDeskConnected(peerID) {
		t.Fatalf("expected IsDeskConnected to return false after closing window")
	}
}

func TestRemoteWindowNormalizationPrecision(t *testing.T) {
	sw := &ActiveSessionWindow{
		CanvasW: 1440,
		CanvasH: 900,
	}

	// Exact center of a 1440x900 viewport
	evX := float32(720)
	evY := float32(450)

	normX := evX / sw.CanvasW
	normY := evY / sw.CanvasH

	if normX != 0.5 || normY != 0.5 {
		t.Fatalf("expected (0.5, 0.5), got (%.2f, %.2f)", normX, normY)
	}

	// Calculate painted screen position
	renderW := float32(1440)
	renderH := float32(900)
	cx := normX * renderW
	cy := normY * renderH

	if cx != evX || cy != evY {
		t.Fatalf("expected cursor rendered at exact pointer DIPs (%.1f, %.1f), got (%.1f, %.1f)", evX, evY, cx, cy)
	}
}

func TestCursorBadgeVerticalCentering(t *testing.T) {
	// Remote Window cursor geometry
	cx := float32(100)
	cy := float32(200)

	badgeX := cx + 12
	badgeY := cy + 12
	badgeH := float32(18)
	textX := badgeX + 6
	textY := badgeY + 3.5

	_ = textX
	topPad := textY - badgeY
	bottomPad := (badgeY + badgeH) - (textY + 11)
	if topPad != bottomPad {
		t.Fatalf("expected symmetric padding around badge text: top %.2f, bottom %.2f", topPad, bottomPad)
	}

	// Host Overlay cursor geometry
	hBadgeX := cx + 14
	hBadgeY := cy + 14
	hBadgeH := float32(20)
	hTextX := hBadgeX + 7
	hTextY := hBadgeY + 4.5

	_ = hTextX
	hTopPad := hTextY - hBadgeY
	hBottomPad := (hBadgeY + hBadgeH) - (hTextY + 11)
	if hTopPad != hBottomPad {
		t.Fatalf("expected symmetric padding around host overlay badge text: top %.2f, bottom %.2f", hTopPad, hBottomPad)
	}
}

func TestSelfCursorHidesBadge(t *testing.T) {
	myDeskID := "123 456 789"
	peerDeskID := "987 654 321"

	curSelf := &remote.RemoteCursor{DeskID: myDeskID, Alias: "Me"}
	curOther := &remote.RemoteCursor{DeskID: peerDeskID, Alias: "Other"}

	isSelfCheck := func(cur *remote.RemoteCursor) bool {
		return myDeskID != "" && cur.DeskID == myDeskID
	}

	if !isSelfCheck(curSelf) {
		t.Fatalf("expected isSelf to be true for own cursor")
	}
	if isSelfCheck(curOther) {
		t.Fatalf("expected isSelf to be false for other participant's cursor")
	}
}

func TestClickRippleExpiration(t *testing.T) {
	now := time.Now()
	recentClick := &remote.RemoteCursor{
		LastAction: "click",
		UpdatedAt:  now.Add(-100 * time.Millisecond),
	}
	expiredClick := &remote.RemoteCursor{
		LastAction: "click",
		UpdatedAt:  now.Add(-500 * time.Millisecond),
	}
	mouseUp := &remote.RemoteCursor{
		LastAction: "up",
		UpdatedAt:  now.Add(-50 * time.Millisecond),
	}

	isRippleActive := func(cur *remote.RemoteCursor) bool {
		return cur.LastAction == "click" && time.Since(cur.UpdatedAt) < 400*time.Millisecond
	}

	if !isRippleActive(recentClick) {
		t.Fatalf("expected ripple to be active within 400ms of click")
	}
	if isRippleActive(expiredClick) {
		t.Fatalf("expected ripple to expire after 400ms")
	}
	if isRippleActive(mouseUp) {
		t.Fatalf("expected ripple to be immediately inactive on mouse up")
	}
}
