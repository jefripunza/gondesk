package uiapp

import (
	"fmt"
	"path/filepath"

	"gondesk/internal/remote"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// TriggerRemotePaste handles Ctrl+V / Cmd+V inside the remote desktop window.
// If local clipboard has files or folders, it opens a dedicated File Paste Status window
// and transfers them directly into the target folder on Host B.
// Otherwise, if local clipboard has text, it sends and pastes the text directly on Host B.
func (a *AppUI) TriggerRemotePaste(c *ui.Context, sw *ActiveSessionWindow) {
	if a.Manager == nil || sw == nil || sw.SessionUUID == "" {
		return
	}

	files, text := remote.ReadLocalClipboardForPaste()
	if len(files) > 0 {
		ft, err := a.Manager.StartPasteFiles(sw.SessionUUID, files)
		if err != nil {
			if c != nil {
				c.Toast("Gagal memulai paste file: " + err.Error())
			}
			return
		}
		a.OpenFilePasteWindow(ft.TransferID)
		return
	}

	if text != "" {
		a.Manager.SendClipboard(sw.SessionUUID, text)
		if c != nil {
			c.Toast("Teks clipboard dikirim & di-paste ke remote desk")
		}
	}
}

// OpenFilePasteWindow creates or focuses a dedicated native window for tracking file/folder paste progress.
func (a *AppUI) OpenFilePasteWindow(transferID string) *mygo.Window {
	if transferID == "" {
		return nil
	}
	if a.PasteWindows == nil {
		a.PasteWindows = make(map[string]*mygo.Window)
	}
	if existing, ok := a.PasteWindows[transferID]; ok && existing != nil {
		existing.Focus()
		return existing
	}

	if !mygo.App.IsReady() {
		return nil
	}

	title := "GonDesk - Status Paste File / Folder"
	if a.Manager != nil {
		for _, ft := range a.Manager.FileTransfersSnapshot() {
			if ft.TransferID == transferID && ft.RootName != "" {
				title = "GonDesk - Paste: " + ft.RootName
				break
			}
		}
	}

	win := mygo.NewWindow(mygo.WindowOptions{
		Title:     title,
		Width:     520,
		Height:    340,
		MinWidth:  440,
		MinHeight: 290,
		Content: ui.View(func(c *ui.Context) {
			a.RenderFilePasteWindow(c, transferID)
		}),
	})
	a.PasteWindows[transferID] = win

	win.OnClose(func(_ *mygo.CloseEvent) {
		delete(a.PasteWindows, transferID)
	})

	return win
}

// UpdateFilePasteWindows synchronizes or opens the file paste status window on state changes.
func (a *AppUI) UpdateFilePasteWindows() {
	if a.Manager == nil {
		return
	}
	if a.PasteWindows == nil {
		a.PasteWindows = make(map[string]*mygo.Window)
	}

	transfers := a.Manager.FileTransfersSnapshot()
	for _, ft := range transfers {
		win, exists := a.PasteWindows[ft.TransferID]
		if !exists && (ft.Status == "transferring" || ft.Status == "preparing" || ft.Status == "pasting") {
			a.OpenFilePasteWindow(ft.TransferID)
			continue
		}
		if win != nil {
			win.Update(func() {})
		}
	}
}

// RenderFilePasteWindow renders the dedicated UI for file/folder paste transfer between A and B.
func (a *AppUI) RenderFilePasteWindow(c *ui.Context, transferID string) {
	anydeskRed := ui.Hex("#EF443B")
	theme := *c.Theme().WithAccent(anydeskRed)
	theme.Radius = 8
	c.SetTheme(&theme)

	var ft *remote.FileTransferState
	if a.Manager != nil {
		for _, item := range a.Manager.FileTransfersSnapshot() {
			if item.TransferID == transferID {
				cp := item
				ft = &cp
				break
			}
		}
	}

	ui.Column(c).Fill().Background(ui.Hex("#0B0F19")).Padding(20).Gap(14).Children(func() {
		if ft == nil {
			ui.Column(c).Fill().Center().Gap(8).Children(func() {
				ui.Text(c, "Data transfer tidak ditemukan").FontSize(14).Bold().TextColor(ui.Hex("#94A3B8"))
			})
			return
		}

		// 1. Header Row: Type Icon + Name + Status Badge
		ui.Row(c.Key("paste-header")).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				iconLabel := "📄"
				typeDesc := "File Paste Transfer"
				if ft.IsFolder {
					iconLabel = "📁"
					typeDesc = fmt.Sprintf("Folder Paste Transfer (%d file)", ft.TotalFiles)
				}
				ui.Box(c).Size(40, 40).Radius(8).Background(ui.Hex("#1E293B")).Center().Children(func() {
					ui.Text(c, iconLabel).FontSize(20)
				})
				ui.Column(c).Gap(2).Children(func() {
					ui.Text(c, ft.RootName).FontSize(15).Bold().TextColor(ui.Hex("#F8FAFC")).SingleLine()
					ui.Text(c, typeDesc).FontSize(11).TextColor(ui.Hex("#94A3B8"))
				})
			})

			switch ft.Status {
			case "completed":
				ui.Badge(c, "SELESAI DITEMPEL").Background(ui.Hex("#16A34A").Alpha(0.2)).TextColor(ui.Hex("#22C55E"))
			case "canceled":
				ui.Badge(c, "DIBATALKAN").Background(ui.Hex("#EF443B").Alpha(0.2)).TextColor(ui.Hex("#EF443B"))
			case "error":
				ui.Badge(c, "GAGAL").Background(ui.Hex("#EF443B").Alpha(0.2)).TextColor(ui.Hex("#EF443B"))
			case "pasting":
				ui.Badge(c, "MENEMPELKAN...").Background(ui.Hex("#EAB308").Alpha(0.2)).TextColor(ui.Hex("#FACC15"))
			default:
				ui.Badge(c, "MENGIRIM...").Background(ui.Hex("#3B82F6").Alpha(0.2)).TextColor(ui.Hex("#60A5FA"))
			}
		})

		// 2. Source & Target Info Card
		ui.Box(c.Key("paste-route-card")).FillWidth().Padding(12, 14).Radius(8).
			Background(ui.Hex("#111827")).Border(1, ui.Hex("#1E293B")).Children(func() {
			ui.Column(c).Gap(6).Children(func() {
				routeLabel := fmt.Sprintf("Perangkat Ini  →  %s (%s)", ft.PeerAlias, ft.PeerDeskID)
				if ft.Direction == "receiving" {
					routeLabel = fmt.Sprintf("%s (%s)  →  Perangkat Ini", ft.PeerAlias, ft.PeerDeskID)
				}
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "Rute:").FontSize(11).Bold().TextColor(ui.Hex("#64748B"))
					ui.Text(c, routeLabel).FontSize(12).TextColor(ui.Hex("#E2E8F0")).SingleLine()
				})

				targetFolder := ft.TargetDir
				if targetFolder == "" {
					targetFolder = "Mendeteksi folder tujuan..."
				}
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "Folder Tujuan:").FontSize(11).Bold().TextColor(ui.Hex("#64748B"))
					ui.Text(c, targetFolder).FontSize(12).Bold().TextColor(ui.Hex("#38BDF8")).SingleLine()
				})
			})
		})

		// 3. Progress Bar & Telemetry
		prog := ft.ProgressFraction()
		pct := int(prog * 100)
		ui.Column(c.Key("paste-progress-col")).FillWidth().Gap(6).Children(func() {
			ui.Row(c).FillWidth().Justify(ui.SpaceBetween).Children(func() {
				curItem := ft.CurrentFile
				if curItem == "" {
					curItem = ft.RootName
				}
				if ft.Status == "completed" {
					ui.Text(c, "Semua item berhasil ditempel ke folder tujuan").FontSize(12).TextColor(ui.Hex("#22C55E")).SingleLine()
				} else {
					ui.Text(c, "Item: "+curItem).FontSize(12).TextColor(ui.Hex("#CBD5E1")).SingleLine()
				}
				ui.Text(c, fmt.Sprintf("%d%%", pct)).FontSize(12).Bold().TextColor(ui.Hex("#F8FAFC"))
			})

			ui.Progress(c, float64(prog)).FillWidth()

			ui.Row(c).FillWidth().Justify(ui.SpaceBetween).Children(func() {
				ui.Text(c, fmt.Sprintf("%s / %s", formatByteSize(ft.TransferredBytes), formatByteSize(ft.TotalBytes))).
					FontSize(11).TextColor(ui.Hex("#94A3B8"))
				ui.Text(c, fmt.Sprintf("%d / %d file selesai", ft.CompletedFiles, ft.TotalFiles)).
					FontSize(11).TextColor(ui.Hex("#94A3B8"))
			})
		})

		ui.Spacer(c)

		// 4. Footer Action Buttons
		ui.Row(c.Key("paste-actions")).FillWidth().Justify(ui.End).Gap(10).Children(func() {
			if ft.Status == "transferring" || ft.Status == "preparing" || ft.Status == "pasting" {
				if ui.Button(c.Key("btn-cancel-paste"), "Batalkan").Clicked() && a.Manager != nil {
					a.Manager.CancelFileTransfer(ft.TransferID)
				}
			} else {
				if ft.Direction == "receiving" && ft.Status == "completed" && ft.TargetDir != "" {
					if ui.Button(c.Key("btn-open-folder"), "📂 Buka Folder").Clicked() && mygo.App.IsReady() {
						dest := filepath.Join(ft.TargetDir, filepath.Base(ft.RootName))
						mygo.Shell.ShowItemInFolder(dest)
					}
				}
				if ui.PrimaryButton(c.Key("btn-close-paste"), "Tutup").Clicked() {
					if a.PasteWindows != nil {
						if win, ok := a.PasteWindows[ft.TransferID]; ok && win != nil {
							delete(a.PasteWindows, ft.TransferID)
							win.Close()
						}
					}
				}
			}
		})
	})
}

func formatByteSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
