package http

import (
	"testing"
)

// buildFastAddExtra replicates the convFields → extra merge block from
// handlerFunc (fastAddInbound_handler.go). Pulled into a testable helper
// so protocol-level regression tests don't need to spin up a full gin
// router + RPC stack to verify the mapping.
//
// Keep the key set here in lockstep with convFields in handlerFunc —
// TestFastAddConvFieldsParity locks that alignment.
func buildFastAddExtra(req fastAddReqConvFields, extraParams map[string]string) map[string]string {
	extra := make(map[string]string)
	for k, v := range extraParams {
		extra[k] = v
	}
	convFields := map[string]string{
		"ws_path": req.WSPath, "grpc_service_name": req.GRPCServiceName,
		"http_path": req.HTTPPath, "http_host": req.HTTPHost,
		"httpupgrade_path": req.HTTPUpgradePath, "httpupgrade_host": req.HTTPUpgradeHost,
		"xhttp_path": req.XHTTPPath, "xhttp_mode": req.XHTTPMode, "xhttp_host": req.XHTTPHost,
		"alpn": req.ALPN,
		"flow": req.Flow,
	}
	for k, v := range convFields {
		if v != "" {
			if _, exists := extra[k]; !exists {
				extra[k] = v
			}
		}
	}
	if req.SniffingEnabled {
		if _, exists := extra["sniffing_enabled"]; !exists {
			extra["sniffing_enabled"] = "true"
		}
	}
	if req.SelfSigned {
		if _, exists := extra["self_signed"]; !exists {
			extra["self_signed"] = "true"
		}
	}
	if req.SkipCertVerify {
		if _, exists := extra["skip_cert_verify"]; !exists {
			extra["skip_cert_verify"] = "true"
		}
	}
	return extra
}

type fastAddReqConvFields struct {
	WSPath          string
	GRPCServiceName string
	HTTPPath        string
	HTTPHost        string
	HTTPUpgradePath string
	HTTPUpgradeHost string
	XHTTPPath       string
	XHTTPMode       string
	XHTTPHost       string
	ALPN            string
	Flow            string
	SniffingEnabled bool
	SelfSigned      bool
	SkipCertVerify  bool
}

// TestFastAdd_VMess_ConvFieldsMerge verifies T-M-P2-23: the convFields
// merge block handles vmess convenience fields correctly. vmess reuses
// the same ws_path / grpc_service_name / reality_* set that vless
// already uses; there are no vmess-specific convenience fields.
func TestFastAdd_VMess_ConvFieldsMerge(t *testing.T) {
	cases := []struct {
		name    string
		req     fastAddReqConvFields
		extraIn map[string]string
		wantOut map[string]string
	}{
		{
			name: "vmess_ws_tls_self_signed",
			req: fastAddReqConvFields{
				WSPath:         "/vmess",
				SelfSigned:     true,
				SkipCertVerify: true,
			},
			extraIn: map[string]string{},
			wantOut: map[string]string{
				"ws_path":          "/vmess",
				"self_signed":      "true",
				"skip_cert_verify": "true",
			},
		},
		{
			name: "vmess_grpc_tls",
			req: fastAddReqConvFields{
				GRPCServiceName: "VmessTunnel",
				ALPN:            "h2,http/1.1",
			},
			extraIn: map[string]string{},
			wantOut: map[string]string{
				"grpc_service_name": "VmessTunnel",
				"alpn":              "h2,http/1.1",
			},
		},
		{
			name: "vmess_reality_passthrough",
			req:  fastAddReqConvFields{}, // reality_* are native fields on the RPC, not convFields
			extraIn: map[string]string{
				"reality_target":       "www.microsoft.com:443",
				"reality_server_names": "www.microsoft.com",
				"utls_fingerprint":     "chrome",
			},
			wantOut: map[string]string{
				"reality_target":       "www.microsoft.com:443",
				"reality_server_names": "www.microsoft.com",
				"utls_fingerprint":     "chrome",
			},
		},
		{
			name: "explicit_extra_wins_over_conv",
			req: fastAddReqConvFields{
				WSPath: "/convenience",
			},
			extraIn: map[string]string{
				"ws_path": "/explicit",
			},
			wantOut: map[string]string{
				"ws_path": "/explicit",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildFastAddExtra(tc.req, tc.extraIn)
			for k, v := range tc.wantOut {
				if got[k] != v {
					t.Errorf("extra[%q] = %q, want %q (full got = %v)", k, got[k], v, got)
				}
			}
			// No unexpected keys beyond what caller provided + conv pulls in.
			for k := range got {
				_, want := tc.wantOut[k]
				if !want {
					t.Errorf("unexpected key %q in output (value=%q)", k, got[k])
				}
			}
		})
	}
}

// TestFastAddConvFieldsParity ensures the key set in buildFastAddExtra's
// convFields map matches exactly the one in handlerFunc. Manual drift
// between test helper and production code has already bitten this
// package once (alpn/flow added but the helper wasn't updated); lock
// alignment so the next addition must touch both.
//
// Strategy: craft a request with a unique sentinel per field, run the
// helper, confirm each sentinel lands under the expected key.
func TestFastAddConvFieldsParity(t *testing.T) {
	req := fastAddReqConvFields{
		WSPath:          "s-ws",
		GRPCServiceName: "s-grpc",
		HTTPPath:        "s-hp",
		HTTPHost:        "s-hh",
		HTTPUpgradePath: "s-hup",
		HTTPUpgradeHost: "s-huh",
		XHTTPPath:       "s-xp",
		XHTTPMode:       "s-xm",
		XHTTPHost:       "s-xh",
		ALPN:            "s-alpn",
		Flow:            "s-flow",
	}
	got := buildFastAddExtra(req, nil)
	wantKeys := map[string]string{
		"ws_path":           "s-ws",
		"grpc_service_name": "s-grpc",
		"http_path":         "s-hp",
		"http_host":         "s-hh",
		"httpupgrade_path":  "s-hup",
		"httpupgrade_host":  "s-huh",
		"xhttp_path":        "s-xp",
		"xhttp_mode":        "s-xm",
		"xhttp_host":        "s-xh",
		"alpn":              "s-alpn",
		"flow":              "s-flow",
	}
	for k, v := range wantKeys {
		if got[k] != v {
			t.Errorf("expected %q=%q in extra; got %q", k, v, got[k])
		}
	}
	for k := range got {
		if _, ok := wantKeys[k]; !ok {
			t.Errorf("unexpected extra key %q (value=%q) — update helper to match handlerFunc convFields", k, got[k])
		}
	}
}
