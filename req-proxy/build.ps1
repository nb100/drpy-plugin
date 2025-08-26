# ===============================
# 配置区
# ===============================
# NDK 路径（Android NDK）
$ndkBin = "E:\Android\android-ndk-r21e\toolchains\llvm\prebuilt\windows-x86_64\bin"

# 最小 Android API Level
$androidApiLevel = 21

# 输出目录
$outDir = "dist"
if (-not (Test-Path $outDir)) {
    New-Item -ItemType Directory -Path $outDir | Out-Null
}

# 版本号（可修改）
$version = "1.0.0"

# 目标平台列表
$targets = @(
    @{GOOS="windows"; GOARCH="amd64"; OUT="req-proxy-windows.exe"; CC=$null},
    @{GOOS="linux";   GOARCH="amd64"; OUT="req-proxy-linux";     CC=$null},
    @{GOOS="android"; GOARCH="arm64"; OUT="req-proxy-android";   CC="$ndkBin\aarch64-linux-android$androidApiLevel-clang.cmd"}
)

# ===============================
# 编译流程
# ===============================
foreach ($t in $targets) {
    Write-Host "Building $($t.OUT) for $($t.GOOS)/$($t.GOARCH)..."

    # 设置环境变量
    $env:GOOS = $t.GOOS
    $env:GOARCH = $t.GOARCH
    if ($t.CC) {
        $env:CC = $t.CC
        $env:CGO_ENABLED = "1"
    } else {
        $env:CC = $null
        $env:CGO_ENABLED = "0"
    }

    # 输出路径
    $outPath = Join-Path $outDir $t.OUT

    # 执行编译：去掉调试信息，嵌入版本号
    go build -ldflags "-s -w -X main.version=$version" -o $outPath main.go
    if ($LASTEXITCODE -ne 0) {
        Write-Error "Build failed for $($t.OUT)"
        exit 1
    }

    Write-Host "Built $outPath successfully."

    # 自动 UPX 压缩（如果已安装）
    if (Get-Command upx -ErrorAction SilentlyContinue) {
        Write-Host "Compressing $($t.OUT) with UPX..."
        upx --best --lzma $outPath
        if ($LASTEXITCODE -ne 0) {
            Write-Warning "UPX compression failed for $($t.OUT)"
        } else {
            Write-Host "UPX compression done for $($t.OUT)"
        }
    }
}

Write-Host "All builds completed successfully!"
Write-Host "Binaries are in the '$outDir' directory."
