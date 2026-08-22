.PHONY: build build-mac clean manifest manifest-mac

# NetSentry 编译成两个 Windows 产物,必须一起分发到同一目录下再跑 install:
#
#   netsentry.exe       console 子系统,backup/watch/diag/install/uninstall/
#                        setup-netclient 走这个——人工在终端里手动跑,或者被
#                        计划任务调起时,需要能看到 fmt.Println 输出。
#   netsentry-tray.exe   GUI 子系统(-H=windowsgui),只给 tray 这条路径用——
#                        开机自启/双击拉起时不会附带一个可以被手滑关掉、
#                        连带杀死托盘进程的控制台窗口。
#
# 两者是同一份源码的两次构建,不要试图合并成一个二进制:GUI 子系统的进程即使
# 从已经打开的终端里手动跑起来,也不会附着到那个终端的控制台上,标准输出没地方
# 写、看不到任何输出——这是 Windows 本身的行为,不是构建配置没调对。详细原因见
# cmd/netsentry/main.go 里 runTray 函数上方"子系统选择"的注释。
build:
	mkdir -p dist
	GOOS=windows GOARCH=amd64 go build -o dist/netsentry.exe ./cmd/netsentry
	GOOS=windows GOARCH=amd64 go build -ldflags="-H=windowsgui" -o dist/netsentry-tray.exe ./cmd/netsentry
	$(MAKE) manifest

# manifest 生成 dist/version.json——自动升级(internal/selfupdate)从镜像上读的
# 版本清单,内容是当前版本号 + 两个 exe 的 SHA256。发布新版本时把 dist/ 下的
# 三个文件一起上传到镜像目录即可。
manifest:
	@VERSION=$$(sed -n 's/.*Guard = "\(.*\)"/\1/p' internal/appversion/appversion.go); \
	SUM1=$$(shasum -a 256 dist/netsentry.exe | cut -d' ' -f1); \
	SUM2=$$(shasum -a 256 dist/netsentry-tray.exe | cut -d' ' -f1); \
	printf '{"version":"%s","files":{"netsentry.exe":"%s","netsentry-tray.exe":"%s"}}\n' "$$VERSION" "$$SUM1" "$$SUM2" > dist/version.json; \
	cat dist/version.json

# macOS 版:单一 universal 二进制(arm64 + amd64 lipo 合并),产物名就叫
# netsentry(无扩展名)——version-mac.json 清单里的文件 key 与安装路径
# /usr/local/bin/netsentry 的文件名一致,自动升级换入时不用改名。
build-mac:
	mkdir -p dist
	GOOS=darwin GOARCH=arm64 go build -o dist/netsentry-arm64 ./cmd/netsentry-mac
	GOOS=darwin GOARCH=amd64 go build -o dist/netsentry-amd64 ./cmd/netsentry-mac
	lipo -create -output dist/netsentry dist/netsentry-arm64 dist/netsentry-amd64
	rm dist/netsentry-arm64 dist/netsentry-amd64
	$(MAKE) manifest-mac

manifest-mac:
	@VERSION=$$(sed -n 's/.*Guard = "\(.*\)"/\1/p' internal/appversion/appversion.go); \
	SUM=$$(shasum -a 256 dist/netsentry | cut -d' ' -f1); \
	printf '{"version":"%s","files":{"netsentry":"%s"}}\n' "$$VERSION" "$$SUM" > dist/version-mac.json; \
	cat dist/version-mac.json

clean:
	rm -rf dist
