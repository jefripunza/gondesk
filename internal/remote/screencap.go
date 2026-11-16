package remote

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// DesktopInfo holds telemetry information rendered into synthetic desktop frames.
type DesktopInfo struct {
	HostDeskID       string
	HostAlias        string
	ParticipantCount int
	FrameSeq         uint64
	Timestamp        time.Time
}

// ScreenCaptureEngine coordinates OS-native screen captures with synthetic fallback.
type ScreenCaptureEngine struct {
	mu                 sync.Mutex
	customCap          func() ([]byte, error)
	tmpFile            string
	frameCounter       uint64
	lastErrLog         time.Time
	linuxAutoInstalled bool
	needsWindowsPrompt bool
	windowsDialogShown bool
	streamCmdWriter    io.WriteCloser
}

// NewScreenCaptureEngine initializes the capture engine.
func NewScreenCaptureEngine() *ScreenCaptureEngine {
	tmpDir := os.TempDir()
	pid := os.Getpid()
	tmpFile := filepath.Join(tmpDir, fmt.Sprintf("gondesk_cap_%d.jpg", pid))
	return &ScreenCaptureEngine{
		tmpFile: tmpFile,
	}
}

// SetCustomCaptureFunc allows injecting a custom screen capture provider.
func (e *ScreenCaptureEngine) SetCustomCaptureFunc(fn func() ([]byte, error)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.customCap = fn
}

// NeedsWindowsDialog reports whether Windows needs a user confirmation popup to install/enable screen capture.
func (e *ScreenCaptureEngine) NeedsWindowsDialog() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.needsWindowsPrompt && !e.windowsDialogShown
}

// SetWindowsDialogShown marks the Windows dialog as shown so it doesn't repeatedly popup.
func (e *ScreenCaptureEngine) SetWindowsDialogShown() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.windowsDialogShown = true
}

// SendLinuxStreamCommand delivers an OS command (e.g. mouse move/button) to the Linux compositor pipe if active.
func (e *ScreenCaptureEngine) SendLinuxStreamCommand(cmd string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.streamCmdWriter != nil {
		_, _ = io.WriteString(e.streamCmdWriter, cmd)
	}
}

// CaptureFrame captures a single desktop frame across any Linux, macOS, or Windows environment,
// downscales to standard remote resolution, and returns compressed JPEG bytes.
func (e *ScreenCaptureEngine) CaptureFrame(info DesktopInfo) ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.frameCounter++
	info.FrameSeq = e.frameCounter
	if info.Timestamp.IsZero() {
		info.Timestamp = time.Now()
	}

	if e.customCap != nil {
		data, err := e.customCap()
		if err == nil && len(data) > 0 {
			return data, nil
		}
	}

	// 1. Attempt native OS screen capture
	nativeBytes, err := e.captureNative()
	if err == nil && len(nativeBytes) > 0 {
		return nativeBytes, nil
	}

	// Log throttled capture warning (once every 10 seconds)
	if time.Since(e.lastErrLog) > 10*time.Second {
		e.lastErrLog = time.Now()
		log.Printf("[ScreenCap] Native desktop capture not available (%v), using telemetry display stream", err)
	}

	// 2. Fallback to responsive telemetry desktop frame
	return GenerateSyntheticDesktopFrame(1280, 720, info), nil
}

