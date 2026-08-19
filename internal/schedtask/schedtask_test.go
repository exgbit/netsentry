package schedtask

import (
	"encoding/xml"
	"io"
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

	// 确认是合法 XML。模板声明的 encoding 是 UTF-16,但字符串本身、以及后续写盘
	// 都是原样的 UTF-8 字节(这个不一致是故意的,详见 schedtask.go 里
	// resumeTriggerTaskXMLTemplate 上方的注释:真机实测 schtasks.exe 就是要这么
	// 声明才认)。Go 标准库的 xml.Unmarshal 默认会严格校验声明的 encoding,不认
	// "UTF-16" 之外配一个 CharsetReader 就直接报错——这里给一个直接透传字节的
	// CharsetReader,只关心"整体上是不是合法 XML 结构",不去校验声明是否与实际
	// 字节编码一致(和 schtasks.exe 实测的宽松行为一致)。
	dec := xml.NewDecoder(strings.NewReader(got))
	dec.CharsetReader = func(charset string, input io.Reader) (io.Reader, error) {
		return input, nil
	}
	var doc struct {
		XMLName xml.Name `xml:"Task"`
	}
	if err := dec.Decode(&doc); err != nil {
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
