package remote

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"runtime"
	"strconv"
	"sync"
	"time"
)

// InputControlEngine handles real-time OS cursor movement and mouse button injection on Host B.
type InputControlEngine struct {
	mu           sync.Mutex
	screenEngine *ScreenCaptureEngine
	lastMoveAt   time.Time
	customMoveFn func(normX, normY float32)
	customBtnFn  func(button int, down bool)
	winStdin     io.WriteCloser
}

// NewInputControlEngine creates a new OS mouse/input controller on the host.
func NewInputControlEngine(screenEngine *ScreenCaptureEngine) *InputControlEngine {
	return &InputControlEngine{
		screenEngine: screenEngine,
	}
}

// SetCustomHandlers sets test callbacks for verifying mouse move and click injection in unit tests.
func (ic *InputControlEngine) SetCustomHandlers(moveFn func(normX, normY float32), btnFn func(button int, down bool)) {
	ic.mu.Lock()
	defer ic.mu.Unlock()
	ic.customMoveFn = moveFn
	ic.customBtnFn = btnFn
}

// MoveMouse moves the host's native OS mouse cursor to normalized coordinates (0.0..1.0).
func (ic *InputControlEngine) MoveMouse(normX, normY float32) {
	if normX < 0 {
		normX = 0
	} else if normX > 1 {
		normX = 1
	}
	if normY < 0 {
		normY = 0
	} else if normY > 1 {
		normY = 1
	}

	ic.mu.Lock()
	if ic.customMoveFn != nil {
		fn := ic.customMoveFn
		ic.mu.Unlock()
		fn(normX, normY)
		return
	}
	// Throttle OS cursor injection to at most ~60 Hz (16ms)
	if time.Since(ic.lastMoveAt) < 15*time.Millisecond {
		ic.mu.Unlock()
		return
	}
	ic.lastMoveAt = time.Now()
	ic.mu.Unlock()

	go ic.dispatchOSMove(normX, normY)
}

// MouseButton injects a native OS mouse button press (down=true) or release (down=false).
func (ic *InputControlEngine) MouseButton(normX, normY float32, button int, down bool) {
	ic.mu.Lock()
	if ic.customBtnFn != nil {
		fn := ic.customBtnFn
		ic.mu.Unlock()
		fn(button, down)
		return
	}
	ic.mu.Unlock()

	go func() {
		ic.dispatchOSMove(normX, normY)
		ic.dispatchOSButton(button, down)
	}()
}

func (ic *InputControlEngine) dispatchOSMove(normX, normY float32) {
	switch runtime.GOOS {
	case "linux":
		// 1. Send move command to persistent GNOME Mutter / Wayland RemoteDesktop daemon if active
		if ic.screenEngine != nil {
			ic.screenEngine.SendLinuxStreamCommand(fmt.Sprintf("MOVE %.4f %.4f\n", normX, normY))
		}
		// 2. Also warp X11 / XWayland pointer via pure-Go X11 socket (opcode 41 WarpPointer, <0.1ms)
		if err := warpX11Pointer(normX, normY); err != nil {
			// Fallback to xdotool / ydotool if available
			if p, lerr := exec.LookPath("xdotool"); lerr == nil && p != "" {
				px := int(normX * 1920)
				py := int(normY * 1080)
				// #nosec G204 -- safe internal mouse move command
				_ = exec.Command(p, "mousemove", strconv.Itoa(px), strconv.Itoa(py)).Run()
			} else if p, lerr := exec.LookPath("ydotool"); lerr == nil && p != "" {
				px := int(normX * 1920)
				py := int(normY * 1080)
				// #nosec G204 -- safe internal ydotool mouse move command
				_ = exec.Command(p, "mousemove", "-a", strconv.Itoa(px), strconv.Itoa(py)).Run()
			}
		}

	case "darwin":
		jxa := fmt.Sprintf(`ObjC.import("CoreGraphics"); var d = $.CGMainDisplayID(); var w = $.CGDisplayPixelsWide(d); var h = $.CGDisplayPixelsHigh(d); $.CGWarpMouseCursorPosition($.CGPointMake(%.4f * w, %.4f * h));`, normX, normY)
		// #nosec G204 -- safe internal macOS CoreGraphics cursor warp
		_ = exec.Command("osascript", "-l", "JavaScript", "-e", jxa).Run()

	case "windows":
		ic.sendWindowsCommand(fmt.Sprintf("MOVE %.4f %.4f\n", normX, normY))
	}
}

