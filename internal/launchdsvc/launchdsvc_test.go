package launchdsvc

import "testing"

func TestParseRunning(t *testing.T) {
	cases := []struct {
		name   string
		output string
		want   bool
	}{
		{"运行中", "system/com.gravitl.netclient = {\n\tactive count = 1\n\tstate = running\n\tprogram = /usr/local/bin/netclient\n}", true},
		{"等待中(崩溃间隙)", "system/com.gravitl.netclient = {\n\tstate = waiting\n}", false},
		{"没有 state 行", "system/com.gravitl.netclient = {\n\tprogram = /usr/local/bin/netclient\n}", false},
		{"空输出", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ParseRunning(c.output); got != c.want {
				t.Errorf("ParseRunning(%q) = %v, want %v", c.output, got, c.want)
			}
		})
	}
}
