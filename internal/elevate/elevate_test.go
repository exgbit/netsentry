package elevate

import (
	"strings"
	"testing"
)

func TestRelaunchCommand_SingleArg(t *testing.T) {
	got := relaunchCommand(`C:\ProgramData\netclient-guard\netclient-guard.exe`, []string{"install"})
	want := `try { $p = Start-Process -FilePath 'C:\ProgramData\netclient-guard\netclient-guard.exe' -ArgumentList 'install' -Verb RunAs -Wait -PassThru -ErrorAction Stop; exit $p.ExitCode } catch { exit 1 }`
	if got != want {
		t.Errorf("relaunchCommand() = %q, want %q", got, want)
	}
}

func TestRelaunchCommand_MultipleArgs(t *testing.T) {
	got := relaunchCommand(`C:\ProgramData\netclient-guard\netclient-guard.exe`, []string{"uninstall", "--purge"})
	want := `try { $p = Start-Process -FilePath 'C:\ProgramData\netclient-guard\netclient-guard.exe' -ArgumentList 'uninstall','--purge' -Verb RunAs -Wait -PassThru -ErrorAction Stop; exit $p.ExitCode } catch { exit 1 }`
	if got != want {
		t.Errorf("relaunchCommand() = %q, want %q", got, want)
	}
}

func TestRelaunchCommand_NoArgs(t *testing.T) {
	got := relaunchCommand(`C:\ProgramData\netclient-guard\netclient-guard.exe`, nil)
	want := `try { $p = Start-Process -FilePath 'C:\ProgramData\netclient-guard\netclient-guard.exe' -Verb RunAs -Wait -PassThru -ErrorAction Stop; exit $p.ExitCode } catch { exit 1 }`
	if got != want {
		t.Errorf("relaunchCommand() = %q, want %q", got, want)
	}
}

// TestRelaunchCommand_ArgWithSingleQuote 覆盖 9c 面板把自由文本的 setup-netclient
// token 接到这条链路上之后的场景:token 里带一个单引号(比如 it's-a-token)必须被
// 转义成两个单引号,否则会提前把 PowerShell 单引号字符串截断,搞坏整条 relaunch 命令。
func TestRelaunchCommand_ArgWithSingleQuote(t *testing.T) {
	got := relaunchCommand(`C:\ProgramData\netclient-guard\netclient-guard.exe`, []string{"setup-netclient", "-t", "it's-a-token"})
	want := `try { $p = Start-Process -FilePath 'C:\ProgramData\netclient-guard\netclient-guard.exe' -ArgumentList 'setup-netclient','-t','it''s-a-token' -Verb RunAs -Wait -PassThru -ErrorAction Stop; exit $p.ExitCode } catch { exit 1 }`
	if got != want {
		t.Errorf("relaunchCommand() = %q, want %q", got, want)
	}
	// 附加检查:确认单引号确实被"打包"进同一对外层单引号里(双写成 '' 之后,
	// 词法上仍然只是这一个参数字符串内部的转义,不会多出一对引号边界)。
	const wantArg = `'it''s-a-token'`
	if !strings.Contains(got, wantArg) {
		t.Errorf("relaunchCommand() output %q does not contain expected quoted arg %q", got, wantArg)
	}
}
