//go:build experimental

package skillcnl

import (
	"strings"
	"testing"
)

// TestEncodeWord_SpecExamples covers the worked examples from the
// dhnt language specification (https://github.com/dhnt/dhnt).
func TestEncodeWord_SpecExamples(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"bash", "basohe"},
		{"cd", "cada"},
		{"ls", "liso"},
		{"cp", "capo"},
		{"mv", "mivu"},
		{"rm", "romi"},
		{"hello", "helilo"},
		{"how", "howu"},
		{"are", "are"},
		{"you", "you"},
		{"fine", "fine"},
		{"thanks", "tohanikiso"},
	}
	for _, tc := range cases {
		got, err := EncodeWord(tc.in)
		if err != nil {
			t.Errorf("EncodeWord(%q) error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("EncodeWord(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestEncodeWord_RejectsNonLatinLowercase(t *testing.T) {
	cases := []string{"Bash", "git rebase", "bûsí", ""}
	for _, in := range cases {
		if in == "" {
			got, err := EncodeWord(in)
			if err != nil || got != "" {
				t.Errorf("EncodeWord(\"\") = (%q, %v), want (\"\", nil)", got, err)
			}
			continue
		}
		if _, err := EncodeWord(in); err == nil {
			t.Errorf("EncodeWord(%q) accepted non-a-z input", in)
		}
	}
}

func TestEncodePhrase(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"how are you", "howu are you"},
		{"  hello  world  ", "helilo worolida"},
		{"", ""},
	}
	for _, tc := range cases {
		got, err := EncodePhrase(tc.in)
		if err != nil {
			t.Errorf("EncodePhrase(%q) error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("EncodePhrase(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestIsCanonical(t *testing.T) {
	good := []string{
		"a", "are", "you", "basohe", "cada", "liso", "tohanikiso",
		"helilo", "fine", "mivu",
	}
	for _, s := range good {
		if !IsCanonical(s) {
			t.Errorf("IsCanonical(%q) = false, want true", s)
		}
	}
	bad := []string{
		"",        // empty
		"bash",    // sh cluster
		"git",     // t at end with no row vowel
		"hello",   // double l cluster
		"AB",      // uppercase
		"a b",     // contains space
		"thanks!", // punctuation
	}
	for _, s := range bad {
		if IsCanonical(s) {
			t.Errorf("IsCanonical(%q) = true, want false", s)
		}
	}
}

// TestEncodeWord_RoundtripIsCanonical asserts that any output produced
// by the encoder is itself canonical Layer-1.5 dhnt — the encoder is
// idempotent on its own output.
func TestEncodeWord_RoundtripIsCanonical(t *testing.T) {
	corpus := []string{
		"step", "needs", "git", "github", "slack", "skill",
		"flow", "budget", "capability", "primitive", "effect",
		"intent", "rationale", "retry", "escalate",
		"bumpversion", "writenotes", "announce",
	}
	for _, w := range corpus {
		enc, err := EncodeWord(w)
		if err != nil {
			t.Fatalf("EncodeWord(%q) error: %v", w, err)
		}
		if !IsCanonical(enc) {
			t.Errorf("EncodeWord(%q) = %q is not canonical", w, enc)
		}
		// idempotent: encoding the encoded form should not change it.
		again, err := EncodeWord(enc)
		if err != nil {
			t.Fatalf("EncodeWord(%q) second pass error: %v", enc, err)
		}
		if again != enc {
			t.Errorf("EncodeWord not idempotent: %q → %q → %q", w, enc, again)
		}
	}
}

func TestDecimalNumeral(t *testing.T) {
	cases := []struct {
		n    uint64
		want string
	}{
		{0, "juji"},   // ju + j (digit 0) + i (j's row vowel)
		{1, "jua"},    // ju + a (digit 1, vowel itself)
		{9, "jui"},    // ju + i (digit 9, vowel itself)
		{10, "juaji"}, // ju + a (1) + ji (0)
		{12, "juaba"}, // ju + a (1) + ba (2)
		{18, "juahe"}, // ju + a (1) + he (8)
		{2018, "jubajiahe"},
	}
	for _, tc := range cases {
		got := EncodeDecimal(tc.n)
		if got != tc.want {
			t.Errorf("EncodeDecimal(%d) = %q, want %q", tc.n, got, tc.want)
		}
		back, err := DecodeDecimal(got)
		if err != nil {
			t.Errorf("DecodeDecimal(%q) error: %v", got, err)
			continue
		}
		if back != tc.n {
			t.Errorf("Decimal roundtrip: %d → %q → %d", tc.n, got, back)
		}
	}
}

func TestDecimalNumeral_RejectsBadInput(t *testing.T) {
	bad := []string{
		"abc",   // no ju prefix
		"juabz", // z is not a digit letter
		"ju",    // empty body
	}
	for _, s := range bad {
		if _, err := DecodeDecimal(s); err == nil {
			t.Errorf("DecodeDecimal(%q) accepted bad input", s)
		}
	}
}

// TestEncodeWord_NoConsonantClusters asserts the structural property
// that drives Layer 1.5 parseability: the encoder never produces two
// adjacent consonants.
func TestEncodeWord_NoConsonantClusters(t *testing.T) {
	corpus := strings.Split(
		"the quick brown fox jumps over the lazy dog skill step needs git",
		" ",
	)
	for _, w := range corpus {
		enc, err := EncodeWord(w)
		if err != nil {
			t.Fatalf("EncodeWord(%q) error: %v", w, err)
		}
		for i := 0; i < len(enc)-1; i++ {
			if !isVowel(enc[i]) && !isVowel(enc[i+1]) {
				t.Errorf("EncodeWord(%q) = %q: consonant cluster at position %d", w, enc, i)
				break
			}
		}
	}
}
