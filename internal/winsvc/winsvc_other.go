//go:build !windows

package winsvc

import "errors"

// SCController 在非 Windows 平台上不可用,只是为了让 go build ./... 能跨平台通过。
type SCController struct {
	Name string
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
