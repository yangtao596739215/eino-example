# Parallel 设计原理

## 概述

Eino ADK 的 **Parallel Workflow** 实现了多个 sub-agent 的并发执行机制。通过 goroutine 并发模型、独立的 `runContext` 隔离、以及灵活的中断恢复策略，Parallel 模式在保证高性能的同时，确保了每个并行分支的执行独立性和可恢复性。

## 核心设计理念

### 1. 并发执行模型

```
                    ┌─────────────────────────┐
                    │  workflowAgent.Run()   │
                    │    (mode=Parallel)      │
                    └──────────┬──────────────┘
                               │
                               v
                    ┌─────────────────────────┐
                    │   runParallel()         │
                    └──────────┬──────────────┘
                               │
                ┌──────────────┼──────────────┐
                │              │              │
                v              v              v
         ┌──────────┐   ┌──────────┐   ┌──────────┐
         │ Agent A  │   │ Agent B  │   │ Agent C  │
         │(goroutine│   │(goroutine│   │(main     │
         │  #1)     │   │  #2)     │   │ goroutine│
         └──────────┘   └──────────┘   └──────────┘
              │              │              │
              └──────────────┴──────────────┘
                           │
                           v
                  ┌────────────────┐
                  │  sync.WaitGroup │
                  │  等待所有完成    │
                  └────────────────┘
```

**设计要点**：
- 第一个 sub-agent 在主 goroutine 执行
- 其余 sub-agent 各自启动独立的 goroutine
- 使用 `sync.WaitGroup` 等待所有并行任务完成
- 所有事件通过共享的 `generator` 转发

### 2. runContext 隔离策略

**关键问题**：多个 goroutine 并发执行，如何避免 `runContext` 冲突？

**解决方案**：每个 sub-agent 通过 `initRunCtx()` 获得独立的 `runContext`

```go
// workflow.go:358-360
for _, subAgent := range subAgents {
    sa := subAgent
    ret = append(ret, func(ctx context.Context) *AsyncIterator[*AgentEvent] {
        return sa.Run(ctx, input, opts...)  // ← 每个 sa.Run 内部会调用 initRunCtx
    })
}

// runctx.go:241-254
func initRunCtx(ctx context.Context, agentName string, input *AgentInput) (context.Context, *runContext) {
    runCtx := getRunCtx(ctx)
    if runCtx != nil {
        runCtx = runCtx.deepCopy()  // ← 深拷贝，确保隔离
    } else {
        runCtx = &runContext{Session: newRunSession()}
    }
    
    runCtx.RunPath = append(runCtx.RunPath, RunStep{agentName})  // ← 追加当前 agent 名称
    if runCtx.isRoot() {
        runCtx.RootInput = input
    }
    
    return setRunCtx(ctx, runCtx), runCtx  // ← 返回新的 context
}
```

**隔离机制**：
1. **深拷贝**：`runCtx.deepCopy()` 为每个并行分支创建独立的 `runContext` 副本
2. **独立 RunPath**：每个分支的 `RunPath` 只包含自己的名称（外加父 workflow agent）
3. **无合并**：并行分支之间的 `runContext` **完全独立**，执行完成后**不进行合并**

**RunPath 示例**（假设 ParallelAgent 名称为 "Parallel"，subAgents = [A, B, C]）：

```
初始 ctx:
  runCtx.RunPath = [Parallel]

并行执行时：
  Branch A: runCtx.RunPath = [Parallel, A]  ← 独立副本 1
  Branch B: runCtx.RunPath = [Parallel, B]  ← 独立副本 2
  Branch C: runCtx.RunPath = [Parallel, C]  ← 独立副本 3

执行后：
  ✓ 每个分支的 RunPath 独立维护
  ✗ 不存在合并操作
  → 各分支通过 AgentEvent.RunPath 向外传递路径信息
```

### 3. 无合并设计的合理性

**为什么不合并 `runContext`？**

| 维度 | 原因 |
|------|------|
| **语义独立性** | 并行分支代表独立的执行路径，合并 RunPath 会破坏这种独立性 |
| **信息完整性** | 每个 `AgentEvent` 携带自己的 `RunPath`，已足够追溯来源 |
| **并发安全** | 避免多个 goroutine 同时写入共享的 `runContext`，简化同步逻辑 |
| **中断恢复** | 通过 `interruptMap` 分别保存各分支的中断信息，恢复时各自独立 |

## 实现细节

### 1. 并行执行核心逻辑

