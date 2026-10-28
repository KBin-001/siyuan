package api

import (
	archivezip "archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/siyuan-note/siyuan/kernel/conf"
	"github.com/siyuan-note/siyuan/kernel/model"
	"github.com/siyuan-note/siyuan/kernel/util"
)

func prepareS3OnboardingTest(t *testing.T) *gin.Engine {
	t.Helper()
	syncTestConfiguration(t)
	previousConfDir, previousRepo, previousData := util.ConfDir, util.RepoDir, util.DataDir
	t.Cleanup(func() { util.ConfDir, util.RepoDir, util.DataDir = previousConfDir, previousRepo, previousData })
	root := t.TempDir()
	util.ConfDir, util.RepoDir, util.DataDir = filepath.Join(root, "conf"), filepath.Join(root, "repo"), filepath.Join(root, "data")
	if err := os.MkdirAll(util.ConfDir, 0755); err != nil {
		t.Fatal(err)
	}
	util.ReadOnly = false
	model.Conf.Repo.Store(conf.NewRepo())
	model.Conf.Repo.Load().Key = bytes.Repeat([]byte{7}, 32)
	model.Conf.Sync.Load().S3 = &conf.S3{Endpoint: "https://example.r2.cloudflarestorage.com/", AccessKey: "test-key",
		SecretKey: "test-secret", Bucket: "notes", Region: "auto", Timeout: 30, ConcurrentReqs: 4, PathStyle: true}
	model.Conf.Sync.Load().Provider = conf.ProviderS3
	model.Conf.Sync.Load().Enabled = true
	model.Conf.Save()
	engine := gin.New()
	engine.Use(gin.Recovery())
	engine.POST("/api/sync/importSyncProviderS3", importSyncProviderS3)
	engine.POST("/api/sync/exportSyncProviderS3", exportSyncProviderS3)
	return engine
}

func importS3Package(t *testing.T, engine *gin.Engine, name string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/sync/importSyncProviderS3", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	return response
}

func TestS3OnboardingRejectsCorruptPackageWithoutChangingWorkspace(t *testing.T) {
	engine := prepareS3OnboardingTest(t)
	before, err := os.ReadFile(filepath.Join(util.ConfDir, "conf.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{"", "00", "not-encrypted-json", util.AESEncrypt("{"), util.AESEncrypt(strings.Repeat("x", 32))[:32]} {
		t.Run(content, func(t *testing.T) {
			response := importS3Package(t, engine, "siyuan-s3.json", []byte(content))
			var result struct{ Code int }
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != http.StatusOK || result.Code != -1 {
				t.Fatalf("corrupt package must return a configuration error: %d %s (%v)", response.Code, response.Body.String(), err)
			}
			after, err := os.ReadFile(filepath.Join(util.ConfDir, "conf.json"))
			if err != nil || !bytes.Equal(before, after) || !bytes.Equal(model.Conf.Repo.Load().Key, bytes.Repeat([]byte{7}, 32)) || !model.Conf.Sync.Load().Enabled {
				t.Fatal("corrupt package changed persisted configuration, key or enabled state")
			}
		})
	}
}

func TestS3OnboardingExportImportPersistsOnFreshDesktopWithoutReplacingKey(t *testing.T) {
	engine := prepareS3OnboardingTest(t)
	expected := *model.Conf.Sync.Load().S3
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/sync/exportSyncProviderS3", nil))
	var exported struct {
		Code int                        `json:"code"`
		Data struct{ Name, Zip string } `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &exported); err != nil || exported.Code != 0 {
		t.Fatalf("export failed: %s (%v)", response.Body.String(), err)
	}
	zipPath := filepath.Join(util.TempDir, filepath.FromSlash(strings.TrimPrefix(exported.Data.Zip, "/")))
	zipData, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := archivezip.OpenReader(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	if len(archive.File) != 1 {
		t.Fatal("export must contain one configuration file")
	}
	reader, err := archive.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	jsonData, err := io.ReadAll(reader)
	reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range []struct {
		name string
		data []byte
	}{{exported.Data.Name, jsonData}, {exported.Data.Name + ".zip", zipData}} {
		t.Run(pkg.name, func(t *testing.T) {
			second := prepareS3OnboardingTest(t)
			model.Conf.Sync.Load().S3 = nil
			model.Conf.Sync.Load().Enabled = false
			key := bytes.Repeat([]byte{9}, 32)
			model.Conf.Repo.Load().Key = append([]byte(nil), key...)
			if err := os.MkdirAll(util.DataDir, 0755); err != nil {
				t.Fatal(err)
			}
			note := filepath.Join(util.DataDir, "existing-note.txt")
			if err := os.WriteFile(note, []byte("keep my local work"), 0644); err != nil {
				t.Fatal(err)
			}
			imported := importS3Package(t, second, pkg.name, pkg.data)
			var result struct{ Code int }
			if err := json.Unmarshal(imported.Body.Bytes(), &result); err != nil || result.Code != 0 {
				t.Fatalf("fresh desktop import failed: %s (%v)", imported.Body.String(), err)
			}
			saved, err := os.ReadFile(filepath.Join(util.ConfDir, "conf.json"))
			if err != nil {
				t.Fatal(err)
			}
			restarted := model.NewAppConf()
			if err = json.Unmarshal(saved, restarted); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(restarted.Sync.Load().S3, &expected) || !bytes.Equal(restarted.Repo.Load().Key, key) || restarted.GetUser() != nil || restarted.Sync.Load().Enabled {
				t.Fatalf("restarted state: same settings=%t, same key=%t, signed out=%t, sync enabled=%t",
					reflect.DeepEqual(restarted.Sync.Load().S3, &expected), bytes.Equal(restarted.Repo.Load().Key, key),
					restarted.GetUser() == nil, restarted.Sync.Load().Enabled)
			}
			if data, err := os.ReadFile(note); err != nil || string(data) != "keep my local work" {
				t.Fatalf("import lost existing local work: %q (%v)", data, err)
			}
		})
	}
}
