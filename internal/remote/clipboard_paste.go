package remote

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"gondesk/internal/server"

	"github.com/egoist/mygo"
	"github.com/google/uuid"
)

const fileChunkSize = 64 * 1024 // 64 KB per chunk

// FileTransferState tracks real-time progress of a file or folder paste operation between A and B.
type FileTransferState struct {
	TransferID       string    `json:"transfer_id"`
	SessionUUID      string    `json:"session_uuid"`
	Direction        string    `json:"direction"` // "sending" or "receiving"
	PeerDeskID       string    `json:"peer_desk_id"`
	PeerAlias        string    `json:"peer_alias"`
	RootName         string    `json:"root_name"`
	IsFolder         bool      `json:"is_folder"`
	TotalFiles       int       `json:"total_files"`
	CompletedFiles   int       `json:"completed_files"`
	TotalBytes       int64     `json:"total_bytes"`
	TransferredBytes int64     `json:"transferred_bytes"`
	CurrentFile      string    `json:"current_file"`
	TargetDir        string    `json:"target_dir"`
	Status           string    `json:"status"` // "preparing", "transferring", "pasting", "completed", "error", "canceled"
	ErrorMessage     string    `json:"error_message"`
	StartedAt        time.Time `json:"started_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// ProgressFraction returns a 0.0 to 1.0 float32 progress value for UI progress bars.
func (ft *FileTransferState) ProgressFraction() float32 {
	if ft == nil {
		return 0
	}
	if ft.Status == "completed" {
		return 1.0
	}
	if ft.TotalBytes <= 0 {
		if ft.TotalFiles > 0 {
			return float32(ft.CompletedFiles) / float32(ft.TotalFiles)
		}
		return 0
	}
	frac := float32(ft.TransferredBytes) / float32(ft.TotalBytes)
	if frac < 0 {
		return 0
	}
	if frac > 1 {
		return 1
	}
	return frac
}

type fileTransferStartPayload struct {
	TransferID string `json:"transfer_id"`
	RootName   string `json:"root_name"`
	IsFolder   bool   `json:"is_folder"`
	TotalFiles int    `json:"total_files"`
	TotalBytes int64  `json:"total_bytes"`
}

type fileTransferChunkPayload struct {
	TransferID  string `json:"transfer_id"`
	RelPath     string `json:"rel_path"`
	IsDir       bool   `json:"is_dir"`
	ChunkIndex  int    `json:"chunk_index"`
	DataBase64  string `json:"data_base64"`
	IsLastChunk bool   `json:"is_last_chunk"`
}

type fileTransferProgressPayload struct {
	TransferID       string `json:"transfer_id"`
	TransferredBytes int64  `json:"transferred_bytes"`
	CompletedFiles   int    `json:"completed_files"`
	CurrentFile      string `json:"current_file"`
	TargetDir        string `json:"target_dir"`
	Status           string `json:"status"`
	ErrorMessage     string `json:"error_message,omitempty"`
}

// ReadLocalClipboardForPaste inspects the local OS clipboard for copied files/folders first, then text.
func ReadLocalClipboardForPaste() (files []string, text string) {
	if mygo.App.IsReady() {
		clipFiles, _ := mygo.Clipboard.ReadFiles()
		for _, f := range clipFiles {
			f = strings.TrimSpace(f)
			if f != "" {
				if _, err := os.Stat(f); err == nil {
					files = append(files, f)
				}
			}
		}
	}

	if len(files) == 0 {
		files = readOSClipboardFilesFallback()
	}
	if len(files) > 0 {
		return files, ""
	}

	if mygo.App.IsReady() {
		text = mygo.Clipboard.ReadText()
	}
	if text == "" {
		text = readOSClipboardTextFallback()
	}
	return nil, text
}

func readOSClipboardFilesFallback() []string {
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	var rawOut []byte
	var err error

	switch runtime.GOOS {
	case "darwin":
		jxa := `ObjC.import("AppKit"); var pb = $.NSPasteboard.generalPasteboard; var urls = pb.readObjectsForClassesOptions($([$.NSURL.class]), $({})); var out = []; if (urls) { for (var i = 0; i < urls.count; i++) { var p = ObjC.unwrap(urls.objectAtIndex(i).path); if (p) out.push(p); } } out.join("\n");`
		// #nosec G204 -- safe internal osascript clipboard inspection
		rawOut, err = exec.CommandContext(ctx, "osascript", "-l", "JavaScript", "-e", jxa).Output()
	case "windows":
		ps := `Get-Clipboard -Format FileDropList | ForEach-Object { $_.FullName }`
		// #nosec G204 -- safe internal powershell clipboard inspection
		rawOut, err = exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", ps).Output()
	case "linux":
		if p, lerr := exec.LookPath("wl-paste"); lerr == nil && p != "" {
			// #nosec G204 -- safe internal wl-paste clipboard inspection
			rawOut, err = exec.CommandContext(ctx, p, "--type", "text/uri-list").Output()
		}
		if len(rawOut) == 0 {
			if p, lerr := exec.LookPath("xclip"); lerr == nil && p != "" {
				// #nosec G204 -- safe internal xclip clipboard inspection
				rawOut, err = exec.CommandContext(ctx, p, "-selection", "clipboard", "-t", "text/uri-list", "-o").Output()
			}
		}
	}

	if err != nil || len(rawOut) == 0 {
		return nil
	}

	var result []string
	lines := strings.Split(string(rawOut), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || line == "copy" || line == "cut" {
			continue
		}
		if strings.HasPrefix(line, "file://") {
			if u, uerr := url.Parse(line); uerr == nil && u.Path != "" {
				if unescaped, perr := url.PathUnescape(u.Path); perr == nil {
					line = unescaped
				} else {
					line = u.Path
				}
			}
		}
		if _, serr := os.Stat(line); serr == nil {
			result = append(result, line)
		}
	}
	return result
}

func readOSClipboardTextFallback() string {
	ctx, cancel := context.WithTimeout(context.Background(), 1000*time.Millisecond)
	defer cancel()

	switch runtime.GOOS {
	case "darwin":
		// #nosec G204 -- safe internal pbpaste invocation
		if out, err := exec.CommandContext(ctx, "pbpaste").Output(); err == nil {
			return string(out)
		}
	case "windows":
		// #nosec G204 -- safe internal powershell Get-Clipboard invocation
		if out, err := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", "Get-Clipboard").Output(); err == nil {
			return strings.TrimSuffix(strings.TrimSuffix(string(out), "\n"), "\r")
		}
	case "linux":
		if p, err := exec.LookPath("wl-paste"); err == nil && p != "" {
			// #nosec G204 -- safe internal wl-paste invocation
			if out, oerr := exec.CommandContext(ctx, p, "-n").Output(); oerr == nil {
				return string(out)
			}
		}
		if p, err := exec.LookPath("xclip"); err == nil && p != "" {
			// #nosec G204 -- safe internal xclip invocation
			if out, oerr := exec.CommandContext(ctx, p, "-selection", "clipboard", "-o").Output(); oerr == nil {
				return string(out)
			}
		}
	}
	return ""
}

// TriggerOSPasteText writes text to the host clipboard and simulates a native paste keystroke (Ctrl+V / Cmd+V).
func TriggerOSPasteText(text string) {
	if text == "" {
		return
	}
	if mygo.App.IsReady() {
		mygo.Clipboard.WriteText(text)
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2000*time.Millisecond)
		defer cancel()

		switch runtime.GOOS {
		case "darwin":
			// #nosec G204 -- safe internal pbcopy & osascript keystroke simulation
			cmd := exec.CommandContext(ctx, "pbcopy")
			cmd.Stdin = strings.NewReader(text)
			_ = cmd.Run()
			// #nosec G204 -- safe internal osascript paste keystroke
			_ = exec.CommandContext(ctx, "osascript", "-e", `tell application "System Events" to keystroke "v" using command down`).Run()

		case "windows":
			// #nosec G204 -- safe internal powershell paste simulation
			ps := `$wshell = New-Object -ComObject WScript.Shell; $wshell.SendKeys('^v')`
			_ = exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", ps).Run()

		case "linux":
			if p, err := exec.LookPath("wl-copy"); err == nil && p != "" {
				// #nosec G204 -- safe internal wl-copy invocation
				cmd := exec.CommandContext(ctx, p)
				cmd.Stdin = strings.NewReader(text)
				_ = cmd.Run()
			}
			if p, err := exec.LookPath("xclip"); err == nil && p != "" {
				// #nosec G204 -- safe internal xclip invocation
				cmd := exec.CommandContext(ctx, p, "-selection", "clipboard")
				cmd.Stdin = strings.NewReader(text)
				_ = cmd.Run()
			}

			// Simulate Ctrl+V on active window
			if p, err := exec.LookPath("xdotool"); err == nil && p != "" {
				// #nosec G204 -- safe internal xdotool paste keystroke
				if err := exec.CommandContext(ctx, p, "key", "--clearmodifiers", "ctrl+v").Run(); err == nil {
					return
				}
			}
			if p, err := exec.LookPath("wtype"); err == nil && p != "" {
				// #nosec G204 -- safe internal wtype paste keystroke
				if err := exec.CommandContext(ctx, p, "-M", "ctrl", "v", "-m", "ctrl").Run(); err == nil {
					return
				}
			}
			if p, err := exec.LookPath("ydotool"); err == nil && p != "" {
				// #nosec G204 -- safe internal ydotool paste keystroke
				_ = exec.CommandContext(ctx, p, "key", "29:1", "47:1", "47:0", "29:0").Run()
			}
		}
	}()
}

// DetectActiveTargetDirectory determines the folder currently open/targeted on Host B.
func DetectActiveTargetDirectory(customOverride string) string {
	if customOverride != "" {
		return customOverride
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer cancel()

	homeDir, _ := os.UserHomeDir()

	switch runtime.GOOS {
	case "darwin":
		script := `tell application "Finder" to if (count of Finder windows) > 0 then get POSIX path of (target of front Finder window as alias) else get POSIX path of (desktop as alias)`
		// #nosec G204 -- safe internal Finder active folder query
		if out, err := exec.CommandContext(ctx, "osascript", "-e", script).Output(); err == nil {
			cand := strings.TrimSpace(string(out))
			if info, serr := os.Stat(cand); serr == nil && info.IsDir() {
				return cand
			}
		}

	case "windows":
		ps := `$shell = New-Object -ComObject Shell.Application; $w = $shell.Windows() | Where-Object { $_.LocationURL -like 'file://*' } | Select-Object -First 1; if ($w) { [uri]::UnescapeDataString($w.LocationURL).Replace('file:///', '').Replace('/', '\') }`
		// #nosec G204 -- safe internal Explorer active folder query
		if out, err := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", ps).Output(); err == nil {
			cand := strings.TrimSpace(string(out))
			if info, serr := os.Stat(cand); serr == nil && info.IsDir() {
				return cand
			}
		}

	case "linux":
		if p, err := exec.LookPath("xdotool"); err == nil && p != "" {
			// 1. Check active window title for path
			// #nosec G204 -- safe internal xdotool active window query
			if out, oerr := exec.CommandContext(ctx, p, "getactivewindow", "getwindowname").Output(); oerr == nil {
				title := strings.TrimSpace(string(out))
				if dir := resolveDirFromWindowTitle(title, homeDir); dir != "" {
					return dir
				}
			}
			// 2. Check active window PID working directory
			// #nosec G204 -- safe internal xdotool active window PID query
			if out, oerr := exec.CommandContext(ctx, p, "getactivewindow", "getwindowpid").Output(); oerr == nil {
				pidStr := strings.TrimSpace(string(out))
				if pidStr != "" {
					if cwd, lerr := os.Readlink(string(filepath.Separator) + filepath.Join("proc", pidStr, "cwd")); lerr == nil {
						if info, serr := os.Stat(cwd); serr == nil && info.IsDir() && cwd != "/" {
							return cwd
						}
					}
				}
			}
		}
	}

	if homeDir != "" {
		desktopDir := filepath.Join(homeDir, "Desktop")
		if info, err := os.Stat(desktopDir); err == nil && info.IsDir() {
			return desktopDir
		}
		downloadsDir := filepath.Join(homeDir, "Downloads")
		if info, err := os.Stat(downloadsDir); err == nil && info.IsDir() {
			return downloadsDir
		}
		return homeDir
	}

	return os.TempDir()
}

