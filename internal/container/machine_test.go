package container

import (
	"strings"
	"testing"
)

func TestResolveMachineImage(t *testing.T) {
	got := resolveMachineImage()
	if got == "" {
		t.Fatal("resolveMachineImage() returned empty — libpod's NewOCIArtifactPull rejects this as 'no machine image endpoint provided'")
	}
	if !strings.HasPrefix(got, "docker://") && !strings.HasPrefix(got, "http") && !strings.HasPrefix(got, "/") {
		t.Errorf("resolveMachineImage() = %q; expected docker:// / http(s):// / absolute path (per diskpull.GetDisk dispatch)", got)
	}
}
