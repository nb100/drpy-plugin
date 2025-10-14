# MediaProxy PowerShell Build Script
# Designed for Windows development environment with multi-platform compilation and size optimization

param(
    [string]$Platform = "",
    [switch]$All = $false,
    [switch]$Clean = $false,
    [switch]$Help = $false,
    [switch]$NoUpx = $false,
    [switch]$Dev = $false,
    [switch]$Verbose = $false,
    [switch]$Special = $false
)

# Set error handling
$ErrorActionPreference = "Stop"

# Project information
$AppName = "mediaProxy"
$BuildDir = "build"
$DistDir = "dist"

# Supported platforms
$Platforms = @(
    "linux/amd64",
    "linux/arm64", 
    "linux/386",
    "windows/amd64",
    "windows/386",
    "darwin/amd64",
    "darwin/arm64",
    "freebsd/amd64"
)

# Color output function
function Write-ColorOutput {
    param(
        [string]$Message,
        [string]$Color = "White"
    )
    
    switch ($Color) {
        "Red" { Write-Host $Message -ForegroundColor Red }
        "Green" { Write-Host $Message -ForegroundColor Green }
        "Yellow" { Write-Host $Message -ForegroundColor Yellow }
        "Blue" { Write-Host $Message -ForegroundColor Blue }
        "Cyan" { Write-Host $Message -ForegroundColor Cyan }
        default { Write-Host $Message -ForegroundColor White }
    }
}

# Get version information
function Get-VersionInfo {
    try {
        $version = git describe --tags --always --dirty 2>$null
        if (-not $version) { $version = "dev" }
    } catch {
        $version = "dev"
    }
    
    try {
        $gitCommit = git rev-parse --short HEAD 2>$null
        if (-not $gitCommit) { $gitCommit = "unknown" }
    } catch {
        $gitCommit = "unknown"
    }
    
    $buildTime = (Get-Date).ToUniversalTime().ToString("yyyy-MM-dd_HH:mm:ss_UTC")
    
    return @{
        Version = $version
        GitCommit = $gitCommit
        BuildTime = $buildTime
    }
}

# Clean function
function Clear-BuildDirs {
    Write-ColorOutput "Cleaning build directories..." "Yellow"
    
    if (Test-Path $BuildDir) {
        Remove-Item $BuildDir -Recurse -Force
    }
    if (Test-Path $DistDir) {
        Remove-Item $DistDir -Recurse -Force
    }
    
    Write-ColorOutput "Clean completed" "Green"
}

# Create directories
function New-BuildDirs {
    Write-ColorOutput "Creating build directories..." "Blue"
    
    New-Item -ItemType Directory -Path $BuildDir -Force | Out-Null
    New-Item -ItemType Directory -Path $DistDir -Force | Out-Null
}

# Check Go environment
function Test-GoEnvironment {
    try {
        $goVersion = go version
        Write-ColorOutput "Go environment check passed" "Green"
        Write-ColorOutput $goVersion "Cyan"
        return $true
    } catch {
        Write-ColorOutput "Error: Go environment not found" "Red"
        return $false
    }
}

# Check UPX availability
function Test-UpxAvailable {
    try {
        upx --version | Out-Null
        return $true
    } catch {
        return $false
    }
}

