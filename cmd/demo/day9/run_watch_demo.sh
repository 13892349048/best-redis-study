#!/bin/bash

echo "=== WATCH机制详细演示 ==="
echo

# 检查Redis是否运行
if ! redis-cli ping > /dev/null 2>&1; then
    echo "❌ Redis未运行，请先启动Redis服务器"
    echo "提示: docker run -d -p 6379:6379 redis:latest"
    exit 1
fi

echo "✅ Redis服务器运行正常"
echo

# 运行WATCH机制演示
echo "🚀 开始演示WATCH机制..."
cd "$(dirname "$0")"
go run watch_mechanism_demo.go

echo
echo "📚 关键要点："
echo "1. WATCH的键如果在事务执行前被修改，整个事务会被丢弃"
echo "2. EXEC返回nil表示事务失败，所有修改都不会生效"
echo "3. 需要实现重试机制来处理乐观锁冲突"
echo "4. 指数退避可以减少重试时的冲突概率"