func (ic *InputControlEngine) dispatchOSButton(button int, down bool) {
	switch runtime.GOOS {
	case "linux":
		action := "UP"
		if down {
			action = "DOWN"
		}
		if ic.screenEngine != nil {
			ic.screenEngine.SendLinuxStreamCommand(fmt.Sprintf("%s %d\n", action, button))
		}
		xdoBtn := "1"
		switch button {
		case 1:
			xdoBtn = "3"
		case 2:
			xdoBtn = "2"
		}
		if p, err := exec.LookPath("xdotool"); err == nil && p != "" {
			subCmd := "mouseup"
			if down {
				subCmd = "mousedown"
			}
			// #nosec G204 -- safe internal xdotool mouse button injection
			_ = exec.Command(p, subCmd, xdoBtn).Run()
		} else if p, err := exec.LookPath("ydotool"); err == nil && p != "" {
			subCmd := "0x80"
			if down {
				subCmd = "0xC0"
			}
			// #nosec G204 -- safe internal ydotool mouse button injection
			_ = exec.Command(p, "click", subCmd).Run()
		}

	case "darwin":
		// CoreGraphics mouse event: kCGEventLeftMouseDown=1, kCGEventLeftMouseUp=2, kCGEventRightMouseDown=3, kCGEventRightMouseUp=4
		evType := 1
		cgBtn := 0
		if button == 1 {
			cgBtn = 1
			if down {
				evType = 3
			} else {
				evType = 4
			}
		} else if !down {
			evType = 2
		}
		jxa := fmt.Sprintf(`ObjC.import("CoreGraphics"); var ev = $.CGEventCreate(null); var loc = $.CGEventGetLocation(ev); var mev = $.CGEventCreateMouseEvent(null, %d, loc, %d); $.CGEventPost($.kCGHIDEventTap, mev);`, evType, cgBtn)
		// #nosec G204 -- safe internal macOS CoreGraphics click injection
		_ = exec.Command("osascript", "-l", "JavaScript", "-e", jxa).Run()

	case "windows":
		action := "UP"
		if down {
			action = "DOWN"
		}
		ic.sendWindowsCommand(fmt.Sprintf("%s %d\n", action, button))
	}
}

func (ic *InputControlEngine) sendWindowsCommand(line string) {
	ic.mu.Lock()
	defer ic.mu.Unlock()

	if ic.winStdin == nil {
		psScript := `
Add-Type -AssemblyName System.Windows.Forms
Add-Type @"
using System;
using System.Runtime.InteropServices;
public class GonDeskMouse {
    [DllImport("user32.dll")]
    public static extern void mouse_event(uint dwFlags, uint dx, uint dy, uint dwData, int dwExtraInfo);
}
"@
while ($line = [Console]::In.ReadLine()) {
    $parts = $line.Split(' ')
    if ($parts[0] -eq 'MOVE' -and $parts.Length -ge 3) {
        $b = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds
        $x = [int]([double]$parts[1] * $b.Width)
        $y = [int]([double]$parts[2] * $b.Height)
        [System.Windows.Forms.Cursor]::Position = New-Object System.Drawing.Point($x, $y)
    } elseif ($parts[0] -eq 'DOWN') {
        [GonDeskMouse]::mouse_event(0x0002, 0, 0, 0, 0)
    } elseif ($parts[0] -eq 'UP') {
        [GonDeskMouse]::mouse_event(0x0004, 0, 0, 0, 0)
    }
}
`
		// #nosec G204 -- safe internal Windows cursor helper process
		cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", psScript)
		if stdin, err := cmd.StdinPipe(); err == nil {
			if err := cmd.Start(); err == nil {
				ic.winStdin = stdin
			}
		}
	}

	if ic.winStdin != nil {
		if _, err := io.WriteString(ic.winStdin, line); err != nil {
			_ = ic.winStdin.Close()
			ic.winStdin = nil
		}
	}
}

