package uiapp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gondesk/internal/database"
	"gondesk/internal/remote"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// ActiveSessionWindow tracks an active remote desktop session window.
type ActiveSessionWindow struct {
	DeskID        string
	Alias         string
	Password      string
	SessionUUID   string
	Win           *mygo.Window
	Status        string // "connecting", "connected", "error"
	ErrorMessage  string
	NotchOffsetX  float32
	NotchDragging bool
	FileStatus    string
	CanvasW       float32
	CanvasH       float32
	FrameBitmap   *ui.Bitmap
	LastFrameSeq  uint64
}

// RenderConnectingView renders a pure black screen with "Connecting ..." centered in white text.
func RenderConnectingView(c *ui.Context) {
	ui.Column(c).Fill().Background(ui.Hex("#000000")).Center().Gap(10).Children(func() {
		ui.Text(c, "Connecting ...").FontSize(22).Bold().TextColor(ui.Hex("#FFFFFF"))
		ui.Text(c, "Menghubungkan ke desk tujuan...").FontSize(13).TextColor(ui.Hex("#94A3B8"))
	})
}

// RenderRemoteWindow renders the dynamic state of an active remote session window:
// - "connecting": black screen with "Connecting ..."
// - "error": black screen with failure message and a "Reconnect" button
// - "connected": remote viewport with top draggable poni toolbar, clipboard, file transfer, and multi-cursor
func (a *AppUI) RenderRemoteWindow(c *ui.Context, normDeskID string) {
	if a.ActiveWindows == nil {
		RenderConnectingView(c)
		return
	}
	sw, ok := a.ActiveWindows[normDeskID]
	if !ok {
		RenderConnectingView(c)
		return
	}

	// 1. Error state: remote host is offline, connection refused, or session rejected
	if sw.Status == "error" {
		ui.Column(c.Key("remote-error-col")).Fill().Background(ui.Hex("#000000")).Center().Gap(16).Children(func() {
			ui.Text(c, "Connection Failed").FontSize(24).Bold().TextColor(ui.Hex("#EF443B"))
			errMsg := sw.ErrorMessage
			if errMsg == "" {
				errMsg = fmt.Sprintf("Tidak dapat terhubung ke %s: Aplikasi tujuan tidak aktif atau tidak terjangkau", sw.DeskID)
			}
			ui.Text(c, errMsg).FontSize(14).TextColor(ui.Hex("#E2E8F0")).MaxWidth(520)
			ui.PrimaryButton(c.Key("reconnect-btn"), "Reconnect").OnClick(func() {
				a.ReconnectRemoteWindow(sw.DeskID)
			})
		})
		return
	}

	// 2. Connecting state
	if sw.Status != "connected" {
		RenderConnectingView(c)
		return
	}

	// 3. Connected state: Top Poni Panel (Draggable Grip) + Multi-Cursor Remote Canvas
	var activeSess *remote.ActiveSession
	if a.Manager != nil {
		_, _, _, _, sessions := a.Manager.Snapshot()
		for i := range sessions {
			if database.NormalizeDeskID(sessions[i].PeerDeskID) == normDeskID ||
				(sw.SessionUUID != "" && sessions[i].SessionUUID == sw.SessionUUID) {
				activeSess = &sessions[i]
				break
			}
		}
	}

	ui.Column(c.Key("connected-remote-col")).Fill().Background(ui.Hex("#08080C")).Children(func() {
		// Top floating poni / notch panel
		ui.Row(c.Key("top-poni-row")).FillWidth().Padding(6, 12).Justify(ui.Center).Children(func() {
			ui.Box(c.Key("poni-notch")).Radius(12).Background(ui.Hex("#181820")).
				Border(1, ui.Hex("#2E2E3E")).Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.65)).
				Padding(6, 14).Margin(0, 0, 0, sw.NotchOffsetX).Children(func() {
				ui.Row(c.Key("poni-items")).Gap(10).AlignItems(ui.Center).Children(func() {
					// Draggable grip handle
					grip := ui.Box(c.Key("poni-grip")).Padding(4, 8).Radius(6).
						Background(ui.Hex("#262634")).Cursor(ui.CursorGrab).
						Tooltip("Klik dan geser horizontal untuk memindahkan poni")
					grip.Children(func() {
						ui.Text(c, "⠿").FontSize(14).TextColor(ui.Hex("#94A3B8"))
					})
					dx, _, held := grip.Dragged()
					if held {
						sw.NotchOffsetX += dx
						sw.NotchDragging = true
					} else if grip.PressCanceled() || !held {
						sw.NotchDragging = false
					}

					// Desk identity & status badge
					ui.Badge(c, "CONNECTED").Background(ui.Hex("#16A34A").Alpha(0.2)).TextColor(ui.Hex("#22C55E"))
					label := sw.DeskID
					if sw.Alias != "" && sw.Alias != sw.DeskID {
						label = sw.Alias + " (" + sw.DeskID + ")"
					}
					ui.Text(c, label).FontSize(13).Bold().TextColor(ui.Hex("#F8FAFC"))

					// Send Clipboard action
					if ui.Button(c.Key("btn-send-clipboard"), "📋 Send Clipboard").
						Tooltip("Kirim isi clipboard lokal ke host remote").Clicked() {
						txt := ""
						if mygo.App.IsReady() {
							txt = mygo.Clipboard.ReadText()
						}
						if txt == "" {
							txt = "Clipboard from " + a.DeskID
						}
						if a.Manager != nil && sw.SessionUUID != "" {
							a.Manager.SendClipboard(sw.SessionUUID, txt)
						}
						c.Toast("Clipboard sent to remote desk")
					}

					// Send File action
					if ui.Button(c.Key("btn-send-file"), "📁 Send File").
						Tooltip("Pilih dan kirim file ke host remote").Clicked() {
						go func() {
							paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
								Title: "Pilih file untuk dikirim ke remote desk",
							})
							if err == nil && len(paths) > 0 {
								filePath := paths[0]
								// #nosec G304 -- user explicitly selected file via native system file picker
								data, rErr := os.ReadFile(filePath)
								if rErr == nil && a.Manager != nil && sw.SessionUUID != "" {
									fileName := filepath.Base(filePath)
									a.Manager.SendFile(sw.SessionUUID, fileName, data)
									sw.FileStatus = fmt.Sprintf("File %s (%d bytes) terkirim", fileName, len(data))
									if sw.Win != nil {
										sw.Win.Invalidate()
									}
								}
							}
						}()
					}

					// Disconnect button
					ui.PrimaryButton(c.Key("btn-poni-disconnect"), "Disconnect").OnClick(func() {
						a.DisconnectDesk(c, sw.DeskID)
					})
				})
			})
		})

		// Check for incoming frame updates from host
		if activeSess != nil && activeSess.FrameSeq != sw.LastFrameSeq && len(activeSess.LatestFrame) > 0 {
			bmp, err := ui.DecodeBitmap(activeSess.LatestFrame)
			if err == nil {
				sw.FrameBitmap = bmp
				sw.LastFrameSeq = activeSess.FrameSeq
			}
		}

		// File transfer status message banner
		if sw.FileStatus != "" {
			ui.Row(c.Key("file-status-row")).Justify(ui.Center).Margin(2, 0).Children(func() {
				ui.Badge(c, sw.FileStatus).Background(ui.Hex("#1E293B")).TextColor(ui.Hex("#38BDF8"))
			})
		}

		// Remote Stream Canvas Viewport with collaborative multi-cursor
		canvas := ui.Box(c.Key("remote-canvas")).Fill().Margin(8).Radius(8).
			Background(ui.Hex("#040407")).Border(1, ui.Hex("#1C1C28")).
			Cursor(ui.CursorCrosshair)

		// Capture mouse movements and clicks to send to remote host
		canvas.HandleInput(func(ev ui.InputEvent) bool {
			if a.Manager != nil && sw.SessionUUID != "" {
				cw := sw.CanvasW
				if cw <= 1.0 {
					cw = 1024
				}
				ch := sw.CanvasH
				if ch <= 1.0 {
					ch = 640
				}
				normX := ev.X / cw
				normY := ev.Y / ch
				if normX < 0 {
					normX = 0
				} else if normX > 1 {
					normX = 1
				}
				if normY < 0 {
					normY = 0
				} else if normY > 1 {
					normY = 1
				}
				switch ev.Kind {
				case ui.InputPointerMove:
					a.Manager.SendMouseMove(sw.SessionUUID, normX, normY)
					if sw.Win != nil {
						sw.Win.Invalidate()
					}
				case ui.InputPointerDown:
					a.Manager.SendMouseClick(sw.SessionUUID, normX, normY, ev.Button)
					if sw.Win != nil {
						sw.Win.Invalidate()
					}
				case ui.InputPointerUp:
					a.Manager.SendMouseUp(sw.SessionUUID, normX, normY, ev.Button)
					if sw.Win != nil {
						sw.Win.Invalidate()
					}
				}
			}
			return true
		})

		// Draw stream canvas and all active collaborative remote cursors (N participants)
		canvas.DrawOver(func(p *ui.Painter, r ui.Rect) {
			sw.CanvasW = r.W
			sw.CanvasH = r.H

			// 1. Draw live screen capture frame if available, else subtle grid placeholder
			if sw.FrameBitmap != nil {
				p.Image(sw.FrameBitmap, r, ui.FillBox)
			} else {
				for gy := r.Y; gy < r.Y+r.H; gy += 48 {
					line := (&ui.Path{}).MoveTo(r.X, gy).LineTo(r.X+r.W, gy)
					p.StrokePath(line, 0.5, ui.Hex("#0E0E18"))
				}
				p.Text(r.X+16, r.Y+24, "GonDesk Encrypted Remote Stream (TLS 1.3 + Pion TURNS)", 12, ui.Hex("#475569"))
			}

			// 2. Draw all collaborative remote participant cursors (N users dynamically)
			if activeSess != nil && activeSess.Participants != nil {
				st, _ := a.Store.GetSettings()
				for _, cur := range activeSess.Participants {
					if cur == nil {
						continue
					}
					cx := r.X + cur.NormX*r.W
					cy := r.Y + cur.NormY*r.H
					if cx < r.X {
						cx = r.X
					}
					if cx > r.X+r.W-16 {
						cx = r.X + r.W - 16
					}
					if cy < r.Y {
						cy = r.Y
					}
					if cy > r.Y+r.H-16 {
						cy = r.Y + r.H - 16
					}

					curColor := ui.Hex(cur.Color)
					if cur.Color == "" {
						curColor = ui.Hex("#3B82F6")
					}

					// Draw arrow cursor shape
					arrow := (&ui.Path{}).MoveTo(cx, cy).
						LineTo(cx, cy+14).LineTo(cx+3.5, cy+10.5).
						LineTo(cx+7, cy+17).LineTo(cx+9, cy+16).
						LineTo(cx+5.5, cy+9.5).LineTo(cx+10, cy+9.5).Close()
					p.FillPath(arrow, curColor)
					p.StrokePath(arrow, 1, ui.Hex("#FFFFFF"))

					// If not self, draw participant alias / ID tag badge (self only has the clean arrow)
					isSelf := (st.DeskID != "" && cur.DeskID == st.DeskID)
					if !isSelf {
						tag := cur.Alias
						if tag == "" {
							tag = cur.DeskID
						}
						tagW, _ := p.MeasureText(0, ui.Span{Text: tag, Size: 11})
						badgeW := tagW + 12
						badgeH := float32(18)
						badgeX := cx + 12
						badgeY := cy + 12
						p.Fill(ui.Rect{X: badgeX, Y: badgeY, W: badgeW, H: badgeH}, curColor, 4)
						p.Text(badgeX + 6, badgeY + 3.5, tag, 11, ui.Hex("#FFFFFF"))
					}

					// If click was triggered, draw click ripple (expires on mouse up or after 400ms)
					if cur.LastAction == "click" && time.Since(cur.UpdatedAt) < 400*time.Millisecond {
						ripple := (&ui.Path{}).Circle(cx, cy, 9)
						p.StrokePath(ripple, 1.5, curColor)
					}
				}
			}
		})
	})
}

