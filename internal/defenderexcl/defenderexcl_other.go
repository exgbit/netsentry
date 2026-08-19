//go:build !windows

// Package defenderexcl 管理 Windows Defender 的排除路径列表。
package defenderexcl

import "errors"

// Add 在非 Windows 平台上不可用,只是为了让 go build ./... 能跨平台通过。
func Add(path string) error {
	return errors.New("defenderexcl: not supported on this platform")
}

// Remove 在非 Windows 平台上不可用,只是为了让 go build ./... 能跨平台通过。
func Remove(path string) error {
	return errors.New("defenderexcl: not supported on this platform")
}