func (e *ScreenCaptureEngine) captureNative() ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3000*time.Millisecond)
	defer cancel()

	targetFile := e.tmpFile
	_ = os.Remove(targetFile)

	switch runtime.GOOS {
	case "darwin":
		// macOS native screencapture utility
		// #nosec G204 -- safe internal desktop screen capture utility invocation
		cmd := exec.CommandContext(ctx, "/usr/sbin/screencapture", "-x", "-t", "jpg", "-T", "0", targetFile)
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("macOS screencapture: %w", err)
		}

	case "windows":
		psScript := fmt.Sprintf(`Add-Type -AssemblyName System.Windows.Forms,System.Drawing; $b = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds; $bmp = New-Object System.Drawing.Bitmap $b.Width, $b.Height; $g = [System.Drawing.Graphics]::FromImage($bmp); $g.CopyFromScreen($b.Location, [System.Drawing.Point]::Empty, $b.Size); $bmp.Save('%s', [System.Drawing.Imaging.ImageFormat]::Jpeg); $g.Dispose(); $bmp.Dispose()`, targetFile)
		// #nosec G204 -- safe internal powershell screen capture invocation
		cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", psScript)
		if err := cmd.Run(); err != nil {
			e.needsWindowsPrompt = true
			return nil, fmt.Errorf("windows screen capture: %w", err)
		}

	case "linux":
		isWayland := strings.EqualFold(os.Getenv("XDG_SESSION_TYPE"), "wayland") || os.Getenv("WAYLAND_DISPLAY") != ""

		// On native X11 sessions, try pure-Go X11 socket capture first
		if !isWayland {
			if x11Bytes, err := captureX11Screen(); err == nil && len(x11Bytes) > 0 {
				return x11Bytes, nil
			}
		}

		// Try all available desktop environment tools (GNOME Wayland/X11, KDE, Sway/Hyprland, XFCE, MATE, Cinnamon, X11)
		cliBytes, err := e.captureLinuxCLI(ctx, targetFile)
		if err == nil && len(cliBytes) > 0 {
			return cliBytes, nil
		}

		// Fallback to X11 socket if Wayland CLI tools failed (e.g. X11 desktop with WAYLAND_DISPLAY leftover)
		if isWayland {
			if x11Bytes, xerr := captureX11Screen(); xerr == nil && len(x11Bytes) > 0 {
				return x11Bytes, nil
			}
		}

		e.triggerLinuxAutoInstall()
		return nil, err

	default:
		return nil, fmt.Errorf("unsupported os for direct screencap: %s", runtime.GOOS)
	}

	// #nosec G304 -- reading internal temporary screen capture file
	rawBytes, err := os.ReadFile(targetFile)
	if err != nil || len(rawBytes) == 0 {
		return nil, fmt.Errorf("read captured screen file: %w", err)
	}

	return processRawCapturedImage(rawBytes)
}

// captureLinuxCLI attempts capture across silent Wayland & X11 tools without flash animations or camera shutter sounds.
func (e *ScreenCaptureEngine) captureLinuxCLI(ctx context.Context, targetFile string) ([]byte, error) {
	candidates := []struct {
		bin  string
		args []string
	}{
		// 1. GStreamer with PipeWire (Used by AnyDesk & RustDesk on GNOME/KDE Wayland & X11, completely silent without flash or shutter sound)
		{"gst-launch-1.0", []string{"pipewiresrc", "num-buffers=1", "!", "videoconvert", "!", "jpegenc", "quality=60", "!", "filesink", "location=" + targetFile}},
		// 2. Wayland wlroots (Sway, Hyprland, Wayfire, River - completely silent)
		{"grim", []string{targetFile}},
		// 3. Fast modern X11 (maim is silent)
		{"maim", []string{"-f", "png", targetFile}},
		// 4. Standard X11 (scrot with -z = silent flag, no beep or shutter sound)
		{"scrot", []string{"-o", "-z", "-q", "60", targetFile}},
		// 5. GStreamer ximagesrc (silent X11)
		{"gst-launch-1.0", []string{"ximagesrc", "num-buffers=1", "!", "videoconvert", "!", "jpegenc", "quality=60", "!", "filesink", "location=" + targetFile}},
		// 6. Standard X11 xwd with -silent flag
		{"xwd", []string{"-root", "-silent", "-out", targetFile}},
		// 7. FFmpeg x11grab (silent)
		{"ffmpeg", []string{"-nostats", "-loglevel", "0", "-f", "x11grab", "-i", os.Getenv("DISPLAY"), "-vframes", "1", "-q:v", "3", "-y", targetFile}},
		// 8. ImageMagick import
		{"import", []string{"-window", "root", "-quality", "60", targetFile}},
	}

	for _, cand := range candidates {
		if path, err := exec.LookPath(cand.bin); err == nil && path != "" {
			_ = os.Remove(targetFile)
			// #nosec G204 -- safe internal desktop screen capture utility invocation
			cmd := exec.CommandContext(ctx, cand.bin, cand.args...)
			if err := cmd.Run(); err == nil {
				// #nosec G304 -- reading internal temporary screen capture file
				if raw, rerr := os.ReadFile(targetFile); rerr == nil && len(raw) > 0 {
					if processed, perr := processRawCapturedImage(raw); perr == nil && len(processed) > 0 {
						return processed, nil
					}
				}
			}
		}
	}

	return nil, errors.New("no supported linux screen capture tool succeeded with non-blank frame")
}

