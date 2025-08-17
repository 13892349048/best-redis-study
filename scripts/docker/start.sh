#!/bin/bash

# Redis 启动脚本

echo "启动 Redis 单机版..."
cd "$(dirname "$0")"

# 检查 Docker 是否运行
if ! docker info >/dev/null 2>&1; then
    echo "错误：Docker 未运行，请先启动 Docker"
    exit 1
fi

# 停止并删除已存在的容器
echo "清理已存在的容器..."
docker-compose -f redis-standalone.yml down

# 启动新容器
echo "启动 Redis 容器..."
docker-compose -f redis-standalone.yml up -d

# 等待 Redis 启动
echo "等待 Redis 启动..."
sleep 3

# 检查 Redis 是否正常运行
if docker exec redis-standalone redis-cli ping >/dev/null 2>&1; then
    echo "✅ Redis 启动成功！"
    echo ""
    echo "连接信息："
    echo "  Host: localhost"
    echo "  Port: 6379"
    echo ""
    echo "使用以下命令连接："
    echo "  redis-cli"
    echo "  或者: docker exec -it redis-standalone redis-cli"
    echo ""
    echo "查看日志:"
    echo "  docker-compose -f redis-standalone.yml logs -f"
else
    echo "❌ Redis 启动失败"
    docker-compose -f redis-standalone.yml logs
    exit 1
fi 