package uiapp

import (
	"runtime"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// UpdateHostOverlay manages the lifecycle of the transparent, click-through overlay window on Host B.
func (a *AppUI) UpdateHostOverlay() {
	if a.Manager == nil {
		return
	}

	_, _, _, _, sessions := a.Manager.Snapshot()
	var hasActiveIncoming bool
	for _, s := range sessions {
		if s.Direction == "incoming" && s.Status == "connected" {
			hasActiveIncoming = true
			break
		}
	}

	if !hasActiveIncoming {
		if a.HostOverlayWin != nil {
			a.HostOverlayWin.Destroy()
			a.HostOverlayWin = nil
		}
		return
	}

	if !mygo.App.IsReady() {
		// Headless or test mode
		return
	}

	if a.HostOverlayWin == nil {
		disp := mygo.Screen.PrimaryDisplay()
		w := disp.Bounds.Width
		h := disp.Bounds.Height
		if w <= 0 {
			w = 1920
		}
		if h <= 0 {
			h = 1080
		}

		win := mygo.NewWindow(mygo.WindowOptions{
			Title:         "GonDesk Host Cursor Overlay",
			Panel:         true,
			Frameless:     true,
			Transparent:   true,
			AlwaysOnTop:   true,
			SkipTaskbar:   true,
			DisableShadow: true,
			Width:         w,
			Height:        h,
			X:             disp.Bounds.X,
			Y:             disp.Bounds.Y,
			Content:       ui.View(a.renderHostOverlay),
		})
		win.SetIgnoreMouseEvents(true)
		if runtime.GOOS != "linux" {
			win.SetContentProtection(true)
		}
		a.HostOverlayWin = win
	} else {
		a.HostOverlayWin.Invalidate()
	}
}

// renderHostOverlay renders all collaborative remote participant cursors on Host B's desktop screen.
func (a *AppUI) renderHostOverlay(c *ui.Context) {
	c.Root().Background(ui.Transparent)

	ui.Box(c.Key("host-overlay-canvas")).Fill().DrawOver(func(p *ui.Painter, r ui.Rect) {
		if a.Manager == nil {
			return
		}

		_, _, _, _, sessions := a.Manager.Snapshot()
		for _, s := range sessions {
			if s.Direction != "incoming" || s.Status != "connected" || s.Participants == nil {
				continue
			}

			for _, cur := range s.Participants {
				if cur == nil {
					continue
				}

				cx := r.X + cur.NormX*r.W
				cy := r.Y + cur.NormY*r.H

				curColor := ui.Hex(cur.Color)
				if cur.Color == "" {
					curColor = ui.Hex("#3B82F6")
				}

				// Draw high-visibility shadow arrow cursor on host screen
				arrow := (&ui.Path{}).MoveTo(cx, cy).
					LineTo(cx, cy+16).LineTo(cx+4, cy+12).
					LineTo(cx+8, cy+20).LineTo(cx+11, cy+18.5).
					LineTo(cx+6.5, cy+11).LineTo(cx+12, cy+11).Close()
				p.FillPath(arrow, curColor)
				p.StrokePath(arrow, 1.5, ui.Hex("#FFFFFF"))

				// Draw participant name / alias badge tag
				tag := cur.Alias
				if tag == "" {
					tag = cur.DeskID
				}
				tagW, _ := p.MeasureText(0, ui.Span{Text: tag, Size: 11})
				badgeW := tagW + 14
				badgeH := float32(20)
				badgeX := cx + 16
				badgeY := cy + 14
				p.Fill(ui.Rect{X: badgeX, Y: badgeY, W: badgeW, H: badgeH}, curColor, 4)
				p.Text(badgeX + 7, badgeY + 4.5, tag, 11, ui.Hex("#FFFFFF"))

				// If click was triggered, draw ripple indicator (expires on mouse up or after 400ms)
				if cur.LastAction == "click" && time.Since(cur.UpdatedAt) < 400*time.Millisecond {
					ripple := (&ui.Path{}).Circle(cx, cy, 14)
					p.StrokePath(ripple, 2, curColor)
				}
			}
		}
	})
}
