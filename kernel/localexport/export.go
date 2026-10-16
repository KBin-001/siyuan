// SPDX-License-Identifier: AGPL-3.0-or-later

// Package localexport 管理本机 Markdown 镜像、持久化队列及文件冲突，不接触源文档格式。
package localexport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

type Config struct {
	Enabled bool   `json:"enabled"`
	Box     string `json:"box"`
	Root    string `json:"root"`
	Target  string `json:"target"`
}

var ErrOutOfScope = errors.New("document is outside the selected scope")

type Entry struct {
	ID          string `json:"id"`
	Path        string `json:"path"`
	Base        string `json:"base"`
	Hash        string `json:"hash"`
	State       string `json:"state"`
	Error       string `json:"error"`
	Conflict    string `json:"conflict"`
	LastSuccess string `json:"lastSuccess"`
}

type Status struct {
	Config      Config  `json:"config"`
	State       string  `json:"state"`
	Error       string  `json:"error"`
	LastSuccess string  `json:"lastSuccess"`
	Entries     []Entry `json:"entries"`
	Pending     int     `json:"pending"`
}

type diskState struct {
	Version     int               `json:"version"`
	Config      Config            `json:"config"`
	Entries     map[string]Entry  `json:"entries"`
	Pending     map[string]int64  `json:"pending"`
	Owners      map[string]string `json:"owners"`
	Sources     map[string]Source `json:"sources,omitempty"`
	State       string            `json:"state"`
	Error       string            `json:"error"`
	LastSuccess string            `json:"lastSuccess"`
}

// Manager 的文件操作由 worker 串行执行，通知仅更新内存队列，不阻塞源文档保存。
type Manager struct {
	mu sync.Mutex
	io sync.Mutex
	diskState
	file, dataDir, state, failure, lastSuccess string
	dirty                                      bool
}

