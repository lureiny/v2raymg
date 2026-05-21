package http

import (
	"testing"
	"time"
)

func TestFormatFloat2dp(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "0"},
		{1, "1"},
		{1.5, "1.5"},
		{1.50, "1.5"},
		{1.53, "1.53"},
		{1.25, "1.25"},
		{1.40625, "1.41"},      // exactly representable; rounds .40625 → 1.41
		{1023.99, "1023.99"},
	}
	for _, c := range cases {
		got := formatFloat2dp(c.in)
		if got != c.want {
			t.Errorf("formatFloat2dp(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestAutoUnit(t *testing.T) {
	cases := []struct {
		v    int64
		want string
	}{
		{0, "0 B"},
		{1, "1 B"},
		{1023, "1023 B"},
		{1024, "1 KB"},
		{1536, "1.5 KB"},                               // 1.5 KB
		{1024*1024 - 1, "1024 KB"},                     // boundary just under 1 MB
		{1024 * 1024, "1 MB"},                          // exactly 1 MB
		{1024*1024*3/2 + 1, "1.5 MB"},                  // ~1.5 MB
		{1024 * 1024 * 1024, "1 GB"},                   // exactly 1 GB
		{1024 * 1024 * 1024 * 5 / 2, "2.5 GB"},         // ~2.5 GB
		{1024 * 1024 * 1024 * 1024, "1024 GB"},         // 1 TB, still GB by spec
	}
	for _, c := range cases {
		got := autoUnit(c.v)
		if got != c.want {
			t.Errorf("autoUnit(%d) = %q, want %q", c.v, got, c.want)
		}
	}
}

func TestBuildSubUserInfoVarMap_Expiry(t *testing.T) {
	// Known expiry → "2006-01-02 15:04:05" in local TZ.
	in := subUserInfoVars{
		Username:   "alice",
		Upload:     1638257504,
		Download:   13418441583,
		Total:      1073839341568,
		ExpireUnix: 1791390742,
		ExpiryTime: time.Unix(1791390742, 0),
	}
	m := buildSubUserInfoVarMap(in)

	if m["username"] != "alice" {
		t.Errorf("username = %q, want alice", m["username"])
	}
	if m["upload"] != "1638257504" {
		t.Errorf("upload = %q", m["upload"])
	}
	if m["expire"] != "1791390742" {
		t.Errorf("expire = %q", m["expire"])
	}
	wantExpire := time.Unix(1791390742, 0).Local().Format("2006-01-02 15:04:05")
	if m["expire_string"] != wantExpire {
		t.Errorf("expire_string = %q, want %q", m["expire_string"], wantExpire)
	}
}

func TestBuildSubUserInfoVarMap_NeverExpires(t *testing.T) {
	m := buildSubUserInfoVarMap(subUserInfoVars{Username: "bob"})
	if m["expire"] != "-1" {
		t.Errorf("expire = %q, want -1", m["expire"])
	}
	if m["expire_string"] != "never" {
		t.Errorf("expire_string = %q, want never", m["expire_string"])
	}
}

func TestBuildSubUserInfoVarMap_ExpireNegative(t *testing.T) {
	// Defensive: a non-zero but non-positive ExpireUnix also collapses to -1.
	m := buildSubUserInfoVarMap(subUserInfoVars{ExpireUnix: -42})
	if m["expire"] != "-1" {
		t.Errorf("expire (negative input) = %q, want -1", m["expire"])
	}
}

func TestBuildSubUserInfoVarMap_TotalNonPositive(t *testing.T) {
	// Total <= 0 → "-1" for the raw `total` only.
	for _, total := range []int64{0, -1, -1024} {
		m := buildSubUserInfoVarMap(subUserInfoVars{Total: total})
		if m["total"] != "-1" {
			t.Errorf("total(%d) = %q, want -1", total, m["total"])
		}
	}

	// Total = 0 → unit variants render as their natural zero (no sentinel),
	// so unit suffixes stay meaningful when the format uses them.
	m := buildSubUserInfoVarMap(subUserInfoVars{Total: 0})
	if m["total_kb"] != "0" || m["total_mb"] != "0" || m["total_gb"] != "0" {
		t.Errorf("unit variants for Total=0; got kb=%q mb=%q gb=%q",
			m["total_kb"], m["total_mb"], m["total_gb"])
	}
	if m["total_auto"] != "0 B" {
		t.Errorf("total_auto for Total=0 = %q, want 0 B", m["total_auto"])
	}

	// Positive Total keeps its raw byte count.
	m = buildSubUserInfoVarMap(subUserInfoVars{Total: 1024})
	if m["total"] != "1024" {
		t.Errorf("total(1024) = %q, want 1024", m["total"])
	}
}

func TestRenderSubUserInfoFormat_Default(t *testing.T) {
	in := subUserInfoVars{
		Upload:     1638257504,
		Download:   13418441583,
		Total:      1073839341568,
		ExpireUnix: 1791390742,
		ExpiryTime: time.Unix(1791390742, 0),
	}
	got := renderSubUserInfoFormat(DefaultSubUserInfoFormat, buildSubUserInfoVarMap(in))
	want := "upload=1638257504; download=13418441583; total=1073839341568; expire=1791390742"
	if got != want {
		t.Errorf("render(default) =\n  %q\nwant\n  %q", got, want)
	}
}

func TestRenderSubUserInfoFormat_UnknownVar(t *testing.T) {
	vars := buildSubUserInfoVarMap(subUserInfoVars{Upload: 1024})
	// Unknown ${nope} → empty; known ${upload_auto} → "1 KB".
	got := renderSubUserInfoFormat("u=${upload_auto}; x=${nope}", vars)
	want := "u=1 KB; x="
	if got != want {
		t.Errorf("render = %q, want %q", got, want)
	}
}

func TestIsClashClient(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"clash", true},
		{"Clash", true},
		{"clash.meta", true},
		{"ClashX Pro/1.95.0 (com.west2online.ClashX; build:1.95.0; macOS 14.4)", true},
		{"mihomo party Clash", true},
		{"surge", false},
		{"", false},
		{"qv2ray", false},
		{"sing-box", false},
	}
	for _, c := range cases {
		if got := isClashClient(c.in); got != c.want {
			t.Errorf("isClashClient(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestStripClashEmptyFields(t *testing.T) {
	cases := []struct {
		name                    string
		header                  string
		dropTotal, dropExpire   bool
		want                    string
	}{
		{
			name:       "nothing to drop",
			header:     "upload=10; download=20; total=100; expire=999",
			dropTotal:  false,
			dropExpire: false,
			want:       "upload=10; download=20; total=100; expire=999",
		},
		{
			name:       "drop total only",
			header:     "upload=10; download=20; total=-1; expire=999",
			dropTotal:  true,
			dropExpire: false,
			want:       "upload=10; download=20; expire=999",
		},
		{
			name:       "drop expire only",
			header:     "upload=10; download=20; total=100; expire=-1",
			dropTotal:  false,
			dropExpire: true,
			want:       "upload=10; download=20; total=100",
		},
		{
			name:       "drop both",
			header:     "upload=10; download=20; total=-1; expire=-1",
			dropTotal:  true,
			dropExpire: true,
			want:       "upload=10; download=20",
		},
		{
			name:       "custom key NOT stripped",
			header:     "up=10; quota=-1; deadline=-1",
			dropTotal:  true,
			dropExpire: true,
			want:       "up=10; quota=-1; deadline=-1",
		},
		{
			name:       "key match is exact",
			header:     "total_kb=0; subtotal=1; total=-1",
			dropTotal:  true,
			dropExpire: false,
			want:       "total_kb=0; subtotal=1",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := stripClashEmptyFields(c.header, c.dropTotal, c.dropExpire)
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestResolveSubUserInfoFormat(t *testing.T) {
	cases := []struct {
		query, config, want string
	}{
		{"", "", DefaultSubUserInfoFormat},
		{"", "cfg-fmt", "cfg-fmt"},
		{"q-fmt", "cfg-fmt", "q-fmt"},
		{"q-fmt", "", "q-fmt"},
		{"   ", "  cfg  ", "cfg"},
		{"  q  ", "cfg", "q"},
	}
	for _, c := range cases {
		got := resolveSubUserInfoFormat(c.query, c.config)
		if got != c.want {
			t.Errorf("resolve(%q,%q) = %q, want %q", c.query, c.config, got, c.want)
		}
	}
}
