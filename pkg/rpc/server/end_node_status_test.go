package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCountLinesInFile(t *testing.T) {
	largeLine := fmt.Sprintf("   0: 0100007F:1F90 00000000:0000 0A %s\n", strings.Repeat("0", 50))
	var largeBuilder strings.Builder
	largeBuilder.WriteString("  sl  local_address rem_address st\n")
	for i := 0; i < 1200; i++ {
		largeBuilder.WriteString(largeLine)
	}

	tests := []struct {
		name    string
		content string
		missing bool
		want    int
	}{
		{
			name:    "missing_file",
			missing: true,
			want:    0,
		},
		{
			name:    "empty_file",
			content: "",
			want:    0,
		},
		{
			name:    "header_only_no_newline",
			content: "  sl  local_address rem_address st",
			want:    0,
		},
		{
			name:    "header_and_blank_whitespace_lines",
			content: "  sl  local_address\n   \n\t\r\n\n",
			want:    0,
		},
		{
			name:    "header_and_crlf_data_lines",
			content: "header\r\n 0: 0100\r\n 1: 0200\r\n",
			want:    2,
		},
		{
			name:    "data_line_without_trailing_newline",
			content: "header\nline1\nline2",
			want:    2,
		},
		{
			name:    "multi_chunk_over_80kb",
			content: largeBuilder.String(),
			want:    1200,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "proc_net_tcp")
			if !tc.missing {
				if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
					t.Fatalf("WriteFile: %v", err)
				}
			}
			if got := countLinesInFile(path); got != tc.want {
				t.Fatalf("countLinesInFile() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestCountLinesInFile_NoLargePerCallAlloc(t *testing.T) {
	var b strings.Builder
	b.WriteString("  sl  local_address rem_address   st tx_queue rx_queue\n")
	line := fmt.Sprintf("   0: 0100007F:1F90 00000000:0000 0A %s\n", strings.Repeat("0", 40))
	for i := 0; i < 200; i++ {
		b.WriteString(line)
	}
	path := filepath.Join(t.TempDir(), "tcp")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Warm up pool so steady-state benchmark measures per-call allocations only.
	if got := countLinesInFile(path); got != 200 {
		t.Fatalf("warmup count = %d, want 200", got)
	}

	res := testing.Benchmark(func(bench *testing.B) {
		for i := 0; i < bench.N; i++ {
			if got := countLinesInFile(path); got != 200 {
				bench.Fatalf("unexpected count: %d", got)
			}
		}
	})
	maxAllocs := int64(4096)
	if raceDetectorEnabled {
		// Go's race runtime randomly drops ~25% of sync.Pool.Put calls, so a
		// 32 KB pooled buffer averages ~8-12 KB/op under -race while still
		// catching any 256 KB per-call regression.
		maxAllocs = 64 * 1024
	}
	if allocs := res.AllocedBytesPerOp(); allocs >= maxAllocs {
		t.Fatalf("countLinesInFile allocated %d B/op (expected < %d B/op without 256 KB per-call buffer)", allocs, maxAllocs)
	}
}
