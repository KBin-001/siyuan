package model

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/88250/lute/parse"
	"github.com/siyuan-note/siyuan/kernel/conf"
	"github.com/siyuan-note/siyuan/kernel/filesys"
	"github.com/siyuan-note/siyuan/kernel/localexport"
	"github.com/siyuan-note/siyuan/kernel/treenode"
	"github.com/siyuan-note/siyuan/kernel/util"
)

func TestLocalMarkdownSaveAndConversion(t *testing.T) {
	fixture := setupFileOperationTest(t)
	oldWorkspace := util.WorkspaceDir
	util.WorkspaceDir = filepath.Dir(util.DataDir)
	t.Cleanup(func() { util.WorkspaceDir = oldWorkspace })
	Conf.Editor.Store(conf.NewEditor())
	Conf.Export.Store(conf.NewExport())
	Conf.Appearance.Store(conf.NewAppearance())
	Conf.Search.Store(conf.NewSearch())
	old := localMarkdown.manager
	m, err := localexport.Open(filepath.Join(t.TempDir(), "mirror.json"), util.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	localMarkdown.manager = m
	filesys.SetTreeSavedObserver(func(id, box string) { m.Notify(id, box, false) })
	t.Cleanup(func() { localMarkdown.manager = old; filesys.SetTreeSavedObserver(func(string, string) {}) })
	c := localexport.Config{Enabled: true, Box: fixture.box.ID, Target: filepath.Join(t.TempDir(), Conf.Box(fixture.box.ID).Name)}
	if err = m.Configure(c); err != nil {
		t.Fatal(err)
	}
	asset := []byte("test image bytes")
	assetName := "assets/test-20261015000000-abcdefg.png"
	if err = os.MkdirAll(filepath.Join(util.DataDir, "assets"), 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(util.DataDir, assetName), asset, 0644); err != nil {
		t.Fatal(err)
	}
	input := "# Heading\n\n- item\n\n| A | B |\n| --- | --- |\n| one | two |\n\n```go\nfmt.Println(1)\n```\n\n[website](https://example.com)\n\n![image](" + assetName + ")\n\n<audio controls src=\"" + assetName + "\"></audio>\n"
	input += "\n`" + assetName + "`\n\n```text\n" + assetName + "\n```\n"
	input += "\n[attachment anchor](" + assetName + "#page=2)\n"
	tree := parse.Parse("", []byte(input), NewLute().ParseOptions)
	tree.ID, tree.Root.ID, tree.Box, tree.Path = fixture.sourceID, fixture.sourceID, c.Box, fixture.sourcePath
	tree.Root.SetIALAttr("id", tree.ID)
	tree.Root.SetIALAttr("title", "Export note")
	if _, err = filesys.WriteTree(tree); err != nil {
		t.Fatal(err)
	}
	treenode.UpsertBlockTree(tree)
	if m.Status().Pending != 1 {
		t.Fatal("successful write did not notify exporter")
	}
	m.Notify(tree.ID, c.Box, true)
	m.Tick(exportLocalMarkdown)
	s := m.Status()
	if s.State != "success" {
		t.Fatal(s)
	}
	output, err := os.ReadFile(filepath.Join(c.Target, "Export note.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# Export note", "Heading", "item", "one", "two", "```go", "fmt.Println(1)", "https://example.com", "assets/" + localexport.Digest(asset) + ".png", "<audio"} {
		if !strings.Contains(string(output), want) {
			t.Fatalf("missing %q in %s", want, output)
		}
	}
	if _, err = os.Stat(filepath.Join(c.Target, "assets", localexport.Digest(asset)+".png")); err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(output), assetName) != 2 {
		t.Fatal("attachment rewriting changed a code literal")
	}
	if !strings.Contains(string(output), localexport.Digest(asset)+".png#page=2") {
		t.Fatal("attachment fragment was lost")
	}
	// 本地文件保存沿用同一转换与附件流程，保持文件名且不重复添加文档标题。
	sourceFile := filepath.Join(t.TempDir(), "original.markdown")
	if err = os.WriteFile(sourceFile, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	if err = m.BindSource(tree.ID, tree.Box, sourceFile, localexport.Digest([]byte("original"))); err != nil {
		t.Fatal(err)
	}
	if err = SaveLocalFile(tree.ID); err != nil {
		t.Fatal(err)
	}
	data, readErr := os.ReadFile(sourceFile)
	if readErr != nil || strings.Contains(string(data), "# Export note") || !strings.Contains(string(data), "# Heading") {
		t.Fatal(string(data), readErr)
	}
	if !strings.Contains(string(data), "assets/"+localexport.Digest(asset)+".png#page=2") {
		t.Fatal("local file attachment path lost")
	}
	if _, err = os.Stat(filepath.Join(filepath.Dir(sourceFile), "assets", localexport.Digest(asset)+".png")); err != nil {
		t.Fatal(err)
	}
	_, dest, err := LocalMarkdownPreview(c, tree.ID)
	if err != nil || dest != filepath.Join(c.Target, "Export note.md") {
		t.Fatalf("mapping duplicated notebook: %s %v", dest, err)
	}
	c.Root = fixture.targetID
	if err = m.Configure(c); err != nil {
		t.Fatal(err)
	}
	if err = exportLocalMarkdown(c, tree.ID); !errors.Is(err, localexport.ErrOutOfScope) {
		t.Fatal("out-of-scope export was not skipped", err)
	}
}

func TestLocalMarkdownDirectoryOwnership(t *testing.T) {
	m, err := localexport.Open(filepath.Join(t.TempDir(), "state.json"), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	old := localMarkdown.manager
	localMarkdown.manager = m
	t.Cleanup(func() { localMarkdown.manager = old })
	c := localexport.Config{Box: "box", Target: filepath.Join(t.TempDir(), "kb")}
	a := &parse.Tree{ID: "a", Path: "/parent-a/a.sy"}
	b := &parse.Tree{ID: "b", Path: "/parent-b/b.sy"}
	first := localMarkdownPath(c, a, []string{"kb", "inbox", "note"})
	second := localMarkdownPath(c, b, []string{"kb", "inbox", "note"})
	if first == second || !strings.Contains(second, "parent-b") {
		t.Fatalf("merged same-title parent directories: %s %s", first, second)
	}
	c.Root = "parent-b"
	got := localMarkdownPath(c, b, []string{"kb", "inbox", "note"})
	if !strings.HasSuffix(got, filepath.Join("inbox-parent-b", "note.md")) {
		t.Fatal(got)
	}
}
