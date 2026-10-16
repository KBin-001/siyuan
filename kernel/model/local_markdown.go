// SPDX-License-Identifier: AGPL-3.0-or-later
package model

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/88250/lute/ast"
	"github.com/88250/lute/parse"
	"github.com/88250/lute/render"
	"github.com/siyuan-note/logging"
	"github.com/siyuan-note/siyuan/kernel/filesys"
	"github.com/siyuan-note/siyuan/kernel/localexport"
	"github.com/siyuan-note/siyuan/kernel/treenode"
	"github.com/siyuan-note/siyuan/kernel/util"
)

var localMarkdown struct {
	sync.Once
	manager *localexport.Manager
	err     error
}

func LocalMarkdownDataDir() string { return util.DataDir }

func InitLocalMarkdown() {
	if util.IsMobileContainer() || util.ReadOnly {
		return
	}
	localMarkdown.Do(func() {
		localMarkdown.manager, localMarkdown.err = localexport.Open(filepath.Join(util.ConfDir, "local-markdown.json"), util.DataDir)
		if localMarkdown.err != nil {
			logging.LogErrorf("local Markdown export: %s", localMarkdown.err)
			return
		}
		filesys.SetTreeSavedObserver(func(id, box string) { localMarkdown.manager.Notify(id, box, false) })
		go func() {
			lastError := ""
			ticker := time.NewTicker(200 * time.Millisecond)
			defer ticker.Stop()
			for range ticker.C {
				if util.IsExiting.Load() {
					return
				}
				if util.IsBooted() {
					localMarkdown.manager.Tick(exportLocalMarkdown)
					state, failure := localMarkdown.manager.Outcome()
					if (state == "failed" || state == "conflict") && failure != "" && failure != lastError {
						sourceConflict := false
						if state == "conflict" {
							for _, entry := range localMarkdown.manager.Status().Entries {
								if entry.Error == failure {
									_, _, sourceConflict = localMarkdown.manager.Source(entry.ID)
									if sourceConflict {
										break
									}
								}
							}
						}
						if !sourceConflict {
							util.PushErrMsg(fmt.Sprintf(Conf.Language(14), failure), 10000)
						}
						lastError = failure
					}
					if state == "success" || state == "idle" {
						lastError = ""
					}
				}
			}
		}()
	})
}

func LocalMarkdownManager() (*localexport.Manager, error) {
	if util.IsMobileContainer() || util.ReadOnly {
		return nil, errors.New("local Markdown export requires a writable desktop kernel")
	}
	InitLocalMarkdown()
	return localMarkdown.manager, localMarkdown.err
}

// LocalMarkdownDocuments 仅在用户选择范围或手动导出时枚举文档元数据，普通保存不扫描笔记本。
func LocalMarkdownDocuments(c localexport.Config) (ids []string, err error) {
	box := Conf.Box(c.Box)
	if box == nil || box.Closed {
		return nil, errors.New("notebook unavailable")
	}
	if IsEncryptedBox(c.Box) && !IsBoxUnlocked(c.Box) {
		return nil, errors.New("encrypted notebook is locked")
	}
	if err = AcquireEncryptedBoxOperation(c.Box); err != nil {
		return nil, err
	}
	defer ReleaseEncryptedBoxOperation(c.Box)
	HoldBoxReadLock(c.Box)
	defer ReleaseBoxReadLock(c.Box)
	prefix := "/"
	if c.Root != "" {
		bt := getExportBlockTreeInBox(c.Root, c.Box)
		if bt == nil || bt.Type != "d" {
			return nil, errors.New("document scope unavailable")
		}
		ids = append(ids, c.Root)
		prefix = strings.TrimSuffix(bt.Path, ".sy")
	}
	for _, file := range box.ListFiles(prefix) {
		id := util.GetTreeID(file.path)
		if id != c.Root && !IsBoxDoc(c.Box, id) {
			ids = append(ids, id)
		}
	}
	return
}

