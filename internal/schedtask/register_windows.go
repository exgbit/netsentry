//go:build windows

package schedtask

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// resumeTaskName 是靠 /XML 注册的第 4 个任务名(其余 3 个任务名已经硬编码在
// Task2 的 *TaskArgs 函数里,这里单独列出来是因为 /XML 那条命令不走 *TaskArgs)。
const resumeTaskName = "NetclientGuardWatchOnResume"

// Register 注册全部 4 个计划任务(exePath 是已安装到位的 netclient-guard.exe 路径)。
// 幂等:每次都用 /F 强制覆盖,重复调用安全。
//
// 单个任务注册失败不会让整体提前返回——尽量把能装的都装上,所有失败原因最后合并返回,
// 由调用方(install)写进 install.log。
func Register(exePath string) error {
	var errs []error

	for _, args := range [][]string{
		BackupTaskArgs(exePath),
		WatchTaskArgs(exePath),
		WatchOnStartTaskArgs(exePath),
	} {
		if out, err := exec.Command("schtasks.exe", args...).CombinedOutput(); err != nil {
			errs = append(errs, fmt.Errorf("schtasks %s: %w: %s", strings.Join(args, " "), err, out))
		}
	}

	if err := registerResumeTask(exePath); err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

// registerResumeTask 注册"系统从睡眠唤醒时跑一次 watch"任务。schtasks 的 /SC 系列 flag
// 表达不了事件触发器,只能把任务定义 XML 写到临时文件,再用 /XML 传给 schtasks。
func registerResumeTask(exePath string) error {
	tmp, err := os.CreateTemp("", "netclient-guard-resume-task-*.xml")
	if err != nil {
		return fmt.Errorf("create temp file for resume task xml: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.WriteString(ResumeTriggerTaskXML(exePath)); err != nil {
		tmp.Close()
		return fmt.Errorf("write resume task xml to %s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close resume task xml %s: %w", tmpPath, err)
	}

	args := []string{"/Create", "/TN", resumeTaskName, "/XML", tmpPath, "/RU", "SYSTEM", "/F"}
	if out, err := exec.Command("schtasks.exe", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("schtasks %s: %w: %s", strings.Join(args, " "), err, out)
	}
	return nil
}

// isTaskNotFoundOutput 判断 schtasks /Delete 的失败输出是不是"任务本来就不存在"。
//
// 已在真实 Windows 11(简体中文)机器上实测确认:
//
//	schtasks /Delete /TN NetclientGuardDoesNotExistTest12345 /F
//	=> 错误: 系统找不到指定的文件。(exit code 1)
//
// 英文系统下对应文案是 "ERROR: The system cannot find the file specified."。团队机器
// 可能是英文或中文 locale,所以两种子串都匹配;中文只匹配核心短语"找不到指定的文件",
// 不含"错误:"前缀,避免因标点/前缀差异漏判。匹配不上的失败仍会正常上报,不会被静默吞掉。
func isTaskNotFoundOutput(output string) bool {
	return strings.Contains(output, "cannot find the file specified") ||
		strings.Contains(output, "找不到指定的文件")
}

// Unregister 删除全部 4 个计划任务。任务本来就不存在时不视为错误,uninstall 要能在
// 部分安装/重复卸载的情况下正常跑完,不半途而废。
func Unregister() error {
	var errs []error

	for _, name := range AllTaskNames() {
		out, err := exec.Command("schtasks.exe", "/Delete", "/TN", name, "/F").CombinedOutput()
		if err != nil && !isTaskNotFoundOutput(string(out)) {
			errs = append(errs, fmt.Errorf("schtasks /Delete /TN %s /F: %w: %s", name, err, out))
		}
	}

	return errors.Join(errs...)
}
