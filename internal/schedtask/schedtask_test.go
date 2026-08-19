package schedtask

import (
	"encoding/xml"
	"reflect"
	"strings"
	"testing"
)

const testExePath = `C:\ProgramData\netclient-guard\netclient-guard.exe`

func TestBackupTaskArgs(t *testing.T) {
	got := BackupTaskArgs(testExePath)
	want := []string{
		"/Create", "/TN", "NetclientGuardBackup",
		"/TR", `"C:\ProgramData\netclient-guard\netclient-guard.exe" backup`,
		"/SC", "MINUTE", "/MO", "30",
		"/RU", "SYSTEM", "/RL", "HIGHEST", "/F",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BackupTaskArgs() = %#v, want %#v", got, want)
	}
}

func TestWatchTaskArgs(t *testing.T) {
	got := WatchTaskArgs(testExePath)
	want := []string{
		"/Create", "/TN", "NetclientGuardWatch",
		"/TR", `"C:\ProgramData\netclient-guard\netclient-guard.exe" watch`,
		"/SC", "MINUTE", "/MO", "5",
		"/RU", "SYSTEM", "/RL", "HIGHEST", "/F",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("WatchTaskArgs() = %#v, want %#v", got, want)
	}
}

func TestWatchOnStartTaskArgs(t *testing.T) {
	got := WatchOnStartTaskArgs(testExePath)
	want := []string{
		"/Create", "/TN", "NetclientGuardWatchOnStart",
		"/TR", `"C:\ProgramData\netclient-guard\netclient-guard.exe" watch`,
		"/SC", "ONSTART", "/DELAY", "0001:00",
		"/RU", "SYSTEM", "/RL", "HIGHEST", "/F",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("WatchOnStartTaskArgs() = %#v, want %#v", got, want)
	}
}

func TestResumeTriggerTaskXML(t *testing.T) {
	got := ResumeTriggerTaskXML(testExePath)

	// 确认是合法 XML。
	var doc struct {
		XMLName xml.Name `xml:"Task"`
	}
	if err := xml.Unmarshal([]byte(got), &doc); err != nil {
		t.Fatalf("ResumeTriggerTaskXML() 不是合法 XML: %v", err)
	}

	if !strings.Contains(got, testExePath) {
		t.Errorf("ResumeTriggerTaskXML() 应包含 exePath %q, got: %s", testExePath, got)
	}
	if !strings.Contains(got, "Power-Troubleshooter") {
		t.Errorf("ResumeTriggerTaskXML() 应包含 %q, got: %s", "Power-Troubleshooter", got)
	}
	if !strings.Contains(got, "EventID=1") {
		t.Errorf("ResumeTriggerTaskXML() 应包含 %q, got: %s", "EventID=1", got)
	}
}

func TestAllTaskNames(t *testing.T) {
	got := AllTaskNames()
	if len(got) != 4 {
		t.Fatalf("AllTaskNames() 长度 = %d, want 4", len(got))
	}
	want := []string{
		"NetclientGuardBackup",
		"NetclientGuardWatch",
		"NetclientGuardWatchOnStart",
		"NetclientGuardWatchOnResume",
	}
	for _, name := range want {
		found := false
		for _, g := range got {
			if g == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("AllTaskNames() 缺少 %q, got: %#v", name, got)
		}
	}
}

func TestIsTaskNotFoundOutput(t *testing.T) {
	cases := []struct {
		name   string
		output string
		want   bool
	}{
		{
			name:   "英文文案",
			output: "ERROR: The system cannot find the file specified.",
			want:   true,
		},
		{
			name:   "中文文案",
			output: "错误: 系统找不到指定的文件。",
			want:   true,
		},
		{
			name:   "真机实测输出(简体中文, schtasks /Delete /TN 一个不存在的任务名 /F)",
			output: "错误: 系统找不到指定的文件。\n",
			want:   true,
		},
		{
			name:   "无关失败不应误判为任务不存在",
			output: "ERROR: Access is denied.",
			want:   false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isTaskNotFoundOutput(c.output); got != c.want {
				t.Errorf("isTaskNotFoundOutput(%q) = %v, want %v", c.output, got, c.want)
			}
		})
	}
}
