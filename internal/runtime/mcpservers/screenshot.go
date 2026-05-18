package mcpservers

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ScreenshotsDir returns the directory used to spill screenshots when
// MaxBytes is exceeded or SavePath is requested. Created on demand.
func ScreenshotsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".agents", "ycode", "browser", "screenshots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// PostprocessScreenshot applies the MaxBytes cap and SavePath from a
// BrowserAction to a freshly-captured PNG (raw, not base64-encoded).
// Returns the Result fields the caller should populate.
//
// Behaviour:
//   - SavePath set (absolute or relative under ScreenshotsDir):
//     always write to disk; return Path; leave Image empty.
//   - MaxBytes > 0 and base64 PNG exceeds MaxBytes:
//     try a JPEG re-encode at q=70 → 50 → 30; if any fits, return
//     that as Image. Otherwise, spill to disk and return Path.
//   - Otherwise, return the original base64 PNG as Image.
//
// The on-disk path is always absolute so foreign agents can read it
// without knowing the screenshots dir layout.
func PostprocessScreenshot(pngBytes []byte, action BrowserAction) (image string, path string, err error) {
	if action.SavePath != "" {
		p, err := saveScreenshot(pngBytes, action.SavePath, "png")
		return "", p, err
	}
	b64 := base64.StdEncoding.EncodeToString(pngBytes)
	if action.MaxBytes <= 0 || len(b64) <= action.MaxBytes {
		return b64, "", nil
	}
	// Try JPEG re-encode at decreasing qualities. JPEG is much
	// smaller than PNG for screenshots; q=70 is typically 4× smaller,
	// q=30 ~10×. Decode the PNG first.
	img, decodeErr := png.Decode(bytes.NewReader(pngBytes))
	if decodeErr == nil {
		for _, q := range []int{70, 50, 30} {
			var buf bytes.Buffer
			if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: q}); err != nil {
				continue
			}
			enc := base64.StdEncoding.EncodeToString(buf.Bytes())
			if len(enc) <= action.MaxBytes {
				return enc, "", nil
			}
		}
	}
	// Still over cap (or decode failed): spill to disk.
	p, err := saveScreenshotAuto(pngBytes)
	return "", p, err
}

func saveScreenshot(pngBytes []byte, target, ext string) (string, error) {
	var abs string
	if filepath.IsAbs(target) {
		abs = target
	} else {
		dir, err := ScreenshotsDir()
		if err != nil {
			return "", err
		}
		abs = filepath.Join(dir, target)
	}
	if !strings.HasSuffix(strings.ToLower(abs), "."+ext) && filepath.Ext(abs) == "" {
		abs = abs + "." + ext
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(abs, pngBytes, 0o644); err != nil {
		return "", err
	}
	return abs, nil
}

func saveScreenshotAuto(pngBytes []byte) (string, error) {
	name := fmt.Sprintf("screenshot-%s.png", time.Now().UTC().Format("20060102T150405.000Z"))
	return saveScreenshot(pngBytes, name, "png")
}
