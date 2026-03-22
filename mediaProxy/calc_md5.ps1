$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
$filePath = Join-Path -Path $scriptDir -ChildPath "custom_spider.jar"

if (Test-Path $filePath) {
    $md5 = (Get-FileHash -Path $filePath -Algorithm MD5).Hash.ToLower()
    Write-Host "文件: custom_spider.jar" -ForegroundColor Cyan
    Write-Host "MD5 : $md5" -ForegroundColor Green
} else {
    Write-Host "未找到文件: $filePath" -ForegroundColor Red
}

# 如果你在资源管理器中双击运行，这行代码可以让窗口保持打开状态方便查看结果
Read-Host "按 Enter 键退出..."
