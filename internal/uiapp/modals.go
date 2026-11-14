package uiapp

import (
	"net/url"
	"strings"

	"gondesk/internal/database"

	"github.com/egoist/mygo/ui"
)

func (a *AppUI) renderAddPeerModal(c *ui.Context, t *ui.Theme) {
	if ui.DialogBase(c, &a.AddPeerModalOpen, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.85))
		panel.Width(390).Padding(20).Gap(14).Radius(12).
			Background(t.Surface).Border(1, t.Border).
			Shadow(0, 16, 40, 0, ui.RGBA(0, 0, 0, 0.7))

		modalTitle := "Add Desk to Address Book"
		saveBtnLabel := "Save Desk"
		if a.IsEditingPeer {
			modalTitle = "Edit Desk"
			saveBtnLabel = "Save Changes"
		}
		ui.Text(c, modalTitle).FontSize(16).Bold()

		ui.Column(c).Gap(4).Children(func() {
			urlIn := ui.TextInput(c.Key("modal-peer-url"), &a.NewPeerURL).
				Placeholder("URL (e.g. https://host:8443)").AutoFocus()
			if urlIn.FocusLeft() || (a.modalPeerURLFocused && !urlIn.Focused()) {
				cleaned, errMsg := ValidateHostURL(a.NewPeerURL)
				a.NewPeerURL = cleaned
				a.NewPeerURLError = errMsg
			}
			a.modalPeerURLFocused = urlIn.Focused()

			if a.NewPeerURLError != "" {
				ui.Text(c, a.NewPeerURLError).FontSize(12).TextColor(ui.Hex("#EF443B"))
			}
		})

		// GonDesk ID Field (Numbers only, styled gap every 3 digits without space character)
		a.syncPeerID()
		hasIDContent := a.NewPeerIDPart1 != "" || a.NewPeerIDPart2 != "" || a.NewPeerIDPart3 != ""
		idBorderColor := t.Border
		if a.PeerIDFocused {
			idBorderColor = t.Accent
		}

		idFieldBox := ui.Row(c.Key("modal-peer-id-field")).
			Padding(8, 12).
			Radius(t.Radius).
			Background(t.Surface).
			Border(1, idBorderColor).
			AlignItems(ui.Center).
			Gap(8)

		idFieldBox.Children(func() {
			if !hasIDContent && !a.PeerIDFocused {
				ui.Text(c, "GonDesk ID (e.g. 612 840 193)").FontSize(13).TextColor(t.TextMuted)
			} else {
				in1 := ui.TextInputBase(c.Key("modal-peer-id-1"), &a.NewPeerIDPart1).
					Placeholder("612").Width(28).Font("monospace").FontSize(14)
				if in1.Changed() {
					d := FilterOnlyDigits(a.NewPeerIDPart1, 9)
					if len(d) > 3 {
						a.NewPeerIDPart1 = d[0:3]
						if len(d) > 6 {
							a.NewPeerIDPart2 = d[3:6]
							a.NewPeerIDPart3 = d[6:]
							a.PeerIDReqFocus = 3
						} else {
							a.NewPeerIDPart2 = d[3:]
							a.PeerIDReqFocus = 2
						}
					} else {
						a.NewPeerIDPart1 = d
						if len(a.NewPeerIDPart1) == 3 {
							a.PeerIDReqFocus = 2
						}
					}
					a.NewPeerID = a.NewPeerIDPart1 + a.NewPeerIDPart2 + a.NewPeerIDPart3
				}

				in2 := ui.TextInputBase(c.Key("modal-peer-id-2"), &a.NewPeerIDPart2).
					Placeholder("840").Width(28).Font("monospace").FontSize(14)
				if in2.Changed() {
					d := FilterOnlyDigits(a.NewPeerIDPart2, 6)
					if len(d) > 3 {
						a.NewPeerIDPart2 = d[0:3]
						a.NewPeerIDPart3 = d[3:]
						a.PeerIDReqFocus = 3
					} else {
						a.NewPeerIDPart2 = d
						if len(a.NewPeerIDPart2) == 3 {
							a.PeerIDReqFocus = 3
						}
					}
					a.NewPeerID = a.NewPeerIDPart1 + a.NewPeerIDPart2 + a.NewPeerIDPart3
				}
				in2.HandleInput(func(ev ui.InputEvent) bool {
					if ev.Kind == ui.InputKeyDown && ev.Key == ui.KeyBackspace {
						if a.NewPeerIDPart2 == "" && a.NewPeerIDPart1 != "" {
							a.NewPeerIDPart1 = a.NewPeerIDPart1[:len(a.NewPeerIDPart1)-1]
							a.PeerIDReqFocus = 1
							a.NewPeerID = a.NewPeerIDPart1 + a.NewPeerIDPart2 + a.NewPeerIDPart3
							return true
						}
					}
					return false
				})

				in3 := ui.TextInputBase(c.Key("modal-peer-id-3"), &a.NewPeerIDPart3).
					Placeholder("193").Width(28).Font("monospace").FontSize(14)
				if in3.Changed() {
					a.NewPeerIDPart3 = FilterOnlyDigits(a.NewPeerIDPart3, 3)
					a.NewPeerID = a.NewPeerIDPart1 + a.NewPeerIDPart2 + a.NewPeerIDPart3
				}
				in3.HandleInput(func(ev ui.InputEvent) bool {
					if ev.Kind == ui.InputKeyDown && ev.Key == ui.KeyBackspace {
						if a.NewPeerIDPart3 == "" && a.NewPeerIDPart2 != "" {
							a.NewPeerIDPart2 = a.NewPeerIDPart2[:len(a.NewPeerIDPart2)-1]
							a.PeerIDReqFocus = 2
							a.NewPeerID = a.NewPeerIDPart1 + a.NewPeerIDPart2 + a.NewPeerIDPart3
							return true
						}
					}
					return false
				})

				switch a.PeerIDReqFocus {
				case 1:
					in1.Focus().SetTextSelection(len(a.NewPeerIDPart1), len(a.NewPeerIDPart1))
					a.PeerIDReqFocus = 0
				case 2:
					in2.Focus().SetTextSelection(len(a.NewPeerIDPart2), len(a.NewPeerIDPart2))
					a.PeerIDReqFocus = 0
				case 3:
					in3.Focus().SetTextSelection(len(a.NewPeerIDPart3), len(a.NewPeerIDPart3))
					a.PeerIDReqFocus = 0
				}

				a.PeerIDFocused = in1.Focused() || in2.Focused() || in3.Focused()
			}
		})

		if idFieldBox.Clicked() && !a.PeerIDFocused {
			a.PeerIDFocused = true
			switch {
			case len(a.NewPeerIDPart1) < 3:
				a.PeerIDReqFocus = 1
			case len(a.NewPeerIDPart2) < 3:
				a.PeerIDReqFocus = 2
			default:
				a.PeerIDReqFocus = 3
			}
		}

		ui.TextInput(c.Key("modal-peer-alias"), &a.NewPeerAlias).Placeholder("Alias (e.g. Home-Desktop)")

		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "Cancel").Clicked() {
				a.AddPeerModalOpen = false
				a.IsEditingPeer = false
				a.EditingOriginalDeskID = ""
				a.NewPeerURLError = ""
				a.NewPeerID = ""
				a.NewPeerIDPart1 = ""
				a.NewPeerIDPart2 = ""
				a.NewPeerIDPart3 = ""
				a.PeerIDReqFocus = 0
				a.PeerIDFocused = false
			}
			if ui.PrimaryButton(c, saveBtnLabel).Clicked() {
				cleaned, errMsg := ValidateHostURL(a.NewPeerURL)
				a.NewPeerURL = cleaned
				if errMsg != "" {
					a.NewPeerURLError = errMsg
					c.Toast(errMsg)
					return
				}
				a.NewPeerURLError = ""
				rawURL := cleaned
				peerID := strings.TrimSpace(a.NewPeerID)
				alias := strings.TrimSpace(a.NewPeerAlias)

				if peerID == "" && rawURL != "" {
					peerID = database.GenerateDeskID()
				}
				if alias == "" && rawURL != "" {
					if parsed, err := url.Parse(rawURL); err == nil && parsed.Host != "" {
						alias = parsed.Host
					}
				}
				if rawURL == "" {
					rawURL = a.Cfg.BridgeServerURL
				}

				if peerID != "" {
					if a.IsEditingPeer && a.EditingOriginalDeskID != "" && a.EditingOriginalDeskID != peerID {
						_ = a.Store.DeletePeer(a.EditingOriginalDeskID)
					}
					_, _ = a.Store.UpsertPeer(peerID, alias, "", "remote", rawURL, true)
					isEdit := a.IsEditingPeer
					a.NewPeerURL = ""
					a.NewPeerURLError = ""
					a.NewPeerID = ""
					a.NewPeerIDPart1 = ""
					a.NewPeerIDPart2 = ""
					a.NewPeerIDPart3 = ""
					a.PeerIDReqFocus = 0
					a.PeerIDFocused = false
					a.NewPeerAlias = ""
					a.IsEditingPeer = false
					a.EditingOriginalDeskID = ""
					a.AddPeerModalOpen = false
					a.ReloadFromDB()
					if isEdit {
						c.Toast("Updated desk in SQLite Address Book")
					} else {
						c.Toast("Saved desk to SQLite Address Book")
					}
				}
			}
		})
	}).Dismissed() {
		// PERTAHANKAN MODAL TETAP TERBUKA JIKA DIKLIK DI LUAR
		// Clicks pada backdrop luar mengaktifkan Dismissed(), kita setel kembali ke true
		// sesuai aturan baku AGENT.md sehingga dialog TIDAK AKAN tertutup kecuali tombol Cancel/Save di dalam diklik.
		a.AddPeerModalOpen = true
	}
}

