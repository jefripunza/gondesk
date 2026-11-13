package uiapp

import (
	"fmt"
	"strings"
	"time"

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

	// "Remote Desk" (Me-remote) state bound to controls
	RemoteTargetInput   string
	RemotePasswordInput string
	SearchQuery         string

	// Add Peer Modal state
	AddPeerModalOpen bool
	NewPeerID        string
	NewPeerAlias     string

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
	}
}

func (a *AppUI) renderTopBar(c *ui.Context, t *ui.Theme) {
	bridgeOnline, turnOK, _, _, sessions := a.Manager.Snapshot()

	ui.Row(c).FillWidth().Padding(10, 16).Gap(12).Background(t.Surface).Children(func() {
		// Brand Pill
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(28, 28).Radius(6).Background(ui.Hex("#EF443B")).Center().Children(func() {
				ui.Text(c, "G").FontSize(15).Bold().TextColor(ui.RGB(255, 255, 255))
			})
			ui.Column(c).Gap(1).Children(func() {
				ui.Text(c, "GonDesk").FontSize(15).Bold()
				if a.Cfg.IsServerMode() {
					ui.Text(c, "Global Bridge Server").FontSize(11).TextColor(t.TextMuted)
				} else {
					ui.Text(c, "Remote Desktop").FontSize(11).TextColor(t.TextMuted)
				}
			})
		})

		// Navigation Tabs (Only shown in Desktop mode; Server mode hides desktop tabs)
		if a.Cfg.ShowDesktopFeatures() {
			ui.Row(c).Gap(6).Margin(0, 0, 0, 12).Children(func() {
				homeBtn := ui.Button(c, "New Connection")
				if a.ActiveTab == 0 {
					homeBtn.Background(t.Accent).TextColor(t.AccentText)
				}
				if homeBtn.Clicked() {
					a.ActiveTab = 0
				}

				for i, sess := range sessions {
					idx := i
					tabIdx := idx + 2
					label := fmt.Sprintf("Desk: %s", sess.PeerDeskID)
					tabBtn := ui.Button(c.Key("sess-tab-"+sess.SessionUUID), label)
					if a.ActiveTab == tabIdx {
						tabBtn.Background(t.Accent).TextColor(t.AccentText)
					}
					tabBtn.OnClick(func() {
						a.ActiveTab = tabIdx
					})
				}

				settingsBtn := ui.Button(c, "Settings")
				if a.ActiveTab == 1 {
					settingsBtn.Background(t.Accent).TextColor(t.AccentText)
				}
				if settingsBtn.Clicked() {
					a.ActiveTab = 1
				}
			})
		} else {
			ui.Badge(c, "DESKTOP FEATURES HIDDEN (SERVER BRIDGE MODE)").
				Background(ui.Hex("#2563EB")).TextColor(ui.RGB(255, 255, 255))
		}

		ui.Spacer(c)

		// Single-Port HTTPS + TURN Status Indicator
		statusText := fmt.Sprintf("1-Port HTTPS + TURN (:%d)", a.Cfg.ListenPort)
		statusColor := ui.Hex("#16A34A") // green
		if a.Cfg.ShowDesktopFeatures() && !bridgeOnline && !turnOK {
			statusText = fmt.Sprintf("Local 1-Port Ready (:%d)", a.Cfg.ListenPort)
		}
		ui.Badge(c, statusText).Background(statusColor.Alpha(0.16)).TextColor(statusColor)

		// Mode Switch Button (allows testing both Desktop and Desktop Server modes from 1 codebase)
		modeSwitchLabel := "Switch to Server Mode"
		if a.Cfg.IsServerMode() {
			modeSwitchLabel = "Switch to Desktop Mode"
		}
		if ui.Button(c, modeSwitchLabel).Clicked() {
			if a.Cfg.IsServerMode() {
				a.Cfg.Mode = config.ModeDesktop
				a.Manager.StartBridgeLink()
				c.Toast("Switched to Desktop Mode (Remote & Be-Remoted Enabled)")
			} else {
				a.Cfg.Mode = config.ModeServer
				c.Toast("Switched to Desktop Server Mode (Desktop Features Hidden)")
			}
		}
	})
}