func resolveDirFromWindowTitle(title, homeDir string) string {
	if title == "" {
		return ""
	}
	// Check if title contains an explicit path like /home/... or ~/...
	parts := strings.FieldsFunc(title, func(r rune) bool {
		return r == '—' || r == '-' || r == ':' || r == '|'
	})
	for _, part := range parts {
		cand := strings.TrimSpace(part)
		if strings.HasPrefix(cand, "~/") && homeDir != "" {
			cand = filepath.Join(homeDir, cand[2:])
		}
		if filepath.IsAbs(cand) {
			if info, err := os.Stat(cand); err == nil && info.IsDir() {
				return cand
			}
		}
		if homeDir != "" && cand != "" && !strings.Contains(cand, "/") {
			sub := filepath.Join(homeDir, cand)
			if info, err := os.Stat(sub); err == nil && info.IsDir() {
				return sub
			}
		}
	}
	return ""
}

type transferEntry struct {
	absPath string
	relPath string
	isDir   bool
	size    int64
}

func collectTransferEntries(paths []string) (rootName string, isFolder bool, entries []transferEntry, totalFiles int, totalBytes int64, err error) {
	var validPaths []string
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, serr := os.Stat(p); serr == nil {
			validPaths = append(validPaths, p)
		}
	}
	if len(validPaths) == 0 {
		return "", false, nil, 0, 0, errors.New("no valid files or folders found in clipboard")
	}

	if len(validPaths) == 1 {
		info, serr := os.Stat(validPaths[0])
		if serr != nil {
			return "", false, nil, 0, 0, serr
		}
		rootName = filepath.Base(validPaths[0])
		isFolder = info.IsDir()
	} else {
		rootName = fmt.Sprintf("%s (+%d item)", filepath.Base(validPaths[0]), len(validPaths)-1)
		isFolder = true
	}

	for _, rootPath := range validPaths {
		info, serr := os.Stat(rootPath)
		if serr != nil {
			continue
		}
		baseName := filepath.Base(rootPath)
		if !info.IsDir() {
			entries = append(entries, transferEntry{
				absPath: rootPath,
				relPath: filepath.ToSlash(baseName),
				isDir:   false,
				size:    info.Size(),
			})
			totalFiles++
			totalBytes += info.Size()
			continue
		}

		parentDir := filepath.Dir(rootPath)
		walkErr := filepath.WalkDir(rootPath, func(currPath string, d os.DirEntry, wErr error) error {
			if wErr != nil {
				return wErr
			}
			rel, rErr := filepath.Rel(parentDir, currPath)
			if rErr != nil {
				return rErr
			}
			relSlash := filepath.ToSlash(rel)
			if d.IsDir() {
				entries = append(entries, transferEntry{
					absPath: currPath,
					relPath: relSlash,
					isDir:   true,
				})
				return nil
			}
			fInfo, iErr := d.Info()
			var fSize int64
			if iErr == nil {
				fSize = fInfo.Size()
			}
			entries = append(entries, transferEntry{
				absPath: currPath,
				relPath: relSlash,
				isDir:   false,
				size:    fSize,
			})
			totalFiles++
			totalBytes += fSize
			return nil
		})
		if walkErr != nil {
			return "", false, nil, 0, 0, walkErr
		}
	}

	return rootName, isFolder, entries, totalFiles, totalBytes, nil
}

