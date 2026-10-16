package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/88250/lute/ast"
	"github.com/siyuan-note/siyuan/kernel/util"
)

func TestLocalFileReloadIDsAndAssets(t *testing.T) {
	fixture := setupFileOperationTest(t)
	previousWorkspace := util.WorkspaceDir
	util.WorkspaceDir = filepath.Dir(util.DataDir)
	t.Cleanup(func() { util.WorkspaceDir = previousWorkspace })
	before := parseLocalFileText("first\nsecond\nthird")
	oldIDs := []string{}
	for _, node := range blockChildrenOf(before.Root) {
		oldIDs = append(oldIDs, node.ID)
	}
	after := parseLocalFileText("inserted\nfirst\nsecond\nthird")
	pinLocalFileBlockIDs(before.Root, after.Root)
	for i, node := range blockChildrenOf(after.Root)[1:] {
		if node.ID != oldIDs[i] {
			t.Fatal("inserted paragraph changed a following block ID")
		}
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "image.png"), []byte("attachment bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	tree, _, _, _ := parseStdMd([]byte("![image](image.png)\n\n`image.png`\n"), true)
	tree.Box, tree.Path = fixture.box.ID, fixture.sourcePath
	if err := importLocalFileAssets(tree, root); err != nil {
		t.Fatal(err)
	}
	found := false
	ast.Walk(tree.Root, func(n *ast.Node, entering bool) ast.WalkStatus {
		if entering && n.Type == ast.NodeLinkDest {
			if !strings.HasPrefix(n.TokensStr(), "assets/") {
				t.Fatal("external attachment was not imported", n.TokensStr())
			}
			data, err := ReadAssetBytesInBox(tree.Box, n.TokensStr())
			if err != nil || string(data) != "attachment bytes" {
				t.Fatal(string(data), err)
			}
			found = true
		}
		return ast.WalkContinue
	})
	if !found {
		t.Fatal("image link lost")
	}
}
