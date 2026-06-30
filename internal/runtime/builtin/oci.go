package builtin

import (
	"context"
	"fmt"
)

func init() {
	RegisterSkillExecutor("oci", executeOCI)
}

func executeOCI(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("oci skill is not available in lean ycode; delegate container builds to bashy or another external OCI host layer")
}
