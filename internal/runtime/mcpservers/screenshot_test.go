package mcpservers

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qiangli/ycode/pkg/browser/wire"
)

// makePNG returns an N×N solid-color PNG. Size scales superlinearly
// in bytes — 200×200 (~~5 KB), 1024×1024 (~~250 KB).
func makePNG(t *testing.T, n int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			img.Set(x, y, color.RGBA{uint8(x % 255), uint8(y % 255), 100, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func TestPostprocessNoCapReturnsInline(t *testing.T) {
	raw := makePNG(t, 50)
	img, path, err := PostprocessScreenshot(raw, wire.Action{})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if path != "" {
		t.Fatalf("path set without SavePath: %q", path)
	}
	if img == "" {
		t.Fatalf("image empty")
	}
	dec, err := base64.StdEncoding.DecodeString(img)
	if err != nil || !bytes.Equal(dec, raw) {
		t.Fatalf("inline base64 must round-trip to input bytes")
	}
}

func TestPostprocessSavePathWritesFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "shot.png")
	raw := makePNG(t, 100)
	img, path, err := PostprocessScreenshot(raw, wire.Action{SavePath: target})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if img != "" {
		t.Fatalf("image must be empty when SavePath is set; got len=%d", len(img))
	}
	if path != target {
		t.Fatalf("path = %q, want %q", path, target)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatalf("file bytes != input")
	}
}

func TestPostprocessMaxBytesShrinksOrSpills(t *testing.T) {
	// Large PNG, tight cap that JPEG q=30 won't satisfy either → spill.
	raw := makePNG(t, 512)
	overrideScreenshotsDir(t)
	tinyCap := 200
	img, path, err := PostprocessScreenshot(raw, wire.Action{MaxBytes: tinyCap})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if path == "" && img == "" {
		t.Fatalf("must return either path or image")
	}
	if path == "" && len(img) > tinyCap {
		t.Fatalf("inline image %d > cap %d", len(img), tinyCap)
	}
	if path != "" {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("spill path missing: %v", err)
		}
		if !strings.HasPrefix(path, screenshotsDirOverride) {
			t.Fatalf("path %q outside override dir %q", path, screenshotsDirOverride)
		}
	}
}

func TestPostprocessLooseCapKeepsPNGSmall(t *testing.T) {
	// 50×50 PNG ≈ ~500 bytes raw ≈ ~700 base64. A 50000-byte cap means
	// the original PNG already fits — no re-encode needed.
	raw := makePNG(t, 50)
	img, path, err := PostprocessScreenshot(raw, wire.Action{MaxBytes: 50000})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if path != "" {
		t.Fatalf("unexpectedly spilled")
	}
	dec, _ := base64.StdEncoding.DecodeString(img)
	if !bytes.Equal(dec, raw) {
		t.Fatalf("expected original PNG kept; got different bytes")
	}
}

// --- ScreenshotsDir override (avoid polluting $HOME during tests) ---

var screenshotsDirOverride string

func overrideScreenshotsDir(t *testing.T) {
	t.Helper()
	d := t.TempDir()
	t.Setenv("HOME", d)
	screenshotsDirOverride = filepath.Join(d, ".agents", "ycode", "browser", "screenshots")
}
