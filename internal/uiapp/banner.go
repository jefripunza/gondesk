package uiapp

import (
	"gondesk/internal/remote"

	"github.com/egoist/mygo/ui"
)

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
