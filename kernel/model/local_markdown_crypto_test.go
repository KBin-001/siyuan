package model

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/siyuan-note/siyuan/kernel/localexport"
	"github.com/siyuan-note/siyuan/kernel/util"
)

func TestLocalMarkdownLockedNotebook(t *testing.T) {
	fixture := setupFileOperationTest(t)
	m, err := localexport.Open(filepath.Join(t.TempDir(), "state.json"), util.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	old := localMarkdown.manager
	localMarkdown.manager = m
	t.Cleanup(func() { localMarkdown.manager = old })
	localFile := filepath.Join(t.TempDir(), "existing.md")
	if err = os.WriteFile(localFile, []byte("keep local version"), 0644); err != nil {
		t.Fatal(err)
	}
	if err = m.BindSource(fixture.sourceID, fixture.box.ID, localFile, localexport.Digest([]byte("keep local version"))); err != nil {
		t.Fatal(err)
	}
	markRuntimeEncryptedBox(fixture.box.ID)
	t.Cleanup(func() { forgetRuntimeEncryptedBox(fixture.box.ID) })
	source := filepath.Join(util.DataDir, fixture.box.ID, fixture.sourcePath)
	before, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	c := localexport.Config{Enabled: true, Box: fixture.box.ID, Target: filepath.Join(t.TempDir(), "export")}
	if err = exportLocalMarkdown(c, fixture.sourceID); err == nil {
		t.Fatal("locked notebook was exported")
	}
	if err = SaveLocalFile(fixture.sourceID); err == nil {
		t.Fatal("locked notebook saved a local file")
	}
	if _, err = RefreshLocalFile(fixture.sourceID, true); err == nil {
		t.Fatal("locked notebook accepted external contents")
	}
	if data, readErr := os.ReadFile(localFile); readErr != nil || string(data) != "keep local version" {
		t.Fatal("locked save changed the local file", readErr)
	}
	if _, err = os.Stat(c.Target); !os.IsNotExist(err) {
		t.Fatal("locked notebook created an output directory", err)
	}
	after, err := os.ReadFile(source)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed export modified encrypted source", err)
	}
}