// OpenRemoteWindow opens a new window displaying a black screen with "Connecting ...".
func (a *AppUI) OpenRemoteWindow(target, alias string) *mygo.Window {
	norm := database.NormalizeDeskID(target)
	displayTitle := "GonDesk - " + target
	if alias != "" && alias != target {
		displayTitle = fmt.Sprintf("GonDesk - %s (%s)", alias, target)
	}

	if a.ActiveWindows == nil {
		a.ActiveWindows = make(map[string]*ActiveSessionWindow)
	}

	sw, exists := a.ActiveWindows[norm]
	if !exists {
		sw = &ActiveSessionWindow{
			DeskID:  target,
			Alias:   alias,
			Status:  "connecting",
			CanvasW: 1024,
			CanvasH: 640,
		}
		a.ActiveWindows[norm] = sw
	}

	if !mygo.App.IsReady() {
		// Headless or test mode: record simulated active window
		sw.Win = nil
		return nil
	}

	win := mygo.NewWindow(mygo.WindowOptions{
		Title:     displayTitle,
		Width:     1024,
		Height:    640,
		MinWidth:  640,
		MinHeight: 400,
		Content: ui.View(func(c *ui.Context) {
			a.RenderRemoteWindow(c, norm)
		}),
	})
	sw.Win = win

	win.OnClosed(func() {
		if a.Win != nil {
			a.Win.Update(func() {
				a.handleWindowClosed(norm)
			})
		} else {
			a.handleWindowClosed(norm)
		}
	})

	return win
}

