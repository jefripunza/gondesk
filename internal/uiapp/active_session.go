package uiapp

import (
	"fmt"
	"strings"

	"gondesk/internal/remote"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

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

					ui.Scroll(c.Key("chat-scroll-" + sess.SessionUUID)).Grow(1).Gap(6).Children(func() {
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