# Build single platform
function Build-Platform {
    param(
        [string]$PlatformString,
        [hashtable]$VersionInfo,
        [bool]$UseUpx = $true
    )
    
    $parts = $PlatformString.Split('/')
    $goos = $parts[0]
    $goarch = $parts[1]
    
    $outputName = $AppName
    if ($goos -eq "windows") {
        $outputName = "$AppName.exe"
    }
    
    $outputPath = Join-Path $BuildDir "${AppName}_${goos}_${goarch}"
    if ($goos -eq "windows") {
        $outputPath = "$outputPath.exe"
    }
    
    Write-ColorOutput "Building $goos/$goarch..." "Blue"
    
    # Set environment variables
    $env:GOOS = $goos
    $env:GOARCH = $goarch
    $env:CGO_ENABLED = "0"
    
    # Build flags
    $ldflags = "-s -w"
    $ldflags += " -X main.Version=$($VersionInfo.Version)"
    $ldflags += " -X main.BuildTime=$($VersionInfo.BuildTime)"
    $ldflags += " -X main.GitCommit=$($VersionInfo.GitCommit)"
    
    try {
        # Execute build
        if ($Verbose) {
            Write-ColorOutput "Executing: go build -ldflags=`"$ldflags`" -trimpath -o `"$outputPath`" ." "Cyan"
        }
        
        go build -ldflags="$ldflags" -trimpath -o "$outputPath" .
        
        if ($LASTEXITCODE -ne 0) {
            throw "Build failed"
        }
        
        # Get file size
        $fileInfo = Get-Item $outputPath
        $sizeKB = [math]::Round($fileInfo.Length / 1024, 2)
        Write-ColorOutput "✓ $goos/$goarch build successful (size: $sizeKB KB)" "Green"
        
        # UPX compression
        if ($UseUpx -and (Test-UpxAvailable)) {
            Write-ColorOutput "Compressing with UPX..." "Yellow"
            try {
                upx --best --lzma "$outputPath" 2>$null
                $compressedInfo = Get-Item $outputPath
                $compressedSizeKB = [math]::Round($compressedInfo.Length / 1024, 2)
                Write-ColorOutput "✓ Compression completed (compressed: $compressedSizeKB KB)" "Green"
            } catch {
                Write-ColorOutput "UPX compression failed, continuing..." "Yellow"
            }
        }
        
        # Create release package
        New-ReleasePackage -Goos $goos -Goarch $goarch -BinaryPath $outputPath -VersionInfo $VersionInfo
        
        return $true
    } catch {
        Write-ColorOutput "✗ $goos/$goarch build failed: $($_.Exception.Message)" "Red"
        return $false
    } finally {
        # Clean environment variables
        Remove-Item env:GOOS -ErrorAction SilentlyContinue
        Remove-Item env:GOARCH -ErrorAction SilentlyContinue
        Remove-Item env:CGO_ENABLED -ErrorAction SilentlyContinue
    }
}

# Build special platforms with custom names
function Build-SpecialPlatforms {
    param(
        [hashtable]$VersionInfo,
        [bool]$UseUpx = $true
    )
    
    # Define special platforms with custom output names
    $specialPlatforms = @(
        @{ Platform = "android/arm64"; OutputName = "mediaProxy-android" },
        @{ Platform = "windows/amd64"; OutputName = "mediaProxy-win.exe" },
        @{ Platform = "linux/amd64"; OutputName = "mediaProxy-linux" }
    )
    
    Write-ColorOutput "Building special platforms with custom names..." "Cyan"
    Write-Host ""
    
    $successCount = 0
    $totalCount = $specialPlatforms.Count
    
    foreach ($spec in $specialPlatforms) {
        $platformString = $spec.Platform
        $customName = $spec.OutputName
        
        $parts = $platformString.Split('/')
        $goos = $parts[0]
        $goarch = $parts[1]
        
        # For Android, use linux as GOOS
        if ($goos -eq "android") {
            $goos = "linux"
        }
        
        $outputPath = Join-Path $BuildDir $customName
        
        Write-ColorOutput "Building $($spec.Platform) -> $customName..." "Blue"
        
        # Set environment variables
        $env:GOOS = $goos
        $env:GOARCH = $goarch
        $env:CGO_ENABLED = "0"
        
        # Build flags
        $ldflags = "-s -w"
        $ldflags += " -X main.Version=$($VersionInfo.Version)"
        $ldflags += " -X main.BuildTime=$($VersionInfo.BuildTime)"
        $ldflags += " -X main.GitCommit=$($VersionInfo.GitCommit)"
        
        try {
            # Execute build
            if ($Verbose) {
                Write-ColorOutput "Executing: go build -ldflags=`"$ldflags`" -trimpath -o `"$outputPath`" ." "Cyan"
            }
            
            go build -ldflags="$ldflags" -trimpath -o "$outputPath" .
            
            if ($LASTEXITCODE -ne 0) {
                throw "Build failed"
            }
            
            # Get file size
            $fileInfo = Get-Item $outputPath
            $sizeKB = [math]::Round($fileInfo.Length / 1024, 2)
            Write-ColorOutput "✓ $($spec.Platform) -> $customName build successful (size: $sizeKB KB)" "Green"
            
            # UPX compression
            if ($UseUpx -and (Test-UpxAvailable)) {
                Write-ColorOutput "Compressing with UPX..." "Yellow"
                try {
                    upx --best --lzma "$outputPath" 2>$null
                    $compressedInfo = Get-Item $outputPath
                    $compressedSizeKB = [math]::Round($compressedInfo.Length / 1024, 2)
                    Write-ColorOutput "✓ Compression completed (compressed: $compressedSizeKB KB)" "Green"
                } catch {
                    Write-ColorOutput "UPX compression failed, continuing..." "Yellow"
                }
            }
            
            $successCount++
            
        } catch {
            Write-ColorOutput "✗ $($spec.Platform) -> $customName build failed: $($_.Exception.Message)" "Red"
        } finally {
            # Clean environment variables
            Remove-Item env:GOOS -ErrorAction SilentlyContinue
            Remove-Item env:GOARCH -ErrorAction SilentlyContinue
            Remove-Item env:CGO_ENABLED -ErrorAction SilentlyContinue
        }
        
        Write-Host ""
    }
    
    return @{ Success = $successCount; Total = $totalCount }
}

