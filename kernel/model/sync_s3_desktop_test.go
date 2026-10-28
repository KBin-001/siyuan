//go:build fts5

package model

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/siyuan-note/siyuan/kernel/conf"
	"github.com/siyuan-note/siyuan/kernel/sql"
	"github.com/siyuan-note/siyuan/kernel/treenode"
	"github.com/siyuan-note/siyuan/kernel/util"
)

type s3DesktopWorkspace struct {
	config                                                       *AppConf
	workspace, working, data, repo, history, temp, configuration string
}

func newS3DesktopWorkspace(t *testing.T, endpoint, name string) *s3DesktopWorkspace {
	t.Helper()
	prepareManualS3SyncTest(t, endpoint)
	Conf.Sync.Load().Mode = 1
	Conf.Sync.Load().GenerateConflictDoc = true
	Conf.Appearance.Store(conf.NewAppearance())
	Conf.Export.Store(conf.NewExport())
	Conf.System.Load().ID, Conf.System.Load().Name = name, name
	Conf.Save()
	return &s3DesktopWorkspace{Conf, util.WorkspaceDir, util.WorkingDir, util.DataDir, util.RepoDir,
		util.HistoryDir, util.TempDir, util.ConfDir}
}

func (desktop *s3DesktopWorkspace) activate() {
	Conf = desktop.config
	util.WorkspaceDir, util.WorkingDir, util.DataDir, util.RepoDir = desktop.workspace, desktop.working, desktop.data, desktop.repo
	util.HistoryDir, util.TempDir, util.ConfDir = desktop.history, desktop.temp, desktop.configuration
}

func (desktop *s3DesktopWorkspace) write(t *testing.T, name, content string) {
	t.Helper()
	file := filepath.Join(desktop.data, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func (desktop *s3DesktopWorkspace) requireFile(t *testing.T, name, expected string) {
	t.Helper()
	actual, err := os.ReadFile(filepath.Join(desktop.data, filepath.FromSlash(name)))
	if err != nil || string(actual) != expected {
		t.Fatalf("workspace file %s: %q (%v), want %q", name, actual, err, expected)
	}
}

func (desktop *s3DesktopWorkspace) awaitFile(t *testing.T, name, expected string) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		data, err := os.ReadFile(filepath.Join(desktop.data, filepath.FromSlash(name)))
		if err == nil && string(data) == expected {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	desktop.requireFile(t, name, expected)
}

func TestS3DesktopAutomaticSyncLifecycle(t *testing.T) {
	// 两个工作空间的完整同步包含后台通知，在子进程中隔离全局运行状态。
	const childEnv = "SIYUAN_TEST_S3_DESKTOP_LIFECYCLE"
	if os.Getenv(childEnv) == "" {
		command := exec.Command(os.Args[0], "-test.run=^TestS3DesktopAutomaticSyncLifecycle$", "-test.timeout=90s")
		command.Env = append(os.Environ(), childEnv+"=1")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("desktop S3 lifecycle failed: %v\n%s", err, output)
		}
		return
	}
	util.Container = util.ContainerStd
	indexRoot := t.TempDir()
	util.DBPath, util.HistoryDBPath = filepath.Join(indexRoot, "siyuan.db"), filepath.Join(indexRoot, "history.db")
	util.AssetContentDBPath, util.BlockTreeDBPath = filepath.Join(indexRoot, "assets.db"), filepath.Join(indexRoot, "blocktree.db")
	util.QueueDir = filepath.Join(indexRoot, "queue")
	treenode.InitBlockTree(true)
	sql.InitDatabase(true)
	sql.InitHistoryDatabase(true)
	sql.InitAssetContentDatabase(true)
	t.Cleanup(sql.CloseDatabase)
	lastAutoPurgeRepo = time.Now()
	storage := &manualS3Storage{objects: map[string][]byte{}}
	server := httptest.NewServer(storage)
	defer server.Close()
	first := newS3DesktopWorkspace(t, server.URL, "first-desktop")
	second := newS3DesktopWorkspace(t, server.URL, "second-desktop")
	first.activate()
	first.write(t, "notes/travel.txt", "first desktop draft")
	first.write(t, "assets/travel.bin", "attachment version one")
	SyncData(false)
	second.activate()
	BootSyncData()
	if BootSyncSucc.Load() != 0 {
		t.Fatalf("accountless startup sync failed: %s", Conf.Sync.Load().Stat)
	}
	second.awaitFile(t, "notes/travel.txt", "first desktop draft")
	SyncData(false)
	second.requireFile(t, "assets/travel.bin", "attachment version one")
	second.write(t, "notes/return.txt", "written on the second desktop")
	syncData(true, false)
	if ExitSyncSucc != 0 {
		t.Fatalf("exit sync failed: %s", Conf.Sync.Load().Stat)
	}
	first.activate()
	// 将调度时钟推进到到期状态，验证真实定时任务入口而不等待用户设置的间隔。
	planSyncAfter(-time.Second)
	SyncDataJob()
	first.requireFile(t, "notes/return.txt", "written on the second desktop")
	if err := os.Rename(filepath.Join(first.data, "notes", "travel.txt"), filepath.Join(first.data, "notes", "renamed.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(first.data, "assets", "travel.bin")); err != nil {
		t.Fatal(err)
	}
	SyncData(false)
	second.activate()
	SyncData(false)
	second.requireFile(t, "notes/renamed.txt", "first desktop draft")
	for _, name := range []string{"notes/travel.txt", "assets/travel.bin"} {
		if _, err := os.Stat(filepath.Join(second.data, filepath.FromSlash(name))); !os.IsNotExist(err) {
			t.Fatalf("removed file %s remained after sync: %v", name, err)
		}
	}
	storage.mu.Lock()
	storage.unavailable = true
	storage.mu.Unlock()
	second.write(t, "notes/offline.txt", "local work while disconnected")
	SyncData(false)
	second.requireFile(t, "notes/offline.txt", "local work while disconnected")
	if Conf.Sync.Load().Stat == "" || !Conf.Sync.Load().Enabled {
		t.Fatal("storage outage must report a failure without disabling synchronization")
	}
	storage.mu.Lock()
	storage.unavailable = false
	storage.mu.Unlock()
	Conf.Save()
	data, err := os.ReadFile(filepath.Join(second.configuration, "conf.json"))
	if err != nil {
		t.Fatal(err)
	}
	restarted := NewAppConf()
	if err := json.Unmarshal(data, restarted); err != nil {
		t.Fatal(err)
	}
	second.config = restarted
	second.activate()
	SyncData(true)
	first.activate()
	SyncData(false)
	first.requireFile(t, "notes/offline.txt", "local work while disconnected")
	SyncData(false)
	first.requireFile(t, "notes/offline.txt", "local work while disconnected")
}
