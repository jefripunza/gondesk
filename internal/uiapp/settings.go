package uiapp

import (
	"runtime"
	"strings"

	"github.com/egoist/mygo/ui"
)

func (a *AppUI) renderDesktopSettings(c *ui.Context, t *ui.Theme) {
	if a.SettingsSection == "" {
		a.SettingsSection = "general"
	}

	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		// Left: Settings Sidebar Menu
		ui.Column(c).Width(220).Background(t.Surface).
			BorderWidth(0, 1, 0, 0).BorderColor(t.Border).
			Padding(12, 10).Gap(6).
			Children(func() {
				ui.Box(c).Padding(8, 12, 12, 12).Children(func() {
					ui.Text(c, "Preferences").FontSize(14).Bold().TextColor(t.TextMuted)
				})

				ui.Sidebar(c, &a.SettingsSection, func() {
					ui.SidebarItem(c, "general", nil, "General")
					ui.SidebarItem(c, "network", nil, "Network")
					ui.SidebarItem(c, "about", nil, "About")
				}).Grow(1).FillWidth()

				ui.Box(c).Padding(8, 6).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
					if ui.Button(c, "Back to Home").FillWidth().Clicked() {
						a.ActiveTab = 0
					}
				})
			})

		// Right: Settings Content Area
		ui.Scroll(c.Key("settings-content-scroll")).Fill().Grow(1).Padding(24).Gap(16).Children(func() {
			switch a.SettingsSection {
			case "general":
				a.renderSettingsGeneral(c, t)
			case "network":
				a.renderSettingsNetwork(c, t)
			case "about":
				a.renderSettingsAbout(c, t)
			default:
				a.renderSettingsGeneral(c, t)
			}
		})
	})
}

func (a *AppUI) renderSettingsGeneral(c *ui.Context, t *ui.Theme) {
	ui.Column(c).MaxWidth(680).Padding(20).Gap(16).Radius(10).
		Background(t.Surface).Border(1, t.Border).
		Children(func() {
			ui.Text(c, "General Preferences").FontSize(18).Bold()
			ui.Text(c, "Configure device alias, unattended remote access credentials, and host permissions.").
				FontSize(13).TextColor(t.TextMuted)

			ui.Divider(c)

			ui.Text(c, "Device Alias").FontSize(13).Bold()
			ui.TextInput(c.Key("cfg-alias"), &a.AliasInput).Placeholder("e.g. Workstation-MacBook")

			ui.Text(c, "Unattended Access Password").FontSize(13).Bold()
			ui.TextInput(c.Key("cfg-access-pw"), &a.AccessPassword).Password().Placeholder("Password for unattended incoming connections")

			ui.Divider(c)

			ui.Text(c, "Remote Permissions").FontSize(13).Bold()
			ui.Checkbox(c, &a.UnattendedEnabled, "Enable Unattended Remote Access")
			ui.Checkbox(c, &a.AllowControl, "Allow Remote Keyboard and Mouse Control")
			ui.Checkbox(c, &a.AllowClipboard, "Allow Remote Clipboard Synchronization")

			ui.Row(c).Gap(10).Children(func() {
				if ui.PrimaryButton(c, "Save General Settings").Clicked() {
					if a.Store != nil {
						if st, err := a.Store.GetSettings(); err == nil {
							st.Alias = strings.TrimSpace(a.AliasInput)
							st.AccessPassword = a.AccessPassword
							st.UnattendedEnabled = a.UnattendedEnabled
							st.AllowControl = a.AllowControl
							st.AllowClipboard = a.AllowClipboard
							_ = a.Store.SaveSettings(&st)
							c.Toast("General settings saved successfully")
						}
					}
				}
			})
		})
}

