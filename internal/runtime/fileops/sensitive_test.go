package fileops

import "testing"

func TestCheckSensitiveFile(t *testing.T) {
	tests := []struct {
		path string
		want SensitiveFileAction
	}{
		// .env files
		{".env", FileAskUser},
		{"/path/to/.env", FileAskUser},
		{".env.local", FileAskUser},
		{".env.production", FileAskUser},
		{".env.development", FileAskUser},

		// Safe .env variants
		{".env.example", FileAllowed},
		{".env.sample", FileAllowed},
		{".env.template", FileAllowed},

		// Credential files
		{"credentials.json", FileAskUser},
		{"/home/user/credentials.json", FileAskUser},
		{"server.pem", FileAskUser},
		{"private.key", FileAskUser},
		{"id_rsa", FileAskUser},
		{"id_rsa.pub", FileAskUser},
		{"cert.pfx", FileAskUser},

		// Normal files
		{"main.go", FileAllowed},
		{"README.md", FileAllowed},
		{"/path/to/config.yaml", FileAllowed},
		{"package.json", FileAllowed},
		{".gitignore", FileAllowed},
		{"envfile.txt", FileAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := CheckSensitiveFile(tt.path)
			if got != tt.want {
				t.Errorf("CheckSensitiveFile(%q) = %d, want %d", tt.path, got, tt.want)
			}
		})
	}
}