// StartPasteFiles initiates a chunked file or folder transfer from Client A to Host B.
func (m *Manager) StartPasteFiles(sessionUUID string, paths []string) (*FileTransferState, error) {
	rootName, isFolder, entries, totalFiles, totalBytes, err := collectTransferEntries(paths)
	if err != nil {
		return nil, err
	}

	st, _ := m.Store.GetSettings()
	transferID := uuid.NewString()

	m.mu.Lock()
	var peerID, peerAlias string
	for _, s := range m.sessions {
		if s.SessionUUID == sessionUUID {
			peerID = s.PeerDeskID
			peerAlias = s.PeerAlias
			break
		}
	}
	if peerAlias == "" {
		peerAlias = peerID
	}

	if m.fileTransfers == nil {
		m.fileTransfers = make(map[string]*FileTransferState)
	}
	ftState := &FileTransferState{
		TransferID:  transferID,
		SessionUUID: sessionUUID,
		Direction:   "sending",
		PeerDeskID:  peerID,
		PeerAlias:   peerAlias,
		RootName:    rootName,
		IsFolder:    isFolder,
		TotalFiles:  totalFiles,
		TotalBytes:  totalBytes,
		TargetDir:   "Mendeteksi folder tujuan di " + peerAlias + "...",
		Status:      "transferring",
		StartedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	m.fileTransfers[transferID] = ftState
	m.mu.Unlock()
	m.notify()

	go m.runFileTransferSender(ftState, entries, st.DeskID, st.Alias, peerID)
	return ftState, nil
}

func (m *Manager) runFileTransferSender(ftState *FileTransferState, entries []transferEntry, fromDeskID, fromAlias, peerID string) {
	startPayload, _ := json.Marshal(fileTransferStartPayload{
		TransferID: ftState.TransferID,
		RootName:   ftState.RootName,
		IsFolder:   ftState.IsFolder,
		TotalFiles: ftState.TotalFiles,
		TotalBytes: ftState.TotalBytes,
	})
	m.sendSessionSignal(ftState.SessionUUID, server.SignalMessage{
		Type:        "file_transfer_start",
		SessionUUID: ftState.SessionUUID,
		FromDeskID:  fromDeskID,
		FromAlias:   fromAlias,
		ToDeskID:    peerID,
		Payload:     string(startPayload),
		Timestamp:   time.Now().Format("15:04:05"),
	})

	buf := make([]byte, fileChunkSize)

	for _, entry := range entries {
		m.mu.Lock()
		canceled := ftState.Status == "canceled" || ftState.Status == "error"
		if !canceled {
			ftState.CurrentFile = entry.relPath
			ftState.UpdatedAt = time.Now()
		}
		m.mu.Unlock()
		if canceled {
			return
		}

		if entry.isDir {
			chunkPayload, _ := json.Marshal(fileTransferChunkPayload{
				TransferID:  ftState.TransferID,
				RelPath:     entry.relPath,
				IsDir:       true,
				IsLastChunk: true,
			})
			m.sendSessionSignal(ftState.SessionUUID, server.SignalMessage{
				Type:        "file_transfer_chunk",
				SessionUUID: ftState.SessionUUID,
				FromDeskID:  fromDeskID,
				FromAlias:   fromAlias,
				ToDeskID:    peerID,
				Payload:     string(chunkPayload),
				Timestamp:   time.Now().Format("15:04:05"),
			})
			continue
		}

		// #nosec G304 -- reading user-copied file for explicit remote paste transfer
		f, err := os.Open(entry.absPath)
		if err != nil {
			continue
		}

		chunkIdx := 0
		for {
			m.mu.Lock()
			isCanceled := ftState.Status == "canceled" || ftState.Status == "error"
			m.mu.Unlock()
			if isCanceled {
				_ = f.Close()
				return
			}

			n, rErr := f.Read(buf)
			isEOF := rErr == io.EOF || n == 0
			isLast := isEOF || int64((chunkIdx+1)*fileChunkSize) >= entry.size

			if n > 0 || (chunkIdx == 0 && entry.size == 0) {
				var encoded string
				if n > 0 {
					encoded = base64.StdEncoding.EncodeToString(buf[:n])
				}
				chunkPayload, _ := json.Marshal(fileTransferChunkPayload{
					TransferID:  ftState.TransferID,
					RelPath:     entry.relPath,
					IsDir:       false,
					ChunkIndex:  chunkIdx,
					DataBase64:  encoded,
					IsLastChunk: isLast,
				})
				m.sendSessionSignal(ftState.SessionUUID, server.SignalMessage{
					Type:        "file_transfer_chunk",
					SessionUUID: ftState.SessionUUID,
					FromDeskID:  fromDeskID,
					FromAlias:   fromAlias,
					ToDeskID:    peerID,
					Payload:     string(chunkPayload),
					Timestamp:   time.Now().Format("15:04:05"),
				})

				m.mu.Lock()
				ftState.TransferredBytes += int64(n)
				if ftState.TransferredBytes > ftState.TotalBytes && ftState.TotalBytes > 0 {
					ftState.TransferredBytes = ftState.TotalBytes
				}
				ftState.UpdatedAt = time.Now()
				m.mu.Unlock()
				m.notify()
				chunkIdx++
			}

			if isEOF || rErr != nil {
				break
			}
		}
		_ = f.Close()

		m.mu.Lock()
		ftState.CompletedFiles++
		ftState.UpdatedAt = time.Now()
		m.mu.Unlock()
		m.notify()
	}

	m.mu.Lock()
	if ftState.Status != "canceled" && ftState.Status != "error" {
		ftState.Status = "pasting"
		ftState.TransferredBytes = ftState.TotalBytes
		ftState.UpdatedAt = time.Now()
	}
	m.mu.Unlock()
	m.notify()

	endPayload, _ := json.Marshal(fileTransferProgressPayload{
		TransferID:       ftState.TransferID,
		TransferredBytes: ftState.TotalBytes,
		CompletedFiles:   ftState.TotalFiles,
		Status:           "completed",
	})
	m.sendSessionSignal(ftState.SessionUUID, server.SignalMessage{
		Type:        "file_transfer_end",
		SessionUUID: ftState.SessionUUID,
		FromDeskID:  fromDeskID,
		FromAlias:   fromAlias,
		ToDeskID:    peerID,
		Payload:     string(endPayload),
		Timestamp:   time.Now().Format("15:04:05"),
	})
}

// CancelFileTransfer aborts an active file or folder transfer.
func (m *Manager) CancelFileTransfer(transferID string) {
	st, _ := m.Store.GetSettings()
	m.mu.Lock()
	ft, ok := m.fileTransfers[transferID]
	if !ok || ft == nil {
		m.mu.Unlock()
		return
	}
	ft.Status = "canceled"
	ft.UpdatedAt = time.Now()
	sessUUID := ft.SessionUUID
	peerID := ft.PeerDeskID
	m.mu.Unlock()

	cancelPayload, _ := json.Marshal(fileTransferProgressPayload{
		TransferID: transferID,
		Status:     "canceled",
	})
	m.sendSessionSignal(sessUUID, server.SignalMessage{
		Type:        "file_transfer_cancel",
		SessionUUID: sessUUID,
		FromDeskID:  st.DeskID,
		FromAlias:   st.Alias,
		ToDeskID:    peerID,
		Payload:     string(cancelPayload),
		Timestamp:   time.Now().Format("15:04:05"),
	})
	m.notify()
}

// FileTransfersSnapshot returns a thread-safe slice of active/recent file transfers.
func (m *Manager) FileTransfersSnapshot() []FileTransferState {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]FileTransferState, 0, len(m.fileTransfers))
	for _, ft := range m.fileTransfers {
		if ft != nil {
			out = append(out, *ft)
		}
	}
	return out
}