func (a *AppUI) renderSettingsNetwork(c *ui.Context, t *ui.Theme) {
	ui.Column(c).MaxWidth(680).Padding(20).Gap(16).Radius(10).
		Background(t.Surface).Border(1, t.Border).
		Children(func() {
			ui.Text(c, "GonDesk Network & Single-Port Configuration").FontSize(18).Bold()
			ui.Text(c, "Both Desktop and Desktop Server communicate over 1 single port using HTTPS (GoFiber v2) + Pion TURN.").
				FontSize(13).TextColor(t.TextMuted)

			ui.Divider(c)

			ui.Text(c, "Global Bridge Server URL (Desktop Server)").FontSize(13).Bold()
			ui.TextInput(c.Key("cfg-bridge-url"), &a.BridgeURLInput).Placeholder("https://bridge.example.com:8443")

			ui.Row(c).Gap(10).Children(func() {
				if ui.PrimaryButton(c, "Save & Reconnect Bridge").Clicked() {
					if a.Store != nil {
						if st, err := a.Store.GetSettings(); err == nil {
							st.BridgeServerURL = strings.TrimSpace(a.BridgeURLInput)
							_ = a.Store.SaveSettings(&st)
							if a.Cfg != nil {
								a.Cfg.BridgeServerURL = st.BridgeServerURL
							}
							if a.Manager != nil {
								go a.Manager.ReconnectBridge()
							}
							c.Toast("Network configuration saved & reconnecting bridge")
						}
					}
				}
			})

			ui.Divider(c)

			ui.Text(c, "Single-Port Status").FontSize(13).Bold()
			ui.Column(c).Padding(12).Gap(6).Radius(8).Background(t.Background).Border(1, t.Border).Children(func() {
				ui.Row(c).Justify(ui.SpaceBetween).Children(func() {
					ui.Text(c, "Port Multiplexer").FontSize(13).TextColor(t.TextMuted)
					ui.Text(c, "Port 8443 (Active)").FontSize(13).Bold().TextColor(ui.Hex("#22C55E"))
				})
				ui.Row(c).Justify(ui.SpaceBetween).Children(func() {
					ui.Text(c, "HTTPS / REST / WebSocket").FontSize(13).TextColor(t.TextMuted)
					ui.Text(c, "GoFiber v2 Engine").FontSize(13)
				})
				ui.Row(c).Justify(ui.SpaceBetween).Children(func() {
					ui.Text(c, "TURN / STUN Relay").FontSize(13).TextColor(t.TextMuted)
					ui.Text(c, "Pion TURN (TLS+UDP)").FontSize(13)
				})
			})
		})
}

func (a *AppUI) renderSettingsAbout(c *ui.Context, t *ui.Theme) {
	ui.Column(c).MaxWidth(680).Padding(20).Gap(16).Radius(10).
		Background(t.Surface).Border(1, t.Border).
		Children(func() {
			ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(48, 48).Radius(10).Background(ui.Hex("#EF443B")).
					Justify(ui.Center).AlignItems(ui.Center).Children(func() {
						ui.Text(c, "G").FontSize(24).Bold().TextColor(ui.Hex("#FFFFFF"))
					})
				ui.Column(c).Gap(2).Children(func() {
					ui.Text(c, "GonDesk").FontSize(20).Bold()
					ui.Text(c, "Single-Port Remote Desktop Solution").FontSize(13).TextColor(t.TextMuted)
				})
			})

			ui.Divider(c)

			ui.Text(c, "About GonDesk").FontSize(14).Bold()
			ui.Text(c, "GonDesk is a fast, secure, modern open-source remote desktop alternative inspired by AnyDesk and RustDesk, architected to multiplex all HTTPS, WebSocket signaling, and Pion TURN traffic onto a single network port (8443).").
				FontSize(13).TextColor(t.TextMuted)

			ui.Column(c).Padding(12).Gap(8).Radius(8).Background(t.Background).Border(1, t.Border).Children(func() {
				ui.Row(c).Justify(ui.SpaceBetween).Children(func() {
					ui.Text(c, "Version").FontSize(13).TextColor(t.TextMuted)
					ui.Text(c, "v0.1.0").FontSize(13).Bold()
				})
				ui.Row(c).Justify(ui.SpaceBetween).Children(func() {
					ui.Text(c, "Platform").FontSize(13).TextColor(t.TextMuted)
					ui.Text(c, runtime.GOOS+"/"+runtime.GOARCH).FontSize(13)
				})
				ui.Row(c).Justify(ui.SpaceBetween).Children(func() {
					ui.Text(c, "Go Runtime").FontSize(13).TextColor(t.TextMuted)
					ui.Text(c, runtime.Version()).FontSize(13)
				})
				ui.Row(c).Justify(ui.SpaceBetween).Children(func() {
					ui.Text(c, "UI Framework").FontSize(13).TextColor(t.TextMuted)
					ui.Text(c, "MyGo Native UI (github.com/egoist/mygo)").FontSize(13)
				})
				ui.Row(c).Justify(ui.SpaceBetween).Children(func() {
					ui.Text(c, "Database").FontSize(13).TextColor(t.TextMuted)
					ui.Text(c, "Pure-Go SQLite (modernc.org/sqlite)").FontSize(13)
				})
				ui.Row(c).Justify(ui.SpaceBetween).Children(func() {
					ui.Text(c, "License").FontSize(13).TextColor(t.TextMuted)
					ui.Text(c, "MIT License").FontSize(13)
				})
			})
		})
}
