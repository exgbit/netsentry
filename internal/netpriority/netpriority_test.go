package netpriority

import "testing"

func TestParseMetrics(t *testing.T) {
	got := parseMetrics("5\r\n5\r\n")
	want := []int{5, 5}
	if len(got) != len(want) {
		t.Fatalf("parseMetrics() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("parseMetrics()[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestParseMetrics_Empty(t *testing.T) {
	got := parseMetrics("")
	if len(got) != 0 {
		t.Errorf("parseMetrics(\"\") = %v, want empty", got)
	}
}

func TestNeedsFix_BelowTarget(t *testing.T) {
	fix, min := needsFix([]int{5, 5})
	if !fix {
		t.Error("needsFix = false, want true for metrics below target")
	}
	if min != 5 {
		t.Errorf("min = %d, want 5", min)
	}
}

func TestNeedsFix_OneAddressFamilyBelowTarget(t *testing.T) {
	// IPv4 过低、IPv6 已经达标的场景——只要有一条低于目标就要修。
	fix, min := needsFix([]int{5, 100})
	if !fix {
		t.Error("needsFix = false, want true when any address family is below target")
	}
	if min != 5 {
		t.Errorf("min = %d, want 5", min)
	}
}

func TestNeedsFix_AtOrAboveTarget(t *testing.T) {
	fix, _ := needsFix([]int{100, 100})
	if fix {
		t.Error("needsFix = true, want false when already at target")
	}
}

func TestNeedsFix_Empty(t *testing.T) {
	fix, min := needsFix(nil)
	if fix {
		t.Error("needsFix = true, want false when interface does not exist")
	}
	if min != 0 {
		t.Errorf("min = %d, want 0", min)
	}
}
