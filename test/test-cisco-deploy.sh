#!/bin/bash

# Cisco配置下发测试脚本
# 用于验证deploy服务中Cisco IOS设备的配置下发功能

set -e

# 配置参数
API_URL="http://localhost:18000/api/v1/deploy/fast"
PAYLOAD_FILE="/tmp/cisco-deploy-test-payload.json"

# 创建测试payload
cat > "$PAYLOAD_FILE" << 'EOF'
{
  "task_id": "DEMO-20251017-01",
  "task_name": "config-deploy-demo",
  "retry_flag": 1,
  "task_type": "exec",
  "task_timeout": 15,
  "status_check_enable": 1,
  "devices": [
    {
      "device_ip": "139.196.196.96",
      "device_port": 21201,
      "device_name": "test-out-r1",
      "device_platform": "cisco_ios",
      "collect_protocol": "ssh",
      "user_name": "eccom123",
      "password": "Eccom@12345",
      "enable_password": "Eccom@12345",
      "status_check_list": ["terminal length 0", "show running-config", "show version"],
      "config_deploy": "interface g1/0\ndescri test1234\n"
    }
  ]
}
EOF

echo "=== Cisco配置下发测试 ==="
echo "API URL: $API_URL"
echo "Payload文件: $PAYLOAD_FILE"
echo ""

# 显示payload内容
echo "=== 请求Payload ==="
cat "$PAYLOAD_FILE" | jq .
echo ""

# 发送请求
echo "=== 发送配置下发请求 ==="
RESPONSE=$(curl -s -X POST \
  -H "Content-Type: application/json" \
  -d @"$PAYLOAD_FILE" \
  "$API_URL")

# 检查响应
if [ $? -eq 0 ]; then
    echo "=== 响应结果 ==="
    echo "$RESPONSE" | jq .
    
    # 检查是否成功
    ERROR_COUNT=$(echo "$RESPONSE" | jq -r '.results[0].error // empty' | wc -l)
    DEPLOY_LOGS=$(echo "$RESPONSE" | jq -r '.results[0].deploy_log_exec // []')
    
    echo ""
    echo "=== 测试结果分析 ==="
    
    if [ -n "$(echo "$RESPONSE" | jq -r '.results[0].error // empty')" ]; then
        echo "❌ 配置下发失败"
        echo "错误信息: $(echo "$RESPONSE" | jq -r '.results[0].error')"
    else
        echo "✅ 配置下发成功"
    fi
    
    # 分析部署日志
    echo ""
    echo "=== 部署日志分析 ==="
    echo "$RESPONSE" | jq -r '.results[0].deploy_log_exec[]? | "命令: \(.command)\n输出: \(.output)\n错误: \(.error // "无")\n退出码: \(.exit_code)\n耗时: \(.elapsed)\n---"'
    
else
    echo "❌ 请求失败"
    exit 1
fi

# 清理临时文件
rm -f "$PAYLOAD_FILE"

echo ""
echo "=== 测试完成 ==="