//go:build windows

package selfcleanup

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// selfCleanupTaskName 是本包注册的一次性清理任务名。一次性、自删除,不需要
// 随机化避免冲突——万一上次调用失败留下了同名任务,/F 会直接覆盖。
const selfCleanupTaskName = "NetclientGuardSelfCleanup"

// selfCleanupDelay 是从注册计划任务到它触发之间预留的提前量。
//
// schtasks /ST 只精确到分钟(不接受秒),传入的目标时间会被向下取整到分钟。
// 提前量太短(比如几秒钟)取整后完全可能落进"现在这一分钟"甚至已经过去的
// 那一分钟,ONCE 触发器会被当成错过了,不会执行。2 分钟的提前量能保证取整
// 之后,触发时刻相对调用时刻始终还有至少 1 分钟、最多 2 分钟的真实等待——
// 早就够当前进程退出、释放对自身 exe 文件句柄了。
const selfCleanupDelay = 2 * time.Minute

// SpawnDelayedRemoveAll 注册一个一次性("ONCE")Windows 计划任务,在当前进程
// 退出、释放对自身可执行文件的句柄之后,由该任务删除 dir 整个目录(此时目录下
// 应该只剩当前正在运行的那个可执行文件)。不等待任务真正触发——调用后当前
// 进程可以正常退出,真正的删除发生在这之后,异步完成。任务触发后会顺带删掉
// 自己的计划任务注册,不在 Task Scheduler 里留下垃圾条目。
//
// 早期实现是直接 exec.Command 起一个 DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP
// 的 cmd.exe 子进程。真机测试(通过 SSH 调用 uninstall --purge)证实这个办法
// 不可靠:Windows OpenSSH Server 会把它起的子进程放进一个 Job Object,并配置成
// "SSH 会话的进程树退出时,连带杀掉这个 Job 里的所有进程";DETACHED_PROCESS /
// CREATE_NEW_PROCESS_GROUP 并不能让新进程豁免已存在 Job Object 的这条策略
// (需要显式的 CREATE_BREAKAWAY_FROM_JOB,而且哪怕加了这个 flag,如果 Job 本身
// 没开 JOB_OBJECT_LIMIT_BREAKAWAY_OK 一样会失败)——那个 helper 进程在 ping 的
// 延时跑完之前就被连带杀掉了,从来没真正执行到 rmdir。改用计划任务是因为它是
// Windows 里"起一个完全脱离调用者进程树"的一等机制:任务触发时是由 Task
// Scheduler 服务另起的进程,根本不在调用者所在的 Job Object 里,不会被这类
// "进程树退出连坐"的策略影响。不要图省事改回裸的 detached 子进程,除非先在
// 类似 SSH 会话这种会用到 Job Object 的环境里真机验证过。
//
// /RU SYSTEM:复用 internal/schedtask 里其余计划任务的约定,配合 uninstall 早已
// 要求的 ensureElevated() 提权上下文,从已提权进程注册这种 /RU SYSTEM 任务没问题。
//
// 故意不传 /SD(开始日期):/SD 的日期格式在不同系统区域设置下可能不一样(这个
// 项目里 schedtask 包的 resumeTriggerTaskXMLTemplate 就是一个真机验证过的、
// "看起来该这样写"实际却因为区域设置踩坑的前车之鉴),贸然写死一个格式有踩
// 同样坑的风险;不传 /SD 时 schtasks 默认用系统当前日期,只有调用这个函数的
// 时刻恰好在一天里最后 2 分钟内,才可能因为没跨日期导致触发时间被当成"今天
// 已经过去的时间"而不触发——概率极低,而且就算真触发不了,后果也只是 guardDir
// 目录没被清理干净(和这个函数要修的原始 bug 是同一种"体面地部分失败",不是
// 更糟的结果),不值得为了这个边界情况引入一个没在真机验证过的日期格式。
func SpawnDelayedRemoveAll(dir string) error {
	// rmdir 不需要、也不能安全地接受一个末尾紧跟着闭合引号的路径:cmd.exe 对
	// "路径\" 这种"反斜杠正好卡在引号前面"的写法有个广为人知的坑——反斜杠会把
	// 紧跟着的引号"转义"掉,引号不再被当成闭合定界符,后面 & 之后的
	// schtasks /Delete 也就执行不到了。去掉末尾的反斜杠就没有这个问题,rmdir
	// 本来就不关心目录参数末尾有没有分隔符。
	cleanDir := strings.TrimSuffix(dir, `\`)

	// 经典的"批处理自删除"写法:cmd.exe /C 对形如 "cmd1 "arg" & cmd2" 的字符串,
	// 只会剥掉最外层的头尾两个引号,中间嵌套的引号原样保留给它自己接下来对
	// rmdir/schtasks 的解析——先删目录,再删掉这个一次性任务自身的注册。
	action := `cmd.exe /C "rmdir /S /Q "` + cleanDir + `" & schtasks /Delete /TN ` + selfCleanupTaskName + ` /F"`

	startTime := time.Now().Add(selfCleanupDelay).Format("15:04")
	args := []string{
		"/Create", "/TN", selfCleanupTaskName,
		"/TR", action,
		"/SC", "ONCE", "/ST", startTime,
		"/RU", "SYSTEM", "/F",
	}
	if out, err := exec.Command("schtasks.exe", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("schtasks %s: %w: %s", strings.Join(args, " "), err, out)
	}
	return nil
}
