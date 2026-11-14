package uiapp

import (
	"fmt"
	"net/url"
	"runtime"
	"strings"

	"gondesk/internal/database"

	"github.com/egoist/mygo/ui"
)

// renderDesksView renders the main full-width view containing the desk list,
// filter tabs (All Desks, Favorites, Online), search bar, and peer cards.
func (a *AppUI) renderDesksView(c *ui.Context, t *ui.Theme) {
	ui.Scroll(c.Key("desks-view-scroll")).Fill().Padding(20).Gap(18).Children(func() {
		// 1. Filter & Search Bar for Address Book / Recent Sessions (GORM SQLite)
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Tabs(c, &a.FilterTab, "All Desks", "Favorites ★", "Online")
			ui.Spacer(c)
			ui.SearchField(c.Key("peer-search"), &a.SearchQuery).Placeholder("Search ID or alias...").Width(240)
			if ui.Button(c, "+ Add Desk").Clicked() {
				a.AddPeerModalOpen = true
			}
		})

		// 2. Peer Cards List (from SQLite)
		filteredPeers := a.filteredPeers()
		if len(filteredPeers) == 0 {
			ui.Box(c).Padding(32).Radius(10).Background(t.Surface).Border(1, t.Border).Center().Children(func() {
				ui.Text(c, "No matching desks found in SQLite Address Book.").TextColor(t.TextMuted)
			})
		} else {
			ui.Column(c).Gap(10).Children(func() {
				for _, peer := range filteredPeers {
					p := peer
					ui.Row(c.Key("peer-card-"+p.DeskID)).Padding(14, 16).Gap(14).Radius(10).
						Background(t.Surface).Border(1, t.Border).AlignItems(ui.Center).
						Children(func() {
							// Monitor / Status Avatar Box
							statusColor := ui.Hex("#16A34A")
							statusLabel := "ONLINE"
							if !p.Online {
								statusColor = t.TextMuted
								statusLabel = "OFFLINE"
							}
							ui.Box(c).Size(46, 46).Radius(8).
								Background(statusColor.Alpha(0.14)).Center().
								Children(func() {
									ui.Text(c, "DESK").FontSize(11).Bold().TextColor(statusColor)
								})

							ui.Column(c).Grow(1).Gap(4).Children(func() {
								ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
									ui.Text(c, p.Alias).FontSize(15).Bold()
									ui.Badge(c, statusLabel).Background(statusColor.Alpha(0.15)).TextColor(statusColor)
									if p.OS != "" {
										ui.Badge(c, p.OS)
									}
								})
								ui.Textf(c, "Address: %s   •   Endpoint: %s   •   Sessions: %d", p.DeskID, p.EndpointURL, p.ConnectionCount).
									FontSize(12).TextColor(t.TextMuted)
							})

							favLabel := "☆"
							if p.IsFavorite {
								favLabel = "★"
							}
							ui.Button(c, favLabel).Tooltip("Toggle Favorite in SQLite").OnClick(func() {
								_ = a.Store.ToggleFavorite(p.DeskID)
								a.ReloadFromDB()
							})

							if ui.Button(c, "🗑").Tooltip("Hapus Desk dari Buku Alamat").Clicked() {
								a.PendingDeletePeerID = p.DeskID
								a.PendingDeletePeerAlias = p.Alias
								if a.PendingDeletePeerAlias == "" {
									a.PendingDeletePeerAlias = p.DeskID
								}
								a.DeleteConfirmModalOpen = true
							}

							ui.PrimaryButton(c, "Connect").OnClick(func() {
								a.RemoteTargetInput = p.DeskID
								a.startOutgoingConnection(c, p.DeskID, a.RemotePasswordInput)
							})
						})
				}
			})
		}
	})
}

func (a *AppUI) startOutgoingConnection(c *ui.Context, target, password string) {
	sess, err := a.Manager.ConnectToPeer(target, password)
	if err != nil {
		c.Toast("Connect error: " + err.Error())
		return
	}
	a.ReloadFromDB()
	_, _, _, _, sessions := a.Manager.Snapshot()
	for i, s := range sessions {
		if s.SessionUUID == sess.SessionUUID {
			a.ActiveTab = i + 2
			break
		}
	}
	c.Toast("Connected to " + sess.PeerDeskID + " via Single-Port HTTPS/TURN")
}

func (a *AppUI) filteredPeers() []database.PeerRecord {
	q := strings.ToLower(strings.TrimSpace(a.SearchQuery))
	out := make([]database.PeerRecord, 0, len(a.Peers))
	for _, p := range a.Peers {
		// Dilarang menampilkan desk lokal ("This Desk") di daftar Remote Desks
		if database.NormalizeDeskID(p.DeskID) == database.NormalizeDeskID(a.DeskID) {
			continue
		}
		if a.FilterTab == 1 && !p.IsFavorite {
			continue
		}
		if a.FilterTab == 2 && !p.Online {
			continue
		}
		if q != "" {
			if !strings.Contains(strings.ToLower(p.DeskID), q) &&
				!strings.Contains(strings.ToLower(p.Alias), q) &&
				!strings.Contains(strings.ToLower(p.OS), q) {
				continue
			}
		}
		out = append(out, p)
	}
	return out
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
