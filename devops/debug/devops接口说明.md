# Eino DevOps 接口说明

## 📖 简介

Eino DevOps 提供了一套完整的调试和监控接口，用于在开发阶段调试 Graph、Chain 和 StateGraph 等组件。通过这些接口，您可以：

- 查看已注册的 Graph 列表
- 获取 Graph 的画布信息（拓扑结构）
- 创建调试会话（Thread）
- 执行调试运行并实时查看节点状态
- 流式查看服务日志
- 查看支持的输入类型

## 🚀 服务启动

### 启动示例代码

```go
package main

import (
    "context"
    "github.com/cloudwego/eino-ext/devops"
)

func main() {
    ctx := context.Background()
    
    // 初始化 DevOps 服务器，默认端口会自动分配
    err := devops.Init(ctx)
    if err != nil {
        panic(err)
    }
    
    // 注册你的 Graph/Chain/StateGraph
    // chain.RegisterSimpleChain(ctx)
    // graph.RegisterSimpleGraph(ctx)
    
    // 保持服务运行...
}
```

### 服务配置

服务启动后会自动分配端口（默认范围：50000-60000），并在日志中输出：

```
[eino devops][INFO] start debug http server at port=52538
```

## 🌐 基础接口

所有接口的基础路径为：`http://localhost:{port}/eino/devops`

### 1. Ping - 健康检查

**接口地址：** `GET /eino/devops/ping`

**功能：** 检查服务是否正常运行

**响应示例：**
```json
{
  "code": 0,
  "msg": "success",
  "data": "pong"
}
```

**curl 示例：**
```bash
curl http://localhost:52538/eino/devops/ping
```

---

### 2. Version - 获取版本信息

**接口地址：** `GET /eino/devops/version`

**功能：** 获取 DevOps 服务的版本号

**响应示例：**
```json
{
  "code": 0,
  "msg": "success",
  "data": "0.1.7"
}
```

**curl 示例：**
```bash
curl http://localhost:52538/eino/devops/version
```

---

### 3. StreamLog - 流式日志

**接口地址：** `GET /eino/devops/stream_log`

**功能：** 通过 Server-Sent Events (SSE) 实时推送服务日志

**响应格式：** `text/event-stream`

**使用场景：** 实时监控服务运行状态和调试信息

**curl 示例：**
```bash
curl -N http://localhost:52538/eino/devops/stream_log
```

---

## 🔍 Debug 接口

所有 Debug 接口的基础路径为：`http://localhost:{port}/eino/devops/debug/v1`

### 1. ListInputTypes - 列出支持的输入类型

**接口地址：** `GET /eino/devops/debug/v1/input_types`

**功能：** 获取所有已注册的 Go 类型的 JSON Schema 定义

**响应格式：**
```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "types": [
      {
        "type_name": "string",
        "schema": {...}
      },
      {
        "type_name": "MyCustomType",
        "schema": {...}
      }
    ]
  }
}
```

**使用场景：** 在构造调试输入时，查看可用的数据类型定义

**curl 示例：**
```bash
curl http://localhost:52538/eino/devops/debug/v1/input_types
```

---

### 2. ListGraphs - 列出所有 Graph

**接口地址：** `GET /eino/devops/debug/v1/graphs`

**功能：** 获取所有已注册的 Graph/Chain/StateGraph 列表

**响应格式：**
```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "graphs": [
      {
        "id": "CJQ1OC",
        "name": "chain.RegisterSimpleChain:42"
      },
      {
        "id": "sl6TJE",
        "name": "graph.RegisterSimpleGraph:58"
      },
      {
        "id": "MVLdhH",
        "name": "state_graph.RegisterSimpleStateGraph:70"
      }
    ]
  }
}
```

**字段说明：**
- `id`: Graph 的唯一标识符，用于后续接口调用
- `name`: Graph 的名称，格式为 `包名.注册函数名:行号`

**curl 示例：**
```bash
curl http://localhost:52538/eino/devops/debug/v1/graphs
```

---

### 3. GetCanvasInfo - 获取画布信息

**接口地址：** `GET /eino/devops/debug/v1/graphs/{graph_id}/canvas`

