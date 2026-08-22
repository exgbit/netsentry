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
#   5. 用本机私钥(~/.config/netsentry/signing.key,cmd/signmanifest 生成)对
#      version.json 做 ed25519 签名,并核对私钥与代码里编译进客户端的公钥配对
#      ——客户端只认这把钥匙签出的清单,镜像服务器本身不在信任链里;
#   6. git tag v<版本> 并 push,gh release create 同步发布到 GitHub;
#   7. 部署内网镜像:exe 传到 /var/www/netsentry/<版本>/(带版本号的路径,
#      内容永不变更),最后一步才原子替换根目录的 version.json + .sig——
#      客户端不会看到"版本号已更新但 exe 还没传完"的中间态。
#
# version.json 换上之后,所有客户端的 watch 巡检会在 1 小时内发现新版本并自动
# 升级(验签、下载、SHA256 校验、替换 exe),无需任何人工操作。客户端只升不降
# (防重放旧清单),所以回滚 = 检出旧代码、改一个更高的版本号、重新跑本脚本。
set -euo pipefail

VERSION=${1:?用法: scripts/release.sh <版本号,如 0.6.1>}
cd "$(dirname "$0")/.."

MIRROR_HOST=aliyun-gateway
MIRROR_DIR=/var/www/netsentry

# 本机残留的 GOROOT 环境变量指向旧版 Go,会让 go 命令报 stdlib 版本不匹配;
# 清掉让 go 自己探测(在没有这个问题的机器上清掉也无副作用)。
unset GOROOT

[ -z "$(git status --porcelain)" ] || { echo "错误: git 工作区不干净,先提交或还原改动"; exit 1; }

CODE_VERSION=$(sed -n 's/.*Guard = "\(.*\)"/\1/p' internal/appversion/appversion.go)
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
make build-mac

echo "== 签名 version.json =="
SIGNING_KEY="$HOME/.config/netsentry/signing.key"
[ -f "$SIGNING_KEY" ] || {
	echo "错误: 缺少签名私钥 $SIGNING_KEY"
	echo "生成: go run ./cmd/signmanifest gen -out $SIGNING_KEY(公钥要与代码一致)"
	exit 1
}
PUB_CODE=$(sed -n 's/.*UpdatePublicKeyHex = "\(.*\)"/\1/p' internal/selfupdate/selfupdate.go)
PUB_KEY=$(go run ./cmd/signmanifest pub -key "$SIGNING_KEY")
[ "$PUB_CODE" = "$PUB_KEY" ] || {
	echo "错误: 私钥 $SIGNING_KEY 与代码里的 UpdatePublicKeyHex 不配对,客户端将拒绝这份签名"
	exit 1
}
go run ./cmd/signmanifest sign -key "$SIGNING_KEY" -in dist/version.json -out dist/version.json.sig
go run ./cmd/signmanifest sign -key "$SIGNING_KEY" -in dist/version-mac.json -out dist/version-mac.json.sig

echo "== GitHub 发布 v$VERSION =="
git tag "v$VERSION"
git push origin main "v$VERSION"
gh release create "v$VERSION" dist/netsentry.exe dist/netsentry-tray.exe "dist/netsentry#netsentry-mac (universal)" \
	--title "v$VERSION" \
	--notes "见 internal/trayui/changelog.go 中 $VERSION 的条目。"

echo "== 部署内网镜像 =="
ssh "$MIRROR_HOST" "mkdir -p $MIRROR_DIR/$VERSION"
scp dist/netsentry.exe dist/netsentry-tray.exe dist/netsentry "$MIRROR_HOST:$MIRROR_DIR/$VERSION/"
scp dist/version.json "$MIRROR_HOST:$MIRROR_DIR/version.json.new"
scp dist/version.json.sig "$MIRROR_HOST:$MIRROR_DIR/version.json.sig.new"
scp dist/version-mac.json "$MIRROR_HOST:$MIRROR_DIR/version-mac.json.new"
scp dist/version-mac.json.sig "$MIRROR_HOST:$MIRROR_DIR/version-mac.json.sig.new"
ssh "$MIRROR_HOST" "cd $MIRROR_DIR && mv version.json.sig.new version.json.sig && mv version.json.new version.json && mv version-mac.json.sig.new version-mac.json.sig && mv version-mac.json.new version-mac.json"

echo "== 完成:v$VERSION 已发布,全部客户端将在 1 小时内自动升级 =="
