package uiapp

import (
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
	ActiveTab     int // 0 = Home / Console, 1 = Settings, >=2 = Active Session index (idx - 2)
	FilterTab     int // 0 = All Desks, 1 = Favorites, 2 = Online Only
	LeftPaneWidth float32

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

	// Add Peer Modal state
	AddPeerModalOpen bool
	NewPeerURL       string
	NewPeerID        string
	NewPeerAlias     string

	// Delete confirmation modal state
	DeleteConfirmModalOpen bool
	PendingDeletePeerID    string
	PendingDeletePeerAlias string

	// Focus tracking for onblur URL normalization
	quickConnectURLFocused bool
	modalPeerURLFocused    bool

	// Active session chat draft
	ChatDraft string

	// Server Mode: Add TURN Credential form
	NewTurnUser string
	NewTurnPass string

	// Cached SQLite records refreshed on state changes
	Peers       []database.PeerRecord
	SessionLogs []database.SessionLog
	TurnCreds   []database.TurnCredential
}

// NewAppUI initializes the UI state from SQLite and Configuration.
func NewAppUI(cfg *config.Config, store *database.Store, node *server.UnifiedNode, mgr *remote.Manager) *AppUI {
	a := &AppUI{
		Cfg:           cfg,
		Store:         store,
		Node:          node,
		Manager:       mgr,
		LeftPaneWidth: 340,
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
	if a.Win != nil {
		a.Win.Update(func() {
			a.ReloadFromDB()
		})
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

		// If running in Server Mode (--mode=server), hide ALL desktop remote & be-remoted UI!
		if a.Cfg.IsServerMode() {
			a.renderServerBridgeDashboard(c, t)
			return
		}

		// Desktop Mode (can Remote and Be Remoted)
		_, _, _, incoming, sessions := a.Manager.Snapshot()
		if incoming != nil {
			a.renderIncomingBanner(c, t, incoming)
		}

		if a.ActiveTab == 0 {
			a.renderDesktopMainSplit(c, t)
		} else if a.ActiveTab == 1 {
			a.renderDesktopSettings(c, t)
		} else {
			sessIdx := a.ActiveTab - 2
			if sessIdx >= 0 && sessIdx < len(sessions) {
				a.renderActiveSessionView(c, t, sessions[sessIdx])
			} else {
				a.ActiveTab = 0
				a.renderDesktopMainSplit(c, t)
			}
		}
	})

	if a.Cfg.ShowDesktopFeatures() {
		a.renderAddPeerModal(c, t)
		a.renderDeleteConfirmModal(c, t)
	}
}

func (a *AppUI) renderDesktopMainSplit(c *ui.Context, t *ui.Theme) {
	if a.LeftPaneWidth < 280 {
		a.LeftPaneWidth = 280
	} else if a.LeftPaneWidth > 490 {
		a.LeftPaneWidth = 490
	}
	ui.Split(c, &a.LeftPaneWidth, func() {
		a.renderLeftThisDeskPane(c, t)
	}, func() {
		a.renderRightRemoteDeskPane(c, t)
	}).SplitReset(340).Grow(1).FillWidth()
	if a.LeftPaneWidth < 280 {
		a.LeftPaneWidth = 280
	} else if a.LeftPaneWidth > 490 {
		a.LeftPaneWidth = 490
	}
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
