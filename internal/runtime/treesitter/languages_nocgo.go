//go:build !cgo

package treesitter

// GetLanguage returns nil when built without CGO (no languages available).
func GetLanguage(name string) any {
	return nil
}

// SupportedLanguages returns an empty slice when built without CGO.
func SupportedLanguages() []string {
	return nil
}

// IsSupported returns false when built without CGO.
func IsSupported(name string) bool {
	return false
}
