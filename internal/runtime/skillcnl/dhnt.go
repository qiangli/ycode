//go:build experimental

package skillcnl

import (
	"fmt"
	"strings"
)

// rowVowel maps each lowercase consonant to its dhnt "row vowel" — the
// leading vowel of the row that consonant lives in:
//
//	row a: b c d
//	row e: f g h
//	row i: j k l m n
//	row o: p q r s t
//	row u: v w x y z
//
// Vowels themselves return 0; callers check isVowel first.
func rowVowel(c byte) byte {
	switch c {
	case 'b', 'c', 'd':
		return 'a'
	case 'f', 'g', 'h':
		return 'e'
	case 'j', 'k', 'l', 'm', 'n':
		return 'i'
	case 'p', 'q', 'r', 's', 't':
		return 'o'
	case 'v', 'w', 'x', 'y', 'z':
		return 'u'
	}
	return 0
}

func isVowel(c byte) bool {
	return c == 'a' || c == 'e' || c == 'i' || c == 'o' || c == 'u'
}

func isLowerLetter(c byte) bool {
	return c >= 'a' && c <= 'z'
}

// EncodeWord applies the dhnt vowel-insertion rule to a single
// lowercase a-z word. The output is the canonical full form: a sequence
// of (V|CV) syllables with no consonant clusters and (apart from
// distinct adjacent single-vowel syllables) no vowel clusters either.
//
// Rules, applied character-by-character:
//   - A vowel emits itself.
//   - A consonant followed by a vowel emits the consonant plus that
//     vowel (the input vowel takes precedence over the row vowel).
//   - A consonant followed by a consonant or by end-of-word emits the
//     consonant plus its row vowel.
//
// Returns an error if the input contains any character outside [a-z].
func EncodeWord(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	var b strings.Builder
	b.Grow(len(s) * 2)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !isLowerLetter(c) {
			return "", fmt.Errorf("dhnt: input must be lowercase a-z, got %q at position %d in %q", c, i, s)
		}
		if isVowel(c) {
			b.WriteByte(c)
			continue
		}
		// consonant: write it, then write the next vowel (input or row).
		b.WriteByte(c)
		if i+1 < len(s) && isVowel(s[i+1]) {
			b.WriteByte(s[i+1])
			i++
			continue
		}
		v := rowVowel(c)
		if v == 0 {
			return "", fmt.Errorf("dhnt: no row vowel for byte %q", c)
		}
		b.WriteByte(v)
	}
	return b.String(), nil
}

// EncodePhrase splits on whitespace, encodes each word, and rejoins
// with single spaces. Empty input returns empty output.
func EncodePhrase(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	words := strings.Fields(strings.ToLower(s))
	out := make([]string, len(words))
	for i, w := range words {
		enc, err := EncodeWord(w)
		if err != nil {
			return "", err
		}
		out[i] = enc
	}
	return strings.Join(out, " "), nil
}

// IsCanonical reports whether s is a valid dhnt full-form word: a
// non-empty sequence of (V|CV) syllables in [a-z]+, with no consonant
// clusters. It is the parser-side validator for Layer 1.5 word tokens.
func IsCanonical(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !isLowerLetter(c) {
			return false
		}
		if isVowel(c) {
			continue
		}
		// consonant must be followed by a vowel (no end-of-word
		// bare consonant in full form, and no consonant cluster).
		if i+1 >= len(s) || !isVowel(s[i+1]) {
			return false
		}
		i++ // consume the vowel of this CV syllable
	}
	return true
}

// EncodeDecimal encodes a non-negative decimal integer into the dhnt
// numeral form with the "ju" prefix. The digit mapping follows the
// dhnt spec: 1→a, 2→b, 3→c, 4→d, 5→e, 6→f, 7→g, 8→h, 9→i, 0→j. Each
// digit letter is paired with its row vowel ("a"→"a" since 'a' is a
// vowel itself, "b"→"ba", ..., "j"→"ji") to produce the full form.
//
// Examples:
//
//	0     → "juj"            (contracted form of "ju" + "ji")
//	12    → "juaba"          (ju + a + ba)
//	2018  → "jubajiabahe"    (ju + ba + ji + a + ba + he)  -- full form
func EncodeDecimal(n uint64) string {
	if n == 0 {
		return "ju" + decimalDigit('0')
	}
	// build digits high-to-low
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	var b strings.Builder
	b.WriteString("ju")
	for _, d := range digits {
		b.WriteString(decimalDigit(d))
	}
	return b.String()
}

// decimalDigit returns the dhnt full-form syllable for a single ASCII
// decimal digit byte ('0'..'9').
func decimalDigit(d byte) string {
	letter := digitLetter(d)
	if isVowel(letter) {
		return string([]byte{letter})
	}
	v := rowVowel(letter)
	return string([]byte{letter, v})
}

func digitLetter(d byte) byte {
	switch d {
	case '1':
		return 'a'
	case '2':
		return 'b'
	case '3':
		return 'c'
	case '4':
		return 'd'
	case '5':
		return 'e'
	case '6':
		return 'f'
	case '7':
		return 'g'
	case '8':
		return 'h'
	case '9':
		return 'i'
	case '0':
		return 'j'
	}
	return 0
}

// DecodeDecimal parses a dhnt decimal numeral (with required "ju"
// prefix in this alpha) and returns its uint64 value. It accepts both
// full and contracted forms by virtue of mapping each digit letter
// independently. Returns an error on malformed input.
func DecodeDecimal(s string) (uint64, error) {
	if !strings.HasPrefix(s, "ju") {
		return 0, fmt.Errorf("dhnt: decimal numeral must start with %q, got %q", "ju", s)
	}
	rest := s[2:]
	if rest == "" {
		return 0, fmt.Errorf("dhnt: decimal numeral has no digits: %q", s)
	}
	var n uint64
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		d, ok := letterDigit(c)
		if !ok {
			return 0, fmt.Errorf("dhnt: not a digit letter %q in numeral %q", c, s)
		}
		n = n*10 + uint64(d)
		// skip a trailing row vowel if present (full form)
		if !isVowel(c) && i+1 < len(rest) && rest[i+1] == rowVowel(c) {
			i++
		}
	}
	return n, nil
}

func letterDigit(c byte) (uint8, bool) {
	switch c {
	case 'a':
		return 1, true
	case 'b':
		return 2, true
	case 'c':
		return 3, true
	case 'd':
		return 4, true
	case 'e':
		return 5, true
	case 'f':
		return 6, true
	case 'g':
		return 7, true
	case 'h':
		return 8, true
	case 'i':
		return 9, true
	case 'j':
		return 0, true
	}
	return 0, false
}
