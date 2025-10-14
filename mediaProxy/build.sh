#!/bin/bash

# MediaProxy 跨平台构建脚本
# 支持多平台编译，优化二进制文件体积

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 项目信息
APP_NAME="mediaProxy"
VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(date -u '+%Y-%m-%d_%H:%M:%S_UTC')
GIT_COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")

# 构建目录
BUILD_DIR="build"
DIST_DIR="dist"

# 支持的平台
PLATFORMS=(
    "linux/amd64"
    "linux/arm64"
    "linux/386"
    "windows/amd64"
    "windows/386"
    "darwin/amd64"
    "darwin/arm64"
    "freebsd/amd64"
)

# 清理函数
cleanup() {
    echo -e "${YELLOW}清理构建目录...${NC}"
    rm -rf "$BUILD_DIR" "$DIST_DIR"
}

# 创建目录
setup_dirs() {
    echo -e "${BLUE}创建构建目录...${NC}"
    mkdir -p "$BUILD_DIR" "$DIST_DIR"
}

# 构建单个平台
build_platform() {
    local platform=$1
    local goos=$(echo $platform | cut -d'/' -f1)
    local goarch=$(echo $platform | cut -d'/' -f2)
    
    local output_name="$APP_NAME"
    if [ "$goos" = "windows" ]; then
        output_name="$APP_NAME.exe"
    fi
    
    local output_path="$BUILD_DIR/${APP_NAME}_${goos}_${goarch}"
    if [ "$goos" = "windows" ]; then
        output_path="${output_path}.exe"
    fi
    
    echo -e "${BLUE}构建 $goos/$goarch...${NC}"
    
    # 设置构建参数
    export GOOS=$goos
    export GOARCH=$goarch
    export CGO_ENABLED=0
    
    # 构建标志，优化体积
    local ldflags="-s -w"
    ldflags="$ldflags -X main.Version=$VERSION"
    ldflags="$ldflags -X main.BuildTime=$BUILD_TIME"
    ldflags="$ldflags -X main.GitCommit=$GIT_COMMIT"
    
    # 执行构建
    go build \
        -ldflags="$ldflags" \
        -trimpath \
        -o "$output_path" \
        .
    
    if [ $? -eq 0 ]; then
        # 获取文件大小
        local size=$(du -h "$output_path" | cut -f1)
        echo -e "${GREEN}✓ $goos/$goarch 构建成功 (大小: $size)${NC}"
        
        # 使用UPX压缩 (如果可用)
        if command -v upx >/dev/null 2>&1; then
            echo -e "${YELLOW}使用UPX压缩...${NC}"
            upx --best --lzma "$output_path" 2>/dev/null || true
            local compressed_size=$(du -h "$output_path" | cut -f1)
            echo -e "${GREEN}✓ 压缩完成 (压缩后: $compressed_size)${NC}"
        fi
        
        # 创建发布包
        create_release_package "$goos" "$goarch" "$output_path"
    else
        echo -e "${RED}✗ $goos/$goarch 构建失败${NC}"
        return 1
    fi
}

# 创建发布包
create_release_package() {
    local goos=$1
    local goarch=$2
    local binary_path=$3
    
    local package_name="${APP_NAME}_${VERSION}_${goos}_${goarch}"
    local package_dir="$DIST_DIR/$package_name"
    
    mkdir -p "$package_dir"
    
    # 复制二进制文件
    cp "$binary_path" "$package_dir/"
    
    # 复制文档
    cp README.md "$package_dir/"
    
    # 创建启动脚本
    if [ "$goos" = "windows" ]; then
        cat > "$package_dir/start.bat" << 'EOF'
@echo off
echo Starting MediaProxy...
mediaProxy.exe -port 57574
pause
EOF
    else
        cat > "$package_dir/start.sh" << 'EOF'
#!/bin/bash
echo "Starting MediaProxy..."
./mediaProxy -port 57574
EOF
        chmod +x "$package_dir/start.sh"
    fi
    
    # 创建配置文件示例
    cat > "$package_dir/config.example" << 'EOF'
# MediaProxy 配置示例
# 使用方法: ./mediaProxy -port 57574 -dns 8.8.8.8 -debug

# 默认端口
PORT=57574

# DNS服务器
DNS=8.8.8.8

# 调试模式
DEBUG=false
EOF
    
    # 打包
    cd "$DIST_DIR"
    if [ "$goos" = "windows" ]; then
        zip -r "${package_name}.zip" "$package_name" >/dev/null
        echo -e "${GREEN}✓ 创建发布包: ${package_name}.zip${NC}"
    else
        tar -czf "${package_name}.tar.gz" "$package_name" >/dev/null
        echo -e "${GREEN}✓ 创建发布包: ${package_name}.tar.gz${NC}"
    fi
    cd - >/dev/null
    
    # 清理临时目录
    rm -rf "$package_dir"
}

