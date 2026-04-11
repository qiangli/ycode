package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/qiangli/ycode/internal/runtime/vfs"
)

// maxImageSize is the maximum image file size (10 MB).
const maxImageSize = 10 * 1024 * 1024

// supportedImageExts maps file extensions to MIME media types.
var supportedImageExts = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".svg":  "image/svg+xml",
	".webp": "image/webp",
	".bmp":  "image/bmp",
}

// RegisterImageHandler registers the view_image tool handler.
func RegisterImageHandler(r *Registry, v *vfs.VFS) {
	spec, ok := r.Get("view_image")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			FilePath string `json:"file_path"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse view_image input: %w", err)
		}
		if params.FilePath == "" {
			return "", fmt.Errorf("file_path is required")
		}
		return viewImage(ctx, v, params.FilePath)
	}
}

// viewImage reads an image file and returns it as a JSON object.
func viewImage(ctx context.Context, v *vfs.VFS, filePath string) (string, error) {
	absPath, err := v.ValidatePath(ctx, filePath)
	if err != nil {
		return "", err
	}

	ext := strings.ToLower(filepath.Ext(absPath))
	mediaType, ok := supportedImageExts[ext]
	if !ok {
		return "", fmt.Errorf("unsupported image type %q; supported: png, jpg, jpeg, gif, svg, webp, bmp", ext)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", absPath, err)
	}
	if info.Size() > maxImageSize {
		return "", fmt.Errorf("image file too large: %d bytes (max %d)", info.Size(), maxImageSize)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", absPath, err)
	}

	// For SVG files, return raw text.
	if ext == ".svg" {
		result := map[string]string{
			"type":       "image",
			"media_type": mediaType,
			"data":       string(data),
		}
		out, _ := json.Marshal(result)
		return string(out), nil
	}

	// For binary images, return base64-encoded data.
	encoded := base64.StdEncoding.EncodeToString(data)
	result := map[string]string{
		"type":       "image",
		"media_type": mediaType,
		"data":       encoded,
	}
	out, _ := json.Marshal(result)
	return string(out), nil
}
