package uiapp

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

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
