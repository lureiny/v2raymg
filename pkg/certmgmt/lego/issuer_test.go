package certmgmtlego

import (
	"testing"
	"time"

	"github.com/lureiny/v2raymg/pkg/certmgmt/domain"
)

func TestRenewBeforeDuration(t *testing.T) {
	cases := []struct {
		name string
		req  domain.IssueRequest
		want time.Duration
	}{
		{"RenewBefore wins over days", domain.IssueRequest{RenewBefore: 48 * time.Hour, RenewBeforeDays: 30}, 48 * time.Hour},
		{"days fallback when RenewBefore zero", domain.IssueRequest{RenewBeforeDays: 10}, 10 * 24 * time.Hour},
		{"default 30d when neither set", domain.IssueRequest{}, 30 * 24 * time.Hour},
		// Regression: a window larger than the legacy 30-day default must be honored,
		// not silently capped — this is exactly the gate the hours plumbing fixes.
		{"window larger than 30 days honored", domain.IssueRequest{RenewBefore: 60 * 24 * time.Hour}, 60 * 24 * time.Hour},
	}
	for _, tc := range cases {
		if got := renewBeforeDuration(tc.req); got != tc.want {
			t.Errorf("%s: renewBeforeDuration=%s want %s", tc.name, got, tc.want)
		}
	}
}
