#!/bin/bash

# Day 10 分布式锁演示运行脚本

set -e

echo "=== Day 10: 分布式锁演示 ==="
echo

# 检查Redis是否运行
echo "检查Redis连接..."
if ! redis-cli ping > /dev/null 2>&1; then
    echo "❌ Redis未运行，请先启动Redis服务"
    echo "提示: 可以使用 docker run -d -p 6379:6379 redis:latest 启动Redis"
    exit 1
fi
echo "✅ Redis连接正常"
echo

# 构建程序
echo "构建演示程序..."
cd "$(dirname "$0")"
go build -o distributed_lock_demo distributed_lock_demo.go
go build -o fault_injection_test fault_injection_test.go
echo "✅ 构建完成"
echo

# 运行基本演示
echo "=== 运行基本分布式锁演示 ==="
./distributed_lock_demo
echo

# 询问是否运行故障注入测试
read -p "是否运行故障注入测试? (y/N): " run_fault_test
if [[ $run_fault_test =~ ^[Yy]$ ]]; then
    echo
    echo "=== 运行故障注入测试 ==="
    echo "⚠️  此测试会产生大量日志输出，可能需要几分钟时间"
    echo
    ./fault_injection_test
fi

# 清理
echo
echo "清理临时文件..."
rm -f distributed_lock_demo fault_injection_test
echo "✅ 清理完成"

echo
echo "=== Day 10 演示完成 ==="
echo "📚 查看学习文档: day10-summary.md"