// ReconnectRemoteWindow resets the session window to "connecting" and initiates a new connection attempt.
func (a *AppUI) ReconnectRemoteWindow(target string) {
	norm := database.NormalizeDeskID(target)
	sw, ok := a.ActiveWindows[norm]
	if !ok {
		return
	}
	sw.Status = "connecting"
	sw.ErrorMessage = ""
	if sw.Win != nil {
		sw.Win.Invalidate()
	}

	go func() {
		sess, err := a.Manager.ConnectToPeer(target, sw.Password)
		if sw.Win != nil {
			sw.Win.Update(func() {
				if err != nil {
					sw.Status = "error"
					sw.ErrorMessage = err.Error()
				} else if sess != nil {
					sw.SessionUUID = sess.SessionUUID
					sw.Status = sess.Status
					if sess.ErrorMessage != "" {
						sw.ErrorMessage = sess.ErrorMessage
					}
				}
			})
		} else {
			if err != nil {
				sw.Status = "error"
				sw.ErrorMessage = err.Error()
			} else if sess != nil {
				sw.SessionUUID = sess.SessionUUID
				sw.Status = sess.Status
				if sess.ErrorMessage != "" {
					sw.ErrorMessage = sess.ErrorMessage
				}
			}
		}
		a.NotifyChanged()
	}()
}

