package uiapp

import (
	"net"
	"net/url"
	"strconv"
	"strings"

	"gondesk/internal/config"
	"gondesk/internal/database"
	"gondesk/internal/remote"
	"gondesk/internal/server"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// AppUI holds the persistent application state rendered by MyGo Native UI (package ui).
// Keep ui.Element and *ui.Context out of this struct per MyGo lifetime rules.
type AppUI struct {
	Cfg     *config.Config
	Store   *database.Store
	Node    *server.UnifiedNode
	Manager *remote.Manager
	Win     *mygo.Window

	// Navigation & Tab state
	ActiveTab       int    // 0 = Home / Desks View, 1 = Settings
	SettingsSection string // "general", "network", "about"
	FilterTab       int    // 0 = All Desks, 1 = Favorites, 2 = Online Only

	// Active session windows tracked by normalized DeskID
	ActiveWindows map[string]*ActiveSessionWindow

	// Local Desk ("This Desk" - Di-remote) state bound to controls
	DeskID            string
	AliasInput        string
	AccessPassword    string
	UnattendedEnabled bool
	AllowControl      bool
	AllowClipboard    bool
	BridgeURLInput    string

	// Quick Connect URL form in Left Pane (Name optional + HTTP/HTTPS URL required)
	QuickConnectName string
	QuickConnectURL  string

	// "Remote Desk" (Me-remote) state bound to controls
	RemoteTargetInput   string
	RemotePasswordInput string
	SearchQuery         string

	// Add/Edit Peer Modal state
	AddPeerModalOpen      bool
	IsEditingPeer         bool
	EditingOriginalDeskID string
	NewPeerURL            string
	NewPeerURLError       string
	NewPeerID             string
	NewPeerIDPart1        string
	NewPeerIDPart2        string
	NewPeerIDPart3        string
	PeerIDReqFocus        int
	PeerIDFocused         bool
	NewPeerAlias          string

	// Delete confirmation modal state
	DeleteConfirmModalOpen bool
	PendingDeletePeerID    string
	PendingDeletePeerAlias string

	// Focus tracking for onblur URL normalization
	modalPeerURLFocused bool

	// Cached SQLite records refreshed on state changes
	Peers       []database.PeerRecord
	SessionLogs []database.SessionLog
	TurnCreds   []database.TurnCredential

	// Host desktop shadow cursor overlay window for incoming remote sessions
	HostOverlayWin *mygo.Window

	// Dedicated file/folder paste status windows tracked by TransferID
	PasteWindows map[string]*mygo.Window
}

// NewAppUI initializes the UI state from SQLite and Configuration.
func NewAppUI(cfg *config.Config, store *database.Store, node *server.UnifiedNode, mgr *remote.Manager) *AppUI {
	a := &AppUI{
		Cfg:             cfg,
		Store:           store,
		Node:            node,
		Manager:         mgr,
		SettingsSection: "general",
		ActiveWindows:   make(map[string]*ActiveSessionWindow),
		PasteWindows:    make(map[string]*mygo.Window),
	}
	a.ReloadFromDB()
	return a
}

// ReloadFromDB refreshes cached settings, peers, session logs, and TURN credentials from SQLite.
func (a *AppUI) ReloadFromDB() {
	if a.Store == nil {
		return
	}
	if st, err := a.Store.GetSettings(); err == nil {
		a.DeskID = st.DeskID
		a.AliasInput = st.Alias
		a.AccessPassword = st.AccessPassword
		a.UnattendedEnabled = st.UnattendedEnabled
		a.AllowControl = st.AllowControl
		a.AllowClipboard = st.AllowClipboard
		a.BridgeURLInput = st.BridgeServerURL
	}
	if peers, err := a.Store.ListPeers(); err == nil {
		a.Peers = peers
	}
	if logs, err := a.Store.ListSessionLogs(20); err == nil {
		a.SessionLogs = logs
	}
	if creds, err := a.Store.ListTurnCredentials(); err == nil {
		a.TurnCreds = creds
	}
}

// NotifyChanged safely schedules a UI state refresh on the main thread.
func (a *AppUI) NotifyChanged() {
	if a.Manager != nil && a.ActiveWindows != nil {
		_, _, _, _, sessions := a.Manager.Snapshot()
		for i := range sessions {
			s := &sessions[i]
			norm := database.NormalizeDeskID(s.PeerDeskID)
			if sw, ok := a.ActiveWindows[norm]; ok {
				sw.Status = s.Status
				if s.ErrorMessage != "" {
					sw.ErrorMessage = s.ErrorMessage
				}
				if sw.Win != nil {
					sw.Win.Update(func() {})
				}
			}
		}
	}
	if a.Win != nil {
		a.Win.Update(func() {
			a.ReloadFromDB()
			a.UpdateHostOverlay()
			a.UpdateFilePasteWindows()
			a.checkWindowsScreenCaptureDialog()
		})
	} else {
		a.UpdateFilePasteWindows()
	}
}

func (a *AppUI) checkWindowsScreenCaptureDialog() {
	if a.Manager == nil || a.Manager.ScreenEngine() == nil {
		return
	}
	if a.Manager.ScreenEngine().NeedsWindowsDialog() {
		a.Manager.ScreenEngine().SetWindowsDialogShown()
		go func() {
			res, _ := mygo.Dialog.Message(mygo.MessageOptions{
				Parent:  a.Win,
				Type:    mygo.MessageQuestion,
				Message: "Komponen Screen Capture Diperlukan",
				Detail:  "Windows memerlukan izin atau komponen pendukung untuk penangkapan layar desktop. Pasang sekarang?",
				Buttons: []string{"Pasang Sekarang", "Batal"},
			})
			_ = res
		}()
	}
}

// View is the root MyGo Native UI frame builder.
func (a *AppUI) View(c *ui.Context) {
	// Apply AnyDesk coral-red accent color (#EF443B)
	anydeskRed := ui.Hex("#EF443B")
	theme := *c.Theme().WithAccent(anydeskRed)
	theme.Radius = 8
	c.SetTheme(&theme)
	t := c.Theme()

	ui.Column(c).Fill().Background(t.Background).Children(func() {
		a.renderTopBar(c, t)
		ui.Divider(c)

		// Desktop Mode (can Remote and Be Remoted)
		_, _, _, incoming, _ := a.Manager.Snapshot()
		if incoming != nil {
			a.renderIncomingBanner(c, t, incoming)
		}

		if a.ActiveTab == 1 {
			a.renderDesktopSettings(c, t)
		} else {
			a.renderDesksView(c, t)
		}
	})

	a.renderAddPeerModal(c, t)
	a.renderDeleteConfirmModal(c, t)
}

// CleanURL normalizes a URL or host string:
// 1. Strips any trailing slashes ("/")
// 2. Prepends "http://" if neither "http://" nor "https://" scheme is present
func CleanURL(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	for strings.HasSuffix(s, "/") && s != "http://" && s != "https://" {
		s = strings.TrimSuffix(s, "/")
	}
	lower := strings.ToLower(s)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		s = "http://" + s
	}
	for strings.HasSuffix(s, "/") && s != "http://" && s != "https://" {
		s = strings.TrimSuffix(s, "/")
	}
	return s
}

