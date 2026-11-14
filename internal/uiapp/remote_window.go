package uiapp

import (
	"fmt"

	"gondesk/internal/database"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// ActiveSessionWindow tracks an active remote desktop session window.
type ActiveSessionWindow struct {
	DeskID      string
	SessionUUID string
	Win         *mygo.Window
}

// RenderConnectingView renders a pure black screen with "Connecting ..." centered in white text.
func RenderConnectingView(c *ui.Context) {
	ui.Column(c).Fill().Background(ui.Hex("#000000")).Center().Children(func() {
		ui.Text(c, "Connecting ...").FontSize(22).Bold().TextColor(ui.Hex("#FFFFFF"))
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

	if !mygo.App.IsReady() {
		// Headless or test mode: record simulated active window
		a.ActiveWindows[norm] = &ActiveSessionWindow{
			DeskID: target,
			Win:    nil,
		}
		return nil
	}

	win := mygo.NewWindow(mygo.WindowOptions{
		Title:     displayTitle,
		Width:     1024,
		Height:    640,
		MinWidth:  640,
		MinHeight: 400,
		Content:   ui.View(RenderConnectingView),
	})

	sw := &ActiveSessionWindow{
		DeskID: target,
		Win:    win,
	}
	a.ActiveWindows[norm] = sw

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

// IsDeskConnected reports whether an active window or connection session exists for the given desk ID.
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
			if database.NormalizeDeskID(s.PeerDeskID) == norm && s.Status == "connected" {
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
		c.Toast("Disconnected from " + target)
	}
}
