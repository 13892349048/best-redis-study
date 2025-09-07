#!/bin/bash

echo "=== Day 9: 一致性与事务语义演示 ==="
echo

# 检查Redis是否运行
if ! redis-cli ping > /dev/null 2>&1; then
    echo "❌ Redis未运行，请先启动Redis服务器"
    echo "提示: docker run -d -p 6379:6379 redis:latest"
    exit 1
fi

echo "✅ Redis服务器运行正常"
echo

# 运行演示程序
echo "🚀 开始运行一致性与事务演示..."
cd "$(dirname "$0")"
go run consistency_demo.go

echo
echo "📊 如需运行性能压测，请执行："
echo "cd ../../../scripts/bench/day9 && go run consistency_benchmark.go"
echo
echo "📚 查看学习总结："
echo "cat ../../../day9-summary.md"
