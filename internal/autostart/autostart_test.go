package autostart

import (
	"reflect"
	"testing"
)

const testExePath = `C:\ProgramData\NetSentry\netsentry-tray.exe`

func TestRegisterArgs(t *testing.T) {
	got := RegisterArgs(testExePath)
	want := []string{
		"add", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`,
		"/v", "NetSentryTray", "/t", "REG_SZ",
		"/d", `"C:\ProgramData\NetSentry\netsentry-tray.exe" tray`, "/f",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("RegisterArgs() = %#v, want %#v", got, want)
	}
}

func TestUnregisterArgs(t *testing.T) {
	got := UnregisterArgs()
	want := []string{
		"delete", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`,
		"/v", "NetSentryTray", "/f",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("UnregisterArgs() = %#v, want %#v", got, want)
	}
}

func TestIsValueNotFoundOutput(t *testing.T) {
	cases := []struct {
		name   string
		output string
		want   bool
	}{
		{
			name:   "英文文案",
			output: "ERROR: The system was unable to find the specified registry key or value.",
			want:   true,
		},
		{
			name:   "中文文案",
			output: "错误: 系统找不到指定的注册表项或值。",
			want:   true,
		},
		{
			name:   "真机实测输出(简体中文 Windows 11, reg delete 一个不存在的值)",
			output: "错误: 系统找不到指定的注册表项或值。\n",
			want:   true,
		},
		{
			name:   "无关失败不应误判为值不存在",
			output: "ERROR: Access is denied.",
			want:   false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isValueNotFoundOutput(c.output); got != c.want {
				t.Errorf("isValueNotFoundOutput(%q) = %v, want %v", c.output, got, c.want)
			}
		})
	}
}
