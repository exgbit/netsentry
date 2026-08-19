package trayui

import (
	"strings"
	"testing"
)

func TestParseDiagPath(t *testing.T) {
	cases := []struct {
		name   string
		output string
		want   string
	}{
		{
			"正常输出",
			`diag bundle written to C:\Users\me\Desktop\netclient-diag-20260819-101500.zip`,
			`C:\Users\me\Desktop\netclient-diag-20260819-101500.zip`,
		},
		{
			"输出带换行和多余内容",
			"diag bundle written to C:\\Users\\me\\Desktop\\netclient-diag-20260819-101500.zip\r\nsome trailing line",
			`C:\Users\me\Desktop\netclient-diag-20260819-101500.zip`,
		},
		{"没有匹配的标记", "diag error writing bundle: boom", ""},
		{"空字符串", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseDiagPath(c.output); got != c.want {
				t.Errorf("parseDiagPath(%q) = %q, want %q", c.output, got, c.want)
			}
		})
	}
}

// TestSetupNetclientResult_EmptyOutputOnFailureGetsLogHint 覆盖"提权 relaunch 之后
// 看不到详细输出"这个已知信息缺口的兜底提示:exePath 指向一个不存在的可执行文件,
// 让 runExeCommand 必然失败且 Output 为空(既没有 stdout 也没有 stderr 可捕获),
// 这时 setupNetclientResult 应该把 Output 替换成指向 install.log 的说明,而不是
// 留一个空字符串给用户看。
func TestSetupNetclientResult_EmptyOutputOnFailureGetsLogHint(t *testing.T) {
	const installLogPath = `C:\ProgramData\netclient-guard\install.log`
	result := setupNetclientResult("/nonexistent/netclient-guard-does-not-exist", installLogPath, "some-token")

	if result.Success {
		t.Fatalf("setupNetclientResult() Success = true, want false (exePath does not exist)")
	}
	if result.Output == "" {
		t.Fatalf("setupNetclientResult() Output is empty, want a fallback hint")
	}
	if !strings.Contains(result.Output, installLogPath) {
		t.Errorf("setupNetclientResult() Output = %q, want it to mention %q", result.Output, installLogPath)
	}
}
