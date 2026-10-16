package localexport

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsAtomicReplacementPreservesActualDestination(t *testing.T) {
	dir := t.TempDir()
	from, to, backup := filepath.Join(dir, "staged"), filepath.Join(dir, "note.md"), filepath.Join(dir, "previous")
	if err := os.WriteFile(from, []byte("source version"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, []byte("latest external version"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := replaceManaged(from, to, backup); err != nil {
		t.Fatal(err)
	}
	current, _ := os.ReadFile(to)
	previous, _ := os.ReadFile(backup)
	if string(current) != "source version" || string(previous) != "latest external version" {
		t.Fatal("atomic replacement lost a version")
	}
	if err := publishNew(to, []byte("must not replace")); err == nil {
		t.Fatal("exclusive publication overwrote an existing file")
	}
	current, _ = os.ReadFile(to)
	if string(current) != "source version" {
		t.Fatal("exclusive publication damaged the file")
	}
}
