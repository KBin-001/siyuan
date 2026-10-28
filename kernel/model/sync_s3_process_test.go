//go:build fts5

package model

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/88250/lute/ast"
	"github.com/siyuan-note/siyuan/kernel/conf"
	"github.com/siyuan-note/siyuan/kernel/filesys"
	"github.com/siyuan-note/siyuan/kernel/sql"
	"github.com/siyuan-note/siyuan/kernel/treenode"
	"github.com/siyuan-note/siyuan/kernel/util"
)

type s3ProcessScenario struct {
	Root, Endpoint, Device, Action, Body string
	Modified                             int64
}

func TestS3DesktopSameSecondConflictReproducer(t *testing.T) {
	const scenarioEnv = "SIYUAN_TEST_S3_PROCESS_SCENARIO"
	const boxID = "20261028000000-notes01"
	const docPath = "/20261028000001-note001.sy"
	if scenarioFile := os.Getenv(scenarioEnv); scenarioFile != "" {
		data, err := os.ReadFile(scenarioFile)
		if err != nil {
			t.Fatal(err)
		}
		var scenario s3ProcessScenario
		if err := json.Unmarshal(data, &scenario); err != nil {
			t.Fatal(err)
		}
		prepareManualS3SyncTest(t, scenario.Endpoint)
		util.WorkspaceDir = scenario.Root
		util.DataDir, util.RepoDir = filepath.Join(scenario.Root, "data"), filepath.Join(scenario.Root, "repo")
		util.HistoryDir, util.TempDir = filepath.Join(scenario.Root, "history"), filepath.Join(scenario.Root, "temp")
		util.ConfDir = filepath.Join(scenario.Root, "conf")
		if err := os.MkdirAll(util.ConfDir, 0755); err != nil {
			t.Fatal(err)
		}
		if saved, err := os.ReadFile(filepath.Join(util.ConfDir, "conf.json")); err == nil {
			if err := json.Unmarshal(saved, Conf); err != nil {
				t.Fatal(err)
			}
		}
		Conf.Sync.Load().Mode, Conf.Sync.Load().GenerateConflictDoc = 1, true
		Conf.System.Load().ID, Conf.System.Load().Name = scenario.Device, scenario.Device
		Conf.Appearance.Store(conf.NewAppearance())
		Conf.Export.Store(conf.NewExport())
		util.Container = util.ContainerStd
		util.DBPath, util.HistoryDBPath = filepath.Join(scenario.Root, "siyuan.db"), filepath.Join(scenario.Root, "history.db")
		util.AssetContentDBPath, util.BlockTreeDBPath = filepath.Join(scenario.Root, "assets.db"), filepath.Join(scenario.Root, "blocktree.db")
		util.QueueDir = filepath.Join(util.TempDir, "queue")
		treenode.InitBlockTree(false)
		sql.InitDatabase(false)
		sql.InitHistoryDatabase(false)
		sql.InitAssetContentDatabase(false)
		t.Cleanup(sql.CloseDatabase)
		checkIndex()
		if scenario.Action == "seed" {
			if err := (&Box{ID: boxID}).SaveConf(conf.NewBoxConf()); err != nil {
				t.Fatal(err)
			}
		}
		if scenario.Action == "seed" || scenario.Action == "edit" {
			tree := treenode.NewTree(boxID, docPath, "/Travel", "Travel")
			tree.Root.FirstChild.AppendChild(&ast.Node{Type: ast.NodeText, Tokens: []byte(scenario.Body)})
			if _, err := filesys.WriteTree(tree); err != nil {
				t.Fatal(err)
			}
			stamp := time.Unix(scenario.Modified, 0)
			if err := os.Chtimes(filepath.Join(util.DataDir, boxID, docPath), stamp, stamp); err != nil {
				t.Fatal(err)
			}
		}
		if scenario.Action != "edit" {
			SyncData(false)
			FlushTxQueue()
			SyncData(false)
		}
		Conf.Save()
		return
	}
	if os.Getenv("SIYUAN_REPRO_S3_SAME_SECOND") != "1" {
		t.Skip("known unresolved engine collision; set SIYUAN_REPRO_S3_SAME_SECOND=1 to reproduce ticket 03")
	}
	storage := &manualS3Storage{objects: map[string][]byte{}}
	server := httptest.NewServer(storage)
	defer server.Close()
	firstRoot, secondRoot := t.TempDir(), t.TempDir()
	scenarioFile := filepath.Join(t.TempDir(), "scenario.json")
	step := func(root, device, action, body string, modified int64) {
		t.Helper()
		data, err := json.Marshal(s3ProcessScenario{root, server.URL, device, action, body, modified})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(scenarioFile, data, 0600); err != nil {
			t.Fatal(err)
		}
		command := exec.Command(os.Args[0], "-test.run=^TestS3DesktopSameSecondConflictReproducer$", "-test.timeout=30s")
		command.Env = append(os.Environ(), scenarioEnv+"="+scenarioFile)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("%s/%s failed: %v\n%s", device, action, err, output)
		}
	}
	stamp := time.Now().Add(-time.Minute).Unix()
	step(firstRoot, "first", "seed", "shared baseline", stamp)
	step(secondRoot, "second", "sync", "", 0)
	step(firstRoot, "first", "edit", "first independent offline edit", stamp+10)
	step(secondRoot, "second", "edit", "second independent offline edit", stamp+10)
	step(firstRoot, "first", "sync", "", 0)
	step(secondRoot, "second", "sync", "", 0)
	step(firstRoot, "first", "sync", "", 0)
	for _, root := range []string{firstRoot, secondRoot} {
		files, err := filepath.Glob(filepath.Join(root, "data", boxID, "*.sy"))
		if err != nil {
			t.Fatal(err)
		}
		var firstFound, secondFound bool
		for _, file := range files {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			firstFound = firstFound || bytes.Contains(data, []byte("first independent offline edit"))
			secondFound = secondFound || bytes.Contains(data, []byte("second independent offline edit"))
		}
		if !firstFound || !secondFound {
			t.Fatalf("independent desktops lost an offline edit: first=%t second=%t", firstFound, secondFound)
		}
	}
}