func localMarkdownTree(c localexport.Config, id string) (*parse.Tree, []string, error) {
	bt := getExportBlockTreeInBox(id, c.Box)
	if bt == nil || bt.Type != "d" {
		if other := getExportBlockTree(id); other != nil && other.BoxID != c.Box {
			return nil, nil, localexport.ErrOutOfScope
		}
		return nil, nil, errors.New("document unavailable")
	}
	if c.Root != "" {
		root := getExportBlockTreeInBox(c.Root, c.Box)
		if root == nil {
			return nil, nil, errors.New("document scope unavailable")
		}
		if id != c.Root && !strings.HasPrefix(bt.Path, strings.TrimSuffix(root.Path, ".sy")+"/") {
			return nil, nil, localexport.ErrOutOfScope
		}
	}
	tree, err := filesys.LoadTree(c.Box, bt.Path, NewLute())
	if err != nil {
		return nil, nil, err
	}
	// 从真实祖先文档读取标题，避免重命名和移动后的异步 HPath 索引尚未刷新。
	titles := []string{Conf.Box(c.Box).Name}
	parts := strings.Split(strings.Trim(bt.Path, "/"), "/")
	for i, part := range parts {
		p := "/" + strings.Join(parts[:i+1], "/")
		if !strings.HasSuffix(part, ".sy") {
			p += ".sy"
		}
		parent := tree
		if i != len(parts)-1 {
			parent, err = filesys.LoadTree(c.Box, p, NewLute())
			if err != nil {
				return nil, nil, err
			}
		}
		titles = append(titles, parent.Root.IALAttr("title"))
	}
	tree.HPath = "/" + strings.Join(titles[1:], "/")
	return tree, titles, nil
}

func localMarkdownPath(c localexport.Config, tree *parse.Tree, titles []string) string {
	ids := append([]string{c.Box}, strings.Split(strings.Trim(strings.TrimSuffix(tree.Path, ".sy"), "/"), "/")...)
	start := 0
	if c.Root != "" {
		for i, id := range ids {
			if id == c.Root {
				start = i
				break
			}
		}
	}
	titles, ids = titles[start:], ids[start:]
	if len(titles) > 1 && strings.EqualFold(localexport.SafeName(titles[0]), filepath.Base(c.Target)) {
		titles, ids = titles[1:], ids[1:]
	}
	dir := ""
	for i, title := range titles[:len(titles)-1] {
		dir = localMarkdown.manager.Allocate(c, ids[i], filepath.Join(dir, localexport.SafeName(title)), true)
	}
	return filepath.Join(dir, localexport.SafeName(titles[len(titles)-1])+".md")
}

func LocalMarkdownPreview(c localexport.Config, id string) (string, string, error) {
	var source, dest string
	err := withExportReadLockByBlockID(id, func() error {
		tree, titles, err := localMarkdownTree(c, id)
		if err != nil {
			return err
		}
		source = "/" + strings.Join(titles, "/")
		rel := localMarkdownPath(c, tree, titles)
		dest = filepath.Join(c.Target, localMarkdown.manager.Allocate(c, id, rel, false))
		return nil
	})
	return source, dest, err
}

func exportLocalMarkdown(c localexport.Config, id string) error {
	return localMarkdownOutput(c, id, func(rel string, data []byte, assets map[string][]byte, snapshot string) error {
		err := localMarkdown.manager.Write(c, id, rel, data, assets)
		if err == nil {
			localMarkdown.manager.RememberSource(id, snapshot)
		}
		return err
	})
}

func localMarkdownOutput(c localexport.Config, id string, consume func(string, []byte, map[string][]byte, string) error) error {
	return withExportReadLockByBlockID(id, func() error {
		tree, titles, err := localMarkdownTree(c, id)
		if err != nil {
			return err
		}
		snapshot, original, err := filesys.LoadTreeSnapshot(c.Box, tree.Path, NewLute())
		if err != nil {
			return err
		}
		snapshot.HPath = tree.HPath
		tree = snapshot
		config := resolveExportOptions(nil)
		config.AddTitle = true
		config.BlockRefMode = 3
		source, _, linked := localMarkdown.manager.Source(id)
		linked = linked && filepath.Dir(source.Path) == c.Target && c.Root == id
		if linked && strings.EqualFold(filepath.Ext(source.Path), ".txt") {
			data, encodeErr := source.Encode(localFileText(tree.Root))
			if encodeErr != nil {
				return encodeErr
			}
			return consume(filepath.Base(source.Path), data, nil, localexport.Digest(original))
		}
		if linked {
			config.AddTitle = false
		}
		md := exportMarkdownContent0(id, tree, markdownExportOptions{Config: config, Ext: ".md"})
		if md == "" && !linked {
			return errors.New("Markdown conversion returned no content")
		}
		rel := localMarkdownPath(c, tree, titles)
		if linked {
			rel = filepath.Base(source.Path)
		}
		assets := map[string][]byte{}
		replacements := map[string]string{}
		engine := NewLute()
		engine.SetKramdownIAL(false)
		parsed := parse.Parse("", []byte(md), engine.ParseOptions)
		for _, dest := range getAssetsLinkDests(parsed.Root, false) {
			assetURL, decodeErr := url.Parse(dest)
			if decodeErr != nil {
				return decodeErr
			}
			decoded := assetURL.Path
			if !strings.HasPrefix(decoded, "assets/") {
				continue
			}
			data, readErr := ReadAssetBytesInBox(c.Box, decoded)
			if readErr != nil {
				return readErr
			}
			// 附件按内容寻址，新增版本使用新文件名，不覆盖外部修改的附件。
			name := path.Join("assets", localexport.Digest(data)+strings.ToLower(path.Ext(decoded)))
			assetRel := filepath.Join(filepath.Dir(rel), filepath.FromSlash(name))
			assets[assetRel] = data
			u := url.URL{Path: name, Fragment: assetURL.Fragment, RawFragment: assetURL.RawFragment}
			replacements[dest] = u.String()
		}
		for _, node := range getAssetsLinkDestsInTree(parsed, false) {
			for old, replacement := range replacements {
				setExportAssetLinkDest(node, old, replacement)
			}
		}
		md = string(render.NewFormatRenderer(parsed, engine.RenderOptions, engine.ParseOptions).Render())
		data := []byte(md)
		if linked {
			data, err = source.Encode(md)
			if err != nil {
				return err
			}
		}
		return consume(rel, data, assets, localexport.Digest(original))
	})
}

