// SPDX-License-Identifier: AGPL-3.0-or-later
//go:build windows

package localexport

import (
	"golang.org/x/sys/windows"
	"unsafe"
)

var replaceFileW = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReplaceFileW")

func publishFile(from, to string) error {
	src, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	dest, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(src, dest, windows.MOVEFILE_WRITE_THROUGH)
}

// ReplaceFileW 在一次文件系统操作中替换目标并保留实际被替换的版本。
func replaceManaged(from, to, backup string) error {
	src, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	dest, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	previous, err := windows.UTF16PtrFromString(backup)
	if err != nil {
		return err
	}
	result, _, callErr := replaceFileW.Call(uintptr(unsafe.Pointer(dest)), uintptr(unsafe.Pointer(src)), uintptr(unsafe.Pointer(previous)), 0, 0, 0)
	if result == 0 {
		return callErr
	}
	return nil
}

func replaceFile(from, to string) error {
	src, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	dest, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(src, dest, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}
