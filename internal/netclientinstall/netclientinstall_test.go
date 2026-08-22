package netclientinstall

import "testing"

func TestDownloadURL(t *testing.T) {
	got := DownloadURL("v1.6.0")
	want := "https://downloads.netmaker.io/releases/download/v1.6.0/netclient-windows-amd64.exe"
	if got != want {
		t.Errorf("DownloadURL(%q) = %q, want %q", "v1.6.0", got, want)
	}
}

func TestDecideExisting(t *testing.T) {
	cases := []struct {
		name                                   string
		exeExists, configConsistent, svcExists bool
		want                                   ExistingAction
	}{
		{"没装过", false, false, false, FreshInstall},
		{"没装过但残留配置", false, true, true, FreshInstall},
		{"已装且健康", true, true, true, KeepAndGuard},
		{"已装,配置一致但服务没注册", true, true, false, WipeAndReinstall},
		{"已装,服务在但配置不一致", true, false, true, WipeAndReinstall},
		{"已装,全坏", true, false, false, WipeAndReinstall},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DecideExisting(c.exeExists, c.configConsistent, c.svcExists); got != c.want {
				t.Errorf("DecideExisting(%v,%v,%v) = %v, want %v",
					c.exeExists, c.configConsistent, c.svcExists, got, c.want)
			}
		})
	}
}
