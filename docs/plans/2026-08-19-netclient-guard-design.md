# netclient-guard 设计文档

日期:2026-08-19
状态:设计已确认,待实现

## 背景

Windows 11 上的 netclient(通过 SSH 到 `100.67.147.113` 排查的那台机器,server 为 `tomtoc.cn`)会不定期失效,报错:

```
[netclient.exe] tomtoc.cn is misconfigured: MQID/Password does not match hostid/password
[netclient.exe] Fatal: configuration is invalid, fix before proceeding
```

已从 `gravitl/netclient` 源码(`cmd/root.go` 的 `checkConfig()`)确认根因:程序启动时比较 `servers.json` 里缓存的 `mqid` 字段和 `netclient.json` 里当前的 `id` 字段,不一致就 fatal 退出,不会自愈。

关键机制:`ReadNetclientConfig()` 在 `netclient.json` **不存在**时会静默生成一个全新的空配置并写回磁盘,新配置的 `id` 是空的,`checkConfig()` 发现后会随机生成一个全新的 UUID 保存——如果这时 `servers.json` 还留着旧的 `mqid`,两者就永久对不上。也就是说:**只要 `netclient.json` 单独丢失(`servers.json` 还在),下次启动必炸**,官方没有自动修复路径。

Windows 上 `netclient.json` 平白丢失最可能的原因是杀毒软件/Defender 把它当敏感文件(内含私钥、MQ 密码)隔离删除;其次是历史上不完整的卸载/重装残留导致两个文件不成对。

另外查到 netclient 在 Windows 上用 **WinSW** 包装成服务,自带崩溃后自动重启(5s/15s/30s/120s/300s 递增重试),但这个重启对上述配置错误无效——每次重启都会在同一处 fatal 退出。日志固定输出在 `C:\Program Files (x86)\Netclient\logs\winsw.out.log` / `winsw.err.log`。

配置文件位置(Windows 固定路径 `config.GetNetclientPath()`):`C:\Program Files (x86)\Netclient\netclient.json`(本机身份:`id`、私钥、MQ 密码等)和 `...\servers.json`(每个已加入 server 的缓存,含 `mqid`)。

## 目标与范围

面向自己团队/公司内部的 netclient 用户(非公开分发),做一个 Windows 工具 **netclient-guard**,覆盖:

1. 上述 MQID/密码不匹配问题的定时备份 + 自动恢复
2. 自动检查/添加 Windows Defender 排除项(消除最可能的根因)
3. 睡眠/唤醒后服务失效的自动检测与重启(官方已知 bug,官方无自动化方案)
4. 出问题但自愈失败时,一键生成脱敏诊断包,方便快速定位、迭代
5. 帮助新同事一键安装 netclient 本体并加入网络,免去手动敲命令行

不做:MSI 安装包、开源公开分发适配、自动更新、netclient 多版本管理。

## 架构

单个 Go 二进制 `netclient-guard.exe`,按子命令区分角色,靠 Windows 计划任务的"以 SYSTEM 权限运行"能力做权限分离,不引入 Windows 服务 + IPC 的额外复杂度:

```
netclient-guard.exe install          # 一次性,需要管理员权限(自动 UAC 提权)
netclient-guard.exe uninstall        # 一次性,需要管理员权限
netclient-guard.exe backup           # 无 UI,计划任务(SYSTEM)每 30 分钟调用
netclient-guard.exe watch            # 无 UI,计划任务(SYSTEM)按定时/事件调用
netclient-guard.exe tray             # 有 UI,普通用户登录时启动,无管理员权限
netclient-guard.exe status           # 无 UI,输出当前状态 JSON,给 tray 内部调用
netclient-guard.exe diag             # 普通用户权限即可,生成脱敏诊断包
netclient-guard.exe setup-netclient -t <token>  # 安装/加入网络,需要管理员权限(现场提权)
```

### 解析方式

不依赖 `gravitl/netclient` 模块(会拖带 WireGuard 等依赖,没必要)。自己定义只含所需字段的最小结构体,用标准 `encoding/json` 解析,比正则匹配可靠:

