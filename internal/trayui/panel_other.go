//go:build !windows

package trayui

// ShowPanel 在非 Windows 平台上不可用,只是为了让 go build ./... 能跨平台通过。
func ShowPanel(cfg PanelConfig) {}