func (a *AppUI) renderDeleteConfirmModal(c *ui.Context, t *ui.Theme) {
	if ui.DialogBase(c, &a.DeleteConfirmModalOpen, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.85))
		panel.Width(390).Padding(22).Gap(14).Radius(12).
			Background(t.Surface).Border(1, t.Border).
			Shadow(0, 16, 40, 0, ui.RGBA(0, 0, 0, 0.7))

		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(36, 36).Radius(8).Background(ui.Hex("#EF443B").Alpha(0.15)).Center().Children(func() {
				ui.Text(c, "🗑").FontSize(18)
			})
			ui.Column(c).Gap(2).Children(func() {
				ui.Text(c, "Hapus Desk").FontSize(16).Bold()
				ui.Text(c, "Konfirmasi Penghapusan").FontSize(12).TextColor(t.TextMuted)
			})
		})

		alias := a.PendingDeletePeerAlias
		if alias == "" {
			alias = a.PendingDeletePeerID
		}
		ui.Textf(c, "Apakah Anda yakin ingin menghapus desk \"%s\" (%s) dari buku alamat?", alias, a.PendingDeletePeerID).
			FontSize(13).LineHeight(1.4)

		ui.Row(c).Gap(10).Justify(ui.End).Children(func() {
			if ui.Button(c, "Batal").Clicked() {
				a.DeleteConfirmModalOpen = false
				a.PendingDeletePeerID = ""
				a.PendingDeletePeerAlias = ""
			}
			if ui.PrimaryButton(c, "Hapus Desk").Background(ui.Hex("#EF443B")).Clicked() {
				if a.Store != nil && a.PendingDeletePeerID != "" {
					_ = a.Store.DeletePeer(a.PendingDeletePeerID)
					a.ReloadFromDB()
					c.Toast("Desk " + alias + " berhasil dihapus")
				}
				a.DeleteConfirmModalOpen = false
				a.PendingDeletePeerID = ""
				a.PendingDeletePeerAlias = ""
			}
		})
	}).Dismissed() {
		// PERTAHANKAN MODAL TETAP TERBUKA JIKA DIKLIK DI LUAR
		// Sesuai aturan baku AGENT.md: Clicks pada backdrop luar mengaktifkan Dismissed(),
		// kita setel kembali ke true sehingga dialog TIDAK AKAN tertutup kecuali tombol Batal/Hapus di dalam diklik.
		a.DeleteConfirmModalOpen = true
	}
}
