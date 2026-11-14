package uiapp

import (
	"gondesk/internal/config"

	"github.com/egoist/mygo/ui"
)

func (a *AppUI) renderTopBar(c *ui.Context, t *ui.Theme) {
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
