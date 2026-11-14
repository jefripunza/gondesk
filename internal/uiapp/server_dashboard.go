package uiapp

import (
	"fmt"
	"strings"
	"time"

	"gondesk/internal/database"

	"github.com/egoist/mygo/ui"
)

// renderServerBridgeDashboard renders the Desktop Server (Global Bridge) UI.
// All Desktop Remote ("Remote Desk") and Be-Remoted ("This Desk") features are hidden!
func (a *AppUI) renderServerBridgeDashboard(c *ui.Context, t *ui.Theme) {
	var httpConns, turnConns, tlsConns, turnAuths uint64
	var onlineWS int
	if a.Node != nil {
		httpConns, turnConns, tlsConns = a.Node.Mux.Stats()
		turnAuths = a.Node.TurnEngine.AuthRequests()
		onlineWS = a.Node.OnlinePeerCount()
	}

	ui.Scroll(c.Key("server-dashboard-scroll")).Fill().Padding(22).Gap(18).Children(func() {
		// Banner explaining Server Mode
		ui.Row(c).FillWidth().Padding(16, 20).Gap(14).Radius(10).
			Background(t.Surface).Border(1, t.Border).AlignItems(ui.Center).
			Children(func() {
				ui.Column(c).Grow(1).Gap(4).Children(func() {
					ui.Text(c, "GonDesk Global Bridge & TURN Server Mode").FontSize(20).Bold()
					ui.Text(c, "Running from the unified codebase with Desktop remote/be-remoted features hidden. Acting as a global HTTPS + WSS + Pion TURN relay on 1 port.").
						FontSize(13).TextColor(t.TextMuted)
				})
				if ui.Button(c, "Refresh SQLite Data").Clicked() {
					a.ReloadFromDB()
					c.Toast("Refreshed peers & session logs from SQLite")
				}
			})

		// 4 Metric Cards
		ui.Row(c).FillWidth().Gap(14).Children(func() {
			a.metricCard(c, t, "Single Port (HTTPS + TURN)", fmt.Sprintf(":%d", a.Cfg.ListenPort), a.Cfg.LocalHTTPSURL())
			a.metricCard(c, t, "Online Registered Desks", fmt.Sprintf("%d active (%d saved)", onlineWS, len(a.Peers)), "WebSocket + SQLite Registry")
			a.metricCard(c, t, "HTTPS / WSS Connections", fmt.Sprintf("%d routed (%d TLS)", httpConns, tlsConns), "GoFiber v2 Single-Port")
			a.metricCard(c, t, "Pion TURN / STUN Traffic", fmt.Sprintf("%d TCP/TLS • %d Auth", turnConns, turnAuths), a.Cfg.TurnURI())
		})

		// Registered Peers in Global Bridge (SQLite)
		ui.Column(c).FillWidth().Padding(18).Gap(12).Radius(10).
			Background(t.Surface).Border(1, t.Border).
			Children(func() {
				ui.Row(c).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "Global Peer Registry (GORM SQLite)").FontSize(16).Bold().Grow(1)
					ui.Badge(c, fmt.Sprintf("%d Registered Nodes", len(a.Peers)))
				})
				for _, peer := range a.Peers {
					p := peer
					ui.Row(c.Key("srv-peer-"+p.DeskID)).Padding(10, 14).Gap(12).Radius(8).
						Background(t.Background).AlignItems(ui.Center).
						Children(func() {
							badgeColor := ui.Hex("#16A34A")
							stateStr := "ONLINE"
							if !p.Online {
								badgeColor = t.TextMuted
								stateStr = "OFFLINE"
							}
							ui.Badge(c, stateStr).Background(badgeColor.Alpha(0.15)).TextColor(badgeColor)
							ui.Text(c, p.DeskID).Font("monospace").Bold().Width(120)
							ui.Text(c, p.Alias).Bold().Width(180)
							ui.Text(c, p.OS).TextColor(t.TextMuted).Width(120)
							ui.Text(c, p.EndpointURL).TextColor(t.TextMuted).Grow(1)
							ui.Textf(c, "Seen: %s", p.LastSeenAt.Format("15:04:05")).FontSize(12).TextColor(t.TextMuted)
						})
				}
			})

		// Pion TURN Server Credentials (SQLite) & Audit Logs
		ui.Row(c).FillWidth().Gap(16).AlignItems(ui.Stretch).Children(func() {
			ui.Column(c).Grow(1).Padding(18).Gap(10).Radius(10).
				Background(t.Surface).Border(1, t.Border).
				Children(func() {
					ui.Text(c, "Pion TURN Credentials (SQLite)").FontSize(15).Bold()
					for _, cred := range a.TurnCreds {
						cr := cred
						ui.Row(c.Key("turn-cred-"+cr.Username)).Padding(8, 12).Gap(8).Radius(6).
							Background(t.Background).AlignItems(ui.Center).
							Children(func() {
								ui.Textf(c, "User: %s", cr.Username).Bold().Grow(1)
								ui.Textf(c, "Realm: %s", cr.Realm).FontSize(12).TextColor(t.TextMuted)
								ui.Badge(c, "ACTIVE").Background(ui.Hex("#16A34A").Alpha(0.15)).TextColor(ui.Hex("#16A34A"))
							})
					}
					ui.Row(c).Gap(8).Children(func() {
						ui.TextInput(c.Key("new-turn-user"), &a.NewTurnUser).Placeholder("TURN username").Grow(1)
						ui.TextInput(c.Key("new-turn-pass"), &a.NewTurnPass).Password().Placeholder("TURN password").Grow(1)
						if ui.PrimaryButton(c, "Add Account").Clicked() {
							u := strings.TrimSpace(a.NewTurnUser)
							p := strings.TrimSpace(a.NewTurnPass)
							if u != "" && p != "" {
								_ = a.Store.DB.Create(&database.TurnCredential{
									Username:  u,
									Password:  p,
									Realm:     a.Cfg.TurnRealm,
									Enabled:   true,
									CreatedAt: time.Now(),
								}).Error
								a.NewTurnUser = ""
								a.NewTurnPass = ""
								a.ReloadFromDB()
								c.Toast("Added TURN account to SQLite: " + u)
							}
						}
					})
				})

			ui.Column(c).Grow(1).Padding(18).Gap(10).Radius(10).
				Background(t.Surface).Border(1, t.Border).
				Children(func() {
					ui.Text(c, "Global Bridge Relay Logs (SQLite)").FontSize(15).Bold()
					if len(a.SessionLogs) == 0 {
						ui.Text(c, "No bridge sessions logged yet.").FontSize(12).TextColor(t.TextMuted)
					} else {
						for _, logItem := range a.SessionLogs {
							l := logItem
							ui.Row(c.Key("srv-log-"+l.SessionUUID)).Padding(8, 10).Gap(8).Radius(6).
								Background(t.Background).AlignItems(ui.Center).
								Children(func() {
									ui.Badge(c, strings.ToUpper(l.Status))
									ui.Textf(c, "%s → %s", l.ClientDeskID, l.HostDeskID).FontSize(12).Bold().Grow(1)
									ui.Text(c, l.StartedAt.Format("15:04:05")).FontSize(11).TextColor(t.TextMuted)
								})
						}
					}
				})
		})
	})
}

func (a *AppUI) metricCard(c *ui.Context, t *ui.Theme, title, value, subtitle string) {
	ui.Column(c).Grow(1).Padding(16).Gap(6).Radius(10).
		Background(t.Surface).Border(1, t.Border).
		Children(func() {
			ui.Text(c, title).FontSize(12).TextColor(t.TextMuted)
			ui.Text(c, value).FontSize(20).Bold()
			ui.Text(c, subtitle).FontSize(11).TextColor(t.TextMuted).SingleLine()
		})
}