```go
// workflow.go:271-349
func (a *workflowAgent) runParallel(ctx context.Context, input *AgentInput,
    generator *AsyncGenerator[*AgentEvent], intInfo *WorkflowInterruptInfo, opts ...AgentRunOption) {
    
    if len(a.subAgents) == 0 {
        return
    }
    
    // 1️⃣ 获取所有 runner 函数（初始运行 or 恢复运行）
    runners := getRunners(a.subAgents, input, intInfo, opts...)
    
    var wg sync.WaitGroup
    interruptMap := make(map[int]*InterruptInfo)  // ← 记录各分支的中断信息
    var mu sync.Mutex
    
    // 2️⃣ 启动 goroutine 执行后续 sub-agents（索引 1 到 n-1）
    if len(runners) > 1 {
        for i := 1; i < len(runners); i++ {
            wg.Add(1)
            go func(idx int, runner func(ctx context.Context) *AsyncIterator[*AgentEvent]) {
                defer func() {
                    // panic 恢复
                    panicErr := recover()
                    if panicErr != nil {
                        e := safe.NewPanicErr(panicErr, debug.Stack())
                        generator.Send(&AgentEvent{Err: e})
                    }
                    wg.Done()
                }()
                
                iterator := runner(ctx)  // ← 使用相同的初始 ctx，但内部会 deepCopy
                for {
                    event, ok := iterator.Next()
                    if !ok {
                        break
                    }
                    // 检查是否被中断
                    if event.Action != nil && event.Action.Interrupted != nil {
                        mu.Lock()
                        interruptMap[idx] = event.Action.Interrupted  // ← 记录中断信息
                        mu.Unlock()
                        break
                    }
                    // 转发事件
                    generator.Send(event)
                }
            }(i, runners[i])
        }
    }
    
    // 3️⃣ 主 goroutine 执行第一个 sub-agent
    runner := runners[0]
    iterator := runner(ctx)
    for {
        event, ok := iterator.Next()
        if !ok {
            break
        }
        if event.Action != nil && event.Action.Interrupted != nil {
            mu.Lock()
            interruptMap[0] = event.Action.Interrupted
            mu.Unlock()
            break
        }
        generator.Send(event)
    }
    
    // 4️⃣ 等待所有并行任务完成
    if len(a.subAgents) > 1 {
        wg.Wait()
    }
    
    // 5️⃣ 如果有中断，生成 Parallel 层级的中断事件
    if len(interruptMap) > 0 {
        replaceInterruptRunCtx(ctx, getRunCtx(ctx))
        generator.Send(&AgentEvent{
            AgentName: a.Name(ctx),
            RunPath:   getRunCtx(ctx).RunPath,
            Action: &AgentAction{
                Interrupted: &InterruptInfo{
                    Data: &WorkflowInterruptInfo{
                        OrigInput:             input,
                        ParallelInterruptInfo: interruptMap,  // ← 保存所有分支的中断信息
                    },
                },
            },
        })
    }
}
```

**执行流程图**：

```
                  ┌────────────────────┐
                  │  runParallel()     │
                  └─────────┬──────────┘
                            │
                  ┌─────────▼──────────┐
                  │ getRunners()       │  获取 runner 函数列表
                  └─────────┬──────────┘
                            │
          ┌─────────────────┼─────────────────┐
          │                 │                 │
    ┌─────▼─────┐     ┌─────▼─────┐    ┌─────▼─────┐
    │ goroutine │     │ goroutine │    │   main    │
    │   (A)     │     │   (B)     │    │ goroutine │
    │           │     │           │    │   (C)     │
    │ runner(ctx)│    │ runner(ctx)│   │ runner(ctx)│
    │ ↓ initRunCtx   │ ↓ initRunCtx   │ ↓ initRunCtx
    │   deepCopy  │   │   deepCopy  │  │   deepCopy │
    │ RunPath=[A]│   │ RunPath=[B]│   │ RunPath=[C]│
    └─────┬─────┘     └─────┬─────┘    └─────┬─────┘
          │                 │                 │
          │   generator.Send(event)           │
          └─────────────────┼─────────────────┘
                            │
                  ┌─────────▼──────────┐
                  │   sync.WaitGroup   │  等待所有完成
                  └─────────┬──────────┘
                            │
                  ┌─────────▼──────────┐
                  │  检查 interruptMap  │
                  │  发送中断事件       │
                  └────────────────────┘
```

### 2. Runner 函数生成

`getRunners()` 负责为每个 sub-agent 生成执行函数，支持初始运行和恢复运行两种场景。

#### 场景 1：初始运行 (`intInfo == nil`)

