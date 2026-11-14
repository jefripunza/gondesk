package remote

import (
	"bytes"
	"image/jpeg"
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
