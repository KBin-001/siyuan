//go:build fts5

package model

import (
	"context"
	"github.com/88250/lute/ast"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/siyuan-note/siyuan/kernel/conf"
	"github.com/siyuan-note/siyuan/kernel/filesys"
	"github.com/siyuan-note/siyuan/kernel/localexport"
	"github.com/siyuan-note/siyuan/kernel/treenode"
	"github.com/siyuan-note/siyuan/kernel/util"
)

func TestLocalMarkdownCreateEditRenameMove(t *testing.T) {
	if os.Getenv("SIYUAN_TEST_LOCAL_MARKDOWN") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLocalMarkdownCreateEditRenameMove$", "-test.v")
		cmd.Env = append(os.Environ(), "SIYUAN_TEST_LOCAL_MARKDOWN=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("local Markdown lifecycle: %v\n%s", err, output)
		}
		return
	}
	prepareHPathRefreshTest(t)
	treenode.InitBlockTree(true)
	box := &Box{ID: "20261015120000-localmd"}
	bc := conf.NewBoxConf()
	bc.Name, bc.Closed = "kb", false
	if err := box.SaveConf(bc); err != nil {
		t.Fatal(err)
	}
	m, err := localexport.Open(filepath.Join(util.ConfDir, "local-markdown.json"), util.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	localMarkdown.manager = m
	c := localexport.Config{Enabled: true, Box: box.ID, Target: filepath.Join(t.TempDir(), "kb")}
	if err = m.Configure(c); err != nil {
		t.Fatal(err)
	}
	filesys.SetTreeSavedObserver(func(id, box string) { m.Notify(id, box, false) })
	parent := newHPathTestDoc(t, box.ID, "/", "inbox", 1)
	destination := newHPathTestDoc(t, box.ID, "/", "review", 1)
	child := newHPathTestDoc(t, box.ID, parent.Path, "note", 1)
	if m.Status().Pending != 3 {
		t.Fatal("new documents did not enter the export queue")
	}
	flushChild := func(relative string) {
		t.Helper()
		m.Notify(child.ID, box.ID, true)
		m.Tick(exportLocalMarkdown)
		if m.Status().State != "success" {
			t.Fatal(m.Status())
		}
		if _, err = os.Stat(filepath.Join(c.Target, relative)); err != nil {
			t.Fatal(err)
		}
	}
	flushChild(filepath.Join("inbox", "note.md"))
	paragraph := treenode.NewParagraph(ast.NewNodeID())
	paragraph.AppendChild(&ast.Node{Type: ast.NodeText, Tokens: []byte("latest edited content")})
	child.Root.AppendChild(paragraph)
	if _, err = filesys.WriteTree(child); err != nil {
		t.Fatal(err)
	}
	flushChild(filepath.Join("inbox", "note.md"))
	data, err := os.ReadFile(filepath.Join(c.Target, "inbox", "note.md"))
	if err != nil || !strings.Contains(string(data), "latest edited content") {
		t.Fatal("edit was not exported", err)
	}
	if err = RenameDoc(box.ID, parent.Path, "renamed"); err != nil {
		t.Fatal(err)
	}
	flushChild(filepath.Join("renamed", "note.md"))
	if err = MoveDocs([]string{parent.Path}, box.ID, destination.Path, nil); err != nil {
		t.Fatal(err)
	}
	flushChild(filepath.Join("review", "renamed", "note.md"))
	for _, old := range []string{filepath.Join("inbox", "note.md"), filepath.Join("renamed", "note.md")} {
		if _, err = os.Stat(filepath.Join(c.Target, old)); err != nil {
			t.Fatal("rename or move deleted a local file", err)
		}
	}
}