func Open(file, dataDir string) (*Manager, error) {
	m := &Manager{file: file, dataDir: dataDir, state: "idle"}
	m.Version = 1
	m.Entries = map[string]Entry{}
	m.Pending = map[string]int64{}
	m.Owners = map[string]string{}
	m.Sources = map[string]Source{}
	data, err := os.ReadFile(file)
	if err == nil {
		if err = json.Unmarshal(data, &m.diskState); err != nil {
			return nil, err
		}
		if m.Version != 1 || m.Entries == nil || m.Pending == nil {
			return nil, errors.New("unsupported local export state")
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if m.Owners == nil {
		m.Owners = map[string]string{}
	}
	if m.Sources == nil {
		m.Sources = map[string]Source{}
	}
	if m.diskState.State != "" {
		m.state = m.diskState.State
	}
	m.failure, m.lastSuccess = m.diskState.Error, m.diskState.LastSuccess
	for _, e := range m.Entries {
		if e.LastSuccess > m.lastSuccess {
			m.lastSuccess = e.LastSuccess
		}
	}
	return m, nil
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := Status{Config: m.Config, State: m.state, Error: m.failure, LastSuccess: m.lastSuccess, Entries: []Entry{}, Pending: len(m.Pending)}
	for _, e := range m.Entries {
		s.Entries = append(s.Entries, e)
	}
	sort.Slice(s.Entries, func(i, j int) bool { return s.Entries[i].ID < s.Entries[j].ID })
	return s
}

func (m *Manager) Outcome() (string, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state, m.failure
}

func (m *Manager) Configuration() Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Config
}

func (m *Manager) Configure(c Config) error {
	m.io.Lock()
	defer m.io.Unlock()
	if c.Enabled || c.Target != "" {
		target, err := ValidateTarget(c.Target, m.dataDir)
		if err != nil {
			return err
		}
		c.Target = target
	}
	if c.Enabled && c.Box == "" {
		return errors.New("notebook required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	old := m.Config
	oldState, oldFailure := m.state, m.failure
	m.Config = c
	m.state, m.failure = "idle", ""
	if err := m.persistLocked(); err != nil {
		m.Config = old
		m.state, m.failure = oldState, oldFailure
		return err
	}
	return nil
}

func (m *Manager) Notify(id, box string, immediate bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	source, linked := m.Sources[id]
	if !linked && (!m.Config.Enabled || m.Config.Box != box) {
		return
	}
	if linked && source.Box != box {
		source.Box = box
		m.Sources[id] = source
	}
	due := time.Now().Add(3 * time.Second).UnixMilli()
	if immediate {
		due = 0
	}
	m.Pending[id] = due
	m.dirty = true
}

func (m *Manager) Retry() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, e := range m.Entries {
		_, linked := m.Sources[e.ID]
		if (linked || strings.HasPrefix(k, strings.ToLower(filepath.Clean(m.Config.Target))+"|")) && (e.State == "failed" || e.State == "conflict") {
			m.Pending[e.ID] = 0
		}
	}
	m.dirty = true
}

// Allocate 为文件和目录保留稳定归属，旧路径不再分配给另一文档。
func (m *Manager) Allocate(c Config, id, base string, directory bool) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !directory {
		if e := m.Entries[key(c.Target, id)]; e.Base == base && e.Path != "" {
			return e.Path
		}
	}
	rel := base
	ownerKey := key(c.Target, strings.ToLower(rel))
	for attempt := 0; m.Owners[ownerKey] != "" && m.Owners[ownerKey] != id; attempt++ {
		suffix := "-" + id
		if attempt > 0 {
			suffix += fmt.Sprintf("-%d", attempt)
		}
		if directory {
			rel = base + suffix
		} else {
			rel = strings.TrimSuffix(base, ".md") + suffix + ".md"
		}
		ownerKey = key(c.Target, strings.ToLower(rel))
	}
	m.Owners[ownerKey] = id
	m.dirty = true
	return rel
}

// Tick 先持久化变更集合，再逐个执行到期任务；导出失败不向源保存流程返回错误。
func (m *Manager) Tick(run func(Config, string) error) {
	m.io.Lock()
	defer m.io.Unlock()
	m.mu.Lock()
	if m.dirty {
		if err := m.persistLocked(); err != nil {
			m.state, m.failure = "failed", err.Error()
			m.mu.Unlock()
			return
		}
	}
	var id string
	for candidate, due := range m.Pending {
		_, linked := m.Sources[candidate]
		if (linked || m.Config.Enabled) && due <= time.Now().UnixMilli() && (id == "" || candidate < id) {
			id = candidate
		}
	}
	if id == "" {
		m.mu.Unlock()
		return
	}
	c, due := m.Config, m.Pending[id]
	if source, linked := m.Sources[id]; linked {
		c = source.config(id)
	}
	m.state = "exporting"
	m.mu.Unlock()
	err := runSafely(c, id, run)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.finishLocked(c, id, due, err)
}

func (m *Manager) finishLocked(c Config, id string, due int64, err error) {
	if m.Pending[id] == due {
		delete(m.Pending, id)
	}
	if errors.Is(err, ErrOutOfScope) {
		m.state = "idle"
		m.dirty = true
		if saveErr := m.persistLocked(); saveErr != nil {
			m.state, m.failure = "failed", saveErr.Error()
		}
		return
	}
	e := m.Entries[key(c.Target, id)]
	e.ID = id
	if err != nil {
		e.State, e.Error = "failed", err.Error()
		var conflict *ConflictError
		if errors.As(err, &conflict) {
			e.State, e.Conflict = "conflict", conflict.Copy
		}
	} else if e.State != "conflict" {
		e.State, e.Error, e.Conflict = "success", "", ""
		e.LastSuccess = time.Now().UTC().Format(time.RFC3339)
		m.lastSuccess = e.LastSuccess
	}
	m.Entries[key(c.Target, id)] = e
	m.state, m.failure = e.State, e.Error
	m.dirty = true
	if err = m.persistLocked(); err != nil {
		m.state, m.failure = "failed", err.Error()
	}
}

func runSafely(c Config, id string, run func(Config, string) error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("Markdown export panic: %v", recovered)
		}
	}()
	return run(c, id)
}