func (a *AppUI) renderIncomingBanner(c *ui.Context, t *ui.Theme, inc *remote.IncomingPrompt) {
	ui.Row(c).FillWidth().Padding(12, 18).Gap(12).
		Background(ui.Hex("#EF443B").Alpha(0.14)).
		BorderWidth(0, 0, 1, 0).BorderColor(ui.Hex("#EF443B")).
		Children(func() {
			ui.Badge(c, "INCOMING SESSION").Background(ui.Hex("#EF443B")).TextColor(ui.RGB(255, 255, 255))
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				ui.Textf(c, "%s (%s) is requesting to control This Desk", inc.FromAlias, inc.FromDeskID).Bold()
				ui.Textf(c, "Transport: %s — Verified via TLS 1.3 Single-Port", inc.Transport).FontSize(12).TextColor(t.TextMuted)
			})
			if ui.PrimaryButton(c, "Accept & Share Screen").Clicked() {
				a.Manager.AcceptIncoming()
				_, _, _, _, sessions := a.Manager.Snapshot()
				if len(sessions) > 0 {
					a.ActiveTab = len(sessions) + 1
				}
				c.Toast("Incoming remote session accepted")
			}
			if ui.Button(c, "Reject").Clicked() {
				a.Manager.RejectIncoming()
				c.Toast("Incoming remote session rejected")
			}
		})
}

func (a *AppUI) renderDesktopMainSplit(c *ui.Context, t *ui.Theme) {
	ui.Split(c, &a.LeftPaneWidth, func() {
		a.renderLeftThisDeskPane(c, t)
	}, func() {
		a.renderRightRemoteDeskPane(c, t)
	}).SplitReset(340).Grow(1).FillWidth()
}