func TestLocalFileImportAndSave(t *testing.T) {
	if os.Getenv("SIYUAN_TEST_LOCAL_FILE") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLocalFileImportAndSave$", "-test.v")
		cmd.Env = append(os.Environ(), "SIYUAN_TEST_LOCAL_FILE=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("local file lifecycle: %v\n%s", err, output)
		}
		return
	}
	prepareHPathRefreshTest(t)
	Conf.Appearance.Store(conf.NewAppearance())
	treenode.InitBlockTree(true)
	box := &Box{ID: "20261015120000-srcfile"}
	bc := conf.NewBoxConf()
	bc.Name, bc.Closed = "test", false
	if err := box.SaveConf(bc); err != nil {
		t.Fatal(err)
	}
	m, err := localexport.Open(filepath.Join(util.ConfDir, "local-markdown.json"), util.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	localMarkdown.manager = m
	localMarkdown.Do(func() {})
	filesys.SetTreeSavedObserver(func(id, box string) { m.Notify(id, box, false) })
	root := t.TempDir()
	for _, ext := range []string{".md", ".txt"} {
		file := filepath.Join(root, "original"+ext)
		original := "# literal\r\n\r\nfirst line\r\n"
		if err = os.WriteFile(file, []byte(original), 0644); err != nil {
			t.Fatal(err)
		}
		if err = ImportLocalFile(box.ID, file, "/", false, true); err != nil {
			t.Fatal(err)
		}
		if unchanged, readErr := os.ReadFile(file); readErr != nil || string(unchanged) != original {
			t.Fatal("opening a file changed its contents", readErr)
		}
		var id string
		for _, entry := range m.Status().Entries {
			if source, _, linked := m.Source(entry.ID); linked && source.Path == file {
				id = entry.ID
				break
			}
		}
		if id == "" {
			t.Fatal("import did not retain the original path")
		}
		if m.Status().Config.Enabled {
			t.Fatal("source save required export settings")
		}
		bt := treenode.GetBlockTreeInBox(id, box.ID)
		tree, readErr := filesys.LoadTree(box.ID, bt.Path, NewLute())
		if readErr != nil {
			t.Fatal(readErr)
		}
		if ext == ".txt" && tree.Root.FirstChild.Type != ast.NodeParagraph {
			t.Fatal("TXT interpreted as Markdown")
		}
		if ext == ".txt" && localFileText(tree.Root) != strings.ReplaceAll(original, "\r\n", "\n") {
			t.Fatalf("TXT changed on import: %q (first leaf %v)", localFileText(tree.Root), tree.Root.FirstChild.FirstChild)
		}
		paragraph := treenode.NewParagraph(ast.NewNodeID())
		paragraph.AppendChild(&ast.Node{Type: ast.NodeText, Tokens: []byte("saved edit")})
		tree.Root.AppendChild(paragraph)
		if _, err = filesys.WriteTree(tree); err != nil {
			t.Fatal(err)
		}
		if err = SaveLocalFile(id); err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(file)
		if !strings.Contains(string(data), "saved edit") || !strings.Contains(string(data), "\r\n") || strings.Contains(string(data), "# original") {
			t.Fatal("original file was not saved in its format", string(data))
		}
		if ext == ".txt" && !strings.HasPrefix(string(data), "# literal\r\n\r\nfirst line\r\n") {
			t.Fatal(string(data))
		}
		// 只修改外部文件时自动载入，根 ID 和同类型首区块 ID 保持稳定，不立即回写外部排版。
		beforeReload, _, loadErr := filesys.LoadTreeSnapshot(box.ID, bt.Path, NewLute())
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		firstID := beforeReload.Root.FirstChild.ID
		external := "# external heading\r\n\r\nexternal changed content\r\n"
		if err = os.WriteFile(file, []byte(external), 0644); err != nil {
			t.Fatal(err)
		}
		changed, refreshErr := RefreshLocalFile(id, false)
		if refreshErr != nil || !changed {
			t.Fatal("external-only edit did not reload", changed, refreshErr)
		}
		reloaded, _, reloadErr := filesys.LoadTreeSnapshot(box.ID, bt.Path, NewLute())
		if reloadErr != nil || !strings.Contains(localFileText(reloaded.Root), "external changed content") || reloaded.Root.ID != id || reloaded.Root.FirstChild.ID != firstID {
			t.Fatal("external reload lost contents or stable IDs", reloadErr)
		}
		if untouched, readErr := os.ReadFile(file); readErr != nil || string(untouched) != external {
			t.Fatal("reload rewrote the external file", readErr)
		}
		if m.Status().Pending != 0 {
			t.Fatal("reload created a write-back loop", m.Status())
		}
		if changed, refreshErr = RefreshLocalFile(id, false); refreshErr != nil || changed {
			t.Fatal("unchanged file reloaded again", changed, refreshErr)
		}
		localEdit := treenode.NewParagraph(ast.NewNodeID())
		localEdit.AppendChild(&ast.Node{Type: ast.NodeText, Tokens: []byte("unsynchronized SiYuan edit")})
		reloaded.Root.AppendChild(localEdit)
		if _, err = filesys.WriteTree(reloaded); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(file, []byte("external concurrent edit"), 0644); err != nil {
			t.Fatal(err)
		}
		if changed, refreshErr = RefreshLocalFile(id, false); refreshErr == nil || changed {
			t.Fatal("concurrent edit was silently replaced")
		}
		if changed, refreshErr = RefreshLocalFile(id, true); refreshErr != nil || !changed {
			t.Fatal("confirmed external reload failed", refreshErr)
		}
		kept, readErr := os.ReadFile(file)
		if readErr != nil || string(kept) != "external concurrent edit" {
			t.Fatal("external conflict version changed", readErr)
		}
		copies, _ := filepath.Glob(strings.TrimSuffix(file, ext) + ".siyuan-*" + ext)
		if len(copies) == 0 {
			t.Fatal("SiYuan conflict version was not preserved")
		}
		if err = SaveLocalFile(id); err != nil {
			t.Fatal("confirmed reload did not establish a new baseline", err)
		}
		if err = RenameDoc(box.ID, bt.Path, "renamed"); err != nil {
			t.Fatal(err)
		}
		if err = SaveLocalFile(id); err != nil {
			t.Fatal(err)
		}
		if source, _, _ := m.Source(id); source.Path != file {
			t.Fatal("document rename changed original file path")
		}
		if err = os.WriteFile(file, []byte("external version"), 0644); err != nil {
			t.Fatal(err)
		}
		if err = SaveLocalFile(id); err == nil {
			t.Fatal("external modification was not reported")
		}
		data, _ = os.ReadFile(file)
		if string(data) != "external version" {
			t.Fatal("external version was overwritten")
		}
	}
	// 同一个目录包含 Markdown、TXT 和附件时，仅文档建立本机保存映射。
	folder := filepath.Join(t.TempDir(), "folder")
	if err = os.MkdirAll(folder, 0755); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{"a.md": "# Heading\n", "b.txt": "* literal\n", "image.png": "image"} {
		if err = os.WriteFile(filepath.Join(folder, name), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	before := len(m.Status().Entries)
	if err = ImportLocalFile(box.ID, folder, "/", false, true); err != nil {
		t.Fatal(err)
	}
	if len(m.Status().Entries) != before+2 {
		t.Fatal("directory import failed to associate both text formats")
	}
}
