// signmanifest 是发布流程用的签名小工具(只在开发机上 go run,不随产品分发):
// 对 dist/version.json 做 ed25519 签名,客户端(internal/selfupdate)用编译进
// 二进制的公钥验签——镜像服务器即使被攻破,没有私钥也伪造不出合法的版本清单,
// 自动升级通道不会变成远程执行任意代码的入口。
//
//	go run ./cmd/signmanifest gen  -out ~/.config/netsentry/signing.key   生成私钥,打印公钥
//	go run ./cmd/signmanifest pub  -key <私钥文件>                         打印对应公钥
//	go run ./cmd/signmanifest sign -key <私钥文件> -in version.json -out version.json.sig
//
// 私钥文件内容是 32 字节种子的 hex,权限 0600,绝不能提交进仓库或上传到服务器。
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		fatal("用法: signmanifest gen|pub|sign ...(见文件头注释)")
	}
	switch os.Args[1] {
	case "gen":
		fs := flag.NewFlagSet("gen", flag.ExitOnError)
		out := fs.String("out", "", "私钥输出路径")
		_ = fs.Parse(os.Args[2:])
		if *out == "" {
			fatal("gen 需要 -out <私钥输出路径>")
		}
		if _, err := os.Stat(*out); err == nil {
			fatal("拒绝覆盖已存在的私钥文件: " + *out)
		}
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		check(err)
		check(os.WriteFile(*out, []byte(hex.EncodeToString(priv.Seed())+"\n"), 0o600))
		fmt.Println("私钥已写入:", *out)
		fmt.Println("公钥(填进 internal/selfupdate/selfupdate.go 的 UpdatePublicKeyHex):")
		fmt.Println(hex.EncodeToString(pub))
	case "pub":
		fs := flag.NewFlagSet("pub", flag.ExitOnError)
		key := fs.String("key", "", "私钥文件路径")
		_ = fs.Parse(os.Args[2:])
		priv := loadKey(*key)
		fmt.Println(hex.EncodeToString(priv.Public().(ed25519.PublicKey)))
	case "sign":
		fs := flag.NewFlagSet("sign", flag.ExitOnError)
		key := fs.String("key", "", "私钥文件路径")
		in := fs.String("in", "", "要签名的文件(version.json)")
		out := fs.String("out", "", "签名输出路径(version.json.sig)")
		_ = fs.Parse(os.Args[2:])
		if *in == "" || *out == "" {
			fatal("sign 需要 -key -in -out")
		}
		priv := loadKey(*key)
		data, err := os.ReadFile(*in)
		check(err)
		sig := ed25519.Sign(priv, data)
		check(os.WriteFile(*out, []byte(hex.EncodeToString(sig)+"\n"), 0o644))
		fmt.Println("签名已写入:", *out)
	default:
		fatal("未知子命令: " + os.Args[1])
	}
}

func loadKey(path string) ed25519.PrivateKey {
	if path == "" {
		fatal("需要 -key <私钥文件路径>")
	}
	data, err := os.ReadFile(path)
	check(err)
	seed, err := hex.DecodeString(strings.TrimSpace(string(data)))
	check(err)
	if len(seed) != ed25519.SeedSize {
		fatal(fmt.Sprintf("私钥文件内容应为 %d 字节种子的 hex,实际 %d 字节", ed25519.SeedSize, len(seed)))
	}
	return ed25519.NewKeyFromSeed(seed)
}

func check(err error) {
	if err != nil {
		fatal(err.Error())
	}
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "signmanifest:", msg)
	os.Exit(1)
}
