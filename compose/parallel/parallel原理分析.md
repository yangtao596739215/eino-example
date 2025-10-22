# Compose 层 Parallel 原理分析

## 概述

Compose 层的 Parallel 节点是 Eino 框架中用于实现并行处理的核心组件。它允许在同一个执行阶段中并发运行多个节点，通过共享输入、独立输出、状态协调等机制，实现高效的多任务并行处理。

## 核心设计理念

### 1. 并行处理模型

```
                    ┌─────────────────────────┐
                    │    Parallel Node        │
                    └──────────┬──────────────┘
                               │
                ┌──────────────┼──────────────┐
                │              │              │
                v              v              v
        ┌──────────┐   ┌──────────┐   ┌──────────┐
        │ Node A    │   │ Node B    │   │ Node C    │
        │(goroutine │   │(goroutine │   │(goroutine │
        │  #1)      │   │  #2)      │   │  #3)      │
        └──────────┘   └──────────┘   └──────────┘
              │              │              │
              └──────────────┼──────────────┘
                             │
                    ┌────────────────┐
                    │ 结果汇总        │
                    │ (键值对Map)     │
                    └────────────────┘
```

**设计要点**：
- 所有并行节点接收相同的输入
- 每个节点在独立的执行上下文中运行
- 每个并行节点可以返回不同类型的数据（如 string、int、map[string]any、自定义结构体等）
- Parallel 节点的整体输出**一定是** `map[string]any` 类型
- 框架自动将各节点的输出通过 `outputKey` 收集到统一的 Map 中
- 后续节点使用时需要进行类型断言来获取具体类型的值

### 2. 状态管理策略

#### 2.1 输入状态：只读共享

```go
// 所有并行节点都接收相同的输入
func(ctx context.Context, kvs map[string]any) (string, error) {
    // kvs 是共享的只读输入
    role, ok := kvs["role"].(string)
    return role, nil
}
```

**特点**：
- 输入状态对所有并行节点可见
- 节点只能读取，不能修改输入状态
- 避免了输入阶段的数据竞争

#### 2.2 执行状态：独立处理

```go
// 每个节点独立处理
parallel.AddLambda("role", compose.InvokableLambda(func(ctx context.Context, kvs map[string]any) (string, error) {
    // 独立处理逻辑
    role := kvs["role"].(string)
    return processRole(role), nil
}))
```

**特点**：
- 每个节点有独立的执行上下文
- 节点间不共享执行状态
- 支持并发安全的数据处理

#### 2.3 输出状态：键值对收集

```go
// 最终输出格式
output := map[string]any{
    "role":   "bird",
    "input":  "你的叫声是怎样的？",
    "result": "chirp chirp",
}
```

**特点**：
- 每个节点通过 `outputKey` 标识输出
- 框架自动收集所有输出结果
- 支持类型安全的输出管理

## 并发安全机制

### 1. 无共享状态设计

**核心原则**：Parallel 节点采用"无共享状态"的设计，避免并发修改冲突。

| 维度 | 实现方式 | 并发安全性 |
|------|----------|------------|
| **输入状态** | 只读共享 | ✅ 安全（只读操作） |
| **执行状态** | 独立处理 | ✅ 安全（无共享） |
| **输出状态** | 键值对收集 | ✅ 安全（独立键） |

### 2. 状态协调机制

#### 2.1 ProcessState 原子操作

```go
// 通过 ProcessState 实现状态协调
err := compose.ProcessState[TravelState](ctx, func(_ context.Context, state *TravelState) error {
    // 原子性地访问和修改状态
    state.ExpertCount++
    state.TransportationAdvice = advice
    return nil
})
```

**特点**：
- 原子操作保证状态一致性
- 支持复杂的状态协调逻辑
- 自动处理并发访问同步

#### 2.2 状态依赖管理

```go
// 状态依赖示例
err := compose.ProcessState[TravelState](ctx, func(_ context.Context, state *TravelState) error {
    // 等待所有专家完成
    if state.ExpertCount < 4 {
        return fmt.Errorf("等待专家完成")
    }
    
    // 基于所有专家的建议进行协调
    if state.TransportationAdvice != "" && state.AccommodationAdvice != "" {
        state.CoordinationComplete = true
    }
    
    return nil
})
```

**特点**：
- 支持状态依赖检查
- 实现复杂的协调逻辑
- 保证状态一致性

### 3. 线程安全保证

#### 3.1 输出键唯一性

```go
// Parallel 结构体确保输出键唯一性
type Parallel struct {
    nodes      []nodeOptionsPair
    outputKeys map[string]bool  // 确保键唯一性
    err        error
}

func (p *Parallel) addNode(outputKey string, node *graphNode, options *graphAddNodeOpts) *Parallel {
    // 检查输出键唯一性
    if _, ok := p.outputKeys[outputKey]; ok {
        p.err = fmt.Errorf("parallel add node err, duplicate output key= %s", outputKey)
        return p
    }
    
    node.nodeInfo.outputKey = outputKey
    p.nodes = append(p.nodes, nodeOptionsPair{node, options})
    p.outputKeys[outputKey] = true
    return p
}
```

#### 3.2 并发执行安全

```go
// 并行执行时，每个节点独立运行
for _, node := range parallel.nodes {
    go func(n *graphNode) {
        // 独立执行，无共享状态
        result := n.execute(ctx, input)
        // 通过 outputKey 标识输出
        output[n.nodeInfo.outputKey] = result
    }(node)
}
```

## 实现原理详解

### 1. Parallel 节点结构

