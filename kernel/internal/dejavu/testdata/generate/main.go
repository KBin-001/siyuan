// 使用固定的上游版本生成公开测试夹具，不使用用户数据或真实密钥。
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	legacy "github.com/siyuan-note/dejavu"
)

func main() {
	if len(os.Args) != 2 {
		panic("provide a new output directory")
	}
	out, err := filepath.Abs(os.Args[1])
	if err != nil {
		panic(err)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		panic("output directory must not exist")
	}
	root, err := os.MkdirTemp("", "siyuan-legacy-fixture-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(root)
	data := filepath.Join(root, "data")
	if err := os.MkdirAll(data, 0755); err != nil {
		panic(err)
	}
	file := filepath.Join(data, "note.sy")
	if err := os.WriteFile(file, []byte("legacy v1 fixture\n"), 0644); err != nil {
		panic(err)
	}
	stamp := time.Unix(1700000000, 0)
	if err := os.Chtimes(file, stamp, stamp); err != nil {
		panic(err)
	}
	repoPath := filepath.Join(root, "repo")
	repo, err := legacy.NewRepo(data, repoPath, filepath.Join(root, "history"), filepath.Join(root, "temp"),
		"legacy-fixture", "legacy", "windows", []byte("0123456789abcdef0123456789abcdef"), nil, nil)
	if err != nil {
		panic(err)
	}
	index, err := repo.Index("legacy-v1", true, nil)
	if err != nil {
		panic(err)
	}
	if err := os.MkdirAll(out, 0755); err != nil {
		panic(err)
	}
	if err := os.CopyFS(filepath.Join(out, "repo"), os.DirFS(repoPath)); err != nil {
		panic(err)
	}
	manifest, err := json.MarshalIndent(map[string]string{"version": "v0.0.0-20261011111741-4465ef631977", "index": index.ID,
		"key": "0123456789abcdef0123456789abcdef", "note": "legacy v1 fixture\n"}, "", "  ")
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(out, "manifest.json"), append(manifest, '\n'), 0644); err != nil {
		panic(err)
	}
}