func (a *AppUI) renderLeftThisDeskPane(c *ui.Context, t *ui.Theme) {
	bridgeOnline, turnReachable, turnMapped, _, _ := a.Manager.Snapshot()

	ui.Scroll(c.Key("left-pane-scroll")).Fill().Padding(18).Gap(16).Background(t.Surface).Children(func() {
		// 1. AnyDesk "This Desk / Your Address" Card
		ui.Column(c).Padding(16).Gap(10).Radius(10).
			Background(t.Background).Border(1, t.Border).
			Children(func() {
				ui.Row(c).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "This Desk").FontSize(13).Bold().TextColor(t.TextMuted).Grow(1)
					ui.Badge(c, "Host Ready (Di-remote)").Background(ui.Hex("#16A34A").Alpha(0.15)).TextColor(ui.Hex("#16A34A"))
				})

				ui.Text(c, "Your GonDesk Address").FontSize(12).TextColor(t.TextMuted)

				// Big AnyDesk-style 9-digit ID
				ui.Box(c).Padding(10, 12).Radius(8).
					Background(ui.Hex("#EF443B").Alpha(0.10)).
					Border(1, ui.Hex("#EF443B").Alpha(0.35)).
					Children(func() {
						ui.Text(c, a.DeskID).FontSize(26).Bold().
							Font("monospace").TextColor(ui.Hex("#EF443B")).Selectable()
					})

				ui.Row(c).Gap(8).Children(func() {
					if ui.PrimaryButton(c, "Copy Address").Grow(1).Clicked() {
						mygo.Clipboard.WriteText(a.DeskID)
						c.Toast("Copied Your Address: " + a.DeskID)
					}
					if ui.Button(c, "Test Incoming").Clicked() {
						a.Manager.SimulateIncomingRequest()
					}
				})
			})

		// 2. Unattended Access & Host Security Card (SQLite GORM)
		ui.Column(c).Padding(16).Gap(10).Radius(10).
			Background(t.Background).Border(1, t.Border).
			Children(func() {
				ui.Text(c, "Unattended Access & Permissions").FontSize(14).Bold()
				ui.Text(c, "Allow other GonDesk apps to connect to this desk using a password:").FontSize(12).TextColor(t.TextMuted)

				ui.Checkbox(c, &a.UnattendedEnabled, "Enable Unattended Access Password")
				ui.TextInput(c.Key("host-pwd"), &a.AccessPassword).Password().Placeholder("Set unattended password")

				ui.Checkbox(c, &a.AllowControl, "Allow remote mouse & keyboard control")
				ui.Checkbox(c, &a.AllowClipboard, "Allow clipboard synchronization")

				if ui.Button(c, "Save Host Settings (SQLite)").FillWidth().Clicked() {
					if st, err := a.Store.GetSettings(); err == nil {
						st.UnattendedEnabled = a.UnattendedEnabled
						st.AccessPassword = a.AccessPassword
						st.AllowControl = a.AllowControl
						st.AllowClipboard = a.AllowClipboard
						_ = a.Store.SaveSettings(&st)
						c.Toast("Saved host security settings to SQLite")
					}
				}
			})

		// 3. Single-Port & Global Bridge Status Card
		ui.Column(c).Padding(16).Gap(8).Radius(10).
			Background(t.Background).Border(1, t.Border).
			Children(func() {
				ui.Text(c, "Single-Port HTTPS & TURN Bridge").FontSize(14).Bold()
				ui.Textf(c, "Single Port: :%d (TCP/TLS + UDP)", a.Cfg.ListenPort).FontSize(12)
				ui.Textf(c, "Local HTTPS: %s", a.Cfg.LocalHTTPSURL()).FontSize(12).TextColor(t.TextMuted)
				ui.Textf(c, "Pion TURN URI: %s", a.Cfg.TurnURI()).FontSize(12).TextColor(t.TextMuted)

				wsStatus := "Connected via WSS"
				if !bridgeOnline {
					wsStatus = "Local Standalone Ready"
				}
				ui.Textf(c, "Signaling Channel: %s", wsStatus).FontSize(12)

				if turnReachable {
					ui.Textf(c, "STUN/TURN Verified: %s", turnMapped).FontSize(12).TextColor(ui.Hex("#16A34A"))
				}

				if ui.Button(c, "Reconnect Bridge & Verify TURN").FillWidth().Clicked() {
					go a.Manager.ReconnectBridge()
					c.Toast("Reconnecting to Single-Port Bridge Server...")
				}
			})
	})
}

