#!/usr/bin/env bash
set -euo pipefail

# Linux 采集自测脚本
# 用法：bash test/run-linux.sh
# 说明：会自动启动本地服务（端口 18000），随后用 payload 触发自定义批量采集接口。

PAYLOAD="test/payload-linux.json"
OUTFILE="logs/test-linux-output-after-fix.json"

# 确保日志目录存在
mkdir -p "$(dirname "$OUTFILE")"

# 运行 CLI 自测工具：
# - 自动启动服务（cmd/server/main.go）
# - 提交 payload 到 /api/v1/collector/batch/custom
# - 将响应写入 $OUTFILE 便于分析

go run cmd/cli/test_batch_custom.go \
  -payload "$PAYLOAD" \
  -start_server true \
  -keep_server false \
  -server_main "cmd/server/main.go" \
  -http_timeout 120 \
  -limit 50 \
  -wrap_width 140 \
  -out "$OUTFILE"

echo "[RUN-LINUX] Done. Output saved to: $OUTFILE"