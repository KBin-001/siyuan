package dejavu

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	legacy "github.com/siyuan-note/dejavu"
	"github.com/siyuan-note/dejavu/cloud"
	"github.com/siyuan-note/dejavu/entity"
)

var compatibilityKey = []byte("0123456789abcdef0123456789abcdef")

func compatibilityOptions(root string) Options {
	return Options{DataPath: filepath.Join(root, "data"), RepoPath: filepath.Join(root, "repo"),
		HistoryPath: filepath.Join(root, "history"), TempPath: filepath.Join(root, "temp"),
		DeviceID: "compatibility-device", AESKey: compatibilityKey, ContentFileIDs: true}
}

func writeCompatibilityFile(t *testing.T, root, content string, second int64) {
	t.Helper()
	file := filepath.Join(root, "data", "note.sy")
	if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(second, 0)
	if err := os.Chtimes(file, stamp, stamp); err != nil {
		t.Fatal(err)
	}
}

func TestContentIdentityKeepsOldSnapshotsAndLegacyReader(t *testing.T) {
	root := t.TempDir()
	options := compatibilityOptions(root)
	old, err := legacy.NewRepo(options.DataPath, options.RepoPath, options.HistoryPath, options.TempPath,
		options.DeviceID, "legacy", "windows", options.AESKey, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	writeCompatibilityFile(t, root, "legacy authenticated content", 1700000000)
	before, err := old.Index("legacy snapshot", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := NewRepoWithOptions(options)
	if err != nil {
		t.Fatal(err)
	}
	stable, err := engine.Index("unchanged legacy", true, nil)
	if err != nil || stable.ID != before.ID {
		t.Fatalf("unchanged legacy snapshot churn: %v", err)
	}
	writeCompatibilityFile(t, root, "new authenticated content", 1700000010)
	after, err := engine.Index("content identity", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, snapshot := range []struct {
		index   *entity.Index
		content string
	}{
		{before, "legacy authenticated content"}, {after, "new authenticated content"},
	} {
		for _, reader := range []struct {
			name     string
			checkout func(string, map[string]interface{}) ([]*entity.File, []*entity.File, error)
		}{
			{"new", engine.Checkout}, {"legacy", old.Checkout},
		} {
			if _, _, err := reader.checkout(snapshot.index.ID, nil); err != nil {
				t.Fatalf("%s checkout: %v", reader.name, err)
			}
			actual, err := os.ReadFile(filepath.Join(root, "data", "note.sy"))
			if err != nil || string(actual) != snapshot.content {
				t.Fatalf("%s restored %q (%v), want %q", reader.name, actual, err, snapshot.content)
			}
		}
	}
	stable, err = engine.Index("unchanged new", true, nil)
	if err != nil || stable.ID != after.ID {
		t.Fatalf("new snapshot did not converge: %v", err)
	}
}

func TestStoreCacheCannotBypassWrongRepositoryKey(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root, compatibilityKey)
	if err != nil {
		t.Fatal(err)
	}
	file := entity.NewFile("/note.sy", 4, 1700000000000)
	file.Chunks = []string{"0123456789012345678901234567890123456789"}
	if err := store.PutFile(file); err != nil {
		t.Fatal(err)
	}
	fileCache.Wait()
	wrong, err := NewStore(root, bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wrong.GetFile(file.ID); err == nil {
		t.Fatal("cached metadata bypassed repository key authentication")
	}
}

func TestLegacyFixtureRecoveryAndAuthentication(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("testdata/legacy-v1")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct{ Index, Key, Note string }
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	options := compatibilityOptions(root)
	options.AESKey = []byte(manifest.Key)
	engine, err := NewRepoWithOptions(options)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := engine.Checkout(manifest.Index, nil); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(filepath.Join(options.DataPath, "note.sy"))
	if err != nil || string(actual) != manifest.Note {
		t.Fatalf("legacy recovery: %q (%v)", actual, err)
	}
	index, err := engine.GetIndex(manifest.Index)
	if err != nil {
		t.Fatal(err)
	}
	fileID := index.Files[0]
	wrong, err := NewStore(options.RepoPath, bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wrong.GetFile(fileID); err == nil {
		t.Fatal("wrong key accepted legacy fixture")
	}
	corruptRoot := t.TempDir()
	if err := os.CopyFS(corruptRoot, os.DirFS(options.RepoPath)); err != nil {
		t.Fatal(err)
	}
	corrupt, err := NewStore(corruptRoot, options.AESKey)
	if err != nil {
		t.Fatal(err)
	}
	_, object := corrupt.AbsPath(fileID)
	ciphertext, err := os.ReadFile(object)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext[len(ciphertext)-1] ^= 1
	if err := os.WriteFile(object, ciphertext, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := corrupt.GetFile(fileID); err == nil {
		t.Fatal("corrupt legacy object accepted")
	}
	preserved, err := os.ReadFile(object)
	if err != nil || !bytes.Equal(preserved, ciphertext) {
		t.Fatal("authentication error replaced corrupt source")
	}
}

func TestLegacyAndContentSnapshotsCloudBackupRecovery(t *testing.T) {
	remote := t.TempDir()
	if err := os.MkdirAll(filepath.Join(remote, "main", "refs"), 0755); err != nil {
		t.Fatal(err)
	}
	options := compatibilityOptions(t.TempDir())
	options.Cloud = cloud.NewLocal(&cloud.BaseCloud{Conf: &cloud.Conf{Dir: "main", AvailableSize: 1 << 30,
		Local: &cloud.ConfLocal{Endpoint: remote}}})
	old, err := legacy.NewRepo(options.DataPath, options.RepoPath, options.HistoryPath, options.TempPath,
		options.DeviceID, "legacy", "windows", options.AESKey, nil, options.Cloud)
	if err != nil {
		t.Fatal(err)
	}
	writeCompatibilityFile(t, filepath.Dir(options.DataPath), "legacy backup content", 1700000000)
	before, err := old.Index("legacy backup", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := NewRepoWithOptions(options)
	if err != nil {
		t.Fatal(err)
	}
	writeCompatibilityFile(t, filepath.Dir(options.DataPath), "content identity backup", 1700000010)
	after, err := engine.Index("new backup", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, snapshot := range []struct {
		index     *entity.Index
		body, tag string
	}{
		{before, "legacy backup content", "legacy"}, {after, "content identity backup", "content"},
	} {
		if err := engine.AddTag(snapshot.index.ID, snapshot.tag); err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := engine.UploadTagIndex(snapshot.tag, snapshot.index.ID, nil); err != nil {
			t.Fatal(err)
		}
		fresh := compatibilityOptions(t.TempDir())
		fresh.Cloud = cloud.NewLocal(&cloud.BaseCloud{Conf: &cloud.Conf{Dir: "main", AvailableSize: 1 << 30,
			Local: &cloud.ConfLocal{Endpoint: remote}}})
		restored, err := NewRepoWithOptions(fresh)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := restored.DownloadTagIndex(snapshot.tag, snapshot.index.ID, nil); err != nil {
			t.Fatal(err)
		}
		if _, _, err := restored.Checkout(snapshot.index.ID, nil); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(fresh.DataPath, "note.sy"))
		if err != nil || string(data) != snapshot.body {
			t.Fatalf("backup recovery: %q (%v)", data, err)
		}
		peerOptions := compatibilityOptions(t.TempDir())
		peerOptions.Cloud = &objectsUnavailableCloud{Cloud: cloud.NewLocal(&cloud.BaseCloud{Conf: &cloud.Conf{Dir: "main", AvailableSize: 1 << 30,
			Local: &cloud.ConfLocal{Endpoint: remote}}})}
		peerReader, err := NewRepoWithOptions(peerOptions)
		if err != nil {
			t.Fatal(err)
		}
		peerReader.SetChunkSource(&repositoryPeer{root: options.RepoPath})
		if _, _, _, err := peerReader.DownloadIndex(snapshot.index.ID, nil); err != nil {
			t.Fatalf("%s peer recovery: %v", snapshot.tag, err)
		}
		if _, _, err := peerReader.Checkout(snapshot.index.ID, nil); err != nil {
			t.Fatal(err)
		}
		data, err = os.ReadFile(filepath.Join(peerOptions.DataPath, "note.sy"))
		if err != nil || string(data) != snapshot.body {
			t.Fatalf("peer recovery: %q (%v)", data, err)
		}
	}
}

type objectsUnavailableCloud struct{ cloud.Cloud }

func (backend *objectsUnavailableCloud) DownloadObject(name string) ([]byte, error) {
	if strings.HasPrefix(name, "objects/") {
		return nil, errors.New("cloud objects unavailable")
	}
	return backend.Cloud.DownloadObject(name)
}

type repositoryPeer struct{ root string }

func (peer *repositoryPeer) Name() string           { return "compatibility-peer" }
func (peer *repositoryPeer) GetConcurrentReqs() int { return 1 }
func (peer *repositoryPeer) HasChunks(ids []string) (map[string]bool, error) {
	ret := map[string]bool{}
	for _, id := range ids {
		_, err := os.Stat(filepath.Join(peer.root, "objects", id[:2], id[2:]))
		ret[id] = err == nil
	}
	return ret, nil
}
func (peer *repositoryPeer) HasObjects(ids []string) (map[string]bool, error) {
	return peer.HasChunks(ids)
}
func (peer *repositoryPeer) DownloadChunk(id string) ([]byte, error) {
	return os.ReadFile(filepath.Join(peer.root, "objects", id[:2], id[2:]))
}
func (peer *repositoryPeer) DownloadChunkValidated(id string, validate func([]byte) error) ([]byte, error) {
	data, err := peer.DownloadChunk(id)
	if err == nil {
		err = validate(data)
	}
	return data, err
}
func (peer *repositoryPeer) DownloadObjectValidated(id string, validate func([]byte) error) ([]byte, error) {
	return peer.DownloadChunkValidated(id, validate)
}
