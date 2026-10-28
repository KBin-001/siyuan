package model

import (
	"bytes"
	"crypto/md5"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/siyuan-note/siyuan/kernel/conf"
	"github.com/siyuan-note/siyuan/kernel/util"
)

// manualS3Storage 是仅供集成测试使用的私有 S3 协议边界，不替换应用内的同步逻辑。
type manualS3Storage struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func (storage *manualS3Storage) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	storage.mu.Lock()
	defer storage.mu.Unlock()
	w.Header().Set("Content-Type", "application/xml")
	if !strings.Contains(r.Header.Get("Authorization"), "Credential=test-key/") {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `<Error><Code>AccessDenied</Code></Error>`)
		return
	}
	key := strings.TrimPrefix(r.URL.Path, "/notes.backup/")
	if r.URL.Path == "/notes.backup" || r.URL.Path == "/notes.backup/" {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		type object struct {
			Key          string
			Size         int
			LastModified string
		}
		result := struct {
			XMLName     xml.Name `xml:"ListBucketResult"`
			Name        string
			IsTruncated bool
			Contents    []object
		}{Name: "notes.backup"}
		for name, data := range storage.objects {
			if strings.HasPrefix(name, r.URL.Query().Get("prefix")) {
				result.Contents = append(result.Contents, object{name, len(data), time.Now().UTC().Format(time.RFC3339)})
			}
		}
		sort.Slice(result.Contents, func(i, j int) bool { return result.Contents[i].Key < result.Contents[j].Key })
		_ = xml.NewEncoder(w).Encode(result)
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/notes.backup/repo/") {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodPut:
		data, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		storage.objects[key] = data
		w.Header().Set("ETag", fmt.Sprintf(`"%x"`, md5.Sum(data)))
	case http.MethodGet, http.MethodHead:
		data, found := storage.objects[key]
		if !found {
			w.WriteHeader(http.StatusNotFound)
			if r.Method != http.MethodHead {
				_, _ = io.WriteString(w, `<Error><Code>NoSuchKey</Code></Error>`)
			}
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
		w.Header().Set("ETag", fmt.Sprintf(`"%x"`, md5.Sum(data)))
		if r.Method == http.MethodGet {
			_, _ = w.Write(data)
		}
	case http.MethodDelete:
		delete(storage.objects, key)
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func TestManualS3SyncTransfersDocumentsAndAssetsWithoutAccount(t *testing.T) {
	// 完整同步包含异步界面通知，使用子进程避免影响其他用例的工作空间。
	const childEnv = "SIYUAN_TEST_MANUAL_S3"
	if os.Getenv(childEnv) == "" {
		command := exec.Command(os.Args[0], "-test.run=^TestManualS3SyncTransfersDocumentsAndAssetsWithoutAccount$", "-test.timeout=60s")
		command.Env = append(os.Environ(), childEnv+"=1")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("manual S3 synchronization failed: %v\n%s", err, output)
		}
		return
	}
	storage := &manualS3Storage{objects: map[string][]byte{}}
	server := httptest.NewServer(storage)
	defer server.Close()
	prepareManualS3SyncTest(t, server.URL)
	files := map[string][]byte{
		"20261028000000-notes01/20261028000001-note001.sy": []byte(`{"ID":"20261028000001-note001","Type":"NodeDocument","Properties":{"id":"20261028000001-note001","title":"Private note"},"Children":[]}`),
		"assets/example.bin": []byte("private attachment contents"),
	}
	for name, data := range files {
		file := filepath.Join(util.DataDir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := SyncDataUpload(); err != nil {
		t.Fatalf("first workspace upload failed: %v", err)
	}
	storage.mu.Lock()
	for name, data := range storage.objects {
		if bytes.Contains(data, []byte("Private note")) || bytes.Contains(data, files["assets/example.bin"]) {
			t.Errorf("cloud object %s exposes document or attachment plaintext", name)
		}
	}
	storage.mu.Unlock()
	t.Run("wrong-key-preserves-local-data", func(t *testing.T) {
		prepareManualS3SyncTest(t, server.URL)
		key := []byte("different-key-0123456789abcdef01")
		Conf.Repo.Load().Key = key
		if err := SyncDataDownload(); err == nil {
			t.Fatal("a different repository key must not decrypt cloud data")
		}
		if !bytes.Equal(Conf.Repo.Load().Key, key) || !Conf.Sync.Load().Enabled {
			t.Fatal("authentication failure changed the key or disabled synchronization")
		}
		if data, err := os.ReadFile(filepath.Join(util.DataDir, "note.txt")); err != nil || string(data) != "local note must survive" {
			t.Fatalf("wrong key lost the existing local note: %q, %v", data, err)
		}
	})
	prepareManualS3SyncTest(t, server.URL)
	Conf.SetUser(&conf.User{UserId: "free-account", UserSiYuanSubscriptionStatus: 1})
	if err := SyncDataDownload(); err != nil {
		t.Fatalf("second workspace download failed: %v", err)
	}
	for name, expected := range files {
		actual, err := os.ReadFile(filepath.Join(util.DataDir, filepath.FromSlash(name)))
		if err != nil || !bytes.Equal(actual, expected) {
			t.Fatalf("downloaded file %s differs: %q, %v", name, actual, err)
		}
	}
}