# 显示帮助
show_help() {
    echo "MediaProxy 构建脚本"
    echo ""
    echo "用法: $0 [选项]"
    echo ""
    echo "选项:"
    echo "  -h, --help     显示帮助信息"
    echo "  -c, --clean    清理构建目录"
    echo "  -a, --all      构建所有平台 (默认)"
    echo "  -p, --platform 指定平台 (例如: linux/amd64)"
    echo "  --no-upx       禁用UPX压缩"
    echo ""
    echo "支持的平台:"
    for platform in "${PLATFORMS[@]}"; do
        echo "  $platform"
    done
}

# 主函数
main() {
    local build_all=true
    local target_platform=""
    local use_upx=true
    
    # 解析参数
    while [[ $# -gt 0 ]]; do
        case $1 in
            -h|--help)
                show_help
                exit 0
                ;;
            -c|--clean)
                cleanup
                exit 0
                ;;
            -a|--all)
                build_all=true
                shift
                ;;
            -p|--platform)
                build_all=false
                target_platform="$2"
                shift 2
                ;;
            --no-upx)
                use_upx=false
                shift
                ;;
            *)
                echo -e "${RED}未知参数: $1${NC}"
                show_help
                exit 1
                ;;
        esac
    done
    
    echo -e "${GREEN}MediaProxy 构建脚本${NC}"
    echo -e "${BLUE}版本: $VERSION${NC}"
    echo -e "${BLUE}提交: $GIT_COMMIT${NC}"
    echo ""
    
    # 检查Go环境
    if ! command -v go >/dev/null 2>&1; then
        echo -e "${RED}错误: 未找到Go环境${NC}"
        exit 1
    fi
    
    echo -e "${BLUE}Go版本: $(go version)${NC}"
    echo ""
    
    # 检查UPX
    if [ "$use_upx" = true ] && ! command -v upx >/dev/null 2>&1; then
        echo -e "${YELLOW}警告: 未找到UPX，将跳过压缩步骤${NC}"
        echo -e "${YELLOW}安装UPX可进一步减小二进制文件体积${NC}"
        echo ""
    fi
    
    # 清理并创建目录
    cleanup
    setup_dirs
    
    # 下载依赖
    echo -e "${BLUE}下载依赖...${NC}"
    go mod tidy
    go mod download
    echo ""
    
    # 构建
    local success_count=0
    local total_count=0
    
    if [ "$build_all" = true ]; then
        for platform in "${PLATFORMS[@]}"; do
            total_count=$((total_count + 1))
            if build_platform "$platform"; then
                success_count=$((success_count + 1))
            fi
            echo ""
        done
    else
        total_count=1
        if build_platform "$target_platform"; then
            success_count=1
        fi
    fi
    
    # 构建总结
    echo -e "${GREEN}构建完成!${NC}"
    echo -e "${BLUE}成功: $success_count/$total_count${NC}"
    
    if [ -d "$DIST_DIR" ] && [ "$(ls -A $DIST_DIR)" ]; then
        echo -e "${BLUE}发布包位置: $DIST_DIR/${NC}"
        ls -la "$DIST_DIR"
    fi
    
    if [ $success_count -lt $total_count ]; then
        exit 1
    fi
}

# 执行主函数
main "$@"