// warpX11Pointer sends an X11 WarpPointer request (opcode 41) directly over the local X11 unix socket.
func warpX11Pointer(normX, normY float32) error {
	dispNum := resolveX11DisplayNum()
	socketPath := "/tmp/.X11-unix/X" + dispNum
	// #nosec G704 -- safe internal local unix domain socket for X11 pointer control
	conn, err := net.DialTimeout("unix", socketPath, 200*time.Millisecond)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(300 * time.Millisecond))

	authName, authCookie := readXAuthorityCookie(dispNum)
	return sendX11WarpPointer(conn, normX, normY, authName, authCookie)
}

func sendX11WarpPointer(conn net.Conn, normX, normY float32, authName string, authCookie []byte) error {
	pad4 := func(n int) int { return (n + 3) &^ 3 }
	nameLen := len(authName)
	dataLen := len(authCookie)
	totalSetupLen := 12 + pad4(nameLen) + pad4(dataLen)

	setupReq := make([]byte, totalSetupLen)
	setupReq[0] = 'l'
	binary.LittleEndian.PutUint16(setupReq[2:4], 11)
	binary.LittleEndian.PutUint16(setupReq[4:6], 0)
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
		return err
	}

	setupHeader := make([]byte, 8)
	if _, err := io.ReadFull(conn, setupHeader); err != nil {
		return err
	}
	if setupHeader[0] != 1 {
		return fmt.Errorf("X11 auth rejected: status %d", setupHeader[0])
	}

	addlLen := int(binary.LittleEndian.Uint16(setupHeader[6:8])) * 4
	rest := make([]byte, addlLen)
	if _, err := io.ReadFull(conn, rest); err != nil {
		return err
	}

	vendorLen := int(binary.LittleEndian.Uint16(rest[16:18]))
	numFormats := int(rest[21])
	padVendor := (vendorLen + 3) &^ 3
	screenOffset := 32 + padVendor + numFormats*8

	if len(rest) < screenOffset+24 {
		return errors.New("X11 setup block truncated")
	}

	rootWinID := binary.LittleEndian.Uint32(rest[screenOffset : screenOffset+4])
	scrW := int(binary.LittleEndian.Uint16(rest[screenOffset+20 : screenOffset+22]))
	scrH := int(binary.LittleEndian.Uint16(rest[screenOffset+22 : screenOffset+24]))

	if scrW <= 0 || scrH <= 0 {
		return errors.New("invalid X11 screen dimensions")
	}

	dstX := int16(normX * float32(scrW))
	dstY := int16(normY * float32(scrH))

	// X11 WarpPointer Request (Opcode 41, 24 bytes = 6 units of 4 bytes)
	warpReq := make([]byte, 24)
	warpReq[0] = 41                                          // Opcode 41: WarpPointer
	warpReq[1] = 0                                           // unused
	binary.LittleEndian.PutUint16(warpReq[2:4], 6)           // Request length = 6 (24 bytes)
	binary.LittleEndian.PutUint32(warpReq[4:8], 0)           // src-window = None (0)
	binary.LittleEndian.PutUint32(warpReq[8:12], rootWinID)  // dst-window = Root Window
	binary.LittleEndian.PutUint16(warpReq[12:14], 0)         // src-x = 0
	binary.LittleEndian.PutUint16(warpReq[14:16], 0)         // src-y = 0
	binary.LittleEndian.PutUint16(warpReq[16:18], 0)         // src-width = 0
	binary.LittleEndian.PutUint16(warpReq[18:20], 0)         // src-height = 0
	// #nosec G115 -- screen pixel coordinates fit in uint16/int16
	binary.LittleEndian.PutUint16(warpReq[20:22], uint16(dstX))
	// #nosec G115 -- screen pixel coordinates fit in uint16/int16
	binary.LittleEndian.PutUint16(warpReq[22:24], uint16(dstY))

	_, err := conn.Write(warpReq)
	return err
}
