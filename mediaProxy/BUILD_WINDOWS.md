# Windows PowerShell 构建指南

本文档介绍如何在Windows开发环境中使用PowerShell构建脚本来编译MediaProxy项目。

## 前置要求

1. **Go环境**: 确保已安装Go 1.19或更高版本
2. **PowerShell**: Windows 10/11自带PowerShell 5.1或更高版本
3. **Git**: 用于获取版本信息（可选）
4. **UPX**: 用于二进制文件压缩（可选，推荐安装）

### 安装UPX（推荐）

UPX可以显著减小二进制文件大小：

```powershell
# 使用Chocolatey安装
choco install upx

# 或者从官网下载: https://upx.github.io/
```

## 构建脚本使用

### 基本用法

```powershell
# 显示帮助信息
.\build.ps1 -Help

# 构建所有平台（默认）
.\build.ps1

# 构建指定平台
.\build.ps1 -Platform windows/amd64

# 开发模式构建（快速，无优化）
.\build.ps1 -Dev

# 清理构建目录
.\build.ps1 -Clean
```

### 参数说明

| 参数 | 说明 | 示例 |
|------|------|------|
| `-All` | 构建所有平台（默认行为） | `.\build.ps1 -All` |
| `-Platform` | 指定构建平台 | `.\build.ps1 -Platform linux/amd64` |
| `-Clean` | 清理构建目录 | `.\build.ps1 -Clean` |
| `-Dev` | 开发模式（快速构建，跳过优化） | `.\build.ps1 -Dev` |
| `-NoUpx` | 禁用UPX压缩 | `.\build.ps1 -NoUpx` |
| `-Verbose` | 详细输出 | `.\build.ps1 -Verbose` |
| `-Help` | 显示帮助信息 | `.\build.ps1 -Help` |

### 支持的平台

- `linux/amd64` - Linux 64位
- `linux/arm64` - Linux ARM64
- `linux/386` - Linux 32位
- `windows/amd64` - Windows 64位
- `windows/386` - Windows 32位
- `darwin/amd64` - macOS Intel
- `darwin/arm64` - macOS Apple Silicon
- `freebsd/amd64` - FreeBSD 64位

## 构建示例

### 1. 快速开发构建

适用于开发阶段的快速测试：

```powershell
.\build.ps1 -Platform windows/amd64 -Dev -Verbose
```

### 2. 生产环境构建

构建所有平台的优化版本：

```powershell
.\build.ps1 -Verbose
```

### 3. 特定平台构建

只构建Linux 64位版本：

```powershell
.\build.ps1 -Platform linux/amd64
```

### 4. 无压缩构建

如果没有安装UPX或不想压缩：

```powershell
.\build.ps1 -NoUpx
```

## 构建输出

### 目录结构

```
mediaProxy/
├── build/          # 构建的二进制文件
│   ├── mediaProxy_windows_amd64.exe
│   ├── mediaProxy_linux_amd64
│   └── ...
└── dist/           # 发布包
    ├── mediaProxy_v1.0.0_windows_amd64.zip
    ├── mediaProxy_v1.0.0_linux_amd64.tar.gz
    └── ...
```

### 发布包内容

每个发布包包含：
- 编译好的二进制文件
- README.md 文档
- start.bat/start.sh 启动脚本
- config.example 配置示例

## 构建优化

### 体积优化

1. **Go编译标志**: `-s -w` 去除调试信息
2. **UPX压缩**: 使用LZMA算法进一步压缩
3. **Trimpath**: 去除构建路径信息

### 版本信息

构建时会自动嵌入以下信息：
- Git版本标签或提交哈希
- 构建时间（UTC）
- Git提交哈希

## 故障排除

### 常见问题

1. **Go环境未找到**
   ```
   Error: Go environment not found
   ```
   解决：确保Go已正确安装并添加到PATH

2. **UPX压缩失败**
   ```
   UPX compression failed, continuing...
   ```
   解决：这是警告，不影响构建。可安装UPX或使用`-NoUpx`参数

3. **权限错误**
   ```
   Access denied
   ```
   解决：以管理员身份运行PowerShell

4. **执行策略限制**
   ```
   cannot be loaded because running scripts is disabled
   ```
   解决：
   ```powershell
   Set-ExecutionPolicy -ExecutionPolicy RemoteSigned -Scope CurrentUser
   ```

### 调试模式

使用`-Verbose`参数获取详细的构建信息：

```powershell
.\build.ps1 -Platform windows/amd64 -Verbose
```

## 性能对比

| 构建模式 | 构建时间 | 文件大小 | 适用场景 |
|----------|----------|----------|----------|
| 开发模式 | ~10秒 | ~8MB | 开发测试 |
| 标准模式 | ~30秒 | ~3MB | 生产部署 |
| 全平台 | ~3分钟 | 各平台~3MB | 发布版本 |

## 自动化集成

### 在CI/CD中使用

```powershell
# 检查Go环境
go version

# 清理并构建
.\build.ps1 -Clean
.\build.ps1 -All -Verbose

# 检查构建结果
if ($LASTEXITCODE -eq 0) {
    Write-Host "Build successful"
} else {
    Write-Host "Build failed"
    exit 1
}
```

### 定时构建

可以结合Windows任务计划程序实现定时构建：

```powershell
# 创建构建任务
$action = New-ScheduledTaskAction -Execute "PowerShell.exe" -Argument "-File C:\path\to\build.ps1"
$trigger = New-ScheduledTaskTrigger -Daily -At "02:00"
Register-ScheduledTask -TaskName "MediaProxy-Build" -Action $action -Trigger $trigger
```

## 更多信息

- 项目主页: [MediaProxy GitHub](https://github.com/your-org/mediaProxy)
- 问题反馈: [Issues](https://github.com/your-org/mediaProxy/issues)
- 贡献指南: [CONTRIBUTING.md](CONTRIBUTING.md)