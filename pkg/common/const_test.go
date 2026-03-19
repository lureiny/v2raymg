package common_test

import (
	"testing"

	"github.com/lureiny/v2raymg/pkg/common"
)

func TestEndNodeType(t *testing.T) {
	if common.EndNodeType != "End" {
		t.Errorf("EndNodeType: got %q, want %q", common.EndNodeType, "End")
	}
}

func TestCenterNodeType(t *testing.T) {
	if common.CenterNodeType != "Center" {
		t.Errorf("CenterNodeType: got %q, want %q", common.CenterNodeType, "Center")
	}
}
