package guardconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseNetclientID(t *testing.T) {
	data := []byte(`{"id":"40445c24-cf4d-4653-bf93-1ba975fc5faa","name":"Justin-Win"}`)
	id, err := ParseNetclientID(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "40445c24-cf4d-4653-bf93-1ba975fc5faa" {
		t.Errorf("got id=%q, want 40445c24-cf4d-4653-bf93-1ba975fc5faa", id)
	}
}

func TestParseNetclientID_Empty(t *testing.T) {
	id, err := ParseNetclientID([]byte(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "" {
		t.Errorf("got id=%q, want empty string", id)
	}
}

func TestParseServerMQIDs(t *testing.T) {
	data := []byte(`{"tomtoc.cn":{"mqid":"40445c24-cf4d-4653-bf93-1ba975fc5faa","name":"tomtoc.cn"}}`)
	mqids, err := ParseServerMQIDs(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := mqids["tomtoc.cn"]; got != "40445c24-cf4d-4653-bf93-1ba975fc5faa" {
		t.Errorf("got mqid=%q, want 40445c24-cf4d-4653-bf93-1ba975fc5faa", got)
	}
}

func TestIsConsistent(t *testing.T) {
	cases := []struct {
		name  string
		id    string
		mqids map[string]string
		want  bool
	}{
		{"matching", "abc", map[string]string{"tomtoc.cn": "abc"}, true},
		{"mismatch", "abc", map[string]string{"tomtoc.cn": "xyz"}, false},
		{"no servers", "abc", map[string]string{}, false},
		{"empty id", "", map[string]string{"tomtoc.cn": "abc"}, false},
		{"multiple servers one mismatch", "abc", map[string]string{"a": "abc", "b": "xyz"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsConsistent(c.id, c.mqids); got != c.want {
				t.Errorf("IsConsistent(%q, %v) = %v, want %v", c.id, c.mqids, got, c.want)
			}
		})
	}
}

func TestLoad_BothFilesConsistent(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "netclient.json"), `{"id":"abc"}`)
	writeFile(t, filepath.Join(dir, "servers.json"), `{"tomtoc.cn":{"mqid":"abc","name":"tomtoc.cn"}}`)

	result, err := Load(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.NetclientExists || !result.ServersExists {
		t.Fatalf("expected both files to exist, got %+v", result)
	}
	if !result.Consistent {
		t.Errorf("expected consistent, got %+v", result)
	}
}

func TestLoad_Mismatch(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "netclient.json"), `{"id":"abc"}`)
	writeFile(t, filepath.Join(dir, "servers.json"), `{"tomtoc.cn":{"mqid":"different","name":"tomtoc.cn"}}`)

	result, err := Load(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Consistent {
		t.Errorf("expected inconsistent, got %+v", result)
	}
}

func TestLoad_NetclientMissing(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "servers.json"), `{"tomtoc.cn":{"mqid":"abc","name":"tomtoc.cn"}}`)

	result, err := Load(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NetclientExists {
		t.Errorf("expected netclient.json to be reported missing")
	}
	if result.Consistent {
		t.Errorf("expected inconsistent when netclient.json missing")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
