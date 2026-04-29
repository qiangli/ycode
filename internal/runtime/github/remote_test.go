package github

import "testing"

func TestParseRemoteURL(t *testing.T) {
	tests := []struct {
		url       string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		// HTTPS variants
		{"https://github.com/owner/repo.git", "owner", "repo", false},
		{"https://github.com/owner/repo", "owner", "repo", false},
		{"https://github.com/my-org/my-project.git", "my-org", "my-project", false},

		// SSH variants
		{"git@github.com:owner/repo.git", "owner", "repo", false},
		{"git@github.com:owner/repo", "owner", "repo", false},
		{"git@github.com:my-org/my-project.git", "my-org", "my-project", false},

		// Invalid
		{"https://gitlab.com/owner/repo.git", "", "", true},
		{"not-a-url", "", "", true},
		{"", "", "", true},
	}

	for _, tc := range tests {
		owner, repo, err := ParseRemoteURL(tc.url)
		if (err != nil) != tc.wantErr {
			t.Errorf("ParseRemoteURL(%q): err=%v, wantErr=%v", tc.url, err, tc.wantErr)
			continue
		}
		if owner != tc.wantOwner || repo != tc.wantRepo {
			t.Errorf("ParseRemoteURL(%q) = (%q, %q), want (%q, %q)",
				tc.url, owner, repo, tc.wantOwner, tc.wantRepo)
		}
	}
}
