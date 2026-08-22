package selfupdate

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"testing"
	"time"
)

func TestVerifyManifestSignature(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pubHex := hex.EncodeToString(pub)
	manifest := []byte(`{"version":"0.6.1","files":{}}`)
	sigHex := hex.EncodeToString(ed25519.Sign(priv, manifest)) + "\n"

	if err := VerifyManifestSignature(manifest, sigHex, pubHex); err != nil {
		t.Errorf("合法签名应通过: %v", err)
	}
	if err := VerifyManifestSignature([]byte(`{"version":"9.9.9","files":{}}`), sigHex, pubHex); err == nil {
		t.Error("清单被篡改后必须拒绝")
	}
	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)
	if err := VerifyManifestSignature(manifest, sigHex, hex.EncodeToString(otherPub)); err == nil {
		t.Error("用错误公钥必须拒绝")
	}
	if err := VerifyManifestSignature(manifest, "not-hex", pubHex); err == nil {
		t.Error("签名不是合法 hex 必须拒绝")
	}
}

func TestIsNewerVersion(t *testing.T) {
	cases := []struct {
		candidate, current string
		want               bool
		wantErr            bool
	}{
		{"0.6.1", "0.6.0", true, false},
		{"0.6.0", "0.6.0", false, false},
		{"0.5.9", "0.6.0", false, false},
		{"0.10.0", "0.9.9", true, false},
		{"1.0.0", "0.99.99", true, false},
		{"abc", "0.6.0", false, true},
		{"0.6.0-test", "0.6.0", false, true},
	}
	for _, c := range cases {
		got, err := IsNewerVersion(c.candidate, c.current)
		if (err != nil) != c.wantErr {
			t.Errorf("IsNewerVersion(%q,%q) err=%v, wantErr=%v", c.candidate, c.current, err, c.wantErr)
			continue
		}
		if got != c.want {
			t.Errorf("IsNewerVersion(%q,%q)=%v, want %v", c.candidate, c.current, got, c.want)
		}
	}
}

func TestParseManifest_Valid(t *testing.T) {
	sum := SHA256Hex([]byte("hello"))
	m, err := ParseManifest([]byte(`{"version":"0.6.0","files":{"netsentry.exe":"` + sum + `"}}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Version != "0.6.0" || m.Files["netsentry.exe"] != sum {
		t.Errorf("unexpected manifest: %+v", m)
	}
}

func TestParseManifest_Invalid(t *testing.T) {
	cases := map[string]string{
		"not json":       `{oops`,
		"no version":     `{"files":{"a.exe":"` + SHA256Hex(nil) + `"}}`,
		"no files":       `{"version":"1.0.0"}`,
		"bad sha length": `{"version":"1.0.0","files":{"a.exe":"abc"}}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseManifest([]byte(body)); err == nil {
				t.Errorf("expected error for %s, got nil", name)
			}
		})
	}
}

func TestShouldCheck(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	interval := 20 * time.Hour
	cases := []struct {
		name  string
		stamp string
		want  bool
	}{
		{"never checked (empty)", "", true},
		{"garbage stamp", "not-a-number", true},
		{"checked just now", "1800000000", false},
		{"checked 1 hour ago", "1799996400", false},
		{"checked 21 hours ago", "1799924400", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ShouldCheck(c.stamp, now, interval); got != c.want {
				t.Errorf("ShouldCheck(%q) = %v, want %v", c.stamp, got, c.want)
			}
		})
	}
}