// LocalFileDocument 在读取本机文件映射前验证文档访问权限及加密笔记本租约。
func LocalFileDocument(id string) (box, name string, err error) {
	err = withExportReadLockByBlockID(id, func() error {
		bt := getExportBlockTree(id)
		if bt == nil || bt.Type != "d" {
			return errors.New("document unavailable")
		}
		tree, readErr := filesys.LoadTree(bt.BoxID, bt.Path, NewLute())
		if readErr != nil {
			return readErr
		}
		box, name = bt.BoxID, localexport.SafeName(tree.Root.IALAttr("title"))+".md"
		return nil
	})
	return
}

func SaveLocalFile(id string) error {
	if _, _, err := LocalFileDocument(id); err != nil {
		return err
	}
	return localMarkdown.manager.SaveSource(id, exportLocalMarkdown)
}

// parseLocalFileText 将每行作为普通段落导入，不解释 TXT 中的 Markdown 语法。
func parseLocalFileText(text string) *parse.Tree {
	tree := &parse.Tree{Root: &ast.Node{Type: ast.NodeDocument}}
	for _, line := range strings.Split(text, "\n") {
		paragraph := treenode.NewParagraph(ast.NewNodeID())
		if line != "" {
			paragraph.AppendChild(&ast.Node{Type: ast.NodeText, Tokens: []byte(line)})
		}
		tree.Root.AppendChild(paragraph)
	}
	return tree
}

// localFileText 保留段落、换行和代码内容；TXT 保存的是可见文本，不添加 Markdown 标记。
func localFileText(root *ast.Node) string {
	var out strings.Builder
	ast.Walk(root, func(n *ast.Node, entering bool) ast.WalkStatus {
		if !entering {
			if n.Type == ast.NodeParagraph || n.Type == ast.NodeHeading || n.Type == ast.NodeCodeBlock ||
				n.Type == ast.NodeMathBlock || n.Type == ast.NodeHTMLBlock || n.Type == ast.NodeTableRow {
				out.WriteByte('\n')
			} else if n.Type == ast.NodeTableCell && n.Next != nil {
				out.WriteByte('\t')
			}
			return ast.WalkContinue
		}
		switch n.Type {
		case ast.NodeTextMark:
			out.WriteString(n.TextMarkTextContent)
		case ast.NodeText, ast.NodeLinkText, ast.NodeCodeSpanContent, ast.NodeCodeBlockCode,
			ast.NodeInlineMathContent, ast.NodeMathBlockContent, ast.NodeHTMLBlock,
			ast.NodeBlockRefText, ast.NodeBlockRefDynamicText, ast.NodeBackslashContent:
			out.Write(n.Tokens)
		case ast.NodeSoftBreak, ast.NodeHardBreak:
			out.WriteByte('\n')
		}
		return ast.WalkContinue
	})
	return strings.TrimSuffix(out.String(), "\n")
}

// 仅为受影响子树排队，保留本地旧路径；导出失败不会回滚源文档的重命名或移动。
func notifyLocalMarkdownSubtree(boxID, p string) {
	if localMarkdown.manager == nil {
		return
	}
	c := localMarkdown.manager.Status().Config
	if !c.Enabled || c.Box != boxID {
		return
	}
	if p != "/" {
		localMarkdown.manager.Notify(util.GetTreeID(p), boxID, false)
	}
	box := Conf.Box(boxID)
	if box == nil {
		return
	}
	for _, file := range box.ListFiles(strings.TrimSuffix(p, ".sy")) {
		localMarkdown.manager.Notify(util.GetTreeID(file.path), boxID, false)
	}
}
