package dispatcher

import (
	"context"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestBandwidthLimiterSharesAccount(t *testing.T) {
	bandwidth.Lock()
	bandwidth.nextRead = time.Now().Add(time.Minute)
	bandwidth.config.Users = map[string]bandwidthRule{
		"one": {Account: "shared", Mbps: 20},
		"two": {Account: "shared", Mbps: 20},
		"other": {Account: "other", Mbps: 20},
	}
	bandwidth.Unlock()
	first, node := bandwidthLimiters("one")
	second, _ := bandwidthLimiters("two")
	other, _ := bandwidthLimiters("other")
	if first != second || first == other || first.Limit() != rate.Limit(2500000) || node.Limit() != rate.Inf {
		t.Fatal("expected a shared 20 Mbps limiter per account")
	}
	now := time.Now()
	if !first.AllowN(now, first.Burst()) || second.AllowN(now, 1) {
		t.Fatal("the second device did not share the first device's budget")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := second.WaitN(ctx, 1); err == nil {
		t.Fatal("a canceled connection must not wait for bandwidth")
	}
}
