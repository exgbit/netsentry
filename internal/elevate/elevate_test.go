package elevate

import "testing"

func TestRelaunchCommand_SingleArg(t *testing.T) {
	got := relaunchCommand(`C:\ProgramData\netclient-guard\netclient-guard.exe`, []string{"install"})
	want := `Start-Process -FilePath 'C:\ProgramData\netclient-guard\netclient-guard.exe' -ArgumentList 'install' -Verb RunAs -Wait`
	if got != want {
		t.Errorf("relaunchCommand() = %q, want %q", got, want)
	}
}

func TestRelaunchCommand_MultipleArgs(t *testing.T) {
	got := relaunchCommand(`C:\ProgramData\netclient-guard\netclient-guard.exe`, []string{"uninstall", "--purge"})
	want := `Start-Process -FilePath 'C:\ProgramData\netclient-guard\netclient-guard.exe' -ArgumentList 'uninstall','--purge' -Verb RunAs -Wait`
	if got != want {
		t.Errorf("relaunchCommand() = %q, want %q", got, want)
	}
}

func TestRelaunchCommand_NoArgs(t *testing.T) {
	got := relaunchCommand(`C:\ProgramData\netclient-guard\netclient-guard.exe`, nil)
	want := `Start-Process -FilePath 'C:\ProgramData\netclient-guard\netclient-guard.exe' -Verb RunAs -Wait`
	if got != want {
		t.Errorf("relaunchCommand() = %q, want %q", got, want)
	}
}
