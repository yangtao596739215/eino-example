# Eino Web Chat Interface

这是一个基于 Eino 框架的 Web 聊天界面，支持工具调用和状态恢复功能。

## 功能特性

- 🤖 **智能对话**: 基于 OpenAI GPT 模型的智能对话
- 🔧 **工具调用**: 支持工具调用和用户确认机制
- 💾 **状态恢复**: 使用本地文件进行状态管理和恢复
- 🌐 **Web 界面**: 现代化的 Web 用户界面
- 🔄 **优雅关闭**: 支持优雅关闭和状态保存
- 📁 **文件存储**: 无需外部依赖，使用本地文件存储

## 环境要求

- Go 1.21+
- OpenAI API Key
- 本地文件系统（用于checkpoint存储）

## 快速开始

### 1. 设置环境变量

```bash
export OPENAI_API_KEY="your-openai-api-key"
export OPENAI_MODEL="gpt-3.5-turbo"  # 可选，默认为 gpt-3.5-turbo
export OPENAI_BASE_URL="https://api.openai.com/v1"  # 可选
export CHECKPOINT_DIR="./checkpoints"  # 可选，默认为 ./checkpoints
export PORT="8080"  # 可选，默认为 8080
```

### 2. 启动 Web 服务器

```bash
# 使用启动脚本（推荐）
./start_web.sh

# 或者手动启动
go run .
```

### 3. 打开浏览器

访问 `http://localhost:8080` 开始聊天！

## 使用说明

### 基本对话

1. 在输入框中输入你的消息
2. 点击"发送"按钮或按回车键
3. 等待助手回复

### 工具调用

当助手需要使用工具时（如预订机票），会显示工具调用信息：

1. 系统会显示将要调用的工具和参数
2. 点击"确认"继续执行工具调用
3. 点击"取消"停止工具调用

### 状态恢复

- 所有对话状态都保存在本地文件中
- 使用固定的对话 ID "1"
- 支持应用重启后恢复对话状态
- 支持优雅关闭时保存状态
- checkpoint文件存储在 `./checkpoints/` 目录中

## API 接口

### POST /chat

发送聊天消息

```json
{
  "message": "用户消息",
  "conversation_id": "1"
}
```

响应：

```json
{
  "response": "助手回复",
  "tool_call": {
    "function": {
      "name": "工具名称",
      "arguments": "工具参数"
    }
  },
  "error": "错误信息"
}
```

### POST /continue

继续对话（确认工具调用后）

```json
{
  "conversation_id": "1"
}
```

## 技术架构

### 前端

- 纯 HTML/CSS/JavaScript
- 响应式设计
- 实时消息显示
- 工具调用确认界面

### 后端

- Go HTTP 服务器
- Eino 框架集成
- 本地文件状态存储
- 优雅关闭处理

### 状态管理

- 使用 `GracefulShutdownStoreManager` 管理状态
- 支持内存缓存和本地文件持久化
- 自动状态恢复和保存
- 原子性文件操作确保数据安全

## 配置选项

| 环境变量 | 默认值 | 说明 |
|---------|--------|------|
| `OPENAI_API_KEY` | - | OpenAI API 密钥（必需） |
| `OPENAI_MODEL` | `gpt-3.5-turbo` | OpenAI 模型名称 |
| `OPENAI_BASE_URL` | `https://api.openai.com/v1` | OpenAI API 基础 URL |
| `CHECKPOINT_DIR` | `./checkpoints` | Checkpoint 文件存储目录 |
| `PORT` | `8080` | Web 服务器端口 |

## 故障排除

### 文件权限错误

```
Error: failed to create checkpoint directory
```

解决方案：
1. 确保应用有权限创建和写入 checkpoint 目录
2. 检查 `CHECKPOINT_DIR` 环境变量指向的路径
3. 手动创建目录：`mkdir -p ./checkpoints`

### OpenAI API 错误

```
Error: OpenAI API error
```

解决方案：
1. 检查 `OPENAI_API_KEY` 是否正确设置
2. 检查 API 密钥是否有足够的配额
3. 检查网络连接

### 端口占用

```
Error: port already in use
```

解决方案：
1. 更改 `PORT` 环境变量
2. 或者停止占用端口的其他服务

## 开发说明

### 项目结构

```
.
├── main.go                    # 主程序文件
├── file_checkpoint.go         # 文件 checkpoint 存储
├── templates/
│   └── index.html            # Web 界面模板
├── checkpoints/              # Checkpoint 文件存储目录
├── start_web.sh              # 启动脚本
└── WEB_README.md             # 说明文档
```

### 添加新工具

1. 在 `getTools()` 函数中添加新工具
2. 更新工具描述和参数
3. 重新编译和启动服务

### 自定义界面

1. 修改 `templates/index.html` 文件
2. 调整 CSS 样式
3. 添加新的 JavaScript 功能

## 许可证

Apache License 2.0
