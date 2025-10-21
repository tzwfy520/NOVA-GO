#!/bin/bash

# cisco_ios设备分页设置测试脚本
# 测试修复后的分页关闭命令是否正确

echo "=== Cisco IOS 分页设置测试 ==="
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
    "timeout": 30,
    "devices": [
      {
        "device_ip": "139.196.196.96",
        "device_port": 21201,
        "device_name": "test-out-sw1",
        "device_platform": "cisco_ios",
        "collect_protocol": "ssh",
        "user_name": "eccom123",
        "password": "Eccom@12345",
        "cli_list": [
          "show version",
          "show run",
          "show start-up",
          "show clock",
          "show interface",
          "show ip int brief"
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
    grep -i "terminal" log/collector.log | tail -10
elif [ -f "logs/collector.log" ]; then
    echo "检查 logs/collector.log 中的分页命令:"
    grep -i "terminal" logs/collector.log | tail -10
else
    echo "未找到collector.log文件"
fi

echo
echo "=== 检查命令回显日志 ==="
if [ -f "log/collector.log" ]; then
    echo "检查 log/collector.log 中的命令回显:"
    grep -A 2 -B 2 "terminal no length" log/collector.log | tail -20
elif [ -f "logs/collector.log" ]; then
    echo "检查 logs/collector.log 中的命令回显:"
    grep -A 2 -B 2 "terminal no length" logs/collector.log | tail -20
else
    echo "未找到相关日志"
fi

echo
echo "=== 测试完成 ==="
echo "请检查上述日志输出，确认:"
echo "1. 分页命令已从 'terminal length 0' 修改为 'terminal no length'"
echo "2. 命令执行没有出现 'Unknown command' 错误"
echo "3. 设备正确响应了分页关闭命令"