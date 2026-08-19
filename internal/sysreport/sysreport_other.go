//go:build !windows

package sysreport

import "errors"

// ServiceStatus 在非 Windows 平台上不可用,只是为了让 go build ./... 能跨平台通过。
func ServiceStatus() (string, error) {
	return "", errors.New("sysreport: not supported on this platform")
}

// ScheduledTasksStatus 在非 Windows 平台上不可用,只是为了让 go build ./... 能跨平台通过。
func ScheduledTasksStatus() (string, error) {
	return "", errors.New("sysreport: not supported on this platform")
}

// DefenderStatus 在非 Windows 平台上不可用,只是为了让 go build ./... 能跨平台通过。
func DefenderStatus() (string, error) {
	return "", errors.New("sysreport: not supported on this platform")
}

// SystemInfo 在非 Windows 平台上不可用,只是为了让 go build ./... 能跨平台通过。
func SystemInfo(guardVersion string) (string, error) {
	return "", errors.New("sysreport: not supported on this platform")
}