**功能：** 获取指定 Graph 的拓扑结构信息，包括节点、边和配置

**路径参数：**
- `graph_id`: Graph 的唯一标识符（从 ListGraphs 接口获取）

**响应格式：**
```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "canvas_info": {
      "nodes": [
        {
          "key": "node_1",
          "name": "Node 1",
          "type": "lambda"
        },
        {
          "key": "node_2",
          "name": "Node 2",
          "type": "lambda"
        }
      ],
      "edges": [
        {
          "source": "node_1",
          "target": "node_2"
        }
      ],
      "config": {
        "entry_point": "node_1",
        "end_points": ["node_2"]
      }
    }
  }
}
```

**使用场景：** 
- 可视化 Graph 的拓扑结构
- 了解节点间的连接关系
- 确定调试的起始节点

**curl 示例：**
```bash
# 使用从 ListGraphs 获取的 graph_id
curl http://localhost:52538/eino/devops/debug/v1/graphs/CJQ1OC/canvas
```

---

### 4. CreateDebugThread - 创建调试会话

**接口地址：** `POST /eino/devops/debug/v1/graphs/{graph_id}/threads`

**功能：** 为指定的 Graph 创建一个新的调试会话（Thread），用于执行调试运行

**路径参数：**
- `graph_id`: Graph 的唯一标识符

**响应格式：**
```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "thread_id": "thread_abc123xyz"
  }
}
```

**字段说明：**
- `thread_id`: 调试会话的唯一标识符，用于后续的调试运行

**使用场景：** 
- 在执行调试运行前，必须先创建一个 Thread
- 每个 Thread 都是独立的调试会话，可以并行执行多个调试

**curl 示例：**
```bash
curl -X POST http://localhost:52538/eino/devops/debug/v1/graphs/CJQ1OC/threads
```

---

### 5. StreamDebugRun - 执行调试运行

**接口地址：** `POST /eino/devops/debug/v1/graphs/{graph_id}/threads/{thread_id}/stream`

**功能：** 在指定的 Thread 中执行 Graph 调试，并通过 SSE 实时推送节点执行状态

**路径参数：**
- `graph_id`: Graph 的唯一标识符
- `thread_id`: 调试会话的唯一标识符（从 CreateDebugThread 接口获取）

**请求 Body：**
```json
{
  "from_node": "node_1",
  "input": "{\"key\":\"value\"}",
  "log_id": "debug_log_001"
}
```

**请求参数说明：**
- `from_node` (必填): 调试的起始节点 key
- `input` (必填): 输入数据，JSON 字符串格式
- `log_id` (可选): 日志标识符，用于追踪调试过程

**响应格式：** `text/event-stream` (SSE)

调试过程中会推送三种类型的事件：

#### 5.1 数据事件 (data)

每当节点执行完成时推送：

```
event: data
data: {
  "type": "data",
  "debug_id": "debug_xyz789",
  "content": {
    "node_key": "node_1",
    "input": "{\"key\":\"value\"}",
    "output": "{\"result\":\"processed\"}",
    "error": "",
    "error_type": "",
    "metrics": {
      "prompt_tokens": 100,
      "completion_tokens": 50,
      "invoke_time_ms": 1200,
      "completion_time_ms": 1150
    }
  }
}
```

**字段说明：**
- `debug_id`: 本次调试运行的唯一标识符
- `node_key`: 当前执行完成的节点
- `input`: 节点的输入数据（JSON 字符串）
- `output`: 节点的输出数据（JSON 字符串）
- `error`: 错误信息（如果有）
- `metrics`: 性能指标
  - `prompt_tokens`: 提示词 token 数（LLM 节点）
  - `completion_tokens`: 完成 token 数（LLM 节点）
  - `invoke_time_ms`: 节点执行总耗时（毫秒）
  - `completion_time_ms`: LLM 完成耗时（毫秒）

#### 5.2 完成事件 (finish)

调试运行完成时推送：

```
event: finish
data: {
  "type": "finish",
  "debug_id": "debug_xyz789"
}
```

#### 5.3 错误事件 (error)

发生错误时推送：

```
event: error
data: {
  "type": "error",
  "debug_id": "debug_xyz789",
  "error": "node execution failed: timeout"
}
```