// triggerLinuxAutoInstall attempts to install pipewire/gstreamer, scrot, grim, and maim in the background via pkexec or package manager.
func (e *ScreenCaptureEngine) triggerLinuxAutoInstall() {
	if e.linuxAutoInstalled {
		return
	}
	e.linuxAutoInstalled = true

	go func() {
		pkgManagers := []struct {
			bin  string
			args []string
		}{
			{"apt-get", []string{"install", "-y", "gstreamer1.0-pipewire", "gstreamer1.0-tools", "scrot", "grim", "maim"}},
			{"dnf", []string{"install", "-y", "pipewire-gstreamer", "gstreamer1-plugins-base-tools", "scrot", "grim", "maim"}},
			{"pacman", []string{"-S", "--noconfirm", "gst-plugin-pipewire", "scrot", "grim", "maim"}},
			{"zypper", []string{"--non-interactive", "install", "gstreamer-plugin-pipewire", "scrot", "grim", "maim"}},
			{"apk", []string{"add", "scrot", "grim"}},
		}

		hasPkexec := false
		if p, err := exec.LookPath("pkexec"); err == nil && p != "" {
			hasPkexec = true
		}

		for _, pm := range pkgManagers {
			if p, err := exec.LookPath(pm.bin); err == nil && p != "" {
				var cmd *exec.Cmd
				if hasPkexec {
					if pm.bin == "apt-get" {
						// Single pkexec invocation that updates and installs silent screen capture tools
						// #nosec G204 -- safe internal package installation invocation
						cmd = exec.Command("pkexec", "sh", "-c", "apt-get update && apt-get install -y gstreamer1.0-pipewire gstreamer1.0-tools scrot grim maim || apt-get install -y scrot maim")
					} else {
						args := append([]string{pm.bin}, pm.args...)
						// #nosec G204 -- safe internal package installation invocation
						cmd = exec.Command("pkexec", args...)
					}
				} else {
					// #nosec G204 -- safe internal package installation invocation
					cmd = exec.Command(pm.bin, pm.args...)
				}
				_ = cmd.Run()
				return
			}
		}
	}()
}

// resolveX11DisplayNum extracts the X11 display number (e.g. "0" from ":0" or ":0.0").
func resolveX11DisplayNum() string {
	displayStr := os.Getenv("DISPLAY")
	if displayStr == "" {
		displayStr = ":0"
	}

	dispNum := "0"
	if idx := strings.Index(displayStr, ":"); idx != -1 {
		part := displayStr[idx+1:]
		if dot := strings.Index(part, "."); dot != -1 {
			part = part[:dot]
		}
		allDigits := true
		for _, r := range part {
			if r < '0' || r > '9' {
				allDigits = false
				break
			}
		}
		if allDigits && part != "" {
			dispNum = part
		}
	}
	return dispNum
}

// captureX11Screen performs pure-Go X11 socket communication on /tmp/.X11-unix/X0.
func captureX11Screen() ([]byte, error) {
	dispNum := resolveX11DisplayNum()
	socketPath := "/tmp/.X11-unix/X" + dispNum
	// #nosec G704 -- safe internal local unix domain socket for X11 screen capture
	conn, err := net.DialTimeout("unix", socketPath, 350*time.Millisecond)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(1500 * time.Millisecond))

	authName, authCookie := readXAuthorityCookie(dispNum)
	return captureX11FromConnWithAuth(conn, authName, authCookie)
}