```go
// workflow.go:354-362
if intInfo == nil {
    for _, subAgent := range subAgents {
        sa := subAgent
        ret = append(ret, func(ctx context.Context) *AsyncIterator[*AgentEvent] {
            return sa.Run(ctx, input, opts...)  // ← 正常启动
        })
    }
    return ret
}
```

**特点**：
- 所有 sub-agent 都会执行
- 每个 runner 闭包捕获对应的 `sa`（sub-agent）
- 调用 `sa.Run()` 时，内部会调用 `initRunCtx()` 创建独立的 `runContext`

#### 场景 2：恢复运行 (`intInfo != nil`)

```go
// workflow.go:364-382
for i, subAgent := range subAgents {
    sa := subAgent
    info, ok := intInfo.ParallelInterruptInfo[i]
    if !ok {
        // ← 该分支已执行完成，跳过
        continue
    }
    // ← 该分支需要恢复
    ret = append(ret, func(ctx context.Context) *AsyncIterator[*AgentEvent] {
        nCtx, runCtx := initRunCtx(ctx, sa.Name(ctx), input)  // ← 创建新的 runContext
        enableStreaming := false
        if runCtx.RootInput != nil {
            enableStreaming = runCtx.RootInput.EnableStreaming
        }
        return sa.Resume(nCtx, &ResumeInfo{
            EnableStreaming: enableStreaming,
            InterruptInfo:   info,  // ← 传入该分支的中断信息
        }, opts...)
    })
}
return ret
```

**恢复逻辑**：
1. 遍历 `intInfo.ParallelInterruptInfo`（`map[int]*InterruptInfo`）
2. 索引 `i` 对应 `subAgents[i]`
3. 如果 `interruptMap[i]` 存在 → 该分支被中断 → 需要恢复
4. 如果 `interruptMap[i]` 不存在 → 该分支已完成 → 跳过

**示例**：

```
初始状态:
  subAgents = [A, B, C]

中断时:
  A 完成，B 被中断，C 被中断
  → interruptMap = {
      1: &InterruptInfo{...},  // B 的中断信息
      2: &InterruptInfo{...},  // C 的中断信息
  }

恢复时:
  getRunners() 返回:
    [
      runner_for_B,  // 索引 0 → 恢复 B
      runner_for_C,  // 索引 1 → 恢复 C
    ]
  ✓ A 已完成，不再生成 runner
```

### 3. 中断信息结构

```go
// workflow.go:342-346
&WorkflowInterruptInfo{
    OrigInput:             input,                // 原始输入
    ParallelInterruptInfo: interruptMap,         // map[int]*InterruptInfo
}

// interruptMap 的结构:
// map[int]*InterruptInfo = {
//   0: &InterruptInfo{Data: ...},  // 第 1 个 sub-agent 的中断信息
//   1: &InterruptInfo{Data: ...},  // 第 2 个 sub-agent 的中断信息
//   2: &InterruptInfo{Data: ...},  // 第 3 个 sub-agent 的中断信息
//   ...
// }
```

**设计要点**：
- 使用 `map[int]*InterruptInfo` 而非数组，因为完成的分支不需要保存中断信息
- 索引对应 `subAgents` 的位置
- 每个分支的 `InterruptInfo` 独立保存

### 4. 事件转发机制

```
┌──────────────────────────────────────────────────┐
│           Parallel Workflow                      │
│                                                  │
│  ┌───────────┐   ┌───────────┐   ┌───────────┐ │
│  │Agent A    │   │Agent B    │   │Agent C    │ │
│  │(goroutine)│   │(goroutine)│   │(main)     │ │
│  └─────┬─────┘   └─────┬─────┘   └─────┬─────┘ │
│        │               │               │        │
│        └───────────────┼───────────────┘        │
│                        │                        │
│                        v                        │
│              ┌─────────────────┐                │
│              │ AsyncGenerator  │ ← 所有分支共享 │
│              │   (线程安全)     │                │
│              └────────┬────────┘                │
└───────────────────────┼─────────────────────────┘
                        │
                        v
                ┌───────────────┐
                │ 外部迭代器     │
                │ (消费事件)     │
                └───────────────┘
```

**关键代码**：

```go
// workflow.go:308
generator.Send(event)  // ← 所有 goroutine 都向同一个 generator 发送事件
```

**线程安全性**：
- `AsyncGenerator` 内部实现了线程安全的事件队列
- 多个 goroutine 可以安全地并发调用 `generator.Send()`

## 核心设计原则

### 1. 隔离性（Isolation）

