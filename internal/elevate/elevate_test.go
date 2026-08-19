package elevate

import "testing"

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