func (m *Manager) persistLocked() error {
	m.diskState.State, m.diskState.Error, m.diskState.LastSuccess = m.state, m.failure, m.lastSuccess
	snapshot := m.diskState
	snapshot.Entries, snapshot.Pending, snapshot.Owners = maps.Clone(m.Entries), maps.Clone(m.Pending), maps.Clone(m.Owners)
	snapshot.Sources = maps.Clone(m.Sources)
	m.dirty = false
	m.mu.Unlock()
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err == nil {
		err = atomicWrite(m.file, data)
	}
	m.mu.Lock()
	if err != nil {
		m.dirty = true
	}
	return err
}

func (m *Manager) Flush() error {
	m.io.Lock()
	defer m.io.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.persistLocked()
}

func key(target, id string) string { return strings.ToLower(filepath.Clean(target)) + "|" + id }

func Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// SafeName 对所有平台采用 Windows 的文件名约束，保留可读名称并限制单段长度。
func SafeName(name string) string {
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune("<>:\"/\\|?*", r) {
			return '_'
		}
		return r
	}, name)
	name = strings.TrimRight(strings.TrimSpace(name), ". ")
	if name == "" || name == "." || name == ".." {
		name = "_"
	}
	base := strings.ToUpper(strings.SplitN(name, ".", 2)[0])
	device := strings.TrimPrefix(strings.TrimPrefix(base, "COM"), "LPT")
	if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || base == "CONIN$" || base == "CONOUT$" ||
		(strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && (device == "¹" || device == "²" || device == "³") ||
		len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '0' && base[3] <= '9' {
		name = "_" + name
	}
	if len([]rune(name)) > 80 {
		name = string([]rune(name)[:64]) + "-" + Digest([]byte(name))[:12]
	}
	return name
}

// RelativePath 仅去掉与目标目录同名的第一级目录，避免 kb/kb，同时保留其余目录层级。
func RelativePath(target string, titles []string) string {
	parts := make([]string, 0, len(titles))
	for _, title := range titles {
		parts = append(parts, SafeName(title))
	}
	if len(parts) > 1 && strings.EqualFold(parts[0], filepath.Base(target)) {
		parts = parts[1:]
	}
	return filepath.Join(parts...) + ".md"
}

func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// canonical 解析已有父目录的符号链接，以便校验尚不存在的目标目录。
func canonical(p string) (string, error) {
	p, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	var tail []string
	for {
		resolved, e := filepath.EvalSymlinks(p)
		if e == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, tail[i])
			}
			return resolved, nil
		}
		if !os.IsNotExist(e) {
			return "", e
		}
		parent := filepath.Dir(p)
		if parent == p {
			return "", e
		}
		tail, p = append(tail, filepath.Base(p)), parent
	}
}

func ValidateTarget(target, dataDir string) (string, error) {
	if !filepath.IsAbs(target) {
		return "", errors.New("absolute target directory required")
	}
	target, err := canonical(target)
	if err != nil {
		return "", err
	}
	dataDir, err = canonical(dataDir)
	if err != nil {
		return "", err
	}
	if within(dataDir, target) {
		return "", errors.New("target must be outside the workspace data directory")
	}
	return target, nil
}

func safePath(root, rel string) (string, error) {
	if rel == "" || filepath.IsAbs(rel) || !filepath.IsLocal(rel) {
		return "", errors.New("invalid export relative path")
	}
	p := filepath.Join(root, rel)
	resolved, err := canonical(p)
	if err != nil {
		return "", err
	}
	if !within(root, resolved) || !strings.EqualFold(filepath.Clean(p), filepath.Clean(resolved)) {
		return "", errors.New("export path contains a symbolic link or escapes target")
	}
	return p, nil
}

func (m *Manager) outputPath(root, rel string) (string, error) {
	p, err := safePath(root, rel)
	if err != nil {
		return "", err
	}
	if _, err = ValidateTarget(p, m.dataDir); err != nil {
		return "", err
	}
	return p, nil
}

type ConflictError struct{ Path, Copy string }

func (e *ConflictError) Error() string {
	return fmt.Sprintf("local file conflict: %s; SiYuan copy: %s", e.Path, e.Copy)
}

func fileHash(p string) (string, error) {
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return Digest(data), nil
}

