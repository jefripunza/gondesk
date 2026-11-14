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

		ui.Text(c, "Add Desk to Address Book").FontSize(16).Bold()

		urlIn := ui.TextInput(c.Key("modal-peer-url"), &a.NewPeerURL).
			Placeholder("Endpoint URL (e.g. https://host:8443)").AutoFocus()
		if urlIn.FocusLeft() || (a.modalPeerURLFocused && !urlIn.Focused()) {
			a.NewPeerURL = CleanURL(a.NewPeerURL)
		}
		a.modalPeerURLFocused = urlIn.Focused()

		ui.TextInput(c.Key("modal-peer-id"), &a.NewPeerID).Placeholder("GonDesk ID (e.g. 612 840 193)")
		ui.TextInput(c.Key("modal-peer-alias"), &a.NewPeerAlias).Placeholder("Alias (e.g. Home-Desktop)")

		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "Cancel").Clicked() {
				a.AddPeerModalOpen = false
			}
			if ui.PrimaryButton(c, "Save Desk").Clicked() {
				rawURL := CleanURL(a.NewPeerURL)
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
					_, _ = a.Store.UpsertPeer(peerID, alias, "", "remote", rawURL, true)
					a.NewPeerURL = ""
					a.NewPeerID = ""
					a.NewPeerAlias = ""
					a.AddPeerModalOpen = false
					a.ReloadFromDB()
					c.Toast("Saved desk to SQLite Address Book")
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
