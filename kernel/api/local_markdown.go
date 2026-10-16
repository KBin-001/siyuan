// SPDX-License-Identifier: AGPL-3.0-or-later
package api

import (
	"errors"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/siyuan-note/siyuan/kernel/apicontract"
	"github.com/siyuan-note/siyuan/kernel/localexport"
	"github.com/siyuan-note/siyuan/kernel/model"
)

var localMarkdown = contractHandler(apicontract.LocalMarkdown, func(c *gin.Context, request apicontract.LocalMarkdownRequest) apicontract.Response[apicontract.LocalMarkdownData] {
	m, err := model.LocalMarkdownManager()
	if err != nil {
		return apicontract.Failure[apicontract.LocalMarkdownData](-1, err.Error())
	}
	config := m.Configuration()
	if request.Config != nil {
		config = localexport.Config(*request.Config)
	}
	documents := []apicontract.LocalMarkdownDocument{}
	var file *apicontract.LocalMarkdownFile
	changed := false
	switch request.Action {
	case "", "status":
	case "refresh":
		changed, err = model.RefreshLocalFile(request.ID, request.AcceptExternal)
	case "check":
		if source, entry, linked := m.Source(request.ID); linked {
			_, _, err = model.LocalFileDocument(request.ID)
			if err == nil {
				var revision string
				_, revision, err = m.InspectSource(source.Path)
				changed = revision != "" && revision != entry.Hash
			}
		}
	case "file", "bind":
		var box, name string
		box, name, err = model.LocalFileDocument(request.ID)
		if err != nil {
			break
		}
		file = &apicontract.LocalMarkdownFile{Name: name}
		if source, entry, linked := m.Source(request.ID); linked {
			file.Path, file.Revision, file.Linked = source.Path, entry.Hash, true
		}
		if request.Path != "" {
			file.Path, file.Revision, err = m.InspectSource(request.Path)
			file.Linked = false
		}
		if err != nil {
			break
		}
		if request.Action == "bind" {
			if request.Path == "" || request.Revision == nil {
				err = errors.New("path and revision required")
				break
			}
			err = m.BindSource(request.ID, box, request.Path, *request.Revision)
			file.Linked = err == nil
		}
	case "configure":
		if request.Config == nil {
			err = errors.New("config required")
			break
		}
		if config.Enabled {
			_, err = model.LocalMarkdownDocuments(config)
			if err != nil {
				break
			}
		}
		err = m.Configure(config)
	case "documents", "preview":
		if request.Action == "preview" {
			config.Target, err = localexport.ValidateTarget(config.Target, model.LocalMarkdownDataDir())
			if err != nil {
				break
			}
		}
		if request.Action == "documents" {
			config.Root = ""
		}
		var ids []string
		ids, err = model.LocalMarkdownDocuments(config)
		if err != nil {
			break
		}
		for _, id := range ids {
			source, target, previewErr := model.LocalMarkdownPreview(config, id)
			if previewErr != nil {
				err = previewErr
				break
			}
			var statErr error
			if request.Action == "preview" {
				_, statErr = os.Stat(target)
			} else {
				statErr = os.ErrNotExist
			}
			if statErr != nil && !os.IsNotExist(statErr) {
				err = statErr
				break
			}
			documents = append(documents, apicontract.LocalMarkdownDocument{ID: id, Source: source, Target: target, Exists: statErr == nil})
		}
	case "current":
		if _, _, linked := m.Source(request.ID); linked {
			model.FlushTxQueue()
			err = model.SaveLocalFile(request.ID)
			break
		}
		config = m.Status().Config
		if config.Enabled {
			model.FlushTxQueue()
			_, _, err = model.LocalMarkdownPreview(config, request.ID)
			if errors.Is(err, localexport.ErrOutOfScope) {
				err = nil
				break
			}
			if err == nil {
				m.Notify(request.ID, config.Box, true)
			}
		}
	case "run":
		config = m.Status().Config
		if !config.Enabled {
			err = errors.New("local Markdown export is disabled")
			break
		}
		model.FlushTxQueue()
		var ids []string
		ids, err = model.LocalMarkdownDocuments(config)
		if err == nil {
			for _, id := range ids {
				m.Notify(id, config.Box, true)
			}
		}
	case "retry":
		m.Retry()
	default:
		err = errors.New("unknown local Markdown action")
	}
	if err != nil {
		return apicontract.Failure[apicontract.LocalMarkdownData](-1, err.Error())
	}
	if request.Action == "check" || request.Action == "refresh" {
		return apicontract.Success(apicontract.LocalMarkdownData{Changed: changed, Config: apicontract.LocalMarkdownConfig(config), State: "idle", Entries: []apicontract.LocalMarkdownEntry{}, Documents: []apicontract.LocalMarkdownDocument{}})
	}
	s := m.Status()
	entries := []apicontract.LocalMarkdownEntry{}
	for _, e := range s.Entries {
		entries = append(entries, apicontract.LocalMarkdownEntry{ID: e.ID, Path: e.Path, State: e.State, Error: e.Error, Conflict: e.Conflict, LastSuccess: e.LastSuccess})
	}
	return apicontract.Success(apicontract.LocalMarkdownData{Changed: changed, File: file, Config: apicontract.LocalMarkdownConfig(s.Config), State: s.State, Error: s.Error, LastSuccess: s.LastSuccess, Pending: s.Pending, Entries: entries, Documents: documents})
})
