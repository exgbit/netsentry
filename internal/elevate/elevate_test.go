package elevate

import "testing"

func TestRelaunchCommand_SingleArg(t *testing.T) {
	got := relaunchCommand(`C:\ProgramData\netclient-guard\netclient-guard.exe`, []string{"install"})
	want := `$p = Start-Process -FilePath 'C:\ProgramData\netclient-guard\netclient-guard.exe' -ArgumentList 'install' -Verb RunAs -Wait -PassThru; exit $p.ExitCode`
	if got != want {
		t.Errorf("relaunchCommand() = %q, want %q", got, want)
	}
}

func TestRelaunchCommand_MultipleArgs(t *testing.T) {
	got := relaunchCommand(`C:\ProgramData\netclient-guard\netclient-guard.exe`, []string{"uninstall", "--purge"})
	want := `$p = Start-Process -FilePath 'C:\ProgramData\netclient-guard\netclient-guard.exe' -ArgumentList 'uninstall','--purge' -Verb RunAs -Wait -PassThru; exit $p.ExitCode`
	if got != want {
		t.Errorf("relaunchCommand() = %q, want %q", got, want)
	}
}

func TestRelaunchCommand_NoArgs(t *testing.T) {
	got := relaunchCommand(`C:\ProgramData\netclient-guard\netclient-guard.exe`, nil)
	want := `$p = Start-Process -FilePath 'C:\ProgramData\netclient-guard\netclient-guard.exe' -Verb RunAs -Wait -PassThru; exit $p.ExitCode`
	if got != want {
		t.Errorf("relaunchCommand() = %q, want %q", got, want)
	}
}