// readXAuthorityCookie extracts MIT-MAGIC-COOKIE-1 from $XAUTHORITY or ~/.Xauthority for pure-Go X11 auth.
func readXAuthorityCookie(dispNum string) (string, []byte) {
	candidates := []string{}
	if envAuth := os.Getenv("XAUTHORITY"); envAuth != "" {
		candidates = append(candidates, envAuth)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidates = append(candidates, filepath.Join(home, ".Xauthority"))
	}
	uid := os.Getuid()
	candidates = append(candidates,
		fmt.Sprintf("/run/user/%d/gdm/Xauthority", uid),
		fmt.Sprintf("/run/user/%d/.mutter-Xwaylandauth", uid),
	)

	// Also check dynamic Mutter Xwayland auth files (e.g. /run/user/<uid>/.mutter-Xwaylandauth*)
	if matches, err := filepath.Glob(fmt.Sprintf("/run/user/%d/.mutter-Xwaylandauth*", uid)); err == nil {
		candidates = append(candidates, matches...)
	}
	if matches, err := filepath.Glob(fmt.Sprintf("/run/user/%d/*Xauthority*", uid)); err == nil {
		candidates = append(candidates, matches...)
	}

	for _, p := range candidates {
		// #nosec G304 G703 -- reading local Xauthority file for X11 authentication
		data, err := os.ReadFile(p)
		if err != nil || len(data) < 12 {
			continue
		}
		if name, cookie := parseXAuthorityBytes(data, dispNum); len(cookie) > 0 {
			return name, cookie
		}
	}
	return "", nil
}

func parseXAuthorityBytes(data []byte, dispNum string) (string, []byte) {
	offset := 0
	var fallbackName string
	var fallbackCookie []byte

	readChunk := func() ([]byte, bool) {
		if offset+2 > len(data) {
			return nil, false
		}
		length := int(binary.BigEndian.Uint16(data[offset : offset+2]))
		offset += 2
		if offset+length > len(data) {
			return nil, false
		}
		chunk := data[offset : offset+length]
		offset += length
		return chunk, true
	}

	for offset+2 <= len(data) {
		offset += 2 // skip family uint16
		if _, ok := readChunk(); !ok {
			break
		}
		numBytes, ok := readChunk()
		if !ok {
			break
		}
		nameBytes, ok := readChunk()
		if !ok {
			break
		}
		cookieBytes, ok := readChunk()
		if !ok {
			break
		}
		nameStr := string(nameBytes)
		if nameStr == "MIT-MAGIC-COOKIE-1" && len(cookieBytes) > 0 {
			if string(numBytes) == dispNum || len(numBytes) == 0 {
				return nameStr, cookieBytes
			}
			if len(fallbackCookie) == 0 {
				fallbackName = nameStr
				fallbackCookie = cookieBytes
			}
		}
	}
	return fallbackName, fallbackCookie
}

func captureX11FromConn(conn net.Conn) ([]byte, error) {
	return captureX11FromConnWithAuth(conn, "", nil)
}

