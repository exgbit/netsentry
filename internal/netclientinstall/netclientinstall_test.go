package netclientinstall

import "testing"

func TestDownloadURL(t *testing.T) {
	got := DownloadURL("v1.6.0")
	want := "https://downloads.netmaker.io/releases/download/v1.6.0/netclientbundle.exe"
	if got != want {
		t.Errorf("DownloadURL(%q) = %q, want %q", "v1.6.0", got, want)
	}
}
