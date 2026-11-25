package contracts_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

func TestNormalizeInboundType(t *testing.T) {
	cases := []struct {
		raw     string
		want    contracts.InboundType
		wantErr bool
	}{
		{"", contracts.InboundTypeDirect, false},
		{"   ", contracts.InboundTypeDirect, false},
		{"direct", contracts.InboundTypeDirect, false},
		{" DIRECT ", contracts.InboundTypeDirect, false},
		{"chain", contracts.InboundTypeChain, false},
		{" Chain ", contracts.InboundTypeChain, false},
		{"socks", "", true},
		{"relay", "", true},
	}

	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			got, err := contracts.NormalizeInboundType(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("NormalizeInboundType(%q) = %q, want error", tc.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeInboundType(%q) unexpected error: %v", tc.raw, err)
			}
			if got != tc.want {
				t.Errorf("NormalizeInboundType(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestValidateInboundTag(t *testing.T) {
	valid := []string{
		"a",
		"vless-in_1.2",
		"Chain_SH.01-test",
		strings.Repeat("a", 64),
		"_single_underscore",
	}
	for _, tag := range valid {
		if err := contracts.ValidateInboundTag(tag); err != nil {
			t.Errorf("ValidateInboundTag(%q) unexpected error: %v", tag, err)
		}
	}

	invalid := []string{
		"",
		strings.Repeat("a", 65),
		"__",
		"__chain_out__",
		"__chain_out__:test",
		"bad,tag",
		"bad:tag",
		"bad/tag",
		"bad tag",
		"bad\ntag",
	}
	for _, tag := range invalid {
		if err := contracts.ValidateInboundTag(tag); err == nil {
			t.Errorf("ValidateInboundTag(%q) = nil, want error", tag)
		}
	}
}

func TestNormalizeAccessTags(t *testing.T) {
	t.Run("empty_and_whitespace", func(t *testing.T) {
		for _, in := range [][]string{nil, {}, {"", "   ", "\t"}} {
			got, err := contracts.NormalizeAccessTags(in)
			if err != nil {
				t.Fatalf("NormalizeAccessTags(%v) unexpected error: %v", in, err)
			}
			if len(got) != 0 {
				t.Errorf("NormalizeAccessTags(%v) = %v, want empty", in, got)
			}
		}
	})

	t.Run("trim_dedup_sort", func(t *testing.T) {
		got, err := contracts.NormalizeAccessTags([]string{" vip ", "", "home-sh", "vip", "  alpha.1_2  "})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []string{"alpha.1_2", "home-sh", "vip"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("rejects_invalid_chars_or_length", func(t *testing.T) {
		for _, bad := range [][]string{
			{"good", "bad,tag"},
			{"bad:tag"},
			{"bad tag"},
			{"bad\ntag"},
			{strings.Repeat("x", 65)},
		} {
			if _, err := contracts.NormalizeAccessTags(bad); err == nil {
				t.Errorf("NormalizeAccessTags(%v) expected error, got nil", bad)
			}
		}
	})
}
