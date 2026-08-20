package settings

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoad_MissingFile_ReturnsDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")

	got, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, Default()) {
		t.Errorf("got %+v, want %+v", got, Default())
	}
}

func TestLoad_ValidFile_ReturnsParsedValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"connectivityTargets":["10.0.0.1","10.0.0.2"]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"10.0.0.1", "10.0.0.2"}
	if !reflect.DeepEqual(got.ConnectivityTargets, want) {
		t.Errorf("ConnectivityTargets = %v, want %v", got.ConnectivityTargets, want)
	}
}

func TestLoad_EmptyTargets_FallsBackToDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"connectivityTargets":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got.ConnectivityTargets, Default().ConnectivityTargets) {
		t.Errorf("ConnectivityTargets = %v, want default %v", got.ConnectivityTargets, Default().ConnectivityTargets)
	}
}

func TestLoad_InvalidJSON_ReturnsDefaultAndError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{not valid json`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
	if !reflect.DeepEqual(got, Default()) {
		t.Errorf("got %+v, want default %+v on parse error", got, Default())
	}
}

func TestWriteDefaultIfMissing_CreatesFileWhenAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")

	if err := WriteDefaultIfMissing(path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error reading back written file: %v", err)
	}
	if !reflect.DeepEqual(got, Default()) {
		t.Errorf("got %+v, want %+v", got, Default())
	}
}

func TestWriteDefaultIfMissing_DoesNotOverwriteExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	custom := `{"connectivityTargets":["192.168.1.1"]}`
	if err := os.WriteFile(path, []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := WriteDefaultIfMissing(path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != custom {
		t.Errorf("file was overwritten: got %q, want unchanged %q", data, custom)
	}
}
