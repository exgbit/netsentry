package diag

import (
	"archive/zip"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeNetclientJSON_StripsSecrets(t *testing.T) {
	input := []byte(`{
		"id": "abc-123",
		"version": "v1.6.0",
		"os": "windows",
		"hostpass": "SUPER-SECRET-PASSWORD",
		"privatekey": [1,2,3,4],
		"traffickeyprivate": "base64secret",
		"publickey": [5,6,7,8]
	}`)

	out, err := SanitizeNetclientJSON(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := string(out)
	if strings.Contains(s, "SUPER-SECRET-PASSWORD") {
		t.Errorf("sanitized output must not contain hostpass value, got: %s", s)
	}
	if strings.Contains(s, "hostpass") || strings.Contains(s, "privatekey") ||
		strings.Contains(s, "traffickeyprivate") || strings.Contains(s, "publickey") {
		t.Errorf("sanitized output must not contain secret field names, got: %s", s)
	}
	if !strings.Contains(s, "abc-123") {
		t.Errorf("sanitized output should keep the id field, got: %s", s)
	}
}

func TestSanitizeServersJSON_StripsSecrets(t *testing.T) {
	input := []byte(`{
		"tomtoc.cn": {
			"mqid": "abc-123",
			"name": "tomtoc.cn",
			"accesskey": "SUPER-SECRET-KEY"
		}
	}`)

	out, err := SanitizeServersJSON(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := string(out)
	if strings.Contains(s, "SUPER-SECRET-KEY") || strings.Contains(s, "accesskey") {
		t.Errorf("sanitized output must not contain accesskey, got: %s", s)
	}
	if !strings.Contains(s, "abc-123") {
		t.Errorf("sanitized output should keep the mqid value, got: %s", s)
	}
}

func TestBundle_WritesReadableZip(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "diag.zip")
	sources := []Source{
		{Name: "guard.log", Data: []byte("hello log")},
		{Name: "config-summary/netclient.json", Data: []byte(`{"id":"abc"}`)},
	}

	if err := Bundle(sources, outPath); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r, err := zip.OpenReader(outPath)
	if err != nil {
		t.Fatalf("failed to open produced zip: %v", err)
	}
	defer r.Close()

	got := map[string]string{}
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open zip entry %s: %v", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read zip entry %s: %v", f.Name, err)
		}
		got[f.Name] = string(data)
	}

	if got["guard.log"] != "hello log" {
		t.Errorf("guard.log content mismatch, got %q", got["guard.log"])
	}
	if got["config-summary/netclient.json"] != `{"id":"abc"}` {
		t.Errorf("netclient.json content mismatch, got %q", got["config-summary/netclient.json"])
	}
}
