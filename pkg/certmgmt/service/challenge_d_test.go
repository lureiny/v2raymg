package certmgmtservice

import (
	"testing"

	"github.com/lureiny/v2raymg/pkg/certmgmt/domain"
)

func TestNormalizeChallengeType(t *testing.T) {
	cases := []struct {
		in    string
		want  domain.ChallengeType
		known bool
	}{
		{"dns01", domain.ChallengeDNS01, true},
		{"dns-01", domain.ChallengeDNS01, true},
		{"dns", domain.ChallengeDNS01, true},
		{"DNS", domain.ChallengeDNS01, true},
		{"http01", domain.ChallengeHTTP01, true},
		{"http-01", domain.ChallengeHTTP01, true},
		{"http", domain.ChallengeHTTP01, true},
		{"acme", domain.ChallengeType("acme"), false},
		{"dns-01x", domain.ChallengeType("dns-01x"), false},
	}
	for _, c := range cases {
		got, known := normalizeChallengeType(c.in)
		if got != c.want || known != c.known {
			t.Errorf("normalizeChallengeType(%q) = (%q,%v), want (%q,%v)", c.in, got, known, c.want, c.known)
		}
	}
}

// TestBuildIssueRequest_DNSAliasNotDowngraded covers finding #3: a "dns" (or
// "dns-01") config must produce a canonical dns01 request with DNS config and no
// HTTP config, so the issuer does not silently downgrade it to HTTP-01 on :80.
func TestBuildIssueRequest_DNSAliasNotDowngraded(t *testing.T) {
	for _, typ := range []string{"dns", "dns-01", "dns01"} {
		mgr := NewManagerWithIssuer(Config{
			Email: "test@example.com",
			Path:  t.TempDir(),
			Challenge: ChallengeConfig{
				Type: typ,
				DNS:  DNSConfig{ProviderName: "cloudflare"},
			},
		}, nil)
		req := mgr.buildIssueRequest([]string{"example.com"})
		if req.Challenge.Type != domain.ChallengeDNS01 {
			t.Errorf("type %q: got Challenge.Type %q, want dns01 (would downgrade to HTTP-01)", typ, req.Challenge.Type)
		}
		if req.Challenge.DNS == nil {
			t.Errorf("type %q: DNS config missing", typ)
		}
		if req.Challenge.HTTP != nil {
			t.Errorf("type %q: HTTP config unexpectedly set", typ)
		}
	}
}
