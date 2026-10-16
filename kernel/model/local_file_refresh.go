// SPDX-License-Identifier: AGPL-3.0-or-later
package model

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/88250/lute/ast"
	"github.com/88250/lute/parse"
	"github.com/siyuan-note/siyuan/kernel/filesys"
	"github.com/siyuan-note/siyuan/kernel/localexport"
	"github.com/siyuan-note/siyuan/kernel/sql"
	"github.com/siyuan-note/siyuan/kernel/treenode"
	"github.com/siyuan-note/siyuan/kernel/util"
)

// RefreshLocalFile 只载入关联文件；双方修改时先保留思源副本，显式确认后才载入外部版本。
func RefreshLocalFile(id string, acceptExternal bool) (bool, error) {
	if _, _, linked := localMarkdown.manager.Source(id); !linked {
		return false, nil
	}
	if _, _, err := LocalFileDocument(id); err != nil {
		return false, err
	}
	FlushTxQueue()
	m := localMarkdown.manager
	return m.RefreshSource(id, func(source localexport.Source, entry localexport.Entry, data []byte) error {
		flushLock.Lock()
		defer flushLock.Unlock()
		for _, queued := range takeQueuedTransactions() {
			flushTx(queued)
		}
		c := localexport.Config{Enabled: true, Box: source.Box, Root: id, Target: filepath.Dir(source.Path)}
		var snapshot string
		var current []byte
		var assets map[string][]byte
		if err := localMarkdownOutput(c, id, func(_ string, content []byte, files map[string][]byte, hash string) error {
			current, assets, snapshot = content, files, hash
			return nil
		}); err != nil {
			return err
		}
		clean := snapshot == source.Snapshot
		if source.Snapshot == "" {
			clean = localexport.Digest(current) == entry.Hash
		}
		if !clean || acceptExternal {
			err := m.PreserveSourceCopy(id, current, assets)
			var conflict *localexport.ConflictError
			if !errors.As(err, &conflict) || !acceptExternal {
				return err
			}
		}
		return withExportReadLockByBlockID(id, func() error {
			bt := getExportBlockTreeInBox(id, source.Box)
			if bt == nil {
				return errors.New("document unavailable")
			}
			oldTree, original, err := filesys.LoadTreeSnapshot(source.Box, bt.Path, NewLute())
			if err != nil {
				return err
			}
			if localexport.Digest(original) != snapshot {
				return util.ErrFileChanged
			}
			if !acceptExternal {
				protected := false
				ast.Walk(oldTree.Root, func(n *ast.Node, entering bool) ast.WalkStatus {
					if entering && (n.Type == ast.NodeAttributeView || n.Type == ast.NodeWidget || n.Type == ast.NodeMindmap ||
						n.Type == ast.NodeBlockQueryEmbed || treenode.IsBlockRef(n)) {
						protected = true
					}
					return ast.WalkContinue
				})
				if protected {
					return m.PreserveSourceCopy(id, current, assets)
				}
			}
			text, err := source.Decode(data)
			if err != nil {
				return err
			}
			var tree *parse.Tree
			if strings.EqualFold(filepath.Ext(source.Path), ".txt") {
				tree = parseLocalFileText(text)
			} else {
				tree, _, _, _ = parseStdMd([]byte(text), true)
			}
			if tree == nil {
				return errors.New("local file parsing failed")
			}
			tree.ID, tree.Box, tree.Path, tree.HPath = id, oldTree.Box, oldTree.Path, oldTree.HPath
			tree.Root.ID, tree.Root.Spec = id, oldTree.Root.Spec
			tree.Root.KramdownIAL = oldTree.Root.KramdownIAL
			pinLocalFileBlockIDs(oldTree.Root, tree.Root)
			ast.Walk(tree.Root, func(node *ast.Node, entering bool) ast.WalkStatus {
				if entering && node.IsBlock() && node.IALAttr("id") != "" {
					node.ID = node.IALAttr("id")
					if old := treenode.GetNodeInTree(oldTree, node.ID); old != nil && old.Type == node.Type {
						node.KramdownIAL = old.KramdownIAL
					}
				}
				return ast.WalkContinue
			})
			if err = treenode.ValidateBlockSubtree(tree.Root); err != nil {
				return err
			}
			if err = importLocalFileAssets(tree, filepath.Dir(source.Path)); err != nil {
				return err
			}
			degradeCrossBoundaryBlockRefs(tree.Root, tree.Box)
			latest, err := os.ReadFile(source.Path)
			if err != nil {
				return err
			}
			if localexport.Digest(latest) != localexport.Digest(data) {
				return util.ErrFileChanged
			}
			historyDir, err := getHistoryDir(HistoryOpUpdate)
			if err != nil {
				return err
			}
			if err = generateDocHistoryFromData(tree.Box, filepath.Join(util.DataDir, tree.Box, tree.Path), historyDir, original, NewLute()); err != nil {
				return err
			}
			indexHistoryDir(filepath.Base(historyDir), NewLute())
			size, err := filesys.WriteTreeIfUnchanged(tree, original)
			if err != nil {
				return err
			}
			treenode.RemoveBlockTreesByRootID(tree.Box, id)
			treenode.IndexBlockTree(tree)
			sql.RemoveTreeQueue(tree.Box, id)
			sql.IndexTreeQueue(tree)
			refreshDocInfoWithSize(tree, size)
			invalidateDocumentHistory(id)
			_, saved, err := filesys.LoadTreeSnapshot(tree.Box, tree.Path, NewLute())
			if err != nil {
				return err
			}
			m.AcceptSource(id, source, localexport.Digest(data), localexport.Digest(saved))
			ReloadProtyle(id)
			IncSync()
			return nil
		})
	})
}

