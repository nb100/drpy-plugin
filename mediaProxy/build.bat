@echo off
setlocal enabledelayedexpansion

REM MediaProxy Windows 构建脚本
REM 支持多平台编译，优化二进制文件体积

title MediaProxy Build Script

REM 项目信息
set APP_NAME=mediaProxy
set BUILD_DIR=build
set DIST_DIR=dist

REM 获取版本信息
for /f "tokens=*" %%i in ('git describe --tags --always --dirty 2^>nul') do set VERSION=%%i
if "%VERSION%"=="" set VERSION=dev

for /f "tokens=*" %%i in ('git rev-parse --short HEAD 2^>nul') do set GIT_COMMIT=%%i
if "%GIT_COMMIT%"=="" set GIT_COMMIT=unknown

for /f "tokens=2 delims==" %%i in ('wmic os get localdatetime /value') do set datetime=%%i
set BUILD_TIME=%datetime:~0,4%-%datetime:~4,2%-%datetime:~6,2%_%datetime:~8,2%:%datetime:~10,2%:%datetime:~12,2%_UTC

REM 支持的平台
set PLATFORMS=linux/amd64 linux/arm64 linux/386 windows/amd64 windows/386 darwin/amd64 darwin/arm64 freebsd/amd64

echo.
echo ========================================
echo MediaProxy 构建脚本
echo ========================================
echo 版本: %VERSION%
echo 提交: %GIT_COMMIT%
echo 构建时间: %BUILD_TIME%
echo.

REM 检查Go环境
go version >nul 2>&1
if errorlevel 1 (
    echo [错误] 未找到Go环境
    pause
    exit /b 1
)

echo Go环境检查通过
go version
echo.

REM 解析命令行参数
set BUILD_ALL=1
set TARGET_PLATFORM=
set CLEAN_ONLY=0

:parse_args
if "%~1"=="" goto args_done
if /i "%~1"=="-h" goto show_help
if /i "%~1"=="--help" goto show_help
if /i "%~1"=="-c" set CLEAN_ONLY=1 & goto next_arg
if /i "%~1"=="--clean" set CLEAN_ONLY=1 & goto next_arg
if /i "%~1"=="-a" set BUILD_ALL=1 & goto next_arg
if /i "%~1"=="--all" set BUILD_ALL=1 & goto next_arg
if /i "%~1"=="-p" set BUILD_ALL=0 & set TARGET_PLATFORM=%~2 & shift & goto next_arg
if /i "%~1"=="--platform" set BUILD_ALL=0 & set TARGET_PLATFORM=%~2 & shift & goto next_arg

echo [错误] 未知参数: %~1
goto show_help

:next_arg
shift
goto parse_args

:args_done

if %CLEAN_ONLY%==1 goto clean_exit

REM 清理并创建目录
echo 清理构建目录...
if exist "%BUILD_DIR%" rmdir /s /q "%BUILD_DIR%"
if exist "%DIST_DIR%" rmdir /s /q "%DIST_DIR%"

echo 创建构建目录...
mkdir "%BUILD_DIR%" 2>nul
mkdir "%DIST_DIR%" 2>nul

REM 下载依赖
echo 下载依赖...
go mod tidy
go mod download
echo.

REM 构建
set SUCCESS_COUNT=0
set TOTAL_COUNT=0

if %BUILD_ALL%==1 (
    for %%p in (%PLATFORMS%) do (
        call :build_platform "%%p"
    )
) else (
    call :build_platform "%TARGET_PLATFORM%"
)

echo.
echo ========================================
echo 构建完成!
echo 成功: %SUCCESS_COUNT%/%TOTAL_COUNT%
echo ========================================

if exist "%DIST_DIR%" (
    echo.
    echo 发布包位置: %DIST_DIR%
    dir /b "%DIST_DIR%"
)

if %SUCCESS_COUNT% LSS %TOTAL_COUNT% (
    echo.
    echo [警告] 部分构建失败
    pause
    exit /b 1
)

echo.
echo 所有构建成功完成!
pause
exit /b 0

REM 构建单个平台函数
:build_platform
set PLATFORM=%~1
set /a TOTAL_COUNT+=1

REM 解析平台信息
for /f "tokens=1,2 delims=/" %%a in ("%PLATFORM%") do (
    set GOOS=%%a
    set GOARCH=%%b
)

set OUTPUT_NAME=%APP_NAME%
if "%GOOS%"=="windows" set OUTPUT_NAME=%APP_NAME%.exe

set OUTPUT_PATH=%BUILD_DIR%\%APP_NAME%_%GOOS%_%GOARCH%
if "%GOOS%"=="windows" set OUTPUT_PATH=%OUTPUT_PATH%.exe

echo [构建] %GOOS%/%GOARCH%...

REM 设置环境变量
set GOOS=%GOOS%
set GOARCH=%GOARCH%
set CGO_ENABLED=0

