// SPDX-License-Identifier: AGPL-3.0-or-later
package filesys

import "sync/atomic"

var treeSavedObserver atomic.Pointer[func(string, string)]

// SetTreeSavedObserver 注册只接收文档身份的非阻塞保存通知，不传递可变 AST 或明文内容。
func SetTreeSavedObserver(observer func(string, string)) { treeSavedObserver.Store(&observer) }

func notifyTreeSaved(id, box string) {
	if observer := treeSavedObserver.Load(); observer != nil {
		(*observer)(id, box)
	}
}