func (a *AppUI) renderRightRemoteDeskPane(c *ui.Context, t *ui.Theme) {
	ui.Scroll(c.Key("right-pane-scroll")).Fill().Padding(20).Gap(18).Children(func() {
		// 1. AnyDesk "Remote Desk" Connect Card (Me-remote)
		ui.Column(c).Padding(18).Gap(12).Radius(10).
			Background(t.Surface).Border(1, t.Border).
			Children(func() {
				ui.Row(c).AlignItems(ui.Center).Children(func() {
					ui.Column(c).Grow(1).Gap(2).Children(func() {
						ui.Text(c, "Remote Desk").FontSize(18).Bold()
						ui.Text(c, "Enter a 9-digit GonDesk Address or Single-Port HTTPS URL to remote control another desk").FontSize(12).TextColor(t.TextMuted)
					})
					ui.Badge(c, "HTTPS + TURN 1-Port").Background(t.Accent.Alpha(0.14)).TextColor(t.Accent)
				})

				ui.Row(c).Gap(10).Children(func() {
					targetIn := ui.TextInput(c.Key("remote-target-id"), &a.RemoteTargetInput).
						Placeholder("Enter Remote Desk ID (e.g. 482 910 374) or https://host:8443").Grow(2)
					ui.TextInput(c.Key("remote-target-pwd"), &a.RemotePasswordInput).
						Password().Placeholder("Password (optional)").Width(180)

					connectClicked := ui.PrimaryButton(c, "Connect").Padding(8, 20).Clicked()
					if connectClicked || targetIn.Submitted() {
						a.startOutgoingConnection(c, a.RemoteTargetInput, a.RemotePasswordInput)
					}
				})
			})

		// 2. Filter & Search Bar for Address Book / Recent Sessions (GORM SQLite)
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Discovered & Saved Desks").FontSize(15).Bold()
			ui.Tabs(c, &a.FilterTab, "All Desks", "Favorites ★", "Online")
			ui.Spacer(c)
			ui.SearchField(c.Key("peer-search"), &a.SearchQuery).Placeholder("Search ID or alias...").Width(200)
			if ui.Button(c, "+ Add Desk").Clicked() {
				a.AddPeerModalOpen = true
			}
		})

		// 3. Peer Cards List (from SQLite)
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

							ui.PrimaryButton(c, "Connect").OnClick(func() {
								a.RemoteTargetInput = p.DeskID
								a.startOutgoingConnection(c, p.DeskID, a.RemotePasswordInput)
							})
						})
				}
			})
		}

		// 4. Recent Session History Table/List from SQLite
		ui.Column(c).Padding(16).Gap(10).Radius(10).
			Background(t.Surface).Border(1, t.Border).
			Children(func() {
				ui.Row(c).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "Recent Session History (GORM SQLite)").FontSize(14).Bold().Grow(1)
					ui.Badge(c, fmt.Sprintf("%d records", len(a.SessionLogs)))
				})
				if len(a.SessionLogs) == 0 {
					ui.Text(c, "No remote sessions recorded yet. Click 'Connect' on any desk above to start a session.").
						FontSize(12).TextColor(t.TextMuted)
				} else {
					for _, logItem := range a.SessionLogs {
						l := logItem
						ui.Row(c.Key("sess-log-"+l.SessionUUID)).Padding(8, 10).Gap(10).Radius(6).
							Background(t.Background).AlignItems(ui.Center).
							Children(func() {
								ui.Badge(c, strings.ToUpper(l.Status))
								ui.Textf(c, "%s  →  %s", l.ClientDeskID, l.HostDeskID).Bold().FontSize(13)
								ui.Text(c, l.TransportMode).FontSize(12).TextColor(t.TextMuted).Grow(1)
								ui.Text(c, l.StartedAt.Format("15:04:05")).FontSize(12).TextColor(t.TextMuted)
							})
					}
				}
			})
	})
}

