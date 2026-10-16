// SPDX-License-Identifier: AGPL-3.0-or-later
package localexport

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/unicode"
)

// Source 仅存放于本机工作空间配置，不写入会同步的文档属性。
type Source struct {
	Box      string `json:"box"`
	Path     string `json:"path"`
	Encoding string `json:"encoding,omitempty"`
	CRLF     bool   `json:"crlf,omitempty"`
	Snapshot string `json:"snapshot,omitempty"`
}

func (s Source) config(id string) Config {
	return Config{Enabled: true, Box: s.Box, Root: id, Target: filepath.Dir(s.Path)}
}

func (m *Manager) Source(id string) (Source, Entry, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.Sources[id]
	return s, m.Entries[key(filepath.Dir(s.Path), id)], ok
}

func (m *Manager) RememberSource(id, snapshot string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if source, ok := m.Sources[id]; ok {
		source.Snapshot = snapshot
		m.Sources[id] = source
		m.dirty = true
	}
}

// AcceptSource 在文档条件写成功后更新同步基线，清除本次载入触发的回写任务，避免写回循环。
func (m *Manager) AcceptSource(id string, source Source, revision, snapshot string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	source.Snapshot = snapshot
	m.Sources[id] = source
	e := m.Entries[key(filepath.Dir(source.Path), id)]
	e.Hash, e.State, e.Error, e.Conflict = revision, "success", "", ""
	e.LastSuccess = time.Now().UTC().Format(time.RFC3339)
	m.lastSuccess = e.LastSuccess
	m.Entries[key(filepath.Dir(source.Path), id)] = e
	delete(m.Pending, id)
	m.state, m.failure, m.dirty = "success", "", true
}

// PreserveSourceCopy 只创建冲突副本，不尝试替换外部文件，避免冲突处理中的第二次竞争写入。
func (m *Manager) PreserveSourceCopy(id string, data []byte, assets map[string][]byte) error {
	source, entry, linked := m.Source(id)
	if !linked {
		return errors.New("document has no local file")
	}
	if err := m.writeAssets(filepath.Dir(source.Path), assets); err != nil {
		return err
	}
	ext := filepath.Ext(source.Path)
	base := strings.TrimSuffix(filepath.Base(source.Path), ext) + ".siyuan-" + id + "-" + Digest(data)[:12]
	p, err := m.outputPath(filepath.Dir(source.Path), base+ext)
	if err != nil {
		return err
	}
	previous, err := fileHash(p)
	if err != nil {
		return err
	}
	if previous != "" && previous != Digest(data) {
		p, err = m.outputPath(filepath.Dir(source.Path), base+"-"+time.Now().UTC().Format("20060102T150405.000000000")+ext)
		if err != nil {
			return err
		}
		previous = ""
	}
	if previous == "" {
		if err = publishNew(p, data); err != nil {
			return err
		}
	}
	failure := &ConflictError{Path: source.Path, Copy: p}
	if entry.State == "conflict" && entry.Error == failure.Error() && entry.Conflict == p {
		return failure
	}
	m.mu.Lock()
	entry.State, entry.Error, entry.Conflict = "conflict", failure.Error(), p
	m.Entries[key(filepath.Dir(source.Path), id)] = entry
	m.state, m.failure, m.dirty = "conflict", failure.Error(), true
	m.mu.Unlock()
	return failure
}

