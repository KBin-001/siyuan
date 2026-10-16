package localexport

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSourceSaveWithoutExportSettings(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "conf", "files.json")
	m, err := Open(state, filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, "notes", "original.txt")
	if err = os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	original := []byte("original\r\n")
	if err = os.WriteFile(p, original, 0644); err != nil {
		t.Fatal(err)
	}
	if err = m.BindSource("doc", "box", p, Digest(original)); err != nil {
		t.Fatal(err)
	}
	if m.Status().Config.Enabled {
		t.Fatal("file save enabled optional export")
	}
	m, err = Open(state, filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	source, _, linked := m.Source("doc")
	if !linked || source.Path != p || !source.CRLF {
		t.Fatal(source)
	}
	render := func(c Config, id string) error {
		data, encodeErr := source.Encode("changed\n")
		if encodeErr != nil {
			return encodeErr
		}
		return m.Write(c, id, filepath.Base(p), data, nil)
	}
	m.Notify("doc", "box", false)
	m.Tick(func(Config, string) error { t.Fatal("debounce was skipped"); return nil })
	m.Notify("doc", "box", true)
	m.Tick(render)
	if m.Status().State != "success" {
		t.Fatal("automatic file save required enabled export settings", m.Status())
	}
	if err = m.SaveSource("doc", render); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p)
	if err != nil || string(data) != "changed\r\n" {
		t.Fatal(string(data), err)
	}
	if err = os.WriteFile(p, []byte("external edit"), 0644); err != nil {
		t.Fatal(err)
	}
	var conflict *ConflictError
	if err = m.SaveSource("doc", render); !errors.As(err, &conflict) {
		t.Fatal("external edit was overwritten", err)
	}
	if filepath.Ext(conflict.Copy) != ".txt" {
		t.Fatal(conflict)
	}
	data, _ = os.ReadFile(p)
	if string(data) != "external edit" {
		t.Fatal(string(data))
	}
	if err = m.BindSource("other", "box", p, Digest(data)); err == nil {
		t.Fatal("two document IDs acquired the same file")
	}
	if err = m.BindSource("doc", "box", p, Digest(original)); err == nil {
		t.Fatal("stale association accepted")
	}
	if _, _, err = m.InspectSource(filepath.Join(root, "data", "bad.md")); err == nil {
		t.Fatal("workspace data accepted")
	}
	if _, _, err = m.InspectSource(filepath.Join(root, "bad.exe")); err == nil {
		t.Fatal("unsupported type accepted")
	}
}

func TestSourceEncodingRoundTrip(t *testing.T) {
	for _, encoding := range []string{"utf8", "utf8bom", "utf16le", "utf16be"} {
		t.Run(encoding, func(t *testing.T) {
			s := Source{Encoding: encoding, CRLF: true}
			data, err := s.Encode("# literal\n文本\n")
			if err != nil {
				t.Fatal(err)
			}
			read := Source{}
			text, err := read.Decode(data)
			if err != nil || text != "# literal\n文本\n" || read.Encoding != encoding || !read.CRLF {
				t.Fatal(text, read, err)
			}
			again, err := read.Encode(text)
			if err != nil || !bytes.Equal(data, again) {
				t.Fatal("encoding was lost", err)
			}
		})
	}
	s := Source{}
	if _, err := s.Decode([]byte{0xff, 0x80}); err == nil {
		t.Fatal("invalid encoding accepted")
	}
	for _, data := range [][]byte{{0xff, 0xfe, 0x41}, {0xff, 0xfe, 0x00, 0xd8}} {
		if _, err := s.Decode(data); err == nil {
			t.Fatal("invalid UTF-16 accepted")
		}
	}
}

func TestSourceFirstSaveCreatesFileAndReportsIOFailure(t *testing.T) {
	root := t.TempDir()
	m, err := Open(filepath.Join(root, "state.json"), filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, "new", "note.md")
	if err = m.BindSource("new", "box", p, ""); err != nil {
		t.Fatal(err)
	}
	render := func(c Config, id string) error { return m.Write(c, id, "note.md", []byte("saved"), nil) }
	if err = m.SaveSource("new", render); err != nil {
		t.Fatal(err)
	}
	if data, readErr := os.ReadFile(p); readErr != nil || string(data) != "saved" {
		t.Fatal(string(data), readErr)
	}
	// 用目录占用已关联文件的位置，模拟目标无法作为普通文件写入。
	if err = os.Rename(p, p+".kept"); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(p, 0755); err != nil {
		t.Fatal(err)
	}
	if err = m.SaveSource("new", render); err == nil || m.Status().State != "failed" {
		t.Fatal("I/O failure reported as success", err)
	}
	if data, readErr := os.ReadFile(p + ".kept"); readErr != nil || string(data) != "saved" {
		t.Fatal("failure lost the previous file", readErr)
	}
}

func TestSourceExternalRefreshAndRestart(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "files.json")
	m, err := Open(state, filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, "note.md")
	if err = os.WriteFile(p, []byte("first"), 0644); err != nil {
		t.Fatal(err)
	}
	if err = m.BindSource("doc", "box", p, Digest([]byte("first"))); err != nil {
		t.Fatal(err)
	}
	m.RememberSource("doc", "old-snapshot")
	if err = os.Rename(p, p+".old"); err != nil {
		t.Fatal(err)
	}
	if changed, refreshErr := m.RefreshSource("doc", func(Source, Entry, []byte) error { t.Fatal("missing file imported"); return nil }); changed || refreshErr != nil {
		t.Fatal(changed, refreshErr)
	}
	if err = os.WriteFile(p, []byte("second"), 0644); err != nil {
		t.Fatal(err)
	}
	changed, err := m.RefreshSource("doc", func(source Source, entry Entry, data []byte) error {
		if source.Snapshot != "old-snapshot" || string(data) != "second" {
			t.Fatal(source, string(data))
		}
		m.AcceptSource("doc", source, Digest(data), "new-snapshot")
		return nil
	})
	if err != nil || !changed {
		t.Fatal(changed, err)
	}
	m, err = Open(state, filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	source, entry, _ := m.Source("doc")
	if source.Snapshot != "new-snapshot" || entry.Hash != Digest([]byte("second")) {
		t.Fatal(source, entry)
	}
	if changed, err = m.RefreshSource("doc", func(Source, Entry, []byte) error { t.Fatal("reloaded own baseline"); return nil }); err != nil || changed {
		t.Fatal(changed, err)
	}
}
