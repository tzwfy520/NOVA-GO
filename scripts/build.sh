#!/bin/bash

# SSH采集器构建脚本
# 用于编译和打包应用

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 项目信息
PROJECT_NAME="sshcollector"
VERSION=${VERSION:-"1.0.0"}
BUILD_TIME=$(date '+%Y-%m-%d %H:%M:%S')
BUILD_ID=$(date '+%Y%m%d%H%M')
GIT_COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
GO_VERSION=$(go version | awk '{print $3}')
OUTPUT_ROOT="deploy"
OUTPUT_DIR="${OUTPUT_ROOT}/${BUILD_ID}"

# 构建信息
LDFLAGS="-X 'main.Version=${VERSION}' -X 'main.BuildTime=${BUILD_TIME}' -X 'main.GitCommit=${GIT_COMMIT}' -X 'main.GoVersion=${GO_VERSION}'"

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}  SSH采集器构建脚本${NC}"
echo -e "${BLUE}========================================${NC}"
echo -e "${GREEN}项目名称:${NC} ${PROJECT_NAME}"
echo -e "${GREEN}版本:${NC} ${VERSION}"
echo -e "${GREEN}构建时间:${NC} ${BUILD_TIME}"
echo -e "${GREEN}Git提交:${NC} ${GIT_COMMIT}"
echo -e "${GREEN}Go版本:${NC} ${GO_VERSION}"
echo ""

# 检查Go环境
echo -e "${YELLOW}检查Go环境...${NC}"
if ! command -v go &> /dev/null; then
    echo -e "${RED}错误: 未找到Go环境${NC}"
    exit 1
fi

# 准备输出目录（按打包时间归档）
echo -e "${YELLOW}准备输出目录...${NC}"
mkdir -p "${OUTPUT_DIR}"
echo -e "${GREEN}输出路径:${NC} ${OUTPUT_DIR}"

# 下载依赖
echo -e "${YELLOW}下载依赖...${NC}"
go mod download
go mod tidy

# 运行测试
echo -e "${YELLOW}运行测试...${NC}"
go test -v ./...

# 运行代码检查
echo -e "${YELLOW}运行代码检查...${NC}"
if command -v golangci-lint &> /dev/null; then
    golangci-lint run
else
    echo -e "${YELLOW}警告: 未找到golangci-lint，跳过代码检查${NC}"
fi

###############################################
# 构建三种平台可执行文件: linux / macOS / windows
###############################################

# 构建Linux版本 (amd64)
echo -e "${YELLOW}构建Linux版本 (amd64)...${NC}"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags "${LDFLAGS}" \
    -o "${OUTPUT_DIR}/${PROJECT_NAME}-linux-amd64" \
    ./cmd/server

# 构建macOS版本 (amd64)
echo -e "${YELLOW}构建macOS版本 (amd64)...${NC}"
CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 go build \
    -ldflags "${LDFLAGS}" \
    -o "${OUTPUT_DIR}/${PROJECT_NAME}-darwin-amd64" \
    ./cmd/server

# 构建macOS版本 (arm64 / Apple Silicon)
echo -e "${YELLOW}构建macOS版本 (arm64 / Apple Silicon)...${NC}"
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build \
    -ldflags "${LDFLAGS}" \
    -o "${OUTPUT_DIR}/${PROJECT_NAME}-darwin-arm64" \
    ./cmd/server

# 构建Windows版本 (amd64)
echo -e "${YELLOW}构建Windows版本 (amd64)...${NC}"
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build \
    -ldflags "${LDFLAGS}" \
    -o "${OUTPUT_DIR}/${PROJECT_NAME}-windows-amd64.exe" \
    ./cmd/server

# 复制配置文件
echo -e "${YELLOW}复制配置文件...${NC}"
mkdir -p "${OUTPUT_DIR}/configs"
cp -r configs/* "${OUTPUT_DIR}/configs" || true
cp README.md "${OUTPUT_DIR}/" || true

# 创建压缩包
echo -e "${YELLOW}创建压缩包...${NC}"
cd "${OUTPUT_DIR}"
for binary in ${PROJECT_NAME}-*; do
    if [[ -f "$binary" ]]; then
        if [[ "$binary" == *".exe" ]]; then
            # Windows版本
            zip -q "${binary%.exe}.zip" "$binary" configs/* README.md
        else
            # Unix版本
            tar -czf "${binary}.tar.gz" "$binary" configs README.md
        fi
    fi
done
cd ..

# 显示构建结果
echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}  构建完成${NC}"
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}构建文件位置: ${OUTPUT_DIR}/${NC}"
ls -la "${OUTPUT_DIR}"

echo ""
echo -e "${GREEN}构建成功! 🎉${NC}"