# Create release package
function New-ReleasePackage {
    param(
        [string]$Goos,
        [string]$Goarch,
        [string]$BinaryPath,
        [hashtable]$VersionInfo
    )
    
    $packageName = "${AppName}_$($VersionInfo.Version)_${Goos}_${Goarch}"
    $packageDir = Join-Path $DistDir $packageName
    
    New-Item -ItemType Directory -Path $packageDir -Force | Out-Null
    
    # Copy binary file
    Copy-Item $BinaryPath $packageDir
    
    # Copy documentation
    if (Test-Path "README.md") {
        Copy-Item "README.md" $packageDir
    }
    
    # Create start script
    if ($Goos -eq "windows") {
        $startScript = @"
@echo off
echo Starting MediaProxy...
$AppName.exe -port 57574
pause
"@
        $startScript | Out-File -FilePath (Join-Path $packageDir "start.bat") -Encoding ASCII
    } else {
        $startScript = @"
#!/bin/bash
echo "Starting MediaProxy..."
./$AppName -port 57574
"@
        $startScript | Out-File -FilePath (Join-Path $packageDir "start.sh") -Encoding UTF8
    }
    
    # Create configuration example
    $configExample = @"
# MediaProxy Configuration Example
# Usage: ./$AppName -port 57574 -dns 8.8.8.8 -debug

# Default port
PORT=57574

# DNS server
DNS=8.8.8.8

# Debug mode
DEBUG=false
"@
    $configExample | Out-File -FilePath (Join-Path $packageDir "config.example") -Encoding UTF8
    
    # Package
    try {
        Push-Location $DistDir
        if ($Goos -eq "windows") {
            Compress-Archive -Path $packageName -DestinationPath "$packageName.zip" -Force
            Write-ColorOutput "✓ Created release package: $packageName.zip" "Green"
        } else {
            # For non-Windows platforms, create tar.gz if tar command is available
            if (Get-Command tar -ErrorAction SilentlyContinue) {
                tar -czf "$packageName.tar.gz" $packageName
                Write-ColorOutput "✓ Created release package: $packageName.tar.gz" "Green"
            } else {
                Write-ColorOutput "✓ Created release directory: $packageName" "Green"
            }
        }
    } catch {
        Write-ColorOutput "Packaging failed: $($_.Exception.Message)" "Yellow"
    } finally {
        Pop-Location
    }
    
    # Clean temporary directory
    Remove-Item $packageDir -Recurse -Force -ErrorAction SilentlyContinue
}

# Show help
function Show-Help {
    Write-ColorOutput "MediaProxy PowerShell Build Script" "Cyan"
    Write-Host ""
    Write-ColorOutput "Usage:" "White"
    Write-Host "  .\build.ps1 [options]"
    Write-Host ""
    Write-ColorOutput "Options:" "White"
    Write-Host "  -All              Build all platforms (default)"
    Write-Host "  -Platform <name>  Specify platform (e.g., windows/amd64)"
    Write-Host "  -Special          Build special platforms (Android ARM64, Windows AMD64, Linux AMD64) with custom names"
    Write-Host "  -Clean            Clean build directories"
    Write-Host "  -Dev              Development mode build (fast build, no optimization)"
    Write-Host "  -NoUpx            Disable UPX compression"
    Write-Host "  -Verbose          Verbose output"
    Write-Host "  -Help             Show help information"
    Write-Host ""
    Write-ColorOutput "Supported platforms:" "White"
    foreach ($platform in $Platforms) {
        Write-Host "  $platform"
    }
    Write-Host ""
    Write-ColorOutput "Examples:" "White"
    Write-Host "  .\build.ps1                           # Build all platforms"
    Write-Host "  .\build.ps1 -Platform windows/amd64   # Build Windows 64-bit only"
    Write-Host "  .\build.ps1 -Special                  # Build special platforms (Android, Windows, Linux) with custom names"
    Write-Host "  .\build.ps1 -Dev                      # Development mode build"
    Write-Host "  .\build.ps1 -Clean                    # Clean build directories"
}