func (m *Manager) handleFileTransferSignal(msg server.SignalMessage) {
	st, _ := m.Store.GetSettings()

	switch msg.Type {
	case "file_transfer_start":
		var start fileTransferStartPayload
		if err := json.Unmarshal([]byte(msg.Payload), &start); err != nil || start.TransferID == "" {
			return
		}
		targetDir := DetectActiveTargetDirectory(m.customTargetDir)

		m.mu.Lock()
		if m.fileTransfers == nil {
			m.fileTransfers = make(map[string]*FileTransferState)
		}
		peerAlias := msg.FromAlias
		if peerAlias == "" {
			peerAlias = msg.FromDeskID
		}
		ft := &FileTransferState{
			TransferID:  start.TransferID,
			SessionUUID: msg.SessionUUID,
			Direction:   "receiving",
			PeerDeskID:  msg.FromDeskID,
			PeerAlias:   peerAlias,
			RootName:    start.RootName,
			IsFolder:    start.IsFolder,
			TotalFiles:  start.TotalFiles,
			TotalBytes:  start.TotalBytes,
			TargetDir:   targetDir,
			Status:      "transferring",
			StartedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}
		m.fileTransfers[start.TransferID] = ft
		m.mu.Unlock()
		m.notify()

		// Inform sender of resolved TargetDir on Host B
		progPayload, _ := json.Marshal(fileTransferProgressPayload{
			TransferID: start.TransferID,
			TargetDir:  targetDir,
			Status:     "transferring",
		})
		m.sendSessionSignal(msg.SessionUUID, server.SignalMessage{
			Type:        "file_transfer_progress",
			SessionUUID: msg.SessionUUID,
			FromDeskID:  st.DeskID,
			FromAlias:   st.Alias,
			ToDeskID:    msg.FromDeskID,
			Payload:     string(progPayload),
			Timestamp:   time.Now().Format("15:04:05"),
		})

	case "file_transfer_chunk":
		var chunk fileTransferChunkPayload
		if err := json.Unmarshal([]byte(msg.Payload), &chunk); err != nil || chunk.TransferID == "" {
			return
		}

		m.mu.Lock()
		ft := m.fileTransfers[chunk.TransferID]
		if ft == nil || ft.Status == "canceled" {
			m.mu.Unlock()
			return
		}
		targetDir := ft.TargetDir
		m.mu.Unlock()

		if targetDir == "" {
			targetDir = DetectActiveTargetDirectory(m.customTargetDir)
		}

		// Sanitize relative path to prevent path traversal
		cleanRel := filepath.Clean(filepath.FromSlash(chunk.RelPath))
		if cleanRel == "." || cleanRel == ".." || strings.HasPrefix(cleanRel, ".."+string(filepath.Separator)) || filepath.IsAbs(cleanRel) {
			return
		}
		cleanBase := filepath.Clean(targetDir)
		destPath := filepath.Join(cleanBase, cleanRel)
		if !strings.HasPrefix(destPath, cleanBase+string(filepath.Separator)) {
			return
		}

		if chunk.IsDir {
			// #nosec G301 -- standard user directory permission for pasted folders
			_ = os.MkdirAll(destPath, 0o750)
			return
		}

		// #nosec G301 -- standard user directory permission for parent folder
		_ = os.MkdirAll(filepath.Dir(destPath), 0o750)

		var rawData []byte
		if chunk.DataBase64 != "" {
			var dErr error
			rawData, dErr = base64.StdEncoding.DecodeString(chunk.DataBase64)
			if dErr != nil {
				return
			}
		}

		flags := os.O_CREATE | os.O_WRONLY
		if chunk.ChunkIndex == 0 {
			flags |= os.O_TRUNC
		} else {
			flags |= os.O_APPEND
		}
		// #nosec G304 G302 -- writing user-pasted file inside verified target directory
		if f, oErr := os.OpenFile(destPath, flags, 0o600); oErr == nil {
			if len(rawData) > 0 {
				_, _ = f.Write(rawData)
			}
			_ = f.Close()
		}

		m.mu.Lock()
		ft.CurrentFile = chunk.RelPath
		ft.TransferredBytes += int64(len(rawData))
		if chunk.IsLastChunk {
			ft.CompletedFiles++
		}
		ft.UpdatedAt = time.Now()
		m.mu.Unlock()
		m.notify()

	case "file_transfer_end":
		var end fileTransferProgressPayload
		if err := json.Unmarshal([]byte(msg.Payload), &end); err != nil || end.TransferID == "" {
			return
		}

		m.mu.Lock()
		ft := m.fileTransfers[end.TransferID]
		var targetDir, rootName string
		if ft != nil {
			ft.Status = "completed"
			ft.TransferredBytes = ft.TotalBytes
			ft.CompletedFiles = ft.TotalFiles
			ft.UpdatedAt = time.Now()
			targetDir = ft.TargetDir
			rootName = ft.RootName
			for _, s := range m.sessions {
				if s.SessionUUID == msg.SessionUUID || s.PeerDeskID == msg.FromDeskID {
					desc := fmt.Sprintf("[%s] Pasted %s to %s (%d bytes)", msg.Timestamp, ft.RootName, ft.TargetDir, ft.TotalBytes)
					s.FileTransferLog = append(s.FileTransferLog, desc)
					s.ChatLog = append(s.ChatLog, desc)
				}
			}
		}
		m.mu.Unlock()

		// Also populate Host B's native OS File Clipboard with the pasted root path
		if targetDir != "" && rootName != "" {
			rootDest := filepath.Join(filepath.Clean(targetDir), filepath.Base(rootName))
			if _, serr := os.Stat(rootDest); serr == nil && mygo.App.IsReady() {
				_ = mygo.Clipboard.WriteFiles(rootDest)
			}
		}

		// Notify Client A that paste is 100% completed on Host B
		ackPayload, _ := json.Marshal(fileTransferProgressPayload{
			TransferID:       end.TransferID,
			TransferredBytes: end.TransferredBytes,
			CompletedFiles:   end.CompletedFiles,
			TargetDir:        targetDir,
			Status:           "completed",
		})
		m.sendSessionSignal(msg.SessionUUID, server.SignalMessage{
			Type:        "file_transfer_progress",
			SessionUUID: msg.SessionUUID,
			FromDeskID:  st.DeskID,
			FromAlias:   st.Alias,
			ToDeskID:    msg.FromDeskID,
			Payload:     string(ackPayload),
			Timestamp:   time.Now().Format("15:04:05"),
		})
		m.notify()

	case "file_transfer_progress":
		var prog fileTransferProgressPayload
		if err := json.Unmarshal([]byte(msg.Payload), &prog); err != nil || prog.TransferID == "" {
			return
		}
		m.mu.Lock()
		if ft, ok := m.fileTransfers[prog.TransferID]; ok && ft != nil {
			if prog.TargetDir != "" {
				ft.TargetDir = prog.TargetDir
			}
			if prog.Status == "completed" {
				ft.Status = "completed"
				ft.TransferredBytes = ft.TotalBytes
				ft.CompletedFiles = ft.TotalFiles
			} else if prog.Status != "" {
				ft.Status = prog.Status
			}
			if prog.ErrorMessage != "" {
				ft.ErrorMessage = prog.ErrorMessage
			}
			ft.UpdatedAt = time.Now()
		}
		m.mu.Unlock()
		m.notify()

	case "file_transfer_cancel":
		var cancel fileTransferProgressPayload
		if err := json.Unmarshal([]byte(msg.Payload), &cancel); err != nil || cancel.TransferID == "" {
			return
		}
		m.mu.Lock()
		if ft, ok := m.fileTransfers[cancel.TransferID]; ok && ft != nil {
			ft.Status = "canceled"
			ft.UpdatedAt = time.Now()
		}
		m.mu.Unlock()
		m.notify()
	}
}