| 维度 | 实现机制 |
|------|----------|
| **上下文隔离** | 每个 sub-agent 通过 `initRunCtx()` 获得独立的 `runContext` 副本 |
| **执行隔离** | 每个 sub-agent 在独立的 goroutine 执行（除第一个） |
| **中断隔离** | 每个分支的中断信息独立保存在 `interruptMap[i]` |

### 2. 无状态性（Stateless）

- Parallel Workflow 本身不维护分支间的共享状态
- 所有状态通过 `AgentEvent` 流向外部
- 恢复时仅依赖 `WorkflowInterruptInfo.ParallelInterruptInfo`

### 3. 可恢复性（Resumability）

**中断场景**：
- 任一分支触发中断 → 该分支的 `InterruptInfo` 记录到 `interruptMap`
- 其他分支继续执行或也触发中断
- 最终 Parallel Workflow 生成统一的中断事件

**恢复策略**：
- 遍历 `interruptMap`，仅恢复未完成的分支
- 已完成的分支不再执行（通过 `ok := intInfo.ParallelInterruptInfo[i]` 判断）

## 与 Sequential/Loop 的对比

| 维度 | Sequential | Loop | Parallel |
|------|-----------|------|----------|
| **执行模式** | 顺序执行一次 | 顺序执行 N 次 | 并发执行一次 |
| **底层实现** | `runSequential(iterations=0)` | `runLoop() → runSequential(iterations)` | `runParallel()` |
| **RunPath 构建** | 逐步追加 | 预构建历史路径 | 独立分支路径 |
| **中断信息** | `SequentialInterruptIndex` | `LoopIterations` + `SequentialInterruptIndex` | `ParallelInterruptInfo` (map) |
| **runContext 隔离** | 共享（顺序执行） | 共享（顺序执行） | 独立（并发执行） |
| **并发安全** | 不涉及 | 不涉及 | 通过 deepCopy + mutex 保证 |

## 典型应用场景

### 1. 多专家并行咨询

```
用户问题: "帮我规划一次日本旅行"

         ┌─────────────────────┐
         │  Parallel Workflow  │
         └──────────┬──────────┘
                    │
      ┌─────────────┼─────────────┐
      │             │             │
      v             v             v
┌──────────┐  ┌──────────┐  ┌──────────┐
│ 交通专家  │  │ 住宿专家  │  │ 美食专家  │
└──────────┘  └──────────┘  └──────────┘
      │             │             │
      └─────────────┼─────────────┘
                    v
           汇总所有建议后返回
```

### 2. 并行数据处理

```
输入: 大量文档

         ┌─────────────────────┐
         │  Parallel Workflow  │
         └──────────┬──────────┘
                    │
      ┌─────────────┼─────────────┐
      │             │             │
      v             v             v
┌──────────┐  ┌──────────┐  ┌──────────┐
│文档解析器1│  │文档解析器2│  │文档解析器3│
└──────────┘  └──────────┘  └──────────┘
```

### 3. 多模型对比

```
用户问题: "解释量子纠缠"

         ┌─────────────────────┐
         │  Parallel Workflow  │
         └──────────┬──────────┘
                    │
      ┌─────────────┼─────────────┐
      │             │             │
      v             v             v
┌──────────┐  ┌──────────┐  ┌──────────┐
│  GPT-4   │  │ Claude   │  │  Gemini  │
└──────────┘  └──────────┘  └──────────┘
```

## 关键代码位置

| 功能 | 文件 | 函数/行号 |
|------|------|-----------|
| 并行执行核心 | `workflow.go` | `runParallel()` L271-349 |
| Runner 生成 | `workflow.go` | `getRunners()` L352-385 |
| runContext 初始化 | `runctx.go` | `initRunCtx()` L241-254 |
| runContext 深拷贝 | `runctx.go` | `deepCopy()` L220-228 |
| 中断信息结构 | `workflow.go` | `WorkflowInterruptInfo` |

## 总结

Eino ADK 的 Parallel Workflow 通过以下设计实现了高效、安全、可恢复的并行执行机制：

1. **goroutine 并发模型**：充分利用 Go 的并发特性，提升执行效率
2. **runContext 深拷贝隔离**：确保每个并行分支的执行独立性，避免状态冲突
3. **无合并设计**：简化并发逻辑，通过 `AgentEvent.RunPath` 传递路径信息
4. **分支级中断恢复**：通过 `interruptMap` 精确记录和恢复每个分支的状态
5. **线程安全的事件转发**：`AsyncGenerator` 保证多 goroutine 安全发送事件

这套设计在保证高性能的同时，维持了良好的可维护性和可扩展性，是多 Agent 并行协作场景的理想解决方案。

