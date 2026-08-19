//go:build windows

package selfcleanup

import (
	"fmt"
	"os/exec"
	"syscall"
)

// Win32 进程创建标志,syscall 包没有把这两个导出成常量,数值取自 Win32 API 文档
// (Process Creation Flags): DETACHED_PROCESS 让新进程不继承/创建任何控制台窗口,
// CREATE_NEW_PROCESS_GROUP 让它脱离当前进程组,不随当前进程一起被信号影响。
const (
	detachedProcess       = 0x00000008
	createNewProcessGroup = 0x00000200
)

// SpawnDelayedRemoveAll 启动一个独立于当前进程的 helper 进程,等当前进程退出、
// 释放对自身可执行文件的句柄之后,删除 dir 整个目录(此时目录下应该只剩当前
// 正在运行的那个可执行文件)。不等待 helper 执行完成——调用后当前进程可以正常
// 退出,真正的删除发生在这之后,异步完成。
func SpawnDelayedRemoveAll(dir string) error {
	// ping 两个包(约 1 秒)是一个不需要额外依赖、Windows 自带的经典延时手法,
	// 给当前进程留出退出、释放自身 exe 文件句柄的时间;之后 rmdir /S /Q 递归
	// 强制删除整个目录,包括这时候已经不再被任何进程占用的可执行文件本身。
	cmdLine := fmt.Sprintf(`ping -n 2 127.0.0.1 >NUL & rmdir /S /Q "%s"`, dir)
	cmd := exec.Command("cmd.exe", "/C", cmdLine)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: detachedProcess | createNewProcessGroup,
	}
	return cmd.Start()
}
