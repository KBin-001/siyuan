// SPDX-License-Identifier: AGPL-3.0-or-later
//go:build !windows

package localexport

import "os"

func replaceFile(from, to string) error { return os.Rename(from, to) }

func publishFile(from, to string) error { return os.Link(from, to) }

func replaceManaged(from, to, backup string) error {
	if err := os.Link(to, backup); err != nil {
		return err
	}
	return os.Rename(from, to)
}
