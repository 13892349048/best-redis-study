#!/bin/bash

echo "=== Day 9: 一致性与事务性能压测 ==="
echo

# 检查Redis是否运行
if ! redis-cli ping > /dev/null 2>&1; then
    echo "❌ Redis未运行，请先启动Redis服务器"
    echo "提示: docker run -d -p 6379:6379 redis:latest"
    exit 1
fi

echo "✅ Redis服务器运行正常"
echo

# 创建结果目录
mkdir -p ../../../benchmark_results

# 运行压测
echo "🚀 开始运行一致性与事务性能压测..."
echo "⚠️  注意：压测可能需要几分钟时间，请耐心等待..."
echo

cd "$(dirname "$0")"
go run consistency_benchmark.go

echo
echo "📊 压测完成！结果已保存到 benchmark_results/ 目录"
echo "📈 查看最新结果："
echo "ls -la ../../../benchmark_results/day9_*"
