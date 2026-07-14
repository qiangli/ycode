package api

import "testing"

// A QUOTA EXHAUSTION IS NOT A RATE LIMIT, and they want OPPOSITE responses.
//
// Both arrive as HTTP 429. We retried NINE times against a z.ai quota that would not reset
// for FIFTEEN HOURS — ~30s of backoff on a request that could not possibly succeed.
//
//	1302  "Rate limit reached for requests"  -> too fast. WAIT; it clears in seconds.
//	1308  "Usage limit reached for 5 hour"   -> out of quota. HAND OFF; waiting is futile.
func TestQuotaExhaustionIsNotARateLimit(t *testing.T) {
	quota := []string{
		`{"error":{"code":"1308","message":"Usage limit reached for 5 hour. Your limit will reset at 2026-07-15 05:01:31"}}`,
		`{"error":{"type":"insufficient_quota","message":"You exceeded your current quota"}}`,
		`{"error":{"message":"Your credit balance is too low to access the API"}}`,
	}
	for _, body := range quota {
		if !IsQuotaExhausted(body) {
			t.Errorf("a QUOTA exhaustion was not recognised, so it will be retried into a wall "+
				"that does not move for hours:\n  %s", body)
		}
	}

	// A plain rate limit MUST still be retried — it clears in seconds, and failing fast
	// there would throw away a request that was about to succeed.
	rate := []string{
		`{"error":{"code":"1302","message":"Rate limit reached for requests"}}`,
		`{"error":{"message":"Too many requests, please slow down"}}`,
	}
	for _, body := range rate {
		if IsQuotaExhausted(body) {
			t.Errorf("an ordinary rate limit was classed as quota exhaustion — it would fail "+
				"fast instead of backing off, and lose a request that would have succeeded:\n  %s", body)
		}
	}
}
