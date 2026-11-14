package uiapp

import (
	"fmt"
	"net/url"
	"runtime"
	"strings"

	"gondesk/internal/database"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func (a *AppUI) renderLeftThisDeskPane(c *ui.Context, t *ui.Theme) {
	ui.Scroll(c.Key("left-pane-scroll")).Fill().MinWidth(280).MaxWidth(490).Padding(18).Gap(16).Background(t.Surface).Children(func() {
		// 1. AnyDesk "This Desk / Your Address" Card
		ui.Column(c).Padding(16).Gap(10).Radius(10).
			Background(t.Background).Border(1, t.Border).
			Children(func() {
				ui.Text(c, "Your Address").FontSize(12).TextColor(t.TextMuted)

				// Big AnyDesk-style 9-digit ID (clickable to copy)
				addrBox := ui.Box(c).Padding(10, 12).Radius(8).
					Background(ui.Hex("#EF443B").Alpha(0.10)).
					Border(1, ui.Hex("#EF443B").Alpha(0.35)).
					Cursor(ui.CursorPointer).
					Tooltip("Click to copy address").
					Children(func() {
						ui.Text(c, a.DeskID).FontSize(26).Bold().
							Font("monospace").TextColor(ui.Hex("#EF443B")).Selectable()
					})
				if addrBox.Clicked() {
					mygo.Clipboard.WriteText(a.DeskID)
					c.Toast("Copied Your Address: " + a.DeskID)
				}

				ui.Row(c).Gap(8).Children(func() {
					if ui.PrimaryButton(c, "Copy Address").Grow(1).Clicked() {
						mygo.Clipboard.WriteText(a.DeskID)
						c.Toast("Copied Your Address: " + a.DeskID)
					}
				})
			})
	})
}

// ConnectAndAddURLPeer validates the required http:// or https:// URL, connects to it,
// and adds the desk/endpoint into the Discovered & Saved Desks list in SQLite.
func (a *AppUI) ConnectAndAddURLPeer(c *ui.Context) {
	a.QuickConnectURL = CleanURL(a.QuickConnectURL)
	rawURL := strings.TrimSpace(a.QuickConnectURL)
	lowerURL := strings.ToLower(rawURL)
	if !strings.HasPrefix(lowerURL, "http://") && !strings.HasPrefix(lowerURL, "https://") {
		c.Toast("URL wajib menggunakan http:// atau https://")
		return
	}

	parsed, err := url.Parse(rawURL)
	if err != nil || strings.TrimSpace(parsed.Host) == "" {
		c.Toast("Format URL tidak valid (contoh: https://host:8443)")
		return
	}

	name := strings.TrimSpace(a.QuickConnectName)
	if name == "" {
		name = parsed.Host
	}

	deskID := ""
	for _, p := range a.Peers {
		if strings.EqualFold(p.EndpointURL, rawURL) && strings.EqualFold(p.Alias, name) {
			deskID = p.DeskID
			break
		}
	}
	if deskID == "" {
		deskID = database.GenerateDeskID()
	}

	if a.Store != nil {
		_, _ = a.Store.UpsertPeer(deskID, name, parsed.Hostname(), runtime.GOOS+"/"+runtime.GOARCH, rawURL, true)
		a.Store.IncrementPeerConnection(deskID, rawURL)

		if st, err := a.Store.GetSettings(); err == nil {
			st.BridgeServerURL = rawURL
			_ = a.Store.SaveSettings(&st)
		}
	}

	if a.Cfg != nil {
		a.Cfg.BridgeServerURL = rawURL
	}
	if a.Manager != nil {
		go a.Manager.ReconnectBridge()
	}

	a.QuickConnectName = ""
	a.QuickConnectURL = ""
	a.FilterTab = 0
	a.SearchQuery = ""
	a.ReloadFromDB()
	c.Toast(fmt.Sprintf("Connected to %s (%s) & added to list", name, rawURL))
}