func (a *AppUI) renderActiveSessionView(c *ui.Context, t *ui.Theme, sess remote.ActiveSession) {
	ui.Column(c).Fill().Padding(16).Gap(12).Children(func() {
		// Session Header Toolbar
		ui.Row(c).FillWidth().Padding(12, 16).Gap(12).Radius(10).
			Background(t.Surface).Border(1, t.Border).AlignItems(ui.Center).
			Children(func() {
				ui.Badge(c, strings.ToUpper(sess.Status)).Background(ui.Hex("#16A34A")).TextColor(ui.RGB(255, 255, 255))
				ui.Column(c).Grow(1).Gap(2).Children(func() {
					ui.Textf(c, "%s (%s) — %s", sess.PeerAlias, sess.PeerDeskID, strings.ToTitle(sess.Direction)).FontSize(15).Bold()
					ui.Textf(c, "Transport: %s  |  Latency: %d ms  |  Last Action: %s", sess.Transport, sess.LatencyMs, sess.LastInput).
						FontSize(12).TextColor(t.TextMuted)
				})

				if ui.Button(c, "Send Ctrl+Alt+Del").Clicked() {
					a.Manager.SendRemoteAction(sess.SessionUUID, "Sent Ctrl+Alt+Del key sequence")
					c.Toast("Sent Ctrl+Alt+Del to " + sess.PeerDeskID)
				}
				if ui.Button(c, "Sync Clipboard").Clicked() {
					clip := mygo.Clipboard.ReadText()
					if clip == "" {
						clip = "(clipboard sync check)"
					}
					a.Manager.SendRemoteAction(sess.SessionUUID, "Synced Clipboard ("+fmt.Sprint(len(clip))+" chars)")
					c.Toast("Clipboard synchronized over HTTPS/WSS")
				}
				if ui.PrimaryButton(c, "Disconnect").Clicked() {
					a.Manager.DisconnectSession(sess.SessionUUID)
					a.ReloadFromDB()
					a.ActiveTab = 0
					c.Toast("Disconnected from " + sess.PeerDeskID)
				}
			})

		// Main Session Viewport + Side Chat Panel
		ui.Row(c).Grow(1).FillWidth().Gap(12).AlignItems(ui.Stretch).Children(func() {
			// Remote Screen Canvas (GPU Drawn via ui.Painter)
			ui.Box(c).Grow(1).FillHeight().Radius(10).Clip().
				Border(1, t.Border).
				Draw(func(p *ui.Painter, r ui.Rect) {
					// Dark remote desktop background gradient
					p.FillGradient(r, ui.LinearGradient{
						From:  ui.Hex("#0F172A"),
						To:    ui.Hex("#1E293B"),
						Angle: 135,
					}, 10)

					// Simulated remote desktop top bar
					topBar := ui.Rect{X: r.X, Y: r.Y, W: r.W, H: 32}
					p.Fill(topBar, ui.RGBA(15, 23, 42, 0.85), 0)
					p.Text(r.X+14, r.Y+8, "REMOTE DESKTOP STREAM  •  "+sess.PeerAlias+" ("+sess.PeerDeskID+")", 12, ui.Hex("#94A3B8"))

					// Simulated remote window inside remote screen
					winRect := ui.Rect{X: r.X + 48, Y: r.Y + 64, W: max(260, r.W-96), H: max(140, r.H-128)}
					p.Fill(winRect, ui.Hex("#0B1120"), 8)
					p.Stroke(winRect, ui.Hex("#334155"), 1, 8)

					p.Text(winRect.X+20, winRect.Y+20, "GonDesk Single-Port Encrypted Stream (TLS 1.3 + Pion TURN)", 15, ui.Hex("#F8FAFC"))
					p.Text(winRect.X+20, winRect.Y+48, fmt.Sprintf("Session ID: %s   |   Port: :%d", sess.SessionUUID, a.Cfg.ListenPort), 13, ui.Hex("#38BDF8"))
					p.Text(winRect.X+20, winRect.Y+74, fmt.Sprintf("Latest Input / Event: %s", sess.LastInput), 13, ui.Hex("#4ADE80"))
					p.Text(winRect.X+20, winRect.Y+100, "Click anywhere inside viewport or use toolbar buttons to send remote events.", 12, ui.Hex("#94A3B8"))

					// Taskbar at bottom of remote screen
					taskbar := ui.Rect{X: r.X, Y: r.Y + r.H - 36, W: r.W, H: 36}
					p.Fill(taskbar, ui.RGBA(15, 23, 42, 0.92), 0)
					p.Text(r.X+14, r.Y+r.H-24, "GonDesk Remote OS  •  60 FPS  •  1-Port HTTPS/TURNS Active", 12, ui.Hex("#E2E8F0"))
				}).
				OnClick(func() {
					a.Manager.SendRemoteAction(sess.SessionUUID, fmt.Sprintf("Pointer Click (Frame #%d)", sess.FrameTick+1))
				})

			// Session Chat & Event Log Sidebar
			ui.Column(c).Width(310).FillHeight().Padding(14).Gap(10).Radius(10).
				Background(t.Surface).Border(1, t.Border).
				Children(func() {
					ui.Text(c, "Session Chat & Telemetry").FontSize(14).Bold()
					ui.Divider(c)

					ui.Scroll(c.Key("chat-scroll-"+sess.SessionUUID)).Grow(1).Gap(6).Children(func() {
						for i, line := range sess.ChatLog {
							ui.Text(c.Key(fmt.Sprintf("chat-line-%d", i)), line).FontSize(12)
						}
					})

					ui.Row(c).Gap(6).Children(func() {
						chatIn := ui.TextInput(c.Key("chat-input-"+sess.SessionUUID), &a.ChatDraft).
							Placeholder("Send message...").Grow(1)
						sendClicked := ui.PrimaryButton(c, "Send").Clicked()
						if sendClicked || chatIn.Submitted() {
							a.Manager.SendChat(sess.SessionUUID, a.ChatDraft)
							a.ChatDraft = ""
						}
					})
				})
		})
	})
}

