package uiapp

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func (a *AppUI) renderTopBar(c *ui.Context, t *ui.Theme) {
	ui.Row(c).FillWidth().Padding(10, 16).Gap(12).AlignItems(ui.Center).Background(t.Surface).Children(func() {
		// Brand Pill (Left, click to return Home)
		brand := ui.Row(c.Key("brand-home")).Gap(8).AlignItems(ui.Center).Cursor(ui.CursorPointer)
		brand.Children(func() {
			ui.Box(c).Size(28, 28).Radius(6).Background(ui.Hex("#EF443B")).Center().Children(func() {
				ui.Text(c, "G").FontSize(15).Bold().TextColor(ui.RGB(255, 255, 255))
			})
			ui.Column(c).Gap(1).Children(func() {
				ui.Text(c, "GonDesk").FontSize(15).Bold()
				ui.Text(c, "Remote Desktop").FontSize(11).TextColor(t.TextMuted)
			})
		})
		if brand.Clicked() {
			a.ActiveTab = 0
		}

		ui.Spacer(c)

		// Right Area: "Your Address: <816 692 488>" + "⚙ Settings"
		ui.Row(c).Gap(14).AlignItems(ui.Center).Children(func() {
			// "Your Address: <816 692 488>" (Clickable directly on the number to copy)
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "Your Address:").FontSize(13).TextColor(t.TextMuted)

				addrBox := ui.Box(c.Key("topbar-copy-address")).
					Padding(4, 10).Radius(6).
					Background(ui.Hex("#EF443B").Alpha(0.12)).
					Border(1, ui.Hex("#EF443B").Alpha(0.40)).
					Cursor(ui.CursorPointer).
					Tooltip("Click to copy address").
					Children(func() {
						ui.Text(c, a.DeskID).FontSize(14).Bold().
							Font("monospace").TextColor(ui.Hex("#EF443B"))
					})

				if addrBox.Clicked() {
					mygo.Clipboard.WriteText(a.DeskID)
					c.Toast("Copied Your Address: " + a.DeskID)
				}
			})

			// Settings Button with Cog Icon (Far Right)
			settingsBtn := ui.Button(c, "⚙ Settings")
			if a.ActiveTab == 1 {
				settingsBtn.Background(t.Accent).TextColor(t.AccentText)
			}
			if settingsBtn.Clicked() {
				if a.ActiveTab == 1 {
					a.ActiveTab = 0
				} else {
					a.ActiveTab = 1
				}
			}
		})
	})
}
