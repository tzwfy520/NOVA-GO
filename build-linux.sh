#!/bin/bash
# 使用本地Go环境构建Linux版本
export CGO_ENABLED=1
export GOOS=linux
export GOARCH=amd64

# 如果有gcc-multilib，尝试使用
if command -v x86_64-linux-gnu-gcc >/dev/null 2>&1; then
    export CC=x86_64-linux-gnu-gcc
fi

go build -o sshcollector-linux cmd/server/main.go
