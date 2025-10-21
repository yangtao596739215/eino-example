#!/bin/bash

# Eino Web Chat 启动脚本

echo "🚀 Starting Eino Web Chat Server..."

# 设置默认环境变量
export CHECKPOINT_DIR="${CHECKPOINT_DIR:-./checkpoints}"
export OPENAI_BASE_URL="${OPENAI_BASE_URL:-https://cloud.infini-ai.com/maas/v1}"
export OPENAI_MODEL="${OPENAI_MODEL:-deepseek-v3.1-terminus}"
export OPENAI_API_KEY="${OPENAI_API_KEY:-sk-ziweosysvg6y7kla}"


# 检查环境变量
if [ -z "$OPENAI_API_KEY" ]; then
    echo "❌ Error: OPENAI_API_KEY environment variable is not set"
    echo "Please set your OpenAI API key:"
    echo "export OPENAI_API_KEY='your-api-key-here'"
    exit 1
fi



export PORT=${PORT:-"8080"}

echo "📋 Configuration:"
echo "  - OpenAI Model: $OPENAI_MODEL"
echo "  - OpenAI Base URL: $OPENAI_BASE_URL"
echo "  - Checkpoint Directory: $CHECKPOINT_DIR"
echo "  - Web Server Port: $PORT"
echo ""

# 创建checkpoint目录
echo "📁 Creating checkpoint directory..."
mkdir -p "$CHECKPOINT_DIR"
echo "✅ Checkpoint directory ready: $CHECKPOINT_DIR"
echo ""

# 检查端口是否被占用
echo "🔍 Checking port $PORT..."
if lsof -ti:$PORT > /dev/null 2>&1; then
    echo "⚠️  Port $PORT is already in use"
    echo "🔪 Killing existing process on port $PORT..."
    
    # 获取占用端口的进程ID
    PID=$(lsof -ti:$PORT)
    if [ ! -z "$PID" ]; then
        echo "   Killing process $PID..."
        kill -9 $PID
        sleep 2
        
        # 再次检查是否成功杀掉
        if lsof -ti:$PORT > /dev/null 2>&1; then
            echo "❌ Failed to kill process on port $PORT"
            echo "   Please manually kill the process or use a different port"
            exit 1
        else
            echo "✅ Successfully killed process on port $PORT"
        fi
    fi
else
    echo "✅ Port $PORT is available"
fi
echo ""

# 编译并运行
echo "🔨 Building application..."
if go build -o web_chat .; then
    echo "✅ Build successful"
    echo ""
    echo "🌐 Starting web server..."
    echo "   Open http://localhost:$PORT in your browser"
    echo "   Press Ctrl+C to stop the server"
    echo ""
    
    # 运行应用
    ./web_chat
else
    echo "❌ Build failed"
    exit 1
fi
