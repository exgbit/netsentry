//go:build !windows

package selfcleanup

import "errors"

// SpawnDelayedRemoveAll 在非 Windows 平台上不可用,只是为了让 go build ./... 能跨平台通过。
func SpawnDelayedRemoveAll(dir string) error {
	return errors.New("selfcleanup: not supported on this platform")
}
