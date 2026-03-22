Write-Host "[1/2] Building Android 32-bit (ARM)..."
$env:CGO_ENABLED="0"
$env:GOOS="linux"
$env:GOARCH="arm"
$env:GOARM="7"
go build -trimpath -ldflags="-w -s" -o goProxy/goProxy-arm proxy.go
if ($LASTEXITCODE -ne 0) {
    Write-Error "Failed to build goProxy-arm"
    exit $LASTEXITCODE
}
Write-Host "Success: goProxy/goProxy-arm" -ForegroundColor Green
try {
    upx --best --lzma goProxy/goProxy-arm 2>$null
    Write-Host "Compressed goProxy-arm with UPX" -ForegroundColor Green
} catch {
    Write-Host "UPX compression skipped for goProxy-arm" -ForegroundColor Yellow
}

Write-Host "[2/2] Building Android 64-bit (ARM64)..."
$env:CGO_ENABLED="0"
$env:GOOS="linux"
$env:GOARCH="arm64"
go build -trimpath -ldflags="-w -s" -o goProxy/goProxy-arm64 proxy.go
if ($LASTEXITCODE -ne 0) {
    Write-Error "Failed to build goProxy-arm64"
    exit $LASTEXITCODE
}
Write-Host "Success: goProxy/goProxy-arm64" -ForegroundColor Green
try {
    upx --best --lzma goProxy/goProxy-arm64 2>$null
    Write-Host "Compressed goProxy-arm64 with UPX" -ForegroundColor Green
} catch {
    Write-Host "UPX compression skipped for goProxy-arm64" -ForegroundColor Yellow
}

Write-Host "All builds completed successfully!" -ForegroundColor Green
