package selfupdate

import (
	"testing"
	"time"
)

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
