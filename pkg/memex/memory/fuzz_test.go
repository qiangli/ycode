package memory

import "testing"

func FuzzParseFrontmatter(f *testing.F) {
	// Seed corpus: representative inputs.
	f.Add("---\nname: test\ndescription: desc\ntype: user\n---\n\ncontent here")
	f.Add("---\nname: test\n---\n\ncontent")
	f.Add("no frontmatter at all")
	f.Add("")
	f.Add("---\n---\n\nempty frontmatter")
	f.Add("---\nname: has:colons:in:value\ntype: feedback\n---\n\nbody")
	f.Add("---\n\n\n\n---\n\nnewlines in frontmatter")
	f.Add("---\nname: unicode-日本語\ntype: user\n---\n\n用户偏好")
	f.Add(string([]byte{0x00, 0xFF, 0xFE}))
	f.Add("---\n" + string(make([]byte, 10000)) + "\n---\n\ncontent")

	f.Fuzz(func(t *testing.T, data string) {
		// Must not panic on any input.
		mem := parseFrontmatter(data)
		if mem == nil {
			t.Error("parseFrontmatter should never return nil")
		}
	})
}

func FuzzSanitizeFilename(f *testing.F) {
	f.Add("simple name")
	f.Add("")
	f.Add("UPPERCASE")
	f.Add("special!@#$%^&*()")
	f.Add("unicode-日本語-テスト")
	f.Add("a very long name that exceeds the fifty character maximum limit for filenames and keeps going")
	f.Add(string([]byte{0x00, 0x01, 0x02}))
	f.Add("---")
	f.Add("   ")

	f.Fuzz(func(t *testing.T, name string) {
		result := sanitizeFilename(name)

		// Must never be empty.
		if result == "" {
			t.Errorf("sanitizeFilename(%q) returned empty string", name)
		}

		// Must not exceed 50 chars.
		if len(result) > 50 {
			t.Errorf("sanitizeFilename(%q) returned %d chars, max 50", name, len(result))
		}

		// Must only contain safe chars.
		for _, r := range result {
			if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-') {
				t.Errorf("sanitizeFilename(%q) contains unsafe char %q", name, r)
			}
		}
	})
}