**使用场景：**
- 单步调试 Graph 的执行过程
- 查看每个节点的输入输出
- 监控节点执行性能
- 定位执行错误

**curl 示例：**
```bash
# 创建 Thread
THREAD_ID=$(curl -s -X POST http://localhost:52538/eino/devops/debug/v1/graphs/CJQ1OC/threads | jq -r '.data.thread_id')

# 执行调试运行
curl -N -X POST \
  http://localhost:52538/eino/devops/debug/v1/graphs/CJQ1OC/threads/$THREAD_ID/stream \
  -H "Content-Type: application/json" \
  -d '{
    "from_node": "node_1",
    "input": "{\"message\":\"hello\"}",
    "log_id": "test_001"
  }'
```

---

## 📝 通用响应格式

所有非流式接口都返回统一的 JSON 格式：

### 成功响应

```json
{
  "code": 0,
  "msg": "success",
  "data": { /* 具体数据 */ }
}
```

### 错误响应

```json
{
  "code": 500,
  "msg": "Internal Server Error",
  "data": {
    "biz_code": 500,
    "biz_msg": "具体错误信息"
  }
}
```

**字段说明：**
- `code`: HTTP 状态码，0 表示成功
- `msg`: 响应消息
- `data`: 响应数据或错误详情
  - `biz_code`: 业务错误码
  - `biz_msg`: 业务错误详情

---

## 🔄 完整调试流程示例

### 1. 查看可用的 Graph

```bash
curl http://localhost:52538/eino/devops/debug/v1/graphs
```

### 2. 获取 Graph 的画布信息

```bash
curl http://localhost:52538/eino/devops/debug/v1/graphs/CJQ1OC/canvas
```

### 3. 创建调试会话

```bash
curl -X POST http://localhost:52538/eino/devops/debug/v1/graphs/CJQ1OC/threads
```

响应示例：
```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "thread_id": "550e8400-e29b-41d4-a716-446655440000"
  }
}
```

### 4. 执行调试运行

```bash
curl -N -X POST \
  http://localhost:52538/eino/devops/debug/v1/graphs/CJQ1OC/threads/550e8400-e29b-41d4-a716-446655440000/stream \
  -H "Content-Type: application/json" \
  -d '{
    "from_node": "node_1",
    "input": "{\"text\":\"eino test\"}",
    "log_id": "debug_001"
  }'
```

### 5. 实时查看日志（可选）

在另一个终端中：

```bash
curl -N http://localhost:52538/eino/devops/stream_log
```

---

## 🛠️ 使用建议

### 1. 调试最佳实践

- **先查看画布信息**：执行调试前，先通过 `GetCanvasInfo` 了解 Graph 的结构
- **使用日志追踪**：为每次调试设置唯一的 `log_id`，便于后续分析
- **监控性能指标**：关注 `metrics` 中的执行时间，识别性能瓶颈
- **并行调试**：可以为同一个 Graph 创建多个 Thread，并行执行不同的调试场景

### 2. 常见问题排查

- **无法访问接口**：确认使用了完整的路径前缀 `/eino/devops`
- **Thread 不存在**：确保先调用 `CreateDebugThread` 创建会话
- **输入格式错误**：`input` 字段必须是 JSON 字符串格式，注意转义
- **节点 key 错误**：通过 `GetCanvasInfo` 确认正确的节点 key

### 3. CORS 支持

服务已配置 CORS 中间件，支持跨域请求：
- 允许所有来源 (`*`)
- 支持的方法：`GET`, `POST`, `PUT`, `DELETE`, `OPTIONS`
- 支持的请求头：`Content-Type`, `X-CSRF-Token`, `Authorization`

### 4. 并发限制

- SSE 流式连接最大并发数：10
- 超过限制会返回 400 错误：`too many connections`

---

## 📚 相关资源

- [Eino 官方文档](https://github.com/cloudwego/eino)
- [Eino DevOps 扩展](https://github.com/cloudwego/eino-ext)
- [Graph 设计原理](../intro/workflow/)
- [调试示例代码](./graph/graph.go)

---

## 📄 License

Copyright 2024 CloudWeGo Authors

Licensed under the Apache License, Version 2.0

