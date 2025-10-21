#!/bin/bash

# h3c设备分页设置测试脚本
# 测试h3c设备的分页关闭命令和日志记录

echo "=== H3C 分页设置测试 ==="
echo "测试时间: $(date)"
echo

# 清理旧日志
echo "清理旧日志文件..."
rm -f logs/collector.log log/collector.log

# 发送测试请求
echo "发送测试请求到采集器API..."
curl -X POST http://localhost:18000/api/v1/collector/batch/custom \
  -H "Content-Type: application/json" \
  -d '{
    "task_id": "T-2001",
    "task_name": "custom-batch-check",
    "retry_flag": 0,
    "task_timeout": 30,
    "devices": [
      {
        "device_ip": "139.196.196.96",
        "device_port": 21202,
        "device_name": "test-out-r1",
        "device_platform": "h3c",
        "collect_protocol": "ssh",
        "user_name": "eccom123",
        "password": "Eccom@12345",
        "cli_list": [
          "display version",
          "display license"
        ]
      }
    ]
  }' | jq '.'

echo
echo "等待命令执行完成..."
sleep 5

# 检查日志中的分页命令
echo
echo "=== 检查分页命令日志 ==="
if [ -f "log/collector.log" ]; then
    echo "检查 log/collector.log 中的分页命令:"
    grep -i "screen-length" log/collector.log | tail -10
elif [ -f "logs/collector.log" ]; then
    echo "检查 logs/collector.log 中的分页命令:"
    grep -i "screen-length" logs/collector.log | tail -10
else
    echo "未找到collector.log文件"
fi

echo
echo "=== 检查命令回显日志 ==="
if [ -f "log/collector.log" ]; then
    echo "检查 log/collector.log 中的命令回显:"
    grep -A 3 -B 1 "screen-length disable" log/collector.log | tail -20
elif [ -f "logs/collector.log" ]; then
    echo "检查 logs/collector.log 中的命令回显:"
    grep -A 3 -B 1 "screen-length disable" logs/collector.log | tail -20
else
    echo "未找到相关日志"
fi

echo
echo "=== 分析Unicode字符 ==="
if [ -f "log/collector.log" ]; then
    echo "检查是否存在Unicode转义字符:"
    grep "\\\\u003c\\|\\\\u003e" log/collector.log | tail -5
elif [ -f "logs/collector.log" ]; then
    echo "检查是否存在Unicode转义字符:"
    grep "\\\\u003c\\|\\\\u003e" logs/collector.log | tail -5
fi

echo
echo "=== 检查设备响应 ==="
if [ -f "log/collector.log" ]; then
    echo "检查设备对screen-length disable命令的响应:"
    grep -A 5 "Command echo.*screen-length disable" log/collector.log | tail -15
elif [ -f "logs/collector.log" ]; then
    echo "检查设备对screen-length disable命令的响应:"
    grep -A 5 "Command echo.*screen-length disable" logs/collector.log | tail -15
fi

echo
echo "=== 测试完成 ==="
echo "请检查上述日志输出，确认:"
echo "1. screen-length disable 命令是否正确发送"
echo "2. 设备是否正确响应了分页关闭命令"
echo "3. Unicode转义字符是否为正常的日志格式化结果"
echo "4. 是否存在真实的命令执行错误"