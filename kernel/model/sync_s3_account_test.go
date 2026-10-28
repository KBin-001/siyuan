package model

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/siyuan-note/siyuan/kernel/conf"
	"github.com/siyuan-note/siyuan/kernel/util"
)

func prepareManualS3SyncTest(t *testing.T, endpoint string) {
	t.Helper()
	prepareSyncDirectoryTest(t)
	previousWorkspace, previousWorking := util.WorkspaceDir, util.WorkingDir
	previousStatus, previousErrors, previousBoot := util.StatusBarCfg.Load(), autoSyncErrCount, BootSyncSucc.Load()
	syncPlanTimeLock.Lock()
	previousPlan := syncPlanTime
	syncPlanTimeLock.Unlock()
	t.Cleanup(func() {
		util.WorkspaceDir, util.WorkingDir = previousWorkspace, previousWorking
		util.StatusBarCfg.Store(previousStatus)
		autoSyncErrCount = previousErrors
		BootSyncSucc.Store(previousBoot)
		syncPlanTimeLock.Lock()
		syncPlanTime = previousPlan
		syncPlanTimeLock.Unlock()
	})
	util.WorkspaceDir = filepath.Dir(util.DataDir)
	util.WorkingDir = t.TempDir()
	util.StatusBarCfg.Store(&util.StatusBar{MsgDataSyncDisabled: true})
	Conf.Search.Store(conf.NewSearch())
	Conf.FileTree.Store(conf.NewFileTree())
	Conf.Editor.Store(conf.NewEditor())
	Conf.NotebookCrypto = conf.NewNotebookCrypto()
	Conf.Sync.Load().Enabled = true
	Conf.Sync.Load().Mode = 3
	Conf.Sync.Load().S3.Endpoint = endpoint
	if err := os.MkdirAll(util.DataDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(util.DataDir, "note.txt"), []byte("local note must survive"), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestManualS3SyncReportsCredentialFailureWithoutMembership(t *testing.T) {
	for _, account := range []string{"signed-out", "free-account"} {
		t.Run(account, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`<Error><Code>AccessDenied</Code><Message>Access denied</Message></Error>`))
			}))
			defer server.Close()
			prepareManualS3SyncTest(t, server.URL)
			if account == "free-account" {
				Conf.SetUser(&conf.User{UserId: "free-account", UserSiYuanSubscriptionStatus: 1})
			}
			key := append([]byte(nil), Conf.Repo.Load().Key...)
			if err := SyncDataUpload(); err == nil {
				t.Fatal("denied S3 credentials must return an error")
			}
			if requests.Load() == 0 || Conf.Sync.Load().Stat == "" {
				t.Fatal("manual S3 sync must reach storage and report the storage failure without membership")
			}
			if !Conf.Sync.Load().Enabled || string(Conf.Repo.Load().Key) != string(key) {
				t.Fatal("storage failure disabled synchronization or replaced its recovery key")
			}
			if data, err := os.ReadFile(filepath.Join(util.DataDir, "note.txt")); err != nil || string(data) != "local note must survive" {
				t.Fatalf("storage failure lost the local note: %q, %v", data, err)
			}
		})
	}
}

func TestManualS3SyncPreservesOtherProviderMembership(t *testing.T) {
	for _, provider := range []int{conf.ProviderSiYuan, conf.ProviderWebDAV, conf.ProviderLocal} {
		for _, signedIn := range []bool{false, true} {
			t.Run(conf.ProviderToStr(provider)+"/"+map[bool]string{false: "signed-out", true: "free-account"}[signedIn], func(t *testing.T) {
				prepareManualS3SyncTest(t, "https://storage.example.invalid")
				Conf.Sync.Load().Provider = provider
				if signedIn {
					Conf.SetUser(&conf.User{UserId: "free-account", UserSiYuanSubscriptionStatus: 1})
				}
				if err := SyncDataUpload(); err == nil {
					t.Fatal("other providers must retain their membership requirements")
				}
				if Conf.Sync.Load().Stat != "" || Conf.Sync.Load().Synced != 0 {
					t.Fatal("unauthorized provider started synchronization")
				}
			})
		}
	}
}

func TestManualS3SyncRejectsMissingConfigurationAndKey(t *testing.T) {
	for _, missing := range []string{"configuration", "key"} {
		t.Run(missing, func(t *testing.T) {
			prepareManualS3SyncTest(t, "https://storage.example.invalid")
			if missing == "configuration" {
				Conf.Sync.Load().S3.SecretKey = ""
			} else {
				Conf.Repo.Load().Key = nil
			}
			if err := SyncDataUpload(); err == nil {
				t.Fatalf("missing %s must prevent upload", missing)
			}
			if !Conf.Sync.Load().Enabled {
				t.Fatal("configuration error disabled synchronization")
			}
			if data, err := os.ReadFile(filepath.Join(util.DataDir, "note.txt")); err != nil || string(data) != "local note must survive" {
				t.Fatalf("configuration error lost the local note: %q, %v", data, err)
			}
		})
	}
}
