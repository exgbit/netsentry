// Package schedtask 提供构造 schtasks.exe 命令行参数和 XML 任务定义的纯函数,
// 用来注册 Windows 计划任务。实际执行 schtasks.exe 的逻辑不在本文件中。
package schedtask

import "fmt"

// BackupTaskArgs 构造 schtasks /Create 用来注册"每 30 分钟跑一次 backup"任务的参数。
func BackupTaskArgs(exePath string) []string {
	return []string{
		"/Create", "/TN", "NetclientGuardBackup",
		"/TR", `"` + exePath + `" backup`,
		"/SC", "MINUTE", "/MO", "30",
		"/RU", "SYSTEM", "/RL", "HIGHEST", "/F",
	}
}

// WatchTaskArgs 构造"每 5 分钟跑一次 watch"任务的参数。
func WatchTaskArgs(exePath string) []string {
	return []string{
		"/Create", "/TN", "NetclientGuardWatch",
		"/TR", `"` + exePath + `" watch`,
		"/SC", "MINUTE", "/MO", "5",
		"/RU", "SYSTEM", "/RL", "HIGHEST", "/F",
	}
}

// WatchOnStartTaskArgs 构造"开机 1 分钟后跑一次 watch"任务的参数。
func WatchOnStartTaskArgs(exePath string) []string {
	return []string{
		"/Create", "/TN", "NetclientGuardWatchOnStart",
		"/TR", `"` + exePath + `" watch`,
		"/SC", "ONSTART", "/DELAY", "0001:00",
		"/RU", "SYSTEM", "/RL", "HIGHEST", "/F",
	}
}

// resumeTriggerTaskXMLTemplate 是 Windows 任务计划程序 1.2 版的任务定义模板,
// 用一个事件触发器监听"系统从睡眠恢复"(System 日志、来源
// Microsoft-Windows-Power-Troubleshooter、EventID=1)。RunLevel 用 SID
// S-1-5-18(Local System 的固定 SID,不受系统语言/本地化影响)而不是
// "NT AUTHORITY\SYSTEM" 这种可本地化的名字。
//
// exePath 来自编译期确定的安装路径(不是不受信任的外部输入),因此这里直接用
// fmt.Sprintf 做字符串替换,不做 XML 转义。
const resumeTriggerTaskXMLTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <Triggers>
    <EventTrigger>
      <Subscription>&lt;QueryList&gt;&lt;Query Id="0" Path="System"&gt;&lt;Select Path="System"&gt;*[System[Provider[@Name='Microsoft-Windows-Power-Troubleshooter'] and EventID=1]]&lt;/Select&gt;&lt;/Query&gt;&lt;/QueryList&gt;</Subscription>
    </EventTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <UserId>S-1-5-18</UserId>
      <RunLevel>HighestAvailable</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>%s</Command>
      <Arguments>watch</Arguments>
    </Exec>
  </Actions>
</Task>`

// ResumeTriggerTaskXML 生成"系统从睡眠唤醒时跑一次 watch"任务的 XML 任务定义
// (schtasks 的 /SC 系列 flag 表达不了事件触发器,只能用 /XML 传任务定义文件)。
func ResumeTriggerTaskXML(exePath string) string {
	return fmt.Sprintf(resumeTriggerTaskXMLTemplate, exePath)
}

// AllTaskNames 返回本工具注册的全部计划任务名,uninstall 时用来逐个删除。
func AllTaskNames() []string {
	return []string{
		"NetclientGuardBackup",
		"NetclientGuardWatch",
		"NetclientGuardWatchOnStart",
		"NetclientGuardWatchOnResume",
	}
}
