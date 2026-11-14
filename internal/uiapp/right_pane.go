package uiapp

import (
	"strings"

	"gondesk/internal/database"

	"github.com/egoist/mygo/ui"
)

func (a *AppUI) renderRightRemoteDeskPane(c *ui.Context, t *ui.Theme) {
	ui.Scroll(c.Key("right-pane-scroll")).Fill().Padding(20).Gap(18).Children(func() {
		// 1. Filter & Search Bar for Address Book / Recent Sessions (GORM SQLite)
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Tabs(c, &a.FilterTab, "All Desks", "Favorites ★", "Online")
			ui.Spacer(c)
			ui.SearchField(c.Key("peer-search"), &a.SearchQuery).Placeholder("Search ID or alias...").Width(200)
			if ui.Button(c, "+ Add Desk").Clicked() {
				a.AddPeerModalOpen = true
			}
		})

		// 2. Peer Cards List (from SQLite)
		filteredPeers := a.filteredPeers()
		if len(filteredPeers) == 0 {
			ui.Box(c).Padding(24).Radius(10).Background(t.Surface).Border(1, t.Border).Center().Children(func() {
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
