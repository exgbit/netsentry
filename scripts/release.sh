#!/usr/bin/env bash
# NetSentry 发布脚本:确认当前代码是稳定版之后,执行这一条命令完成全部发布动作。
#
#   scripts/release.sh 0.6.1
#
# 流程(任何一步失败立即停止,不会发布到一半):
#   1. git 工作区必须干净(所有改动已提交);
#   2. 参数里的版本号必须与代码里的 guardVersion 一致(防手滑发错版本);
#   3. go vet + go test 全绿;
#   4. make build 产出 dist/ 下两个 exe + version.json;
#   5. git tag v<版本> 并 push,gh release create 同步发布到 GitHub;
#   6. 部署内网镜像:exe 传到 /var/www/netsentry/<版本>/(带版本号的路径,
#      内容永不变更),最后一步才原子替换根目录的 version.json——客户端不会
#      看到"版本号已更新但 exe 还没传完"的中间态。
#
# version.json 换上之后,所有客户端的 watch 巡检会在 1 小时内发现新版本并自动
# 升级(下载、SHA256 校验、替换 exe),无需任何人工操作。回滚 = 在镜像上把
# version.json 改回旧版本号(旧版本目录一直保留)。
set -euo pipefail

VERSION=${1:?用法: scripts/release.sh <版本号,如 0.6.1>}
cd "$(dirname "$0")/.."

MIRROR_HOST=aliyun-gateway
MIRROR_DIR=/var/www/netsentry

# 本机残留的 GOROOT 环境变量指向旧版 Go,会让 go 命令报 stdlib 版本不匹配;
# 清掉让 go 自己探测(在没有这个问题的机器上清掉也无副作用)。
unset GOROOT

[ -z "$(git status --porcelain)" ] || { echo "错误: git 工作区不干净,先提交或还原改动"; exit 1; }

CODE_VERSION=$(sed -n 's/.*guardVersion = "\(.*\)"/\1/p' cmd/netsentry/main.go)
[ "$CODE_VERSION" = "$VERSION" ] || {
	echo "错误: 参数版本 $VERSION 与代码里的 guardVersion=$CODE_VERSION 不一致"
	exit 1
}

echo "== 测试 =="
go vet ./...
go test ./...

echo "== 构建 =="
make clean
make build

echo "== GitHub 发布 v$VERSION =="
git tag "v$VERSION"
git push origin main "v$VERSION"
gh release create "v$VERSION" dist/netsentry.exe dist/netsentry-tray.exe \
	--title "v$VERSION" \
	--notes "见 internal/trayui/changelog.go 中 $VERSION 的条目。"

echo "== 部署内网镜像 =="
ssh "$MIRROR_HOST" "mkdir -p $MIRROR_DIR/$VERSION"
scp dist/netsentry.exe dist/netsentry-tray.exe "$MIRROR_HOST:$MIRROR_DIR/$VERSION/"
scp dist/version.json "$MIRROR_HOST:$MIRROR_DIR/version.json.new"
ssh "$MIRROR_HOST" "mv $MIRROR_DIR/version.json.new $MIRROR_DIR/version.json"

echo "== 完成:v$VERSION 已发布,全部客户端将在 1 小时内自动升级 =="
