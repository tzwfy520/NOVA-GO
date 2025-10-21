#!/bin/bash

# Cisco配置下发测试脚本（使用本地模拟设备）
# 用于验证deploy服务中Cisco IOS设备的配置下发功能

set -e

# 配置参数
API_URL="http://localhost:18000/api/v1/deploy/fast"
PAYLOAD_FILE="/tmp/cisco-deploy-local-test-payload.json"

# 创建测试payload（使用本地模拟设备）
cat > "$PAYLOAD_FILE" << 'EOF'
{
  "task_id": "DEMO-LOCAL-TEST-01",
  "task_name": "config-deploy-local-demo",
  "retry_flag": 1,
  "task_type": "exec",
  "task_timeout": 15,
  "status_check_enable": 1,
  "devices": [
    {
      "device_ip": "127.0.0.1",
      "device_port": 22001,
      "device_name": "cisco-01",
      "device_platform": "cisco_ios",
      "collect_protocol": "ssh",
      "user_name": "cisco-01",
      "password": "nova",
      "enable_password": "nova",
      "status_check_list": ["terminal length 0", "show running-config"],
      "config_deploy": "interface g1/0\ndescription test-config-deploy\n"
    }
  ]
}
EOF

echo "=== Cisco配置下发测试（本地模拟设备） ==="
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
    ERROR_MSG=$(echo "$RESPONSE" | jq -r '.results[0].error // empty')
    
    echo ""
    echo "=== 测试结果分析 ==="
    
    if [ -n "$ERROR_MSG" ]; then
        echo "❌ 配置下发失败"
        echo "错误信息: $ERROR_MSG"
    else
        echo "✅ 配置下发成功"
    fi
    
    # 分析部署日志
    echo ""
    echo "=== 部署日志分析 ==="
    echo "$RESPONSE" | jq -r '.results[0].deploy_log_exec[]? | "命令: \(.command)\n输出: \(.output)\n错误: \(.error // "无")\n退出码: \(.exit_code)\n耗时: \(.elapsed)\n---"'
    
    # 检查enable命令是否正确执行
    echo ""
    echo "=== Enable命令执行检查 ==="
    ENABLE_FOUND=$(echo "$RESPONSE" | jq -r '.results[0].deploy_log_exec[]? | select(.command == "enable") | .command' | wc -l)
    if [ "$ENABLE_FOUND" -gt 0 ]; then
        echo "✅ Enable命令已正确执行"
        echo "$RESPONSE" | jq -r '.results[0].deploy_log_exec[]? | select(.command == "enable") | "Enable命令输出: \(.output)"'
    else
        echo "❌ Enable命令未找到"
    fi
    
    # 检查配置模式进入
    echo ""
    echo "=== 配置模式进入检查 ==="
    CONFIG_FOUND=$(echo "$RESPONSE" | jq -r '.results[0].deploy_log_exec[]? | select(.command == "configure terminal") | .command' | wc -l)
    if [ "$CONFIG_FOUND" -gt 0 ]; then
        echo "✅ 配置模式进入命令已执行"
        echo "$RESPONSE" | jq -r '.results[0].deploy_log_exec[]? | select(.command == "configure terminal") | "配置模式命令输出: \(.output)"'
    else
        echo "❌ 配置模式进入命令未找到"
    fi
    
else
    echo "❌ 请求失败"
    exit 1
fi

# 清理临时文件
rm -f "$PAYLOAD_FILE"

echo ""
echo "=== 测试完成 ==="