func (a *AppUI) renderDesktopSettings(c *ui.Context, t *ui.Theme) {
	ui.Scroll(c.Key("settings-scroll")).Fill().Padding(24).Gap(16).Children(func() {
		ui.Column(c).MaxWidth(680).Padding(20).Gap(14).Radius(10).
			Background(t.Surface).Border(1, t.Border).
			Children(func() {
				ui.Text(c, "GonDesk Network & Single-Port Configuration").FontSize(18).Bold()
				ui.Text(c, "Both Desktop and Desktop Server communicate over 1 single port using HTTPS (GoFiber v2) + Pion TURN.").
					FontSize(13).TextColor(t.TextMuted)

				ui.Divider(c)

				ui.Text(c, "Device Alias").FontSize(13).Bold()
				ui.TextInput(c.Key("cfg-alias"), &a.AliasInput).Placeholder("Device Alias")

				ui.Text(c, "Global Bridge Server URL (Desktop Server)").FontSize(13).Bold()
				ui.TextInput(c.Key("cfg-bridge-url"), &a.BridgeURLInput).Placeholder("https://bridge.example.com:8443")

				ui.Row(c).Gap(10).Children(func() {
					if ui.PrimaryButton(c, "Save & Reconnect Bridge").Clicked() {
						if st, err := a.Store.GetSettings(); err == nil {
							st.Alias = strings.TrimSpace(a.AliasInput)
							st.BridgeServerURL = strings.TrimSpace(a.BridgeURLInput)
							_ = a.Store.SaveSettings(&st)
							a.Cfg.BridgeServerURL = st.BridgeServerURL
							go a.Manager.ReconnectBridge()
							c.Toast("Configuration saved to SQLite & reconnecting bridge")
						}
					}
					if ui.Button(c, "Back to Home").Clicked() {
						a.ActiveTab = 0
					}
				})
			})
	})
}

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

func (a *AppUI) renderAddPeerModal(c *ui.Context, t *ui.Theme) {
	ui.Modal(c, &a.AddPeerModalOpen, func() {
		ui.Column(c).Width(380).Gap(12).Children(func() {
			ui.Text(c, "Add Desk to Address Book (SQLite)").FontSize(16).Bold()
			ui.TextInput(c.Key("modal-peer-id"), &a.NewPeerID).Placeholder("GonDesk ID (e.g. 612 840 193)").AutoFocus()
			ui.TextInput(c.Key("modal-peer-alias"), &a.NewPeerAlias).Placeholder("Alias (e.g. Home-Desktop)")

			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "Cancel").Clicked() {
					a.AddPeerModalOpen = false
				}
				if ui.PrimaryButton(c, "Save Desk").Clicked() {
					if strings.TrimSpace(a.NewPeerID) != "" {
						_, _ = a.Store.UpsertPeer(a.NewPeerID, a.NewPeerAlias, "", "remote", a.Cfg.BridgeServerURL, true)
						a.NewPeerID = ""
						a.NewPeerAlias = ""
						a.AddPeerModalOpen = false
						a.ReloadFromDB()
						c.Toast("Saved desk to SQLite Address Book")
					}
				}
			})
		})
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
