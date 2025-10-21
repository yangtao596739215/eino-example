# Parallel 并发输出顺序保证机制

## 核心问题

当多个 Agent 在不同的 goroutine 中并发执行时，它们都会向同一个 `AsyncGenerator` 发送事件。那么：

**问题 1**: 多个 goroutine 同时调用 `generator.Send()` 会不会冲突？  
**问题 2**: 事件的顺序是如何保证的？会不会乱序？  
**问题 3**: 如何区分不同 Agent 的输出？

## 答案总览

### 1. 线程安全保证 ✅

通过 **`UnboundedChan`** + **`sync.Mutex`** 实现线程安全，多个 goroutine 可以安全地并发调用 `Send()`。

### 2. 顺序保证机制 📋

**重要**: 并行模式下**不保证全局顺序**，但保证：
- ✅ 每个 Agent 内部的事件顺序是正确的
- ✅ 通过 `event.AgentName` 和 `event.RunPath` 可以识别事件来源
- ✅ 事件按照**先到先得**的原则被接收

### 3. 事件识别机制 🏷️

每个事件携带 `AgentName` 和 `RunPath`，外部消费者可以根据这些信息区分不同 Agent 的输出。

---

## 技术实现详解

### 1. AsyncGenerator 的线程安全实现

#### 数据结构

```go
// adk/utils.go:39-41
type AsyncGenerator[T any] struct {
    ch *internal.UnboundedChan[T]  // ← 底层使用 UnboundedChan
}

// internal/channel.go:22-27
type UnboundedChan[T any] struct {
    buffer   []T        // 内部缓冲区，存储事件
    mutex    sync.Mutex // 互斥锁，保护 buffer 的并发访问
    notEmpty *sync.Cond // 条件变量，用于通知有数据可用
    closed   bool       // 标记通道是否关闭
}
```

#### Send 方法（线程安全）

```go
// internal/channel.go:37-47
func (ch *UnboundedChan[T]) Send(value T) {
    ch.mutex.Lock()         // ← 1️⃣ 加锁，确保同一时间只有一个 goroutine 写入
    defer ch.mutex.Unlock()
    
    if ch.closed {
        panic("send on closed channel")
    }
    
    ch.buffer = append(ch.buffer, value)  // ← 2️⃣ 将事件追加到缓冲区
    ch.notEmpty.Signal()    // ← 3️⃣ 唤醒一个等待接收的 goroutine
}
```

**关键点**：
- `sync.Mutex` 确保 `append` 操作的原子性
- 即使多个 goroutine 同时调用 `Send()`，也会按照**获取锁的顺序**依次写入
- 使用 **无界缓冲区**（UnboundedChan），不会阻塞发送方

#### Receive 方法（阻塞式读取）

```go
// internal/channel.go:50-67
func (ch *UnboundedChan[T]) Receive() (T, bool) {
    ch.mutex.Lock()
    defer ch.mutex.Unlock()
    
    // 等待直到有数据或通道关闭
    for len(ch.buffer) == 0 && !ch.closed {
        ch.notEmpty.Wait()  // ← 阻塞等待，直到有 goroutine 调用 Signal()
    }
    
    if len(ch.buffer) == 0 {
        var zero T
        return zero, false  // 通道已关闭且无数据
    }
    
    val := ch.buffer[0]        // ← 取出第一个元素（FIFO）
    ch.buffer = ch.buffer[1:]  // ← 移除第一个元素
    return val, true
}
```

**关键点**：
- **FIFO（先进先出）**: 按照写入顺序读取事件
- 如果缓冲区为空，会阻塞等待，直到有新事件到来

---

### 2. 并发场景下的事件顺序

#### 场景示例

```
时间轴:
  T0: Agent A 发送 Event A1 → 获取锁 → 写入 buffer[0] → 释放锁
  T1: Agent B 发送 Event B1 → 等待锁...
  T2: Agent C 发送 Event C1 → 等待锁...
  T3: Agent A 发送 Event A2 → 等待锁...
  T4: Agent B 获取锁 → 写入 buffer[1] → 释放锁
  T5: Agent C 获取锁 → 写入 buffer[2] → 释放锁
  T6: Agent A 获取锁 → 写入 buffer[3] → 释放锁

最终 buffer 顺序: [A1, B1, C1, A2]
外部消费顺序:     A1 → B1 → C1 → A2
```

**特点**：
- ✅ **每个 Agent 内部有序**: A1 一定在 A2 之前
- ❌ **Agent 之间无序**: B1 可能在 A2 之前或之后（取决于锁竞争）
- ✅ **消费端按 FIFO**: 按照写入 buffer 的顺序读取