```go
type Parallel struct {
    nodes      []nodeOptionsPair  // 存储所有并行节点
    outputKeys map[string]bool   // 输出键映射，确保唯一性
    err        error             // 错误信息
}

type nodeOptionsPair struct {
    node    *graphNode
    options *graphAddNodeOpts
}
```

### 2. 节点添加机制

```go
// 添加 Lambda 节点
func (p *Parallel) AddLambda(outputKey string, node *Lambda, opts ...GraphAddNodeOpt) *Parallel {
    gNode, options := toLambdaNode(node, append(opts, WithOutputKey(outputKey))...)
    return p.addNode(outputKey, gNode, options)
}

// 添加 ChatModel 节点
func (p *Parallel) AddChatModel(outputKey string, node model.BaseChatModel, opts ...GraphAddNodeOpt) *Parallel {
    gNode, options := toChatModelNode(node, append(opts, WithOutputKey(outputKey))...)
    return p.addNode(outputKey, gNode, options)
}
```

### 3. 并行执行流程

```go
// 并行执行核心逻辑
func (p *Parallel) execute(ctx context.Context, input map[string]any) (map[string]any, error) {
    results := make(map[string]any)
    var wg sync.WaitGroup
    var mu sync.Mutex
    var err error
    
    // 启动所有并行节点
    for _, nodePair := range p.nodes {
        wg.Add(1)
        go func(node *graphNode, outputKey string) {
            defer wg.Done()
            
            // 独立执行节点
            result, nodeErr := node.execute(ctx, input)
            
            mu.Lock()
            if nodeErr != nil {
                err = nodeErr
            } else {
                results[outputKey] = result
            }
            mu.Unlock()
        }(nodePair.node, nodePair.node.nodeInfo.outputKey)
    }
    
    // 等待所有节点完成
    wg.Wait()
    
    if err != nil {
        return nil, err
    }
    
    return results, nil
}
```

## 设计哲学

### 1. 分离关注点（Separation of Concerns）

- **输入处理**：专注于数据读取和验证
- **并行执行**：专注于业务逻辑处理
- **结果汇总**：专注于数据整合和输出

### 2. 最小化共享状态（Minimize Shared State）

- 避免节点间的直接状态共享
- 通过 ProcessState 实现必要的状态协调
- 通过输出键实现结果隔离

### 3. 最大化并发性能（Maximize Concurrency）

- 所有节点并发执行，无依赖关系
- 最小化同步开销
- 支持任意数量的并行节点

### 4. 类型安全（Type Safety）

- 编译时检查节点间类型匹配
- 运行时验证输出键唯一性
- 支持泛型类型系统

## 实际应用场景

### 1. 多专家协作

```go
// 旅游规划多专家并行处理
parallel := compose.NewParallel()
parallel.AddLambda("transportation", transportationExpert)
parallel.AddLambda("accommodation", accommodationExpert)
parallel.AddLambda("food", foodExpert)
parallel.AddLambda("attraction", attractionExpert)
```

### 2. 并行数据处理

```go
// 文档并行处理
parallel := compose.NewParallel()
parallel.AddLambda("parse", documentParser)
parallel.AddLambda("extract", contentExtractor)
parallel.AddLambda("analyze", textAnalyzer)
```

### 3. 多模型对比

```go
// 多模型并行推理
parallel := compose.NewParallel()
parallel.AddChatModel("gpt4", gpt4Model)
parallel.AddChatModel("claude", claudeModel)
parallel.AddChatModel("gemini", geminiModel)
```

## 最佳实践

### 1. 合理设计输出键

```go
// ✅ 好的设计：语义清晰的输出键
parallel.AddLambda("transportation_advice", transportationExpert)
parallel.AddLambda("accommodation_advice", accommodationExpert)

// ❌ 不好的设计：模糊的输出键
parallel.AddLambda("result1", expert1)
parallel.AddLambda("result2", expert2)
```

### 2. 状态协调策略

```go
// ✅ 好的设计：明确的状态依赖
err := compose.ProcessState[TravelState](ctx, func(_ context.Context, state *TravelState) error {
    if state.ExpertCount < 4 {
        return fmt.Errorf("等待所有专家完成")
    }
    // 进行协调处理
    return nil
})

// ❌ 不好的设计：隐式状态依赖
// 直接假设所有专家都已完成
```

### 3. 错误处理

```go
// ✅ 好的设计：完整的错误处理
parallel.AddLambda("expert", compose.InvokableLambda(func(ctx context.Context, input map[string]any) (string, error) {
    result, err := expertProcess(input)
    if err != nil {
        return "", fmt.Errorf("专家处理失败: %w", err)
    }
    return result, nil
}))
```

## 性能优化

### 1. 并发执行优化

- 所有节点并发执行，无阻塞等待
- 最小化同步开销
- 支持 CPU 密集型任务并行化

### 2. 内存使用优化

- 避免不必要的状态复制
- 通过引用共享输入数据
- 及时释放临时资源

### 3. 错误处理优化

- 快速失败机制
- 错误信息聚合
- 支持部分成功场景

## 总结

Compose 层的 Parallel 节点通过以下设计实现了高效、安全、灵活的并行处理：

1. **无共享状态设计**：避免并发修改冲突，保证线程安全
2. **ProcessState 协调机制**：支持复杂的状态协调和依赖管理
3. **键值对输出模式**：实现类型安全的结果收集和管理
4. **分离关注点**：清晰的职责分离，便于维护和扩展

这种设计既保证了并发安全性，又提供了强大的状态协调能力，是构建复杂并行处理系统的理想选择。