# Main function
function Main {
    Write-Host ""
    Write-ColorOutput "========================================" "Cyan"
    Write-ColorOutput "MediaProxy PowerShell Build Script" "Cyan"
    Write-ColorOutput "========================================" "Cyan"
    
    # Show help
    if ($Help) {
        Show-Help
        return
    }
    
    # Clean mode
    if ($Clean) {
        Clear-BuildDirs
        return
    }
    
    # Check Go environment
    if (-not (Test-GoEnvironment)) {
        return
    }
    
    # Get version information
    $versionInfo = Get-VersionInfo
    Write-ColorOutput "Version: $($versionInfo.Version)" "Blue"
    Write-ColorOutput "Commit: $($versionInfo.GitCommit)" "Blue"
    Write-ColorOutput "Build Time: $($versionInfo.BuildTime)" "Blue"
    Write-Host ""
    
    # Check UPX
    $upxAvailable = Test-UpxAvailable
    if (-not $upxAvailable -and -not $NoUpx) {
        Write-ColorOutput "Warning: UPX not found, compression will be skipped" "Yellow"
        Write-ColorOutput "Install UPX to further reduce binary size" "Yellow"
        Write-Host ""
    }
    
    # Clean and create directories
    Clear-BuildDirs
    New-BuildDirs
    
    # Download dependencies
    Write-ColorOutput "Downloading dependencies..." "Blue"
    try {
        go mod tidy
        go mod download
    } catch {
        Write-ColorOutput "Dependency download failed: $($_.Exception.Message)" "Red"
        return
    }
    Write-Host ""
    
    # Build
    $successCount = 0
    $totalCount = 0
    $useUpx = $upxAvailable -and -not $NoUpx -and -not $Dev
    
    if ($Special) {
        # Build special platforms with custom names
        $result = Build-SpecialPlatforms -VersionInfo $versionInfo -UseUpx $useUpx
        $successCount = $result.Success
        $totalCount = $result.Total
    } elseif ($Platform) {
        # Build specified platform
        if ($Platform -notin $Platforms) {
            Write-ColorOutput "Error: Unsupported platform '$Platform'" "Red"
            Write-ColorOutput "Supported platforms: $($Platforms -join ', ')" "Yellow"
            return
        }
        
        $totalCount = 1
        if (Build-Platform -PlatformString $Platform -VersionInfo $versionInfo -UseUpx $useUpx) {
            $successCount = 1
        }
    } else {
        # Build all platforms
        foreach ($platformString in $Platforms) {
            $totalCount++
            if (Build-Platform -PlatformString $platformString -VersionInfo $versionInfo -UseUpx $useUpx) {
                $successCount++
            }
            Write-Host ""
        }
    }
    
    # Build summary
    Write-Host ""
    Write-ColorOutput "========================================" "Cyan"
    Write-ColorOutput "Build completed!" "Green"
    Write-ColorOutput "Success: $successCount/$totalCount" "Blue"
    Write-ColorOutput "========================================" "Cyan"
    
    if (Test-Path $DistDir) {
        $items = Get-ChildItem $DistDir
        if ($items.Count -gt 0) {
            Write-Host ""
            Write-ColorOutput "Release packages location: $DistDir" "Blue"
            $items | ForEach-Object {
                $size = if ($_.PSIsContainer) { "Directory" } else { "$([math]::Round($_.Length / 1MB, 2)) MB" }
                Write-Host "  $($_.Name) ($size)"
            }
        }
    }
    
    if ($successCount -lt $totalCount) {
        Write-Host ""
        Write-ColorOutput "Some builds failed, please check error messages" "Yellow"
        exit 1
    } else {
        Write-Host ""
        Write-ColorOutput "All builds completed successfully!" "Green"
    }
}

# Execute main function
try {
    Main
} catch {
    Write-ColorOutput "Build script execution failed: $($_.Exception.Message)" "Red"
    if ($Verbose) {
        Write-ColorOutput "Detailed error information:" "Red"
        Write-Host $_.Exception.StackTrace
    }
    exit 1
}