#### 实际运行示例

假设旅游规划 Agent 并发执行：

```
可能的输出顺序 1:
  [TransportationExpert] 输出: 推荐乘坐 JAL...
  [AccommodationExpert] 输出: 推荐住在新宿...
  [FoodExpert] 输出: 必尝寿司...

可能的输出顺序 2:
  [FoodExpert] 输出: 必尝寿司...
  [TransportationExpert] 输出: 推荐乘坐 JAL...
  [AccommodationExpert] 输出: 推荐住在新宿...

可能的输出顺序 3（交错）:
  [TransportationExpert] 输出: 推荐乘坐 JAL...
  [FoodExpert] 输出: 必尝寿司...
  [TransportationExpert] 输出: 本地交通...
  [AccommodationExpert] 输出: 推荐住在新宿...
  [FoodExpert] 输出: 推荐餐厅...
```

**结论**：
- 每次运行，输出顺序**可能不同**（因为并发执行的不确定性）
- 但每个 Agent 内部的输出顺序**始终一致**

---

### 3. 事件识别机制

#### AgentEvent 结构

```go
type AgentEvent struct {
    AgentName string     // ← Agent 名称（如 "TransportationExpert"）
    RunPath   []RunStep  // ← 执行路径（如 [TravelPlanningAgent, TransportationExpert]）
    Output    *Output    // ← 输出内容
    Action    *Action    // ← 动作（中断、退出等）
    Err       error      // ← 错误信息
}
```

#### 消费端识别逻辑

```go
// adk/common/prints/util.go:32-33
func Event(event *adk.AgentEvent) {
    fmt.Printf("name: %s\npath: %s", event.AgentName, event.RunPath)
    // ... 打印输出内容
}
```

**示例输出**：

```
name: TransportationExpert
path: [TravelPlanningAgent TransportationExpert]
answer: 推荐乘坐 JAL 直飞东京成田机场...

name: AccommodationExpert
path: [TravelPlanningAgent AccommodationExpert]
answer: 推荐住在新宿或涩谷地区...

name: FoodExpert
path: [TravelPlanningAgent FoodExpert]
answer: 必尝寿司、拉面、天妇罗...
```

**消费端可以通过以下方式组织输出**：

1. **按 Agent 分组**：收集所有事件后，按 `AgentName` 分组展示
2. **实时流式输出**：直接打印，依赖用户通过 `AgentName` 识别
3. **自定义顺序**：根据业务需求重新排序（如优先展示交通建议）

---

## 为什么不保证全局顺序？

### 1. 性能考虑

如果要保证全局顺序，需要：
- 为每个事件添加全局时间戳或序列号
- 消费端缓冲并排序事件
- 增加延迟和复杂度

**ADK 选择**：牺牲全局顺序，换取更高的并发性能和实时性。

### 2. 语义合理性

并行执行的 Agent 本身就是**独立工作**的：
- 交通专家的建议与美食专家的建议无时序依赖
- 用户更关心**每个专家的完整建议**，而非它们之间的顺序
- 通过 `AgentName` 标识已足够区分

### 3. 实际应用场景

在旅游规划示例中：
- ✅ 用户可以同时看到三个专家的建议
- ✅ 每个专家的建议内部是连贯的
- ❌ 不需要"交通建议必须在美食建议之前"的严格顺序

---

## 如何在消费端保证顺序？

如果业务确实需要特定顺序，可以在**消费端**实现：

### 方式 1: 缓冲后分组展示

```go
func consumeWithGrouping(iter *adk.AsyncIterator[*adk.AgentEvent]) {
    eventsByAgent := make(map[string][]*adk.AgentEvent)
    
    // 收集所有事件
    for {
        event, ok := iter.Next()
        if !ok {
            break
        }
        eventsByAgent[event.AgentName] = append(eventsByAgent[event.AgentName], event)
    }
    
    // 按指定顺序展示
    agentOrder := []string{"TransportationExpert", "AccommodationExpert", "FoodExpert"}
    for _, agentName := range agentOrder {
        fmt.Printf("\n=== %s ===\n", agentName)
        for _, event := range eventsByAgent[agentName] {
            prints.Event(event)
        }
    }
}
```

### 方式 2: 实时流式输出（当前默认）

```go
func consumeRealtime(iter *adk.AsyncIterator[*adk.AgentEvent]) {
    for {
        event, ok := iter.Next()
        if !ok {
            break
        }
        // 直接打印，依赖 AgentName 区分
        prints.Event(event)
    }
}
```

