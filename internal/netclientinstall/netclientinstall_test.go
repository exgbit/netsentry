package netclientinstall

import "testing"

func TestDownloadURL(t *testing.T) {
	got := DownloadURL("v1.6.0")
	want := "https://github.com/gravitl/netclient/releases/download/v1.6.0/netclient-windows-amd64.exe"
	if got != want {
		t.Errorf("DownloadURL(%q) = %q, want %q", "v1.6.0", got, want)
	}
}