// 先按内容匹配未修改的区块，再对等长的剩余区块保留 ID，避免插入段落时把后续引用错配到别处。
func pinLocalFileBlockIDs(oldRoot, newRoot *ast.Node) {
	remapped := map[string]string{}
	var pin func(*ast.Node, *ast.Node)
	pin = func(old, next *ast.Node) {
		oldChildren, newChildren := blockChildrenOf(old), blockChildrenOf(next)
		byContent := map[string][]*ast.Node{}
		key := func(n *ast.Node) string { return n.Type.String() + "|" + localFileText(n) }
		for _, n := range oldChildren {
			byContent[key(n)] = append(byContent[key(n)], n)
		}
		used, assigned := map[*ast.Node]bool{}, map[*ast.Node]bool{}
		matches := map[*ast.Node]*ast.Node{}
		assign := func(before, after *ast.Node) {
			if used[before] || assigned[after] {
				return
			}
			remapped[after.ID] = before.ID
			after.ID = before.ID
			after.SetIALAttr("id", before.ID)
			used[before], assigned[after] = true, true
			matches[after] = before
			pin(before, after)
		}
		for _, n := range newChildren {
			if matches := byContent[key(n)]; len(matches) > 0 {
				assign(matches[0], n)
				byContent[key(n)] = matches[1:]
			}
		}
		// 已匹配的区块作为边界，只在边界之间等长且类型相同的区段中复用编辑区块 ID。
		oldStart, newStart := 0, 0
		for nextIndex := 0; nextIndex <= len(newChildren); nextIndex++ {
			oldEnd := len(oldChildren)
			if nextIndex < len(newChildren) {
				anchor := matches[newChildren[nextIndex]]
				if anchor == nil {
					continue
				}
				oldEnd = -1
				for i := oldStart; i < len(oldChildren); i++ {
					if oldChildren[i] == anchor {
						oldEnd = i
						break
					}
				}
				if oldEnd < oldStart {
					newStart = nextIndex + 1
					continue
				}
			}
			if oldEnd-oldStart == nextIndex-newStart {
				for i := 0; i < oldEnd-oldStart; i++ {
					before, after := oldChildren[oldStart+i], newChildren[newStart+i]
					if before.Type == after.Type {
						assign(before, after)
					}
				}
			}
			oldStart, newStart = oldEnd+1, nextIndex+1
		}
		var before, after []*ast.Node
		for _, n := range oldChildren {
			if !used[n] {
				before = append(before, n)
			}
		}
		for _, n := range newChildren {
			if !assigned[n] {
				after = append(after, n)
			}
		}
		if len(before) == len(after) {
			for i, n := range after {
				if before[i].Type == n.Type {
					assign(before[i], n)
				}
			}
		}
		for _, n := range after {
			if assigned[n] {
				continue
			}
			var candidate *ast.Node
			oldCount, newCount := 0, 0
			for _, previous := range before {
				if !used[previous] && previous.Type == n.Type {
					oldCount++
					candidate = previous
				}
			}
			for _, next := range after {
				if !assigned[next] && next.Type == n.Type {
					newCount++
				}
			}
			if oldCount == 1 && newCount == 1 {
				assign(candidate, n)
			}
		}
	}
	pin(oldRoot, newRoot)
	ast.Walk(newRoot, func(n *ast.Node, entering bool) ast.WalkStatus {
		if entering && treenode.IsBlockRef(n) {
			if id := remapped[n.TextMarkBlockRefID]; id != "" {
				n.TextMarkBlockRefID = id
			}
		}
		return ast.WalkContinue
	})
}

