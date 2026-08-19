package main

import "testing"

func TestSamePath(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want bool
	}{
		{"完全相同", `C:\ProgramData\netclient-guard\netclient-guard.exe`, `C:\ProgramData\netclient-guard\netclient-guard.exe`, true},
		{"大小写不同", `c:\programdata\netclient-guard\netclient-guard.exe`, `C:\ProgramData\netclient-guard\netclient-guard.exe`, true},
		{"不同路径", `C:\Users\me\netclient-guard.exe`, `C:\ProgramData\netclient-guard\netclient-guard.exe`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := samePath(c.a, c.b); got != c.want {
				t.Errorf("samePath(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
			}
		})
	}
}

func TestHasPurgeFlag(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{"没有参数", nil, false},
		{"不含 --purge", []string{"uninstall"}, false},
		{"含 --purge", []string{"--purge"}, true},
		{"混合参数", []string{"foo", "--purge", "bar"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := hasPurgeFlag(c.args); got != c.want {
				t.Errorf("hasPurgeFlag(%#v) = %v, want %v", c.args, got, c.want)
			}
		})
	}
}
