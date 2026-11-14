package uiapp

import (
	"fmt"

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
				ui.Text(c, "Remote Desktop").FontSize(11).TextColor(t.TextMuted)
			})
		})

		// Navigation Tabs
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

		ui.Spacer(c)

		// 1-Port HTTPS + TURN Status Badge
		ui.Badge(c, fmt.Sprintf("1-Port HTTPS + TURN (:%d)", a.Cfg.ListenPort)).
			Background(ui.Hex("#16A34A").Alpha(0.15)).TextColor(ui.Hex("#16A34A"))
	})
}