### 方式 3: 使用 OutputKey 汇总

```go
func consumeWithOutputKey(runner *adk.Runner) {
    iter := runner.Query(ctx, "...")
    outputs := make(map[string]string)
    
    for {
        event, ok := iter.Next()
        if !ok {
            break
        }
        if event.Output != nil && event.Output.OutputKey != "" {
            outputs[event.Output.OutputKey] = event.Output.Content
        }
    }
    
    // 按业务逻辑顺序展示
    fmt.Println("交通建议:", outputs["Transportation"])
    fmt.Println("住宿建议:", outputs["Accommodation"])
    fmt.Println("美食建议:", outputs["Food"])
}
```

---

## 终端输出为什么看起来是"有序"的？

### 现象观察

运行旅游规划示例时，你会发现终端输出看起来很"整齐"：

```
name: TransportationExpert
path: [TravelPlanningAgent TransportationExpert]
answer: 推荐乘坐 JAL 直飞东京成田机场...
（完整输出交通建议）

name: AccommodationExpert
path: [TravelPlanningAgent AccommodationExpert]
answer: 推荐住在新宿或涩谷地区...
（完整输出住宿建议）

name: FoodExpert
path: [TravelPlanningAgent FoodExpert]
answer: 必尝寿司、拉面、天妇罗...
（完整输出美食建议）
```

**看起来没有混乱交错**，这是为什么？

### 核心原因：阻塞式流读取 + 串行事件处理

#### 1. 主循环是串行的

```go
// parallel.go:48-59
for {
    event, ok := iter.Next()  // ← 串行读取事件（FIFO 顺序）
    if !ok {
        break
    }
    prints.Event(event)  // ← 阻塞式处理完当前事件才继续
}
```

**关键点**：虽然多个 Agent 并发执行，但消费端是**串行处理事件**的。

#### 2. 每个 Event 的 Stream 会被完整读取

```go
// prints/util.go:49-105
} else if s := event.Output.MessageOutput.MessageStream; s != nil {
    for {
        chunk, err := s.Recv()  // ← 阻塞式读取，直到这个流 EOF
        if err != nil {
            if err == io.EOF {
                break  // ← 只有当前流结束才跳出
            }
            // ...
        }
        fmt.Printf("%v", chunk.Content)  // ← 实时打印当前流的内容
    }
}
```

**关键点**：
- 当处理 `TransportationExpert` 的 event 时，会循环调用 `s.Recv()` 直到该流 EOF
- 这期间即使 `AccommodationExpert` 的 event 已经到达 `UnboundedChan`，也要等待
- 只有当前 Agent 的流**完全读完**后，才会继续 `iter.Next()` 读取下一个 event

#### 3. 每个 Agent 通常只发送一个 Event

```
Agent 执行流程:
  TransportationExpert.Run()
    → 调用 ChatModel
    → 生成完整回答（流式）
    → 发送 1 个 AgentEvent（包含 MessageStream）
    → 结束

AccommodationExpert.Run()
  → 调用 ChatModel
  → 生成完整回答（流式）
  → 发送 1 个 AgentEvent（包含 MessageStream）
  → 结束
```

**关键点**：每个 `ChatModelAgent` 通常只发送**一个 Event**，该 Event 的 `MessageStream` 包含该 Agent 的所有输出。

### 完整执行时序图

```
时间轴 (并发执行):

T0-T5:  TransportationExpert 生成回答中... (goroutine #1)
T0-T7:  AccommodationExpert 生成回答中...   (goroutine #2)
T0-T9:  FoodExpert 生成回答中...           (main goroutine)

T5:     TransportationExpert 完成
        → generator.Send(Event_Transportation) 
        → Event 进入 UnboundedChan.buffer[0]

T7:     AccommodationExpert 完成
        → generator.Send(Event_Accommodation)
        → Event 进入 UnboundedChan.buffer[1]

T9:     FoodExpert 完成
        → generator.Send(Event_Food)
        → Event 进入 UnboundedChan.buffer[2]

消费端 (串行处理):

T5:     iter.Next() → 读取 buffer[0] → Event_Transportation
        prints.Event(Event_Transportation)
          → for { s.Recv() } ← 阻塞式读取流
          → 打印 "推荐乘坐 JAL..."
          → 打印 "本地交通..."
          → ... (完整输出)
          → s.Recv() 返回 EOF
        ✓ Event_Transportation 处理完成

T7+:    iter.Next() → 读取 buffer[1] → Event_Accommodation
        prints.Event(Event_Accommodation)
          → for { s.Recv() } ← 阻塞式读取流
          → 打印 "推荐住在新宿..."
          → ... (完整输出)
          → s.Recv() 返回 EOF
        ✓ Event_Accommodation 处理完成

T9+:    iter.Next() → 读取 buffer[2] → Event_Food
        prints.Event(Event_Food)
          → for { s.Recv() } ← 阻塞式读取流
          → 打印 "必尝寿司..."
          → ... (完整输出)
          → s.Recv() 返回 EOF
        ✓ Event_Food 处理完成
```