func captureX11FromConnWithAuth(conn net.Conn, authName string, authCookie []byte) ([]byte, error) {
	// X11 Connection Setup (little-endian 'l')
	pad4 := func(n int) int { return (n + 3) &^ 3 }
	nameLen := len(authName)
	dataLen := len(authCookie)
	totalSetupLen := 12 + pad4(nameLen) + pad4(dataLen)

	setupReq := make([]byte, totalSetupLen)
	setupReq[0] = 'l'
	binary.LittleEndian.PutUint16(setupReq[2:4], 11) // major 11
	binary.LittleEndian.PutUint16(setupReq[4:6], 0)  // minor 0
	// #nosec G115 -- X11 auth name length is bounded
	binary.LittleEndian.PutUint16(setupReq[6:8], uint16(nameLen))
	// #nosec G115 -- X11 auth cookie length is bounded
	binary.LittleEndian.PutUint16(setupReq[8:10], uint16(dataLen))
	if nameLen > 0 {
		copy(setupReq[12:12+nameLen], authName)
	}
	if dataLen > 0 {
		offset := 12 + pad4(nameLen)
		copy(setupReq[offset:offset+dataLen], authCookie)
	}

	if _, err := conn.Write(setupReq); err != nil {
		return nil, err
	}

	setupHeader := make([]byte, 8)
	if _, err := io.ReadFull(conn, setupHeader); err != nil {
		return nil, err
	}
	if setupHeader[0] != 1 {
		return nil, fmt.Errorf("X11 connection rejected: status %d", setupHeader[0])
	}

	addlLen := int(binary.LittleEndian.Uint16(setupHeader[6:8])) * 4
	rest := make([]byte, addlLen)
	if _, err := io.ReadFull(conn, rest); err != nil {
		return nil, err
	}

	// Extract root window and dimensions from setup block
	vendorLen := int(binary.LittleEndian.Uint16(rest[16:18]))
	numFormats := int(rest[21])
	padVendor := (vendorLen + 3) &^ 3
	screenOffset := 32 + padVendor + numFormats*8

	if len(rest) < screenOffset+24 {
		return nil, errors.New("X11 setup block truncated")
	}

	rootWinID := binary.LittleEndian.Uint32(rest[screenOffset : screenOffset+4])
	scrW := int(binary.LittleEndian.Uint16(rest[screenOffset+20 : screenOffset+22]))
	scrH := int(binary.LittleEndian.Uint16(rest[screenOffset+22 : screenOffset+24]))

	if scrW <= 0 || scrH <= 0 || scrW > 7680 || scrH > 4320 {
		return nil, fmt.Errorf("invalid X11 screen dimensions: %dx%d", scrW, scrH)
	}

	// Send GetImage request (opcode 73, format 2 = ZPixmap)
	// Request is 20 bytes (5 units of 4 bytes)
	getImgReq := make([]byte, 20)
	getImgReq[0] = 73                                          // GetImage opcode
	getImgReq[1] = 2                                           // ZPixmap
	binary.LittleEndian.PutUint16(getImgReq[2:4], 5)           // Length in 4-byte units
	binary.LittleEndian.PutUint32(getImgReq[4:8], rootWinID)   // Drawable (Root window)
	binary.LittleEndian.PutUint16(getImgReq[8:10], 0)          // X
	binary.LittleEndian.PutUint16(getImgReq[10:12], 0)         // Y
	binary.LittleEndian.PutUint16(getImgReq[12:14], uint16(scrW))
	binary.LittleEndian.PutUint16(getImgReq[14:16], uint16(scrH))
	binary.LittleEndian.PutUint32(getImgReq[16:20], 0xFFFFFFFF) // Plane mask

	if _, err := conn.Write(getImgReq); err != nil {
		return nil, err
	}

	// Read GetImage reply (32 bytes header)
	replyHeader := make([]byte, 32)
	if _, err := io.ReadFull(conn, replyHeader); err != nil {
		return nil, err
	}
	if replyHeader[0] != 1 {
		return nil, fmt.Errorf("X11 GetImage error: status %d", replyHeader[0])
	}

	imgDataLen := int(binary.LittleEndian.Uint32(replyHeader[4:8])) * 4
	pixelBytes := make([]byte, imgDataLen)
	if _, err := io.ReadFull(conn, pixelBytes); err != nil {
		return nil, err
	}

	expectedBytes := scrW * scrH * 4
	if len(pixelBytes) < expectedBytes {
		return nil, errors.New("X11 pixel buffer incomplete")
	}

	// Convert BGRA pixel buffer to RGBA
	img := image.NewRGBA(image.Rect(0, 0, scrW, scrH))
	for i := 0; i < expectedBytes; i += 4 {
		img.Pix[i] = pixelBytes[i+2]   // R
		img.Pix[i+1] = pixelBytes[i+1] // G
		img.Pix[i+2] = pixelBytes[i]   // B
		img.Pix[i+3] = 255             // A
	}

	// Reject blank/all-black XWayland root windows so Wayland capture tools are used instead
	if IsBlankOrBlackImage(img) {
		return nil, errors.New("X11 captured frame is blank/all-black (XWayland root window)")
	}

	// Downscale if wider than 1280px
	targetW := scrW
	targetH := scrH
	maxW := 1280
	if targetW > maxW {
		targetH = int(float64(scrH) * (float64(maxW) / float64(scrW)))
		targetW = maxW
	}

	var dst *image.RGBA
	if targetW != scrW {
		dst = image.NewRGBA(image.Rect(0, 0, targetW, targetH))
		xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, img.Bounds(), xdraw.Src, nil)
	} else {
		dst = img
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 65}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// IsBlankOrBlackImage checks whether an image is >=99.5% solid black (e.g., XWayland uncomposited root window).
func IsBlankOrBlackImage(img image.Image) bool {
	if img == nil {
		return true
	}
	b := img.Bounds()
	w := b.Dx()
	h := b.Dy()
	if w <= 0 || h <= 0 {
		return true
	}

	stepX := max(w/40, 1)
	stepY := max(h/40, 1)

	totalSamples := 0
	nonBlackSamples := 0

	for y := b.Min.Y; y < b.Max.Y; y += stepY {
		for x := b.Min.X; x < b.Max.X; x += stepX {
			totalSamples++
			r, g, bl, _ := img.At(x, y).RGBA()
			if (r>>8) > 8 || (g>>8) > 8 || (bl>>8) > 8 {
				nonBlackSamples++
			}
		}
	}

	if totalSamples == 0 {
		return true
	}
	return float64(nonBlackSamples)/float64(totalSamples) < 0.005
}

