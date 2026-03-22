@echo off
setlocal

echo [1/2] Building Android 32-bit (ARM)...
set CGO_ENABLED=0
set GOOS=linux
set GOARCH=arm
set GOARM=7
go build -trimpath -ldflags="-w -s" -o goProxy/goProxy-arm proxy.go
if %errorlevel% neq 0 (
    echo Failed to build goProxy-arm
    exit /b %errorlevel%
)
echo Success: goProxy/goProxy-arm
upx --best --lzma goProxy/goProxy-arm >nul 2>&1
if %errorlevel% equ 0 (
    echo Compressed goProxy-arm with UPX
) else (
    echo UPX compression skipped for goProxy-arm
)

echo [2/2] Building Android 64-bit (ARM64)...
set CGO_ENABLED=0
set GOOS=linux
set GOARCH=arm64
go build -trimpath -ldflags="-w -s" -o goProxy/goProxy-arm64 proxy.go
if %errorlevel% neq 0 (
    echo Failed to build goProxy-arm64
    exit /b %errorlevel%
)
echo Success: goProxy/goProxy-arm64
upx --best --lzma goProxy/goProxy-arm64 >nul 2>&1
if %errorlevel% equ 0 (
    echo Compressed goProxy-arm64 with UPX
) else (
    echo UPX compression skipped for goProxy-arm64
)

echo All builds completed successfully!
endlocal