### 为什么不会混合输出？

**答案**：因为 `prints.Event()` 在处理每个 Event 时，会**完整读完该 Event 的 MessageStream**，然后才返回主循环继续处理下一个 Event。

**对比**：如果代码是这样的（假设）：

```go
// ❌ 假设的非阻塞式读取（实际不是这样）
for {
    event, ok := iter.Next()
    chunk, _ := event.Stream.Recv()  // 只读一个 chunk
    fmt.Print(chunk.Content)         // 立即返回
}
```

那么输出就会混合：
```
推荐乘坐 JAL...推荐住在新宿...必尝寿司...直飞东京...或涩谷地区...拉面...
（混乱交错）
```

**但实际代码是阻塞式读取整个流**，所以输出是"一个 Agent 完整输出后，再输出下一个 Agent"。

### 输出顺序由什么决定？

**答案**：由 **Agent 完成的先后顺序** 决定，而非预设顺序。

```
场景 1: Transportation 最快完成
  → 输出: Transportation → Accommodation → Food

场景 2: Food 最快完成
  → 输出: Food → Transportation → Accommodation

场景 3: 速度接近，按锁竞争顺序
  → 输出: 每次运行可能不同
```

**验证方法**：多次运行，观察输出顺序的变化（如果 Agent 执行时间接近）。

---

## 总结

### ✅ 保证的内容

| 维度 | 保证 |
|------|------|
| **线程安全** | `UnboundedChan` + `sync.Mutex` 确保并发 `Send()` 安全 |
| **FIFO 顺序** | 事件按照写入缓冲区的顺序被消费 |
| **Agent 内部有序** | 每个 Agent 的事件序列保持顺序 |
| **事件可识别** | 通过 `AgentName` 和 `RunPath` 区分来源 |
| **输出不混合** | 每个 Agent 的流被完整读取后才处理下一个 |

### ❌ 不保证的内容

| 维度 | 说明 |
|------|------|
| **全局顺序** | Agent A 和 Agent B 的事件可能交错出现 |
| **确定性顺序** | 每次运行，事件顺序可能不同（并发不确定性） |

### 🎯 设计哲学

```
┌────────────────────────────────────────────────┐
│  ADK Parallel Workflow 的设计哲学              │
├────────────────────────────────────────────────┤
│ 1. 优先考虑并发性能，而非全局顺序              │
│ 2. 通过元数据（AgentName/RunPath）提供识别能力│
│ 3. 将排序逻辑留给消费端，保持框架的灵活性     │
│ 4. 适用于独立任务的并行执行场景               │
└────────────────────────────────────────────────┘
```

### 📊 适用场景判断

**适合 Parallel 的场景**：
- ✅ 多专家并行咨询（旅游规划、投资建议）
- ✅ 数据并行处理（批量文档解析）
- ✅ 多模型对比（同时调用多个 LLM）

**不适合 Parallel 的场景**：
- ❌ 需要严格顺序的流程（先规划再执行 → 用 Sequential）
- ❌ 后续步骤依赖前序结果（用 Sequential）
- ❌ 需要迭代优化的任务（用 Loop）

---

## 关键代码位置

| 功能 | 文件 | 代码位置 |
|------|------|----------|
| AsyncGenerator 定义 | `adk/utils.go` | L39-54 |
| UnboundedChan 实现 | `internal/channel.go` | L22-78 |
| Send 方法（加锁） | `internal/channel.go` | L37-47 |
| Receive 方法（FIFO） | `internal/channel.go` | L50-67 |
| 事件转发 | `adk/workflow.go` | L161, L308, L328 |
| 事件打印 | `adk/common/prints/util.go` | L32-136 |

---

## 实验验证

你可以运行旅游规划示例多次，观察输出顺序的变化：

```bash
# 运行 5 次，观察顺序差异
for i in {1..5}; do
    echo "=== Run $i ==="
    go run parallel.go 2>&1 | grep "name:"
    echo ""
done
```

**预期结果**：每次运行，Agent 的出现顺序可能不同，但每个 Agent 内部的输出始终连贯。