func (a *AppUI) handleWindowClosed(normDeskID string) {
	if a.ActiveWindows != nil {
		if sw, ok := a.ActiveWindows[normDeskID]; ok {
			if sw.SessionUUID != "" && a.Manager != nil {
				a.Manager.DisconnectSession(sw.SessionUUID)
			}
			delete(a.ActiveWindows, normDeskID)
		}
	}
	a.ReloadFromDB()
}

// IsDeskConnected reports whether an active window or outgoing connection session exists for the given desk ID.
func (a *AppUI) IsDeskConnected(deskID string) bool {
	norm := database.NormalizeDeskID(deskID)
	if a.ActiveWindows != nil {
		if _, ok := a.ActiveWindows[norm]; ok {
			return true
		}
	}
	if a.Manager != nil {
		_, _, _, _, sessions := a.Manager.Snapshot()
		for _, s := range sessions {
			if s.Direction == "outgoing" && database.NormalizeDeskID(s.PeerDeskID) == norm && (s.Status == "connected" || s.Status == "connecting") {
				return true
			}
		}
	}
	return false
}

// DisconnectDesk closes any active window and terminates the session for the given desk ID.
func (a *AppUI) DisconnectDesk(c *ui.Context, target string) {
	norm := database.NormalizeDeskID(target)
	if a.ActiveWindows != nil {
		if sw, ok := a.ActiveWindows[norm]; ok {
			if sw.Win != nil {
				sw.Win.Close()
			}
			if sw.SessionUUID != "" && a.Manager != nil {
				a.Manager.DisconnectSession(sw.SessionUUID)
			}
			delete(a.ActiveWindows, norm)
		}
	}
	if a.Manager != nil {
		_, _, _, _, sessions := a.Manager.Snapshot()
		for _, s := range sessions {
			if database.NormalizeDeskID(s.PeerDeskID) == norm {
				a.Manager.DisconnectSession(s.SessionUUID)
			}
		}
	}
	a.ReloadFromDB()
	if c != nil {
		c.Toast("Disconnected from " + strings.TrimSpace(target))
	}
}
