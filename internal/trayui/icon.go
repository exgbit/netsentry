package trayui

import _ "embed"

// GreenIcon / RedIcon 是托盘图标的 .ico 字节内容(32x32 实心圆点,PNG-in-ICO 格式,
// Windows Vista 及以后支持),分别对应"健康"和"不健康"两种状态。
//
//go:embed assets/green.ico
var GreenIcon []byte

//go:embed assets/red.ico
var RedIcon []byte

// IconFor 按 healthy 状态选图标字节内容。
func IconFor(healthy bool) []byte {
	if healthy {
		return GreenIcon
	}
	return RedIcon
}
