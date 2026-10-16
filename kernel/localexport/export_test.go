package localexport

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) (*Manager, Config) {
	t.Helper()
	root := t.TempDir()
	m, err := Open(filepath.Join(root, "conf", "mirror.json"), filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	c := Config{Enabled: true, Box: "box", Target: filepath.Join(root, "kb")}
	if err = m.Configure(c); err != nil {
		t.Fatal(err)
	}
	return m, c
}

func TestSaveDebounceAndRestart(t *testing.T) {
	m, c := fixture(t)
	for range 10 {
		m.Notify("a", c.Box, false)
	}
	calls := 0
	m.Tick(func(Config, string) error { calls++; return nil })
	if calls != 0 || m.Status().Pending != 1 {
		t.Fatal("debounce did not coalesce")
	}
	restarted, err := Open(m.file, m.dataDir)
	if err != nil || restarted.Status().Pending != 1 {
		t.Fatalf("lost pending save: %v", err)
	}
	restarted.Notify("a", c.Box, true)
	restarted.Tick(func(config Config, id string) error {
		calls++
		return restarted.Write(config, id, "note.md", []byte("# First\n"), nil)
	})
	if calls != 1 || restarted.Status().State != "success" {
		t.Fatal(restarted.Status())
	}
	if _, err = os.Stat(filepath.Join(c.Target, "note.md")); err != nil {
		t.Fatal(err)
	}
}

func TestExistingAndExternalConflicts(t *testing.T) {
	for _, first := range []bool{true, false} {
		t.Run(map[bool]string{true: "first export", false: "external edit"}[first], func(t *testing.T) {
			m, c := fixture(t)
			p := filepath.Join(c.Target, "note.md")
			if !first {
				if err := m.Write(c, "a", "note.md", []byte("old"), nil); err != nil {
					t.Fatal(err)
				}
			}
			os.MkdirAll(c.Target, 0755)
			os.WriteFile(p, []byte("local content"), 0644)
			m.Notify("a", c.Box, true)
			m.Tick(func(config Config, id string) error {
				return m.Write(config, id, "note.md", []byte("source content"), nil)
			})
			s := m.Status()
			if s.State != "conflict" {
				t.Fatal(s)
			}
			restarted, restartErr := Open(m.file, m.dataDir)
			if restartErr != nil || restarted.Status().State != "conflict" {
				t.Fatal("lost conflict status after restart", restartErr)
			}
			local, _ := os.ReadFile(p)
			copy, _ := os.ReadFile(s.Entries[0].Conflict)
			if string(local) != "local content" || string(copy) != "source content" {
				t.Fatal("lost one side of conflict")
			}
			m.Retry()
			m.Tick(func(config Config, id string) error {
				return m.Write(config, id, "note.md", []byte("source content"), nil)
			})
			copies, _ := filepath.Glob(filepath.Join(c.Target, "*.siyuan-*.md"))
			if len(copies) != 1 {
				t.Fatal("retry duplicated conflict copy")
			}
		})
	}
}

func TestRenameMoveDuplicatesAndAssets(t *testing.T) {
	m, c := fixture(t)
	asset := map[string][]byte{filepath.Join("assets", "hash.png"): []byte("image")}
	if err := m.Write(c, "a", "same.md", []byte("# A\n![](assets/hash.png)"), asset); err != nil {
		t.Fatal(err)
	}
	if err := m.Write(c, "b", "same.md", []byte("# B"), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(c.Target, "same-b.md")); err != nil {
		t.Fatal(err)
	}
	if err := m.Write(c, "a", filepath.Join("moved", "renamed.md"), []byte("# New"), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(c.Target, "same.md")); err != nil {
		t.Fatal("removed old file")
	}
	if err := m.Write(c, "c", "same.md", []byte("# C"), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(c.Target, "same-c.md")); err != nil {
		t.Fatal("reused historical owner")
	}
	if err := m.Write(c, "a", filepath.Join("moved", "renamed.md"), []byte("# Newer"), nil); err != nil {
		t.Fatal(err)
	}
	if err := m.Write(c, "a", "other.md", []byte("![](assets/hash.png)"), map[string][]byte{filepath.Join("assets", "hash.png"): []byte("different")}); err == nil {
		t.Fatal("overwrote attachment")
	}
}

func TestUnsafePathsAndIOFailures(t *testing.T) {
	m, c := fixture(t)
	ancestor := c
	ancestor.Target = filepath.Dir(m.dataDir)
	if err := m.Write(ancestor, "protected", filepath.Join("data", "protected.md"), []byte("x"), nil); err == nil {
		t.Fatal("wrote into data through an ancestor target")
	}
	for _, p := range []string{filepath.Join(m.dataDir, "exports"), m.dataDir, "relative"} {
		if _, err := ValidateTarget(p, m.dataDir); err == nil {
			t.Fatalf("accepted %s", p)
		}
	}
	if err := m.Write(c, "a", filepath.Join("..", "escape.md"), []byte("x"), nil); err == nil {
		t.Fatal("accepted traversal")
	}
	os.MkdirAll(c.Target, 0755)
	os.WriteFile(filepath.Join(c.Target, "blocked"), []byte("file"), 0644)
	m.Notify("a", c.Box, true)
	m.Tick(func(config Config, id string) error {
		return m.Write(config, id, filepath.Join("blocked", "note.md"), []byte("x"), nil)
	})
	if m.Status().State != "failed" || m.Status().Error == "" {
		t.Fatal("IO failure hidden")
	}
	if err := os.Symlink(m.dataDir, filepath.Join(c.Target, "link")); err == nil {
		if _, err = ValidateTarget(filepath.Join(c.Target, "link", "nested"), m.dataDir); err == nil {
			t.Fatal("accepted data symlink")
		}
	}
}

func TestPathMappingAndNames(t *testing.T) {
	m, c := fixture(t)
	if err := m.Write(c, "unicode", filepath.Join("收件箱", "20261014.md"), []byte("# 中文标题\n"), nil); err != nil {
		t.Fatal(err)
	}
	if got := RelativePath(filepath.Join(t.TempDir(), "kb"), []string{"kb", "inbox", "20261014"}); got != filepath.Join("inbox", "20261014.md") {
		t.Fatal(got)
	}
	for _, name := range []string{"CON", "aux.txt", "LPT1", "a:b?c.", "..", strings.Repeat("x", 200)} {
		got := SafeName(name)
		if strings.ContainsAny(got, "<>:\"/\\|?*") || strings.HasSuffix(got, ".") || len([]rune(got)) > 80 || got == "CON" || got == "aux.txt" {
			t.Fatal(got)
		}
	}
}

func TestChangesDuringExportSurviveAndDisabledIsQuiet(t *testing.T) {
	m, c := fixture(t)
	m.Notify("a", c.Box, true)
	m.Tick(func(Config, string) error { m.Notify("a", c.Box, false); return errors.New("injected error") })
	if m.Status().Pending != 1 {
		t.Fatal("dropped concurrent save")
	}
	c.Enabled = false
	if err := m.Configure(c); err != nil {
		t.Fatal(err)
	}
	m.Tick(func(Config, string) error { t.Fatal("disabled export ran"); return nil })
	data, err := os.ReadFile(m.file)
	if err != nil || len(data) == 0 {
		t.Fatal(err)
	}
	if time.Since(time.UnixMilli(m.Pending["a"])) > time.Second {
		t.Fatal("lost debounce")
	}
}

func TestCollisionReservationsAndRenameConflictOwnership(t *testing.T) {
	m, c := fixture(t)
	if got := m.Allocate(c, "a", "note.md", false); got != "note.md" {
		t.Fatal(got)
	}
	m.Allocate(c, "c", "note-b.md", false)
	if got := m.Allocate(c, "b", "note.md", false); got != "note-b-1.md" {
		t.Fatal(got)
	}
	if err := m.Write(c, "doc", "original.md", []byte("old"), nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.Target, "new.md"), []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := m.Write(c, "doc", "new.md", []byte("new"), nil); err == nil {
			t.Fatal("rename adopted an unowned file")
		}
	}
	m.Notify("panic", c.Box, true)
	m.Tick(func(Config, string) error { panic("injected converter failure") })
	if m.Status().State != "failed" || !strings.Contains(m.Status().Error, "injected converter failure") {
		t.Fatal("converter panic escaped the worker")
	}
}
