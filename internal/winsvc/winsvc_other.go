//go:build !windows

package winsvc

import "errors"

// SCController 在非 Windows 平台上不可用,只是为了让 go build ./... 能跨平台通过。
type SCController struct {
	Name    string
	LogPath string
}

func (c SCController) IsRunning() (bool, error) {
	return false, errors.New("winsvc: not supported on this platform")
}

func (c SCController) Start() error {
	return errors.New("winsvc: not supported on this platform")
}

func (c SCController) Stop() error {
	return errors.New("winsvc: not supported on this platform")
}

func (c SCController) LogSize() (int64, error) {
	return 0, errors.New("winsvc: not supported on this platform")
}

func (c SCController) ReadLogFrom(offset int64) ([]byte, error) {
	return nil, errors.New("winsvc: not supported on this platform")
}
