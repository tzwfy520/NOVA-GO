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
GIT_COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
GO_VERSION=$(go version | awk '{print $3}')

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

# 清理旧的构建文件
echo -e "${YELLOW}清理旧的构建文件...${NC}"
rm -f ${PROJECT_NAME}
rm -rf dist/

# 创建输出目录
mkdir -p dist

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

# 构建Linux版本
echo -e "${YELLOW}构建Linux版本...${NC}"
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build \
    -ldflags "${LDFLAGS}" \
    -o dist/${PROJECT_NAME}-linux-amd64 \
    ./cmd/server

# 构建macOS版本
echo -e "${YELLOW}构建macOS版本...${NC}"
CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 go build \
    -ldflags "${LDFLAGS}" \
    -o dist/${PROJECT_NAME}-darwin-amd64 \
    ./cmd/server

# 构建Windows版本
echo -e "${YELLOW}构建Windows版本...${NC}"
CGO_ENABLED=1 GOOS=windows GOARCH=amd64 go build \
    -ldflags "${LDFLAGS}" \
    -o dist/${PROJECT_NAME}-windows-amd64.exe \
    ./cmd/server

# 构建ARM64版本
echo -e "${YELLOW}构建ARM64版本...${NC}"
CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build \
    -ldflags "${LDFLAGS}" \
    -o dist/${PROJECT_NAME}-linux-arm64 \
    ./cmd/server

# 复制配置文件
echo -e "${YELLOW}复制配置文件...${NC}"
cp -r configs dist/
cp README.md dist/

# 创建压缩包
echo -e "${YELLOW}创建压缩包...${NC}"
cd dist
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
echo -e "${GREEN}构建文件位置: dist/${NC}"
ls -la dist/

echo ""
echo -e "${GREEN}构建成功! 🎉${NC}"