// ValidateHostURL validates that raw is strictly an IP or domain address (with optional port)
// without any path/endpoint, query, or fragment.
// It returns the cleaned URL and an error message string (empty if valid).
func ValidateHostURL(raw string) (string, string) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", ""
	}

	cleaned := CleanURL(trimmed)
	u, err := url.Parse(cleaned)
	if err != nil {
		return cleaned, "Format URL tidak valid"
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return cleaned, "Skema URL harus http:// atau https://"
	}

	if u.Path != "" && u.Path != "/" {
		return cleaned, "URL tidak boleh berisi path atau endpoint (hanya IP atau domain, contoh: https://host:8443)"
	}

	if u.RawQuery != "" || u.Fragment != "" {
		return cleaned, "URL tidak boleh berisi query atau fragment"
	}

	if u.Host == "" {
		return cleaned, "Alamat IP atau domain tidak valid"
	}

	hostname := u.Hostname()
	portStr := u.Port()

	if portStr != "" {
		portNum, err := strconv.Atoi(portStr)
		if err != nil || portNum < 1 || portNum > 65535 {
			return cleaned, "Port tidak valid (1-65535)"
		}
	}

	if ip := net.ParseIP(hostname); ip != nil {
		return cleaned, ""
	}

	if !isValidDomainOrHostname(hostname) {
		return cleaned, "Alamat IP atau domain tidak valid"
	}

	return cleaned, ""
}

// isValidDomainOrHostname checks whether h is a syntactically valid domain or hostname.
// Numeric TLDs (e.g. 192.168.1.300) are rejected as invalid domains.
func isValidDomainOrHostname(h string) bool {
	h = strings.TrimSuffix(h, ".")
	if h == "" || len(h) > 253 {
		return false
	}
	if strings.ContainsAny(h, " /\\:@?#") {
		return false
	}

	labels := strings.Split(h, ".")
	for _, label := range labels {
		if label == "" || len(label) > 63 {
			return false
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := range len(label) {
			c := label[i]
			if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' {
				return false
			}
		}
	}

	lastLabel := labels[len(labels)-1]
	allDigits := true
	for i := range len(lastLabel) {
		if lastLabel[i] < '0' || lastLabel[i] > '9' {
			allDigits = false
			break
		}
	}
	if len(labels) > 1 && allDigits {
		return false
	}
	if len(labels) == 1 && allDigits {
		return false
	}

	return true
}

// FilterOnlyDigits returns only ASCII digits '0'-'9' from s, truncated to maxLen if maxLen > 0.
func FilterOnlyDigits(s string, maxLen int) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
			if maxLen > 0 && b.Len() >= maxLen {
				break
			}
		}
	}
	return b.String()
}

// syncPeerID synchronizes a.NewPeerID with its 3 segments (Part1, Part2, Part3).
// If a.NewPeerID was modified externally, it splits it into 3 segments.
// If any part was modified in the UI, it merges them into a.NewPeerID.
func (a *AppUI) syncPeerID() {
	combined := a.NewPeerIDPart1 + a.NewPeerIDPart2 + a.NewPeerIDPart3
	if a.NewPeerID != combined {
		clean := FilterOnlyDigits(a.NewPeerID, 9)
		switch {
		case len(clean) <= 3:
			a.NewPeerIDPart1 = clean
			a.NewPeerIDPart2 = ""
			a.NewPeerIDPart3 = ""
		case len(clean) <= 6:
			a.NewPeerIDPart1 = clean[0:3]
			a.NewPeerIDPart2 = clean[3:]
			a.NewPeerIDPart3 = ""
		default:
			a.NewPeerIDPart1 = clean[0:3]
			a.NewPeerIDPart2 = clean[3:6]
			a.NewPeerIDPart3 = clean[6:]
		}
		a.NewPeerID = a.NewPeerIDPart1 + a.NewPeerIDPart2 + a.NewPeerIDPart3
	}
}
