param(
    [string]$JarPath = "custom_spider.jar",
    [string]$SourceDir = "goProxy"
)

Set-Location "$PSScriptRoot\.."

# Ensure 7z is available
$7zCmd = Get-Command "7z" -ErrorAction SilentlyContinue
if (-not $7zCmd) {
    Write-Error "Please ensure 7-Zip is installed and added to your system PATH."
    exit 1
}

# Check if target file exists
if (-not (Test-Path $JarPath)) {
    Write-Error "Target file not found: $JarPath"
    exit 1
}

$ArmPath = Join-Path $SourceDir "goProxy-arm"
$Arm64Path = Join-Path $SourceDir "goProxy-arm64"

if (-not (Test-Path $ArmPath) -or -not (Test-Path $Arm64Path)) {
    Write-Error "Compiled goProxy files not found. Please run build_goproxy.ps1 first."
    exit 1
}

Write-Host "Updating assets directory in $JarPath..." -ForegroundColor Cyan

$TempAssetsDir = "temp_assets_for_jar\assets"
New-Item -ItemType Directory -Force -Path $TempAssetsDir | Out-Null
Copy-Item -Path $ArmPath -Destination $TempAssetsDir -Force
Copy-Item -Path $Arm64Path -Destination $TempAssetsDir -Force

try {
    # 为了避免在 jar 中创建 temp_assets_for_jar 目录，我们需要进入到该目录后再执行打包
    # 使用 Push-Location 和 Pop-Location 来切换工作目录
    Push-Location "temp_assets_for_jar"
    
    # 构造相对路径的 jar 文件路径，因为我们改变了工作目录
    $AbsoluteJarPath = Resolve-Path "..\$JarPath"
    $updateCmd = "7z u `"$AbsoluteJarPath`" `"assets\*`""
    
    Invoke-Expression $updateCmd | Out-Null
    
    if ($LASTEXITCODE -ne 0) {
        throw "7z command failed with exit code: $LASTEXITCODE"
    }
    
    Pop-Location
    Write-Host "Successfully updated files to assets directory in $JarPath." -ForegroundColor Green
} catch {
    if ((Get-Location).Path -like "*temp_assets_for_jar*") {
        Pop-Location
    }
    Write-Error "Failed to update jar file: $_"
    Remove-Item -Path "temp_assets_for_jar" -Recurse -Force -ErrorAction SilentlyContinue
    exit 1
} finally {
    Remove-Item -Path "temp_assets_for_jar" -Recurse -Force -ErrorAction SilentlyContinue
}

Write-Host "Calculating MD5 for $JarPath..." -ForegroundColor Cyan
try {
    $md5Hash = (Get-FileHash -Path $JarPath -Algorithm MD5).Hash.ToLower()
    $Md5FilePath = "$JarPath.md5"
    $md5Hash | Out-File -FilePath $Md5FilePath -Encoding ascii
    Write-Host "MD5 Calculation complete: $md5Hash" -ForegroundColor Green
    Write-Host "MD5 written to file: $Md5FilePath" -ForegroundColor Green
} catch {
    Write-Error "Failed to calculate MD5: $_"
    exit 1
}

Write-Host "All operations completed successfully!" -ForegroundColor Green