// RefreshSource 仅检查打开文档对应的文件，外部替换保存产生的短暂缺失由下次检查恢复。
func (m *Manager) RefreshSource(id string, refresh func(Source, Entry, []byte) error) (bool, error) {
	m.io.Lock()
	defer m.io.Unlock()
	source, entry, linked := m.Source(id)
	if !linked {
		return false, nil
	}
	p, _, err := m.InspectSource(source.Path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	revision := Digest(data)
	if revision == entry.Hash {
		return false, nil
	}
	if err = refresh(source, entry, data); err != nil {
		m.mu.Lock()
		var persistErr error
		if m.dirty {
			persistErr = m.persistLocked()
		}
		m.mu.Unlock()
		if persistErr != nil {
			return false, persistErr
		}
		return false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err = m.persistLocked(); err != nil {
		return true, err
	}
	return true, nil
}

func (m *Manager) InspectSource(p string) (string, string, error) {
	if !filepath.IsAbs(p) {
		return "", "", errors.New("absolute file path required")
	}
	switch strings.ToLower(filepath.Ext(p)) {
	case ".md", ".markdown", ".txt":
	default:
		return "", "", errors.New("supported file types: .md, .markdown, .txt")
	}
	dir, err := ValidateTarget(filepath.Dir(p), m.dataDir)
	if err != nil {
		return "", "", err
	}
	p, err = m.outputPath(dir, filepath.Base(p))
	if err != nil {
		return "", "", err
	}
	hash, err := fileHash(p)
	return p, hash, err
}

// BindSource 用已读取或经用户确认的版本建立归属，不把已有文件当作首次导出冲突。
func (m *Manager) BindSource(id, box, p, revision string) error {
	m.io.Lock()
	defer m.io.Unlock()
	p, actual, err := m.InspectSource(p)
	if err != nil {
		return err
	}
	if actual != revision {
		return errors.New("local file changed before association; choose the file again")
	}
	s := Source{Box: box, Path: p}
	if actual != "" {
		data, readErr := os.ReadFile(p)
		if readErr != nil {
			return readErr
		}
		if Digest(data) != revision {
			return errors.New("local file changed before association")
		}
		if _, err = s.Decode(data); err != nil {
			return err
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	ownerKey := key(filepath.Dir(p), strings.ToLower(filepath.Base(p)))
	if owner := m.Owners[ownerKey]; owner != "" && owner != id {
		return errors.New("local file is already associated with another document")
	}
	oldSource, hadSource := m.Sources[id]
	entryKey := key(filepath.Dir(p), id)
	oldEntry, hadEntry := m.Entries[entryKey]
	oldOwner := m.Owners[ownerKey]
	m.Sources[id] = s
	m.Owners[ownerKey] = id
	m.Entries[entryKey] = Entry{ID: id, Path: filepath.Base(p), Base: filepath.Base(p), Hash: revision, State: "idle"}
	if err = m.persistLocked(); err != nil {
		if hadSource {
			m.Sources[id] = oldSource
		} else {
			delete(m.Sources, id)
		}
		if hadEntry {
			m.Entries[entryKey] = oldEntry
		} else {
			delete(m.Entries, entryKey)
		}
		if oldOwner != "" {
			m.Owners[ownerKey] = oldOwner
		} else {
			delete(m.Owners, ownerKey)
		}
	}
	return err
}

// SaveSource 等待当前文件写入完成，期间收到的后续编辑继续保留在防抖队列。
func (m *Manager) SaveSource(id string, run func(Config, string) error) error {
	m.io.Lock()
	defer m.io.Unlock()
	m.mu.Lock()
	s, ok := m.Sources[id]
	if !ok {
		m.mu.Unlock()
		return errors.New("document has no local file")
	}
	due := m.Pending[id]
	m.state, m.failure = "exporting", ""
	m.mu.Unlock()
	c := s.config(id)
	err := runSafely(c, id, run)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.finishLocked(c, id, due, err)
	if err == nil && m.state == "failed" {
		return errors.New(m.failure)
	}
	return err
}

// Decode 保留 UTF-8、带 BOM 的 UTF-16 和换行形式，拒绝不明编码以免损坏源文件。
func (s *Source) Decode(data []byte) (string, error) {
	s.Encoding = "utf8"
	if bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) {
		s.Encoding, data = "utf8bom", data[3:]
	} else if bytes.HasPrefix(data, []byte{0xff, 0xfe}) || bytes.HasPrefix(data, []byte{0xfe, 0xff}) {
		original := data
		order := unicode.LittleEndian
		s.Encoding = "utf16le"
		if data[0] == 0xfe {
			order, s.Encoding = unicode.BigEndian, "utf16be"
		}
		var err error
		data, err = unicode.UTF16(order, unicode.ExpectBOM).NewDecoder().Bytes(data)
		if err != nil {
			return "", err
		}
		roundTrip, encodeErr := unicode.UTF16(order, unicode.UseBOM).NewEncoder().Bytes(data)
		if encodeErr != nil || !bytes.Equal(roundTrip, original) {
			return "", errors.New("invalid UTF-16 text file")
		}
	}
	if !utf8.Valid(data) || bytes.ContainsRune(data, 0) {
		return "", errors.New("local text file must use UTF-8 or UTF-16 with BOM")
	}
	s.CRLF = bytes.Contains(data, []byte("\r\n"))
	return strings.ReplaceAll(string(data), "\r\n", "\n"), nil
}

func (s Source) Encode(text string) ([]byte, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if s.CRLF {
		text = strings.ReplaceAll(text, "\n", "\r\n")
	}
	switch s.Encoding {
	case "utf8bom":
		return append([]byte{0xef, 0xbb, 0xbf}, []byte(text)...), nil
	case "utf16le":
		return unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewEncoder().Bytes([]byte(text))
	case "utf16be":
		return unicode.UTF16(unicode.BigEndian, unicode.UseBOM).NewEncoder().Bytes([]byte(text))
	default:
		return []byte(text), nil
	}
}
