# ChatModel 示例：Agent 中断与恢复机制

## 概述

本示例演示了如何使用 Eino ADK 实现 **Agent 的人工审核中断与状态恢复** 功能。

## 核心功能

### 1. 初始化与查询 (35-43行)

```go
a := subagents.NewBookRecommendAgent()
store := newInMemoryStore()
runner := adk.NewRunner(ctx, adk.RunnerConfig{
    EnableStreaming: true,           // 启用流式输出
    Agent:           a,
    CheckPointStore: store,          // 配置 checkpoint 存储
})
iter := runner.Query(ctx, "recommend a book to me", adk.WithCheckPointID("1"))
```

### 2. 首次执行与中断检测 (44-73行)

迭代处理事件流，检测中断事件：

```go
for {
    event, ok := iter.Next()
    if !ok { break }
    
    // 检测中断事件
    if event.Action != nil && event.Action.Interrupted != nil {
        hasInterrupt = true
    }
}
```

**关键点**：
- 迭代器完成后，checkpoint 已自动保存
- 提示词控制不可靠，重要操作必须在工作流中强制中断

### 3. 中断判断 (76-82行)

```go
if !hasInterrupt {
    // 没有中断 = Agent 正常完成，无需用户输入
    return
}
// 有中断 = checkpoint 已自动保存，等待用户输入
```

### 4. CheckpointId 切换策略 (90-100行)

```go
// 从 checkpoint "1" 读取状态
data, ok, errGet := store.Get(ctx, "1")

// 复制到新的 checkpoint "2"
store.Set(ctx, "2", data)
```

**设计优势**：
- 保留原始 checkpoint "1"（可回溯）
- 新会话用 "2" 继续，互不干扰
- 支持从同一中断点创建多个恢复分支

### 5. 用户输入传递机制 (102行)

```go
iter, err := runner.Resume(ctx, "2", adk.WithToolOptions([]tool.Option{
    subagents.WithNewInput(nInput)  // 通过 tool.WrapImplSpecificOptFn 注入
}))
```

**实现原理**（参考 `ask_for_clarification.go:33`）：

```go
func WithNewInput(input string) tool.Option {
    return tool.WrapImplSpecificOptFn(func(t *askForClarificationOptions) {
        t.newInput = input
    })
}
```

- 使用 `tool.WrapImplSpecificOptFn` 封装用户输入
- 将输入作为工具的特定选项注入
- Resume 时，工具调用能获取到新输入并继续执行

### 6. 恢复执行 (106-117行)

从 checkpoint 恢复后继续执行，处理剩余流程。

## 完整执行流程

```
┌─────────────────────────────────────────────┐
│ 1. Query(checkpointId="1")                  │
│    发起查询："recommend a book to me"        │
└─────────────┬───────────────────────────────┘
              │
              ▼
┌─────────────────────────────────────────────┐
│ 2. Agent 执行 → 触发中断                     │
│    checkpoint 自动保存到 "1"                 │
└─────────────┬───────────────────────────────┘
              │
              ▼
┌─────────────────────────────────────────────┐
│ 3. 等待用户输入                              │
│    scanner.Scan() → nInput                  │
└─────────────┬───────────────────────────────┘
              │
              ▼
┌─────────────────────────────────────────────┐
│ 4. CheckpointId 切换                         │
│    Get("1") → Set("2", data)                │
└─────────────┬───────────────────────────────┘
              │
              ▼
┌─────────────────────────────────────────────┐
│ 5. Resume(checkpointId="2")                 │
│    WithToolOptions(WithNewInput(nInput))    │
└─────────────┬───────────────────────────────┘
              │
              ▼
┌─────────────────────────────────────────────┐
│ 6. 继续执行并完成                            │
└─────────────────────────────────────────────┘
```

## Checkpoint 存储实现 (120-143行)

简单的内存实现：

```go
type inMemoryStore struct {
    mu  sync.RWMutex
    mem map[string][]byte
}
```

生产环境建议使用：
- Redis
- 数据库
- 分布式存储

## 核心要点

1. **中断自动触发**：Agent 需要用户确认时自动中断
2. **Checkpoint 自动保存**：迭代器完成时已完成保存
3. **提示词不可靠**：重要操作的中断必须在工作流中强制定义
4. **CheckpointId 隔离**：通过切换 ID 实现会话分支管理
5. **用户输入注入**：通过 `tool.WrapImplSpecificOptFn` 优雅传递

## 适用场景

- 敏感操作需要人工审核（删除、支付等）
- 需要用户补充信息才能继续
- 长流程中的关键决策点
- 多轮对话中的上下文管理