func processRawCapturedImage(rawBytes []byte) ([]byte, error) {
	// Attempt decoding to verify non-blank content and optionally downscale
	img, format, err := image.Decode(bytes.NewReader(rawBytes))
	if err != nil {
		return nil, err
	}

	if IsBlankOrBlackImage(img) {
		return nil, errors.New("captured frame is blank/all-black (likely XWayland root window)")
	}

	bounds := img.Bounds()
	origW := bounds.Dx()
	origH := bounds.Dy()

	targetW := origW
	targetH := origH
	maxW := 1280
	if targetW > maxW {
		targetH = int(float64(origH) * (float64(maxW) / float64(origW)))
		targetW = maxW
	}

	if format == "jpeg" && targetW == origW {
		return rawBytes, nil
	}

	dst := image.NewRGBA(image.Rect(0, 0, targetW, targetH))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, bounds, xdraw.Src, nil)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 65}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// GenerateSyntheticDesktopFrame draws a responsive virtual desktop display with telemetry.
func GenerateSyntheticDesktopFrame(w, h int, info DesktopInfo) []byte {
	if w <= 0 {
		w = 1280
	}
	if h <= 0 {
		h = 720
	}

	img := image.NewRGBA(image.Rect(0, 0, w, h))

	// Background: dark slate desktop workspace (#0F172A)
	bgColor := color.RGBA{R: 15, G: 23, B: 42, A: 255}
	gridColor := color.RGBA{R: 30, G: 41, B: 59, A: 255}
	for y := range h {
		for x := range w {
			if y%48 == 0 || x%48 == 0 {
				img.SetRGBA(x, y, gridColor)
			} else {
				img.SetRGBA(x, y, bgColor)
			}
		}
	}

	// Top taskbar bar (height 32, #1E293B)
	topBarColor := color.RGBA{R: 30, G: 41, B: 59, A: 255}
	for y := range 32 {
		for x := range w {
			img.SetRGBA(x, y, topBarColor)
		}
	}

	// Bottom taskbar bar (height 40, #1E293B)
	for y := h - 40; y < h; y++ {
		for x := range w {
			img.SetRGBA(x, y, topBarColor)
		}
	}

	// Desktop Central App Window card
	cardX := 80
	cardY := 60
	cardW := w - 160
	cardH := h - 130
	if cardW > 0 && cardH > 0 {
		cardHeader := color.RGBA{R: 51, G: 65, B: 85, A: 255}
		cardBody := color.RGBA{R: 2, G: 6, B: 23, A: 255}
		cardBorder := color.RGBA{R: 71, G: 85, B: 105, A: 255}

		for cy := range cardH {
			for cx := range cardW {
				px := cardX + cx
				py := cardY + cy
				switch {
				case cy == 0 || cy == cardH-1 || cx == 0 || cx == cardW-1:
					img.SetRGBA(px, py, cardBorder)
				case cy < 36:
					img.SetRGBA(px, py, cardHeader)
				default:
					img.SetRGBA(px, py, cardBody)
				}
			}
		}
	}

	// Text rendering via basicfont.Face7x13
	white := &image.Uniform{C: color.RGBA{R: 248, G: 250, B: 252, A: 255}}
	muted := &image.Uniform{C: color.RGBA{R: 148, G: 163, B: 184, A: 255}}
	coral := &image.Uniform{C: color.RGBA{R: 239, G: 68, B: 59, A: 255}}
	green := &image.Uniform{C: color.RGBA{R: 34, G: 197, B: 94, A: 255}}
	yellow := &image.Uniform{C: color.RGBA{R: 234, G: 179, B: 8, A: 255}}

	// Top bar texts
	drawText(img, 16, 20, "GonDesk Host Desktop Stream", coral)
	hostLabel := fmt.Sprintf("Host: %s (%s)", info.HostAlias, info.HostDeskID)
	drawText(img, 240, 20, hostLabel, white)
	timeLabel := info.Timestamp.Format("15:04:05 MST")
	drawText(img, w-140, 20, timeLabel, green)

	// Window header and content
	drawText(img, cardX+20, cardY+23, "GonDesk Remote Display - Live Desktop Session", white)
	drawText(img, cardX+30, cardY+65, fmt.Sprintf("Connected to Host OS: %s/%s", runtime.GOOS, runtime.GOARCH), muted)
	drawText(img, cardX+30, cardY+88, fmt.Sprintf("Active Remote Participants: %d", info.ParticipantCount), green)
	drawText(img, cardX+30, cardY+111, fmt.Sprintf("Stream Frame: #%d | Resolution: %dx%d (60 FPS Ready)", info.FrameSeq, w, h), muted)
	drawText(img, cardX+30, cardY+134, "Single-Port HTTPS + TURNS Active | Encrypted P2P Pipeline", white)

	// Helpful diagnostic notice on host setup
	drawText(img, cardX+30, cardY+165, "Notice: Native desktop screen capture fallback active.", yellow)
	drawText(img, cardX+30, cardY+185, "If screen is dark, install on host: sudo apt install gnome-screenshot scrot", muted)

	// Bottom taskbar texts
	drawText(img, 20, h-16, "Start", coral)
	drawText(img, 80, h-16, "Terminal", muted)
	drawText(img, 160, h-16, "VSCode", muted)
	drawText(img, w-180, h-16, "Network: 1.2 Gbps", green)

	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 65})
	return buf.Bytes()
}

func drawText(dst *image.RGBA, x, y int, text string, src *image.Uniform) {
	d := font.Drawer{
		Dst:  dst,
		Src:  src,
		Face: basicfont.Face7x13,
		Dot:  fixed.P(x, y),
	}
	d.DrawString(text)
}

func init() {
	// Register image decoders
	image.RegisterFormat("png", "\x89PNG\r\n\x1a\n", png.Decode, png.DecodeConfig)
	image.RegisterFormat("jpeg", "\xff\xd8", jpeg.Decode, jpeg.DecodeConfig)
}