// Write 在 worker 内调用；附件先于 Markdown 发布，任何文件均不自动删除。
// 映射记录覆盖历史路径的归属，因此重命名后回到旧路径也不会覆盖其它文档。
func (m *Manager) Write(c Config, id, rel string, markdown []byte, assets map[string][]byte) error {
	target, err := ValidateTarget(c.Target, m.dataDir)
	if err != nil {
		return err
	}
	base := rel
	rel = m.Allocate(c, id, base, false)
	m.mu.Lock()
	e := m.Entries[key(c.Target, id)]
	m.mu.Unlock()
	p, err := m.outputPath(target, rel)
	if err != nil {
		return err
	}
	current, err := fileHash(p)
	if err != nil {
		return err
	}
	expected := ""
	if e.Path == rel {
		expected = e.Hash
	}
	conflict := current != "" && (expected == "" || current != expected)
	if err = m.writeAssets(target, assets); err != nil {
		return err
	}
	if conflict {
		ext := filepath.Ext(rel)
		copyRel := strings.TrimSuffix(rel, ext) + ".siyuan-" + id + "-" + Digest(markdown)[:12] + ext
		copyPath, pathErr := m.outputPath(target, copyRel)
		if pathErr != nil {
			return pathErr
		}
		copyHash, hashErr := fileHash(copyPath)
		if hashErr != nil {
			return hashErr
		}
		if copyHash != "" && copyHash != Digest(markdown) {
			copyPath = strings.TrimSuffix(copyPath, ext) + "-" + time.Now().UTC().Format("20060102T150405.000000000") + ext
		}
		if copyHash != Digest(markdown) {
			if err = publishNew(copyPath, markdown); err != nil {
				return err
			}
		}
		m.mu.Lock()
		if e.Path != rel {
			e.Hash = ""
		}
		e.ID, e.Path, e.Base = id, rel, base
		m.Entries[key(c.Target, id)] = e
		m.mu.Unlock()
		return &ConflictError{Path: p, Copy: copyPath}
	}
	if current == "" {
		err = publishNew(p, markdown)
	} else if current != Digest(markdown) {
		// 替换前再次校验，并保留被替换版本，保障检查与替换之间的外部写入仍可恢复。
		before, readErr := os.ReadFile(p)
		if readErr != nil {
			return readErr
		}
		if Digest(before) != expected {
			return &ConflictError{Path: p}
		}
		backup := p + ".previous-" + time.Now().UTC().Format("20060102T150405.000000000")
		tmp, tempErr := tempWrite(p, markdown)
		if tempErr != nil {
			return tempErr
		}
		defer os.Remove(tmp)
		err = replaceManaged(tmp, p, backup)
		if err == nil {
			actual, hashErr := fileHash(backup)
			if hashErr != nil {
				return hashErr
			}
			if actual != expected {
				return &ConflictError{Path: backup, Copy: p}
			}
		}
	}
	if err != nil {
		return err
	}
	m.mu.Lock()
	e.ID, e.Path, e.Base, e.Hash, e.State = id, rel, base, Digest(markdown), "success"
	m.Entries[key(c.Target, id)] = e
	m.mu.Unlock()
	return nil
}

// 临时文件先 fsync 再发布；首次发布使用硬链接的排他创建，绝不覆盖已有文件。
func tempWrite(p string, data []byte) (string, error) {
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(filepath.Dir(p), ".siyuan-export-*")
	if err != nil {
		return "", err
	}
	name := f.Name()
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(name)
		return "", err
	}
	return name, nil
}

func (m *Manager) writeAssets(target string, assets map[string][]byte) error {
	for name, data := range assets {
		p, err := m.outputPath(target, name)
		if err != nil {
			return err
		}
		hash, err := fileHash(p)
		if err != nil {
			return err
		}
		if hash != "" && hash != Digest(data) {
			return &ConflictError{Path: p}
		}
		if hash == "" {
			if err = publishNew(p, data); err != nil {
				return err
			}
		}
	}
	return nil
}

func publishNew(p string, data []byte) error {
	tmp, err := tempWrite(p, data)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	return publishFile(tmp, p)
}

func atomicWrite(p string, data []byte) error {
	tmp, err := tempWrite(p, data)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	return replaceFile(tmp, p)
}
