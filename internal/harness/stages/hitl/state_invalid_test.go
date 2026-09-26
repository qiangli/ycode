package hitl

import "testing"

// A malformed model call's report is valid only as an incomplete one with a
// reason and a digest; policy can then deny it.
func TestValidateReportInvalidCall(t *testing.T) {
	ok := Preflight{Call: Call{ID: "c", Name: "python", Invalid: "no tool"}, Digest: "sha256:x"}
	if err := validateReport(ok); err != nil {
		t.Fatalf("invalid call report rejected: %v", err)
	}
	complete := ok
	complete.Complete = true
	if validateReport(complete) == nil {
		t.Fatal("a complete report for an invalid call must be rejected")
	}
	if validateReport(Preflight{Call: Call{ID: "c", Name: "python"}, Digest: "sha256:x"}) == nil {
		t.Fatal("a non-bashy call without a reason must still be rejected")
	}
}
