package guardlog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppend_CreatesFileWithTagAndMessage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "guard.log")

	if err := Append(path, "backup", "saved config"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected file to be created, read error: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "[backup] saved config") {
		t.Errorf("expected content to contain tag and message, got %q", content)
	}
}

func TestAppend_AppendsRatherThanOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "guard.log")

	if err := Append(path, "backup", "first line"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := Append(path, "watch", "second line"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "[backup] first line") {
		t.Errorf("expected first line to still be present, got %q", content)
	}
	if !strings.Contains(content, "[watch] second line") {
		t.Errorf("expected second line to be present, got %q", content)
	}
}

func TestAppend_LineFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "guard.log")

	if err := Append(path, "diag", "collected info"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	line := strings.TrimRight(string(data), "\n")
	if !strings.HasPrefix(line, "[") {
		t.Errorf("expected line to start with '[', got %q", line)
	}
	if !strings.Contains(line, "] [diag] collected info") {
		t.Errorf("expected line to contain '] [diag] collected info', got %q", line)
	}
}