REM 构建标志
set LDFLAGS=-s -w -X main.Version=%VERSION% -X main.BuildTime=%BUILD_TIME% -X main.GitCommit=%GIT_COMMIT%

REM 执行构建
go build -ldflags="%LDFLAGS%" -trimpath -o "%OUTPUT_PATH%" .

if errorlevel 1 (
    echo [失败] %GOOS%/%GOARCH% 构建失败
    goto :eof
)

REM 获取文件大小
for %%F in ("%OUTPUT_PATH%") do set FILE_SIZE=%%~zF
set /a FILE_SIZE_KB=!FILE_SIZE!/1024
echo [成功] %GOOS%/%GOARCH% 构建成功 ^(大小: !FILE_SIZE_KB! KB^)

REM 检查UPX
where upx >nul 2>&1
if not errorlevel 1 (
    echo [压缩] 使用UPX压缩...
    upx --best --lzma "%OUTPUT_PATH%" >nul 2>&1
    if not errorlevel 1 (
        for %%F in ("%OUTPUT_PATH%") do set COMPRESSED_SIZE=%%~zF
        set /a COMPRESSED_SIZE_KB=!COMPRESSED_SIZE!/1024
        echo [成功] 压缩完成 ^(压缩后: !COMPRESSED_SIZE_KB! KB^)
    )
)

REM 创建发布包
call :create_release_package "%GOOS%" "%GOARCH%" "%OUTPUT_PATH%"

set /a SUCCESS_COUNT+=1
goto :eof

REM 创建发布包函数
:create_release_package
set GOOS=%~1
set GOARCH=%~2
set BINARY_PATH=%~3

set PACKAGE_NAME=%APP_NAME%_%VERSION%_%GOOS%_%GOARCH%
set PACKAGE_DIR=%DIST_DIR%\%PACKAGE_NAME%

mkdir "%PACKAGE_DIR%" 2>nul

REM 复制二进制文件
copy "%BINARY_PATH%" "%PACKAGE_DIR%\" >nul

REM 复制文档
copy "README.md" "%PACKAGE_DIR%\" >nul

REM 创建启动脚本
if "%GOOS%"=="windows" (
    echo @echo off > "%PACKAGE_DIR%\start.bat"
    echo echo Starting MediaProxy... >> "%PACKAGE_DIR%\start.bat"
    echo %APP_NAME%.exe -port 57574 >> "%PACKAGE_DIR%\start.bat"
    echo pause >> "%PACKAGE_DIR%\start.bat"
) else (
    echo #!/bin/bash > "%PACKAGE_DIR%\start.sh"
    echo echo "Starting MediaProxy..." >> "%PACKAGE_DIR%\start.sh"
    echo ./%APP_NAME% -port 57574 >> "%PACKAGE_DIR%\start.sh"
)

REM 创建配置文件示例
(
echo # MediaProxy 配置示例
echo # 使用方法: ./%APP_NAME% -port 57574 -dns 8.8.8.8 -debug
echo.
echo # 默认端口
echo PORT=57574
echo.
echo # DNS服务器
echo DNS=8.8.8.8
echo.
echo # 调试模式
echo DEBUG=false
) > "%PACKAGE_DIR%\config.example"

REM 打包
pushd "%DIST_DIR%"
if "%GOOS%"=="windows" (
    powershell -command "Compress-Archive -Path '%PACKAGE_NAME%' -DestinationPath '%PACKAGE_NAME%.zip' -Force" >nul 2>&1
    if not errorlevel 1 (
        echo [打包] 创建发布包: %PACKAGE_NAME%.zip
    )
) else (
    REM 对于非Windows平台，创建tar.gz需要额外工具，这里只创建目录
    echo [打包] 创建发布目录: %PACKAGE_NAME%
)
popd

REM 清理临时目录
rmdir /s /q "%PACKAGE_DIR%" 2>nul

goto :eof

:clean_exit
echo 清理构建目录...
if exist "%BUILD_DIR%" rmdir /s /q "%BUILD_DIR%"
if exist "%DIST_DIR%" rmdir /s /q "%DIST_DIR%"
echo 清理完成
pause
exit /b 0

:show_help
echo.
echo MediaProxy 构建脚本
echo.
echo 用法: %~nx0 [选项]
echo.
echo 选项:
echo   -h, --help     显示帮助信息
echo   -c, --clean    清理构建目录
echo   -a, --all      构建所有平台 ^(默认^)
echo   -p, --platform 指定平台 ^(例如: linux/amd64^)
echo.
echo 支持的平台:
for %%p in (%PLATFORMS%) do echo   %%p
echo.
echo 示例:
echo   %~nx0                    # 构建所有平台
echo   %~nx0 -p windows/amd64   # 只构建Windows 64位
echo   %~nx0 -c                 # 清理构建目录
echo.
pause
exit /b 0