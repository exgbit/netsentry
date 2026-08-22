//go:build !darwin

// 非 darwin 平台的占位 main:让 GOOS=windows 等交叉 `go build ./...` 不会因为
// 这个目录所有文件都被 build tag 排除而报错。真正的入口在 main_darwin.go。
package main

func main() {}
