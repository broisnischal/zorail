package api

import "testing"

func TestRateLimiterBurstThenBlock(t *testing.T) {
	rl := newRateLimiter(1, 3) // 1 rps, burst 3
	for i := 0; i < 3; i++ {
		if !rl.allow("client") {
			t.Fatalf("request %d within burst should be allowed", i)
		}
	}
	if rl.allow("client") {
		t.Fatal("4th request should be rejected once burst is spent")
	}
	// A different client has its own independent budget.
	if !rl.allow("other") {
		t.Fatal("distinct client should not share a bucket")
	}
}
