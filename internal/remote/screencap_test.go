package remote

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image/jpeg"
	"io"
	"net"
	"testing"
	"time"
)

func TestGenerateSyntheticDesktopFrame(t *testing.T) {
	info := DesktopInfo{
		HostDeskID:       "170095453",
		HostAlias:        "anjay",
		ParticipantCount: 2,
		FrameSeq:         1,
		Timestamp:        time.Now(),
	}

	data := GenerateSyntheticDesktopFrame(1280, 720, info)
	if len(data) == 0 {
		t.Fatalf("expected non-empty synthetic desktop frame data")
	}

	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("failed to decode generated synthetic frame as JPEG: %v", err)
	}

	if img.Bounds().Dx() != 1280 || img.Bounds().Dy() != 720 {
		t.Fatalf("expected 1280x720, got %dx%d", img.Bounds().Dx(), img.Bounds().Dy())
	}
}

func TestScreenCaptureEngineFallback(t *testing.T) {
	engine := NewScreenCaptureEngine()
	info := DesktopInfo{
		HostDeskID:       "170095453",
		HostAlias:        "anjay",
		ParticipantCount: 1,
	}

	frame, err := engine.CaptureFrame(info)
	if err != nil {
		t.Fatalf("unexpected error capturing frame: %v", err)
	}
	if len(frame) == 0 {
		t.Fatalf("expected non-empty frame bytes")
	}

	_, err = jpeg.Decode(bytes.NewReader(frame))
	if err != nil {
		t.Fatalf("captured frame is not valid JPEG: %v", err)
	}
}

func TestScreenCaptureEngineCustom(t *testing.T) {
	engine := NewScreenCaptureEngine()
	mockJPEG := GenerateSyntheticDesktopFrame(640, 360, DesktopInfo{HostAlias: "mock"})
	engine.SetCustomCaptureFunc(func() ([]byte, error) {
		return mockJPEG, nil
	})

	frame, err := engine.CaptureFrame(DesktopInfo{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(frame) != len(mockJPEG) {
		t.Fatalf("expected custom capture data to be returned")
	}
}

func TestScreenCaptureEngineWindowsDialog(t *testing.T) {
	engine := NewScreenCaptureEngine()
	if engine.NeedsWindowsDialog() {
		t.Fatalf("expected NeedsWindowsDialog to be false initially")
	}

	engine.needsWindowsPrompt = true
	if !engine.NeedsWindowsDialog() {
		t.Fatalf("expected NeedsWindowsDialog to be true when prompt needed")
	}

	engine.SetWindowsDialogShown()
	if engine.NeedsWindowsDialog() {
		t.Fatalf("expected NeedsWindowsDialog to be false once shown")
	}
}

func TestCaptureX11FromConn(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer func() {
		_ = clientConn.Close()
		_ = serverConn.Close()
	}()

	errCh := make(chan error, 1)

	// Mock X11 Server
	go func() {
		defer close(errCh)
		// 1. Read setup request (12 bytes)
		setupReq := make([]byte, 12)
		if _, err := io.ReadFull(serverConn, setupReq); err != nil {
			errCh <- fmt.Errorf("read setupReq: %w", err)
			return
		}

		// 2. Write setup reply header (8 bytes) + rest (64 bytes = 16 4-byte units)
		setupHeader := make([]byte, 8)
		setupHeader[0] = 1 // Success
		binary.LittleEndian.PutUint16(setupHeader[6:8], 16)
		if _, err := serverConn.Write(setupHeader); err != nil {
			errCh <- fmt.Errorf("write setupHeader: %w", err)
			return
		}

		rest := make([]byte, 64)
		// vendorLen = 0 (rest[16:18]), numFormats = 0 (rest[21])
		// screenOffset = 32
		binary.LittleEndian.PutUint32(rest[32:36], 0x12345678) // Root window ID
		binary.LittleEndian.PutUint16(rest[52:54], 64)         // scrW = 64
		binary.LittleEndian.PutUint16(rest[54:56], 48)         // scrH = 48
		if _, err := serverConn.Write(rest); err != nil {
			errCh <- fmt.Errorf("write rest: %w", err)
			return
		}

		// 3. Read GetImage request (20 bytes)
		getImgReq := make([]byte, 20)
		if _, err := io.ReadFull(serverConn, getImgReq); err != nil {
			errCh <- fmt.Errorf("read getImgReq: %w", err)
			return
		}
		if getImgReq[0] != 73 {
			errCh <- fmt.Errorf("expected opcode 73, got %d", getImgReq[0])
			return
		}

		// 4. Write GetImage reply header (32 bytes)
		replyHeader := make([]byte, 32)
		replyHeader[0] = 1 // Reply success
		pixelCount := 64 * 48 * 4
		binary.LittleEndian.PutUint32(replyHeader[4:8], uint32(pixelCount/4))
		if _, err := serverConn.Write(replyHeader); err != nil {
			errCh <- fmt.Errorf("write replyHeader: %w", err)
			return
		}

		// 5. Write pixel bytes (BGRA)
		pixels := make([]byte, pixelCount)
		for i := 0; i < pixelCount; i += 4 {
			pixels[i] = 200   // B
			pixels[i+1] = 100 // G
			pixels[i+2] = 50  // R
			pixels[i+3] = 255 // A
		}
		if _, err := serverConn.Write(pixels); err != nil {
			errCh <- fmt.Errorf("write pixels: %w", err)
			return
		}
	}()

	jpegData, err := captureX11FromConn(clientConn)
	if err != nil {
		t.Fatalf("captureX11FromConn failed: %v", err)
	}

	if srvErr := <-errCh; srvErr != nil {
		t.Fatalf("mock server error: %v", srvErr)
	}

	img, err := jpeg.Decode(bytes.NewReader(jpegData))
	if err != nil {
		t.Fatalf("failed to decode resulting JPEG: %v", err)
	}

	if img.Bounds().Dx() != 64 || img.Bounds().Dy() != 48 {
		t.Fatalf("expected image 64x48, got %dx%d", img.Bounds().Dx(), img.Bounds().Dy())
	}
}
