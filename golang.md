# 跨平台编译

```sh
# Linux
GOOS=linux GOARCH=amd64 go build -o server main.go

# Windows
GOOS=windows GOARCH=amd64 go build -o server.exe main.go

# Android (arm64)
GOOS=android GOARCH=arm64 go build -o server main.go

```

脚本一键编译

```shell
.\build-all.ps1
```

## 安装NDK实现cgo编译安卓

1. 打开 [下载地址](https://developer.android.com/ndk/downloads?hl=zh-cn)
2. 找到 android-ndk-r21e-windows-x86_64.zip 进行下载。这个版本最低支持安卓5.0
3. 下载完成后解压到你希望放置的位置，例如: `E:\Android\android-ndk-r21e`
4. 配置环境变量（可选，但方便命令行使用）在 Windows 系统环境变量里： ANDROID_NDK_HOME 指向 NDK 目录：可选：把 NDK 工具链目录加入 PATH：
5. 使用 Go 配置 NDK 编译 Android Go 的 Android cgo 编译需要指定交叉编译器。假设你要编译 arm64-v8a（GOARCH=arm64）：
```powershell
$env:GOOS="android"
$env:GOARCH="arm64"

# 设置 C 编译器，假设使用 API Level 21
$env:CC="C:\Android\android-ndk-r25b\toolchains\llvm\prebuilt\windows-x86_64\bin\aarch64-linux-android21-clang.cmd"

# 输出二进制
go build -o server-android main.go

```

6. 验证安装
```shell
# 检查 clang 是否可用
E:\Android\android-ndk-r21e\toolchains\llvm\prebuilt\windows-x86_64\bin\aarch64-linux-android21-clang.cmd --version

```
7. 小结

下载并解压 NDK

配置 ANDROID_NDK_HOME 环境变量

根据目标架构设置 GOOS=android, GOARCH=arm64

如果用 cgo，设置 CC 指向 NDK 的 clang

go build 即可生成 Android 二进制


纯go代码关闭cgo

```shell
# 验证版本
& "E:\Android\android-ndk-r21e\toolchains\llvm\prebuilt\windows-x86_64\bin\aarch64-linux-android21-clang.cmd" --version

# 关cgo
$env:CGO_ENABLED = "0"
$env:CC = $null
```

## 测试访问

```text
# 跳过证书验证
http://127.0.0.1:57571/proxy?method=GET&url=https://self-signed.badssl.com/&headers={"User-Agent":"okhttp/4.19"}

# 看ip和请求头
http://127.0.0.1:57571/proxy?method=GET&url=https://httpbin.org/get&headers={"User-Agent":"okhttp/4.19"}
```