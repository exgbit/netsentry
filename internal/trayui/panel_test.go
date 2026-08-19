package trayui

import "testing"

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