// 复用附件存储及 AST 链接更新，沿用本地导入的目录边界。
func importLocalFileAssets(tree *parse.Tree, dir string) error {
	assetDir := GetImportAssetsDir(tree.Box, filepath.Dir(filepath.Join(util.DataDir, tree.Box, tree.Path)))
	dests := map[string]bool{}
	for _, dest := range getAssetsLinkDests(tree.Root, false) {
		dests[dest] = true
	}
	ast.Walk(tree.Root, func(n *ast.Node, entering bool) ast.WalkStatus {
		if entering && n.Type == ast.NodeLinkDest {
			dests[n.TokensStr()] = true
		}
		if entering && n.IsTextMarkType("a") {
			dests[n.TextMarkAHref] = true
		}
		return ast.WalkContinue
	})
	for dest := range dests {
		u, err := url.Parse(dest)
		if err != nil {
			return err
		}
		if u.IsAbs() || u.Host != "" || u.Path == "" || strings.HasPrefix(dest, "#") {
			continue
		}
		if !filepath.IsLocal(filepath.FromSlash(u.Path)) {
			continue
		}
		ext := strings.ToLower(filepath.Ext(u.Path))
		if (ext == ".md" || ext == ".markdown" || ext == ".txt") && !strings.HasPrefix(u.Path, "assets/") {
			continue
		}
		p := filepath.Join(dir, filepath.FromSlash(u.Path))
		resolved, err := filepath.EvalSymlinks(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(dir, resolved)
		if err != nil || !filepath.IsLocal(relative) {
			return errors.New("local attachment escapes the source directory")
		}
		data, err := os.ReadFile(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if err = os.MkdirAll(assetDir, 0755); err != nil {
			return err
		}
		name, err := storeAssetForBox(tree.Box, assetDir, util.FilterUploadFileName(filepath.Base(p)), data)
		if err != nil {
			return err
		}
		replacement := &url.URL{Path: "assets/" + name, Fragment: u.Fragment}
		if IsEncryptedBox(tree.Box) {
			replacement.RawQuery = "box=" + tree.Box
		}
		ast.Walk(tree.Root, func(node *ast.Node, entering bool) ast.WalkStatus {
			if entering {
				setExportAssetLinkDest(node, dest, replacement.String())
			}
			return ast.WalkContinue
		})
	}
	return nil
}