```go
type netclientCfg struct {
    ID string `json:"id"`
}
type serverCfg struct {
    MQID string `json:"mqid"`
    Name string `json:"name"`
}
```

### `install` 做的事(管理员权限跑一次,不是管理员会自动 UAC 提权重启自己)

1. 把自身复制到 `C:\ProgramData\netclient-guard\netclient-guard.exe`
2. 注册 4 个计划任务,均以 `SYSTEM` + 最高权限运行,`/F` 强制覆盖(保证重复运行幂等):
   - `NetclientGuardBackup`:每 30 分钟 `... backup`
   - `NetclientGuardWatch`:每 5 分钟 `... watch`
   - `NetclientGuardWatchOnStart`:开机 1 分钟后 `... watch`
   - `NetclientGuardWatchOnResume`:系统从睡眠唤醒事件触发(XML 事件触发器,监听 `Power-Troubleshooter` 事件)`... watch`
   - 所有任务显式设置 `MultipleInstancesPolicy=IgnoreNew`,避免 `watch` 执行中重叠触发
3. 把 `C:\Program Files (x86)\Netclient\` 加入 Windows Defender 排除列表(加不上则记警告,不阻断安装)
4. 若 netclient 已装好且已 join,立即跑一次 `backup` 建立基线
5. 注册当前用户级别的登录启动项(非管理员那种,如 `HKCU\...\Run`),下次登录自动启动 `tray`
6. 所有步骤写 `C:\ProgramData\netclient-guard\install.log`,便于排查安装本身失败的情况

### `backup` 判定逻辑

1. `netclient.json` 或 `servers.json` 缺失 → 跳过,记日志(不算错误,可能是 netclient 还没装/没 join)
2. 两者都在,解析 `netclient.json` 的 `id` 和 `servers.json` 里所有 `mqid`
3. 只有 **全部一致** 时才覆盖 `backup\netclient.json.good` / `backup\servers.json.good`(用 `last-good.txt` 记时间戳),不一致就跳过,避免把坏状态存成"已知良好"

### `watch` 判定逻辑

1. `netclient.json` 或 `servers.json` 缺失 → 需要修复
2. 两者都在但 `id != mqid`(任一不匹配)→ 需要修复
3. 配置一致但 `netclient` 服务状态非 `Running` → 直接 `Start-Service`,不动备份
4. 需要修复且有可用的一致性备份 → `Stop-Service` → 覆盖两个文件 → `Start-Service` → 记日志
5. 需要修复但没有备份可用 → 只写 `ALERT` 日志,**进程以非零退出码结束**(让计划任务运行历史能反映"修复失败"而不是"跑过一次但没用"),不做任何文件改动,等人工用面板的"重新配置"功能处理

## 托盘 UI

- 图标本体:`github.com/getlantern/systray`,负责系统托盘图标 + 右键精简原生菜单(仅保留退出、重启托盘等兜底操作)
- 左键点击:弹出无边框小面板,用 `go-webview2`(WebView2 的 Go 绑定)渲染 `go:embed` 进二进制的 HTML/CSS/JS。Win11 默认自带 WebView2 运行时,不需要额外安装;`install` 检测运行时是否存在,缺失只记日志提示,不做自动下载
- 面板与后端通过 webview 的 JS bridge 通信,绑定 `getStatus()`(面板打开期间每 3 秒轮询刷新)、`backupNow()`、`repairNow()`、`testConnectivity()`、`generateDiag()`、`setupNetclient(token)`
- **两种面板状态**:
  - 未检测到 `netclient.json`(还没装/没 join):显示 token 输入框 + "安装并加入网络"按钮,点击后走 `setup-netclient` 流程,面板显示进度文字
  - 已检测到配置:显示状态仪表盘——顶部状态色块(绿=配置一致且服务 Running,红=配置不一致或服务非 Running 且无备份可修,没有黄色状态)、server 名称、最近备份/修复时间;四个按钮:立即备份、立即修复、测试连通性(手动 ping,不参与图标颜色判定,因为连不通可能是网络本身问题)、生成诊断包;另加一个"重新配置"入口(见下)

图标颜色判定完全基于本地文件 + 服务状态,每 30 秒刷新一次,不依赖网络请求,响应快、误报率低。

## netclient 安装/配置能力

新增 `setup-netclient -t <token>` 子命令,已核对 `gravitl/netclient` 源码确认真实命令序列(非猜测):

1. 从 GitHub Releases 下载固定版本(编译时常量,如 `v1.6.0`,团队验证过的稳定版本)的 `netclient-windows-amd64.exe` 到临时目录
2. 运行 `<临时路径>\netclient.exe install`(会自我拷贝到 `C:\Program Files (x86)\Netclient\` 并注册 WinSW 服务)
3. 运行安装后的 `netclient.exe join -t <token>`(真正注册、生成 `netclient.json`/`servers.json`)
4. 成功后自动执行 `netclient-guard.exe install`(装看门狗)和一次 `backup`(建基线)

面板的"重新配置"按钮(用于看门狗自愈也救不回来的情况,如没有可用备份、或 server 端已删除该 host 记录):弹 token 输入框,现场对 `setup-netclient` 发起一次 UAC 提权重新执行(不走预注册的 SYSTEM 计划任务,因为 token 每次不固定,没法预先注册)。这是低频、用户主动触发的操作,弹一次 UAC 可接受,不影响日常自愈的免提权体验。

版本升级:只需重新编译发布 guard 工具(改常量版本号),不做远程版本配置。

## 诊断包收集(`diag`)

出问题但看门狗没能自愈时,一键打包给我快速定位、迭代,不用手动翻文件夹。

`netclient-guard.exe diag`(普通用户权限即可,所有要读的内容都在用户可读范围)输出 zip 到 `%USERPROFILE%\Desktop\netclient-diag-<时间戳>.zip`:

1. `winsw.out.log` / `winsw.err.log`(netclient 自身运行日志,原样收录)
2. `guard.log`(本工具的备份/修复历史)
3. `config-summary.json`(**脱敏后**的 `netclient.json` + `servers.json`:只保留 `id`/`mqid`/`version`/`os`/`os_version`/`interface`/`name`/`nodes`/`endpointip`/`created_at`/`updated_at`;**剔除** `privatekey`/`traffickeypublic`/`traffickeyprivate`/`hostpass`/`publickey`/`accesskey`)
4. `service-status.txt`(`Get-Service netclient` 状态 + `winsw.xml` 内容)
5. `scheduled-tasks.txt`(4 个计划任务的上次运行时间/返回码)
6. `defender-status.txt`(当前排除列表 + 过滤出路径含 `Netclient` 的隔离历史)
7. `system-info.txt`(Windows 版本、netclient 版本、guard 工具版本、主机名、生成时间)

脱敏是硬要求——zip 可能通过 IM/邮件传递,私钥/密码类字段绝不能进包。实现后需要人工验证:解压搜一遍确认没有敏感字段。

## `uninstall`

删 4 个计划任务、删 Defender 排除项、删当前用户的托盘登录启动项。`backup\` 目录下的历史备份默认保留(防止误卸载丢了唯一的良好备份),加 `--purge` 参数才连备份一起删。

## 测试计划

无法纯单元测试(涉及真实服务/计划任务/Defender/WebView2),需要在测试机上跑真实场景:

1. 全新安装 → 核对 4 个计划任务、Defender 排除项、首次基线备份都建好
2. **复现真实 bug**:手动删 `netclient.json`(留 `servers.json`)→ 触发 `watch` → 验证自动恢复、服务回到 Running、面板变绿
3. 两个文件都删(模拟无可用备份)→ 验证写了 `ALERT`、退出码非零、面板变红、不会瞎重试
4. 真实睡眠/唤醒一次 → 核对 `NetclientGuardWatchOnResume` 确实触发(看计划任务运行历史)
5. `diag` 生成的 zip 解开搜一遍,确认没有 `privatekey`/`hostpass`/`traffickeyprivate`/`accesskey`
6. `uninstall` → 任务/排除项/启动项清干净,备份默认保留
7. `setup-netclient` 全流程:全新机器(或先 `uninstall` 官方 netclient)→ 输入 token → 验证下载、install、join、guard 联动安装都成功
