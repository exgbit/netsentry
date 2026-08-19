// Package guardlog 提供一个极简的追加写入日志,格式:[时间戳] [tag] message
package guardlog

import (
	"fmt"
	"os"
	"time"
)

// Append 把一行日志追加写入 path,自动创建文件(如果不存在)。
func Append(path, tag, message string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	line := fmt.Sprintf("[%s] [%s] %s\n", time.Now().Format("2006-01-02 15:04:05"), tag, message)
	_, err = f.WriteString(line)
	return err
}
