# 子图方式 vs 多Agent方式对比分析

## 📖 概述

在 Eino 框架中，实现多智能体协作有两种主要方式：
1. **子图方式**（Graph-based）：将每个 Agent 实现为独立的子图，通过 Graph 连接
2. **多Agent方式**（Agent-based）：使用 ADK 提供的 Agent 接口和 Supervisor 模式

本文档深入分析这两种方式的设计理念、实现细节、优缺点和适用场景。

---

## 🔍 1. 核心设计对比

### 1.1 架构层级

| 方面 | 子图方式 | 多Agent方式 |
|------|---------|------------|
| **所在层级** | Compose 层 (底层) | ADK 层 (高层封装) |
| **基础组件** | `compose.Graph` | `adk.Agent` 接口 |
| **连接方式** | `AddGraphNode` + `AddBranch` | `SetSubAgents` |
| **流程控制** | 手动实现 `agentHandOff` 函数 | 框架自动处理（通过 Transfer Tool） |
| **类型系统** | 泛型类型参数 `<I, O>` | 固定类型 `AgentInput` → `AgentEvent` |

### 1.2 实现对比

#### 子图方式 (deer-go/builder.go)

```go
// 1. 定义手动流转函数
func agentHandOff(ctx context.Context, input string) (next string, err error) {
    _ = compose.ProcessState[*model.State](ctx, func(_ context.Context, state *model.State) error {
        next = state.Goto  // 从状态中读取下一个 agent
        return nil
    })
    return next, nil
}

// 2. 创建主 Graph
g := compose.NewGraph[I, O](
    compose.WithGenLocalState(genFunc),
)

// 3. 创建各个子图
coordinatorGraph := NewCAgent[I, O](ctx)
plannerGraph := NewPlanner[I, O](ctx)
researcherGraph := NewResearcher[I, O](ctx)

// 4. 添加子图作为节点
_ = g.AddGraphNode(consts.Coordinator, coordinatorGraph)
_ = g.AddGraphNode(consts.Planner, plannerGraph)
_ = g.AddGraphNode(consts.Researcher, researcherGraph)

// 5. 定义出口映射
outMap := map[string]bool{
    consts.Coordinator: true,
    consts.Planner:     true,
    consts.Researcher:  true,
    compose.END:        true,
}

// 6. 添加分支（每个节点都可以去任意其他节点）
_ = g.AddBranch(consts.Coordinator, compose.NewGraphBranch(agentHandOff, outMap))
_ = g.AddBranch(consts.Planner, compose.NewGraphBranch(agentHandOff, outMap))
_ = g.AddBranch(consts.Researcher, compose.NewGraphBranch(agentHandOff, outMap))

// 7. 设置入口
_ = g.AddEdge(compose.START, consts.Coordinator)
```

#### 多Agent方式 (adk/supervisor/agent.go)

```go
// 1. 创建 Supervisor Agent
supervisor := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
    Name:        "supervisor",
    Description: "负责监督和分配任务",
    Instruction: "根据任务类型选择合适的 agent...",
    Model:       model.NewChatModel(),
    Exit:        &adk.ExitTool{},  // 添加退出工具
})

// 2. 创建子 Agent
searchAgent := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
    Name:        "research_agent",
    Description: "负责搜索互联网信息",
    Instruction: "只处理研究相关任务...",
    Model:       model.NewChatModel(),
    ToolsConfig: adk.ToolsConfig{
        ToolsNodeConfig: compose.ToolsNodeConfig{
            Tools: []tool.BaseTool{searchTool},
        },
    },
})

mathAgent := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
    Name:        "math_agent",
    Description: "负责数学计算",
    Instruction: "只处理数学相关任务...",
    Model:       model.NewChatModel(),
    ToolsConfig: adk.ToolsConfig{
        ToolsNodeConfig: compose.ToolsNodeConfig{
            Tools: []tool.BaseTool{addTool, multiplyTool, divideTool},
        },
    },
})

// 3. 使用 Supervisor 模式建立关系（框架自动注入 Transfer Tools）
return supervisor.New(ctx, &supervisor.Config{
    Supervisor: supervisor,
    SubAgents:  []adk.Agent{searchAgent, mathAgent},
})
```

---

## 🎯 2. 详细特性对比

### 2.1 流程控制方式

#### 子图方式：手动控制流转

```go
// Researcher 子图的 router 函数
func routerResearcher(ctx context.Context, input *schema.Message, opts ...any) (output string, err error) {
    err = compose.ProcessState[*model.State](ctx, func(_ context.Context, state *model.State) error {
        // 1. 手动保存当前结果
        for i, step := range state.CurrentPlan.Steps {
            if step.ExecutionRes == nil {
                str := strings.Clone(input.Content)
                state.CurrentPlan.Steps[i].ExecutionRes = &str
                break
            }
        }
        
        // 2. 手动决定下一个 agent
        state.Goto = consts.ResearchTeam  // 硬编码的流转逻辑
        output = state.Goto
        return nil
    })
    return output, nil
}
```

**特点：**
- ✅ 完全控制：开发者明确指定流转逻辑
- ✅ 灵活性高：可以实现任意复杂的条件跳转
- ❌ 代码量大：需要手动编写所有流转逻辑
- ❌ 维护成本高：新增 agent 需要修改多处代码

#### 多Agent方式：LLM自动路由

```go
// Supervisor 自动生成 Transfer Tools
// 框架会根据 SubAgents 的 Name 和 Description 自动创建类似这样的工具：

TransferTool("research_agent", "the agent responsible to search the internet for info")
TransferTool("math_agent", "the agent responsible to do math")
ExitTool() // 结束对话

// Supervisor 的模型会根据用户查询，自动选择调用哪个 Tool
// 例如用户问："find US GDP in 2024"
// LLM 会分析后调用：TransferTool("research_agent", ...)
```

**特点：**
- ✅ 智能路由：LLM 根据上下文自动选择合适的 agent
- ✅ 代码简洁：框架自动处理路由逻辑
- ✅ 易于扩展：新增 agent 只需加到 SubAgents 列表
- ❌ 不确定性：路由结果依赖 LLM 推理，可能出错
- ❌ 灵活性有限：复杂的条件逻辑难以实现

### 2.2 状态管理

#### 子图方式：自定义 State（父子图共享）

```go
// 完全自定义的状态结构
type State struct {
    UserInput         string
    Locale            string
    MaxStepNum        int
    MaxPlanIterations int
    CurrentPlan       *Plan
    Goto              string  // 手动维护流转目标
    // ... 任意字段
}

// 父图创建时定义 state
g := compose.NewGraph[I, O](
    compose.WithGenLocalState(func(ctx context.Context) *State {
        return &State{}
    }),
)

// 子图创建时不需要定义 state
coordinatorGraph := compose.NewGraph[I, O]()  // 不传 WithGenLocalState

// 子图内的节点可以直接访问父图的 state（通过 context 传递）
func loadMsg(ctx context.Context, name string, opts ...any) ([]*schema.Message, error) {
    var output []*schema.Message
    // 访问父图的 state
    err := compose.ProcessState[*State](ctx, func(_ context.Context, state *State) error {
        // 读写父图的状态
        state.CurrentPlan.Steps[i].ExecutionRes = &result
        state.Goto = consts.NextAgent
        
        // 使用状态中的数据
        output, err = promptTemp.Format(ctx, map[string]any{
            "locale": state.Locale,
            "user_input": state.Messages,
        })
        return err
    })
    return output, err
}
```

**关键机制：State 通过 Context 共享**

```go
// state 存储在 context 中（来自 compose/state.go）
type stateKey struct{}

// 父图编译运行时，将 state 注入到 context
ctx = context.WithValue(ctx, stateKey{}, &internalState{state: yourState})

// 子图节点通过 compose.ProcessState 访问
func getState[S any](ctx context.Context) (S, *sync.Mutex, error) {
    state := ctx.Value(stateKey{})  // 从 context 获取
    // ... 类型检查和返回
}
```

**特点：**
- ✅ **父子图共享**：只需要在父图定义，子图自动共享（通过 context）
- ✅ **类型安全**：编译时检查类型匹配
- ✅ **结构灵活**：可以定义任意复杂的状态
- ✅ **性能优化**：直接操作内存结构，使用 mutex 保证并发安全
- ✅ **透明传递**：通过 context 自然传递，无需显式参数
- ❌ **需要手动管理**：状态的更新逻辑需要自己写

#### 多Agent方式：Session + HistoryEntry

```go
// 框架管理的 Session 结构
type Session struct {
    events []*HistoryEntry
}

type HistoryEntry struct {
    AgentName   string
    Message     Message
    IsUserInput bool
}

// 使用 Session 存储跨 Agent 的共享数据
adk.AddSessionValue(ctx, "user-name", userName)
userName, _ := adk.GetSessionValue(ctx, "user-name")

// 历史消息自动管理和重写
func rewriteMessage(msg Message, agentName string) Message {
    return schema.UserMessage(
        fmt.Sprintf("For context: [%s] said: %s.", agentName, msg.Content))
}
```

**特点：**
- ✅ 自动管理：框架自动维护历史
- ✅ 历史重写：自动添加上下文信息，避免角色混淆
- ✅ 易于使用：简单的 Get/Set 接口
- ❌ 灵活性有限：结构相对固定
- ❌ 性能开销：序列化和消息重写有额外开销

### 2.3 输入输出类型

#### 子图方式：泛型类型

```go
// 每个子图可以有不同的输入输出类型
func NewResearcher[I, O any](ctx context.Context) *compose.Graph[I, O] {
    cag := compose.NewGraph[I, O]()
    
    // load 节点: any -> []*schema.Message
    _ = cag.AddLambdaNode("load", compose.InvokableLambdaWithOption(loadResearcherMsg))
    
    // agent 节点: []*schema.Message -> *schema.Message
    _ = cag.AddLambdaNode("agent", agentLambda)
    
    // router 节点: *schema.Message -> string
    _ = cag.AddLambdaNode("router", compose.InvokableLambdaWithOption(routerResearcher))
    
    return cag
}

// 类型必须匹配，否则编译报错
g.AddEdge("load", "agent")  // []*schema.Message 匹配
```

**特点：**
- ✅ 类型安全：编译时检查类型匹配
- ✅ 灵活性高：每个节点可以有不同类型
- ✅ 性能优化：无需运行时类型转换
- ❌ 学习曲线：需要理解泛型和类型系统

#### 多Agent方式：固定接口

```go
// 所有 Agent 都是统一的接口
type Agent interface {
    Name(ctx context.Context) string
    Description(ctx context.Context) string
    Run(ctx context.Context, input *AgentInput, opts ...AgentRunOption) *AsyncIterator[*AgentEvent]
}

// 固定的输入格式
type AgentInput struct {
    Messages        []Message
    EnableStreaming bool
}

// 固定的输出格式（流式）
type AgentEvent struct {
    Output *AgentOutput  // 包含 Message
    Action *AgentAction  // 包含 ToolCall 等
    Err    error
}
```

**特点：**
- ✅ 统一接口：所有 Agent 都一样，易于理解
- ✅ 易于组合：任意 Agent 都可以互相组合
- ✅ 流式支持：天然支持流式输出
- ❌ 类型固定：无法自定义输入输出类型
- ❌ 需要转换：内部逻辑可能需要类型转换

### 2.4 工具调用

#### 子图方式：直接集成工具

```go
// 在子图内部直接使用 React Agent
researchTools := []tool.BaseTool{}
for _, cli := range infra.MCPServer {
    ts, err := mcp.GetTools(ctx, &mcp.Config{Cli: cli})
    if err != nil {
        ilog.EventError(ctx, err, "builder_error")
    }
    researchTools = append(researchTools, ts...)
}

agent, err := react.NewAgent(ctx, &react.AgentConfig{
    MaxStep:          40,
    ToolCallingModel: infra.ChatModel,
    ToolsConfig:      compose.ToolsNodeConfig{Tools: researchTools},
})

// 将 agent 包装为 Lambda 节点
agentLambda, _ := compose.AnyLambda(agent.Generate, agent.Stream, nil, nil)
_ = cag.AddLambdaNode("agent", agentLambda)
```

**特点：**
- ✅ 直接集成：在节点内部直接使用工具
- ✅ 灵活配置：可以为每个节点配置不同的工具
- ✅ 支持 MCP：可以动态加载 MCP 服务器的工具
- ❌ 需要手动管理：工具的生命周期需要自己控制

#### 多Agent方式：ToolsConfig配置

```go
searchAgent := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
    Name:        "research_agent",
    Description: "负责搜索",
    Model:       model.NewChatModel(),
    ToolsConfig: adk.ToolsConfig{
        ToolsNodeConfig: compose.ToolsNodeConfig{
            Tools: []tool.BaseTool{searchTool},
            UnknownToolsHandler: func(ctx context.Context, name, input string) (string, error) {
                return fmt.Sprintf("unknown tool: %s", name), nil
            },
        },
    },
})

// Supervisor 会自动注入 Transfer Tools
supervisorWithTransfer := supervisor.New(ctx, &supervisor.Config{
    Supervisor: supervisor,
    SubAgents:  []adk.Agent{searchAgent, mathAgent},
    // 框架自动为 Supervisor 添加：
    // - TransferTool(searchAgent)
    // - TransferTool(mathAgent)
})
```

**特点：**
- ✅ 自动注入：框架自动为 Supervisor 添加 Transfer 工具
- ✅ 配置简单：通过 ToolsConfig 统一配置
- ✅ 错误处理：支持 UnknownToolsHandler
- ❌ 灵活性有限：复杂场景可能需要自定义

---

## 🔗 3. State 共享机制详解（重要！）

### 3.1 父子图共享同一个 State 实例

**核心要点：** 子图和父图是**共用同一个 state 实例**的！

```go
// ✅ 正确理解
父图创建 state → state 存入 context → 子图从 context 读取同一个 state

// ❌ 错误理解
每个子图都有自己独立的 state
```

### 3.2 实际案例：deer-go

```go
// 1. 父图（builder.go）定义 state
func Builder[I, O, S any](ctx context.Context, genFunc compose.GenLocalState[S]) compose.Runnable[I, O] {
    g := compose.NewGraph[I, O](
        compose.WithGenLocalState(genFunc),  // 只在父图定义一次！
    )
    
    // 2. 创建子图（不定义 state）
    coordinatorGraph := NewCAgent[I, O](ctx)     // 无 WithGenLocalState
    plannerGraph := NewPlanner[I, O](ctx)        // 无 WithGenLocalState
    researcherGraph := NewResearcher[I, O](ctx)  // 无 WithGenLocalState
    
    // 3. 将子图添加到父图
    _ = g.AddGraphNode(consts.Coordinator, coordinatorGraph)
    _ = g.AddGraphNode(consts.Planner, plannerGraph)
    _ = g.AddGraphNode(consts.Researcher, researcherGraph)
    
    return g.Compile(ctx)
}

// 4. Coordinator 子图内访问父图的 state
func NewCAgent[I, O any](ctx context.Context) *compose.Graph[I, O] {
    cag := compose.NewGraph[I, O]()  // 不定义 state
    
    // 子图的节点函数可以访问父图的 state
    _ = cag.AddLambdaNode("load", compose.InvokableLambdaWithOption(loadMsg))
    _ = cag.AddLambdaNode("router", compose.InvokableLambdaWithOption(router))
    return cag
}

// 5. 节点函数直接访问父图的 state
func router(ctx context.Context, input *schema.Message, opts ...any) (string, error) {
    var output string
    // 通过 ProcessState 访问父图定义的 state
    err := compose.ProcessState[*model.State](ctx, func(_ context.Context, state *model.State) error {
        // 这里的 state 就是父图创建的那个 state 实例！
        state.Goto = consts.Planner  // 修改会影响所有其他节点
        output = state.Goto
        return nil
    })
    return output, err
}

// 6. Planner 子图也能访问到同一个 state
func routerPlanner(ctx context.Context, input *schema.Message, opts ...any) (string, error) {
    var output string
    err := compose.ProcessState[*model.State](ctx, func(_ context.Context, state *model.State) error {
        // 这里读取到的 state 和 Coordinator 修改的是同一个！
        state.CurrentPlan = parsedPlan
        state.Goto = consts.ResearchTeam
        output = state.Goto
        return nil
    })
    return output, err
}
```

### 3.3 State 传递流程

```
父图启动 (Invoke/Stream)
  ↓
创建 state 实例（通过 genFunc）
  ↓
state 存入 context: context.WithValue(ctx, stateKey{}, &internalState{state: state})
  ↓
执行节点 1：Coordinator 子图
  ↓ (context 传递)
Coordinator.load 节点
  ↓ compose.ProcessState[*State](ctx, ...)
  从 ctx.Value(stateKey{}) 获取 state
  ↓
修改 state.Locale = "zh-CN"
  ↓
Coordinator.router 节点
  ↓ compose.ProcessState[*State](ctx, ...)
  读取 state.Locale (值是 "zh-CN")
  修改 state.Goto = consts.Planner
  ↓
agentHandOff 读取 state.Goto
  ↓
执行节点 2：Planner 子图
  ↓ (context 传递，包含同一个 state)
Planner.load 节点
  ↓ compose.ProcessState[*State](ctx, ...)
  读取 state.Locale (值仍是 "zh-CN"！)
  修改 state.CurrentPlan = newPlan
  ↓
Planner.router 节点
  ↓ compose.ProcessState[*State](ctx, ...)
  读取 state.CurrentPlan (刚才设置的 newPlan)
  修改 state.Goto = consts.Researcher
  ↓
... 继续流转，所有子图共享同一个 state 实例
```

### 3.4 并发安全

State 访问通过 mutex 保护，保证并发安全：

```go
// compose/state.go
type internalState struct {
    state any
    mu    sync.Mutex  // 每次访问都会加锁
}

func ProcessState[S any](ctx context.Context, handler func(context.Context, S) error) error {
    s, pMu, err := getState[S](ctx)
    if err != nil {
        return err
    }
    pMu.Lock()          // 加锁
    defer pMu.Unlock()  // 解锁
    return handler(ctx, s)
}
```

### 3.5 对比多Agent方式

| 方面 | 子图方式（State） | 多Agent方式（Session） |
|------|------------------|---------------------|
| **共享方式** | Context 传递，直接引用 | Session 存储，序列化传递 |
| **访问方式** | `compose.ProcessState` | `adk.GetSessionValue` |
| **实例数量** | 整个父图只有 1 个 state | 每个 Agent 有独立 session |
| **并发安全** | Mutex 保护 | 框架管理 |
| **性能** | 高（直接内存访问） | 较低（可能涉及序列化） |
| **灵活性** | 自定义结构 | Key-Value 存储 |

### 3.6 常见误区

❌ **误区 1**：每个子图都有自己的 state
```go
// 错误理解
coordinatorGraph := NewCAgent[I, O](ctx, WithGenLocalState(...))  // ✗ 不要这样做
```
**正确做法**：只在父图定义 state，子图通过 context 访问

❌ **误区 2**：子图修改 state 不会影响其他子图
```go
// Coordinator 修改
state.Locale = "zh-CN"

// Planner 能看到吗？ → ✓ 能！因为是同一个 state
```

❌ **误区 3**：需要手动传递 state
```go
// 错误做法
func loadMsg(ctx context.Context, state *State) { ... }  // ✗ 不需要显式参数

// 正确做法
func loadMsg(ctx context.Context, name string, opts ...any) {
    compose.ProcessState[*State](ctx, func(_ context.Context, state *State) error {
        // state 从 context 自动获取
    })
}
```

### 3.7 优势总结

使用子图共享 state 的优势：

1. **统一状态管理**：所有子图看到的都是同一份数据
2. **无需序列化**：直接内存访问，性能高
3. **类型安全**：编译时检查类型匹配
4. **并发安全**：框架自动加锁保护
5. **透明传递**：通过 context 自然流转，无需显式参数

---

## ⚖️ 4. 优缺点对比

### 4.1 子图方式（Graph-based）

#### ✅ 优点

1. **完全控制**
   - 流程控制完全由代码决定，不依赖 LLM 推理
   - 适合需要精确控制的场景（如严格的审批流程）

2. **性能优化**
   - 直接操作内存状态，无需序列化
   - 类型安全，无运行时类型转换开销
   - 可以精确控制每个节点的执行逻辑

3. **灵活性强**
   - 可以实现任意复杂的 DAG 结构
   - 支持循环、条件跳转、并行等所有流程模式
   - 每个节点可以有不同的输入输出类型

4. **状态透明**
   - 状态结构完全自定义
   - 状态变化在代码中清晰可见
   - 易于调试和追踪

5. **无 LLM 依赖**
   - 流转逻辑不依赖 LLM 判断
   - 结果确定性强，不会出现路由错误

#### ❌ 缺点

1. **开发成本高**
   - 需要手动编写所有流转逻辑
   - 代码量大，维护成本高
   - 新增 agent 需要修改多处代码

2. **缺乏智能性**
   - 无法根据上下文自动选择路径
   - 复杂条件需要大量 if-else 代码
   - 难以处理开放式场景

3. **学习曲线陡峭**
   - 需要理解 Graph、Lambda、泛型等概念
   - 需要熟悉 Compose 层的 API
   - 调试相对复杂

4. **代码耦合度高**
   - agent 之间的流转逻辑硬编码
   - 难以动态调整流程
   - 测试单个 agent 相对困难

### 4.2 多Agent方式（Agent-based）

#### ✅ 优点

1. **开发效率高**
   - 框架自动处理路由逻辑
   - 代码量少，易于维护
   - 新增 agent 只需加到列表即可

2. **智能路由**
   - LLM 根据描述自动选择合适的 agent
   - 适合开放式对话场景
   - 可以处理意图识别等复杂任务

3. **易于理解**
   - 统一的 Agent 接口
   - 清晰的父子关系
   - 符合直觉的层级结构

4. **解耦性好**
   - 每个 agent 独立开发和测试
   - 通过 Description 声明能力
   - 易于组合和复用

5. **历史管理**
   - 框架自动管理消息历史
   - 自动重写历史，避免角色混淆
   - 支持 Session 共享数据

#### ❌ 缺点

1. **不确定性**
   - 路由结果依赖 LLM 推理
   - 可能出现路由错误
   - 难以保证确定性行为

2. **灵活性有限**
   - 主要支持树形层级结构
   - 复杂的 DAG 流程难以实现
   - 条件跳转能力有限

3. **性能开销**
   - 每次路由都需要调用 LLM
   - 历史消息重写有额外开销
   - Session 序列化有性能损耗

4. **调试困难**
   - LLM 决策过程不透明
   - 路由错误难以定位
   - 需要大量日志和追踪

5. **成本考虑**
   - 每次路由都消耗 token
   - 长对话历史导致成本增加
   - Supervisor 调用频率高

---

## 🎨 5. 适用场景

### 5.1 子图方式适合的场景

#### ✅ 推荐使用

1. **确定性流程**
   ```
   场景：工作流自动化、审批流程、数据处理管道
   原因：流程固定，需要精确控制每一步
   ```

2. **性能敏感**
   ```
   场景：高频调用、实时系统、大规模并发
   原因：避免 LLM 调用开销，响应速度快
   ```

3. **复杂 DAG 结构**
   ```
   场景：并行处理、循环迭代、复杂条件跳转
   原因：支持任意图结构，灵活性强
   ```

4. **成本敏感**
   ```
   场景：低成本应用、频繁路由场景
   原因：避免每次路由都调用 LLM
   ```

5. **状态复杂**
   ```
   场景：需要维护复杂状态的应用
   原因：可以自定义任意状态结构
   ```

#### 📋 示例场景

```go
// 场景1: 文章生成流程（固定步骤）
// Outline -> Research -> Draft -> Review -> Publish
// 每步都必须按顺序执行，不能跳过

// 场景2: 数据处理管道（并行+聚合）
// Load -> [Transform1, Transform2, Transform3] -> Aggregate -> Save
// 多个转换并行执行，然后聚合结果

// 场景3: 迭代优化流程（循环）
// Plan -> Execute -> Evaluate -> (back to Plan if not satisfied) -> Finish
// 需要根据评估结果决定是否继续迭代
```

### 5.2 多Agent方式适合的场景

#### ✅ 推荐使用

1. **开放式对话**
   ```
   场景：客服机器人、虚拟助手、智能问答
   原因：需要根据用户意图动态路由
   ```

2. **意图识别**
   ```
   场景：多领域服务、跨部门协作
   原因：LLM 自动判断用户需求
   ```

3. **专家系统**
   ```
   场景：多个专业领域的智能体协作
   原因：根据问题类型自动选择专家
   ```

4. **快速原型**
   ```
   场景：MVP 开发、概念验证
   原因：开发速度快，易于迭代
   ```

5. **简单层级结构**
   ```
   场景：主管-员工模式、路由-执行模式
   原因：清晰的层级关系，易于理解
   ```

#### 📋 示例场景

```go
// 场景1: 智能客服
// User: "查询订单状态"      -> OrderAgent
// User: "推荐产品"          -> RecommendAgent
// User: "退款申请"          -> RefundAgent
// LLM 根据用户意图自动路由

// 场景2: 研究助手
// User: "美国2024年GDP是多少？占全球多少？"
// Supervisor -> ResearchAgent (查询GDP数据)
//            -> MathAgent (计算百分比)
//            -> Reporter (生成报告)

// 场景3: 多领域专家系统
// User: "写一个排序算法并分析复杂度"
// Supervisor -> CoderAgent (实现算法)
//            -> MathAgent (分析复杂度)
//            -> ReviewerAgent (审查代码)
```

---

## 🔀 6. 混合使用模式

实际项目中，可以结合两种方式的优势：

### 6.1 外层 Graph + 内层 Agent

```go
// 外层：使用 Graph 控制大的流程阶段
g := compose.NewGraph[string, string]()

// 内层：每个阶段内使用 Agent 处理复杂逻辑
planAgent := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{...})
executeAgent := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{...})

// 将 Agent 包装成 Graph 节点
g.AddLambdaNode("plan", wrapAgent(planAgent))
g.AddLambdaNode("execute", wrapAgent(executeAgent))

// 用 Graph 控制流程
g.AddEdge("plan", "execute")
g.AddBranch("execute", compose.NewGraphBranch(checkCompletion, map[string]bool{
    "plan": true,    // 如果未完成，回到 plan
    compose.END: true,  // 完成则结束
}))
```

### 6.2 子图内使用 React Agent

```go
// deer-go 的做法：子图内部使用 React Agent
func NewResearcher[I, O any](ctx context.Context) *compose.Graph[I, O] {
    // 使用 React Agent 处理工具调用
    agent, err := react.NewAgent(ctx, &react.AgentConfig{
        MaxStep:          40,
        ToolCallingModel: infra.ChatModel,
        ToolsConfig:      compose.ToolsNodeConfig{Tools: researchTools},
    })
    
    // 包装为 Lambda 节点
    agentLambda, _ := compose.AnyLambda(agent.Generate, agent.Stream, nil, nil)
    
    // 添加到子图
    _ = cag.AddLambdaNode("agent", agentLambda)
}
```

---

## 📊 7. 性能对比

### 7.1 延迟对比

| 操作 | 子图方式 | 多Agent方式 | 差异 |
|------|---------|------------|------|
| **节点流转** | ~1ms (函数调用) | ~1-3s (LLM 推理) | **1000x** |
| **状态读写** | 直接内存访问 | Session 序列化/反序列化 | **10-100x** |
| **历史管理** | 手动（按需） | 自动重写（每次） | 取决于实现 |
| **总体延迟** | 主要是节点处理时间 | 增加路由 LLM 调用 | **+1-3s/hop** |

### 7.2 成本对比

假设一个 5 步流程：

| 方式 | LLM 调用次数 | Token 估算 | 成本（GPT-4） |
|------|-------------|-----------|--------------|
| **子图方式** | 5 (每步业务调用) | 5 × 1000 = 5k tokens | $0.15 |
| **多Agent方式** | 5 (业务) + 5 (路由) = 10 | 10 × 1000 + 5 × 500 (历史重写) = 12.5k tokens | $0.38 |

**注意**：实际成本取决于：
- Prompt 长度
- 历史消息数量
- 路由复杂度
- 重试次数

---

## 🛠️ 8. 决策指南

### 8.1 决策树

```
开始
 |
 ├─ 流程是否固定？
 |   ├─ 是 → 子图方式 ✓
 |   └─ 否 ↓
 |
 ├─ 是否需要意图识别？
 |   ├─ 是 → 多Agent方式 ✓
 |   └─ 否 ↓
 |
 ├─ 性能是否关键？
 |   ├─ 是 → 子图方式 ✓
 |   └─ 否 ↓
 |
 ├─ 是否有复杂 DAG？
 |   ├─ 是 → 子图方式 ✓
 |   └─ 否 ↓
 |
 ├─ 团队熟悉 Graph？
 |   ├─ 否 → 多Agent方式 ✓
 |   └─ 是 → 根据场景选择
```

### 8.2 快速选择表

| 如果你的应用... | 选择 | 原因 |
|----------------|------|------|
| 是客服机器人 | 多Agent | 需要意图识别 |
| 是工作流引擎 | 子图 | 确定性流程 |
| 需要高并发 | 子图 | 性能优先 |
| MVP 快速验证 | 多Agent | 开发速度快 |
| 有复杂状态 | 子图 | 灵活的状态管理 |
| 简单层级结构 | 多Agent | 易于理解 |
| 需要循环迭代 | 子图 | 支持复杂控制流 |
| 成本敏感 | 子图 | 减少 LLM 调用 |

---

## 💡 9. 最佳实践

### 9.1 子图方式最佳实践

1. **明确的状态设计**
   ```go
   // ✅ 好：清晰的状态结构
   type State struct {
       Stage        string    // 当前阶段
       Input        string    // 用户输入
       Plan         *Plan     // 计划
       Results      []Result  // 中间结果
       NextAgent    string    // 下一个agent（明确命名）
   }
   
   // ❌ 差：模糊的字段名
   type State struct {
       Data  interface{}  // 太泛化
       Goto  string       // 语义不清
   }
   ```

2. **清晰的流转函数**
   ```go
   // ✅ 好：逻辑清晰，易于测试
   func routeAfterResearch(ctx context.Context, result *ResearchResult) (string, error) {
       if result.NeedsMoreInfo {
           return consts.Researcher, nil
       }
       if result.ReadyToReport {
           return consts.Reporter, nil
       }
       return compose.END, nil
   }
   
   // ❌ 差：逻辑混乱
   func agentHandOff(ctx context.Context, input string) (string, error) {
       var next string
       compose.ProcessState[*State](ctx, func(_ context.Context, state *State) error {
           next = state.Goto  // 逻辑隐藏在其他地方
           return nil
       })
       return next, nil
   }
   ```

3. **合理的粒度**
   ```go
   // ✅ 好：每个子图有明确的职责
   researcherGraph := NewResearcher[I, O](ctx)    // 负责研究
   reporterGraph := NewReporter[I, O](ctx)        // 负责生成报告
   
   // ❌ 差：粒度太细，管理复杂
   loadGraph := NewLoad[I, O](ctx)
   validateGraph := NewValidate[I, O](ctx)
   transformGraph := NewTransform[I, O](ctx)
   // ... 30 个微小的graph
   ```

### 9.2 多Agent方式最佳实践

1. **清晰的 Agent 描述**
   ```go
   // ✅ 好：描述清晰、具体
   adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
       Name:        "research_agent",
       Description: "Searches the internet for factual information about current events, statistics, and news. Use this agent when you need up-to-date information from web sources.",
       Instruction: "You are a research specialist. Search for reliable information and cite your sources.",
   })
   
   // ❌ 差：描述模糊
   adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
       Name:        "agent1",
       Description: "An agent",  // LLM 无法理解何时使用
   })
   ```

2. **合理的 Agent 数量**
   ```go
   // ✅ 好：3-7 个 SubAgent，职责清晰
   SubAgents: []adk.Agent{
       researchAgent,    // 研究
       mathAgent,        // 计算
       codeAgent,        // 编程
       reportAgent,      // 报告
   }
   
   // ❌ 差：太多 Agent，LLM 难以选择
   SubAgents: []adk.Agent{
       agent1, agent2, ..., agent20,  // 20 个agent
   }
   ```

3. **使用 Exit 工具**
   ```go
   // ✅ 好：明确告诉模型何时结束
   supervisor := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
       Name:  "supervisor",
       Exit:  &adk.ExitTool{},  // 必须添加
   })
   
   // ❌ 差：缺少 Exit，可能无限循环
   supervisor := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
       Name: "supervisor",
       // 缺少 Exit
   })
   ```

---

## 🎓 10. 学习建议

### 10.1 学习路径

1. **入门阶段：多Agent方式**
   - 从 `adk/multiagent/supervisor` 示例开始
   - 理解 Agent 接口和 Transfer 机制
   - 实现简单的 2-3 个 Agent 协作

2. **进阶阶段：子图方式**
   - 学习 `compose.Graph` 的基本用法
   - 理解泛型类型系统
   - 实现简单的 sequential/parallel 流程

3. **高级阶段：混合使用**
   - 分析 deer-go 的实现
   - 理解何时使用哪种方式
   - 设计复杂的多智能体系统

### 10.2 推荐阅读

1. **基础概念**
   - `adk/intro/transfer/transfer设计哲学和实现原理.md`
   - `adk/multiagent/多智能体协作设计和原理分析.md`

2. **实战示例**
   - `adk/multiagent/supervisor/` - 简单监督者模式
   - `adk/multiagent/plan-execute-replan/` - 计划执行模式
   - `flow/agent/deer-go/` - 复杂子图实现

3. **API 文档**
   - `compose.Graph` API
   - `adk.Agent` 接口
   - `compose.Lambda` 包装

---

## 📖 11. 总结

### 11.1 核心差异

| 维度 | 子图方式 | 多Agent方式 |
|------|---------|------------|
| **控制** | 代码控制，确定性强 | LLM 控制，智能但不确定 |
| **开发** | 代码量大，灵活性高 | 代码量少，易于上手 |
| **性能** | 快速，低延迟 | 较慢，需 LLM 路由 |
| **成本** | 低（仅业务调用） | 高（额外路由调用） |
| **适用** | 固定流程、复杂 DAG | 开放对话、意图识别 |

### 11.2 选择建议

```go
// 选择子图方式，如果你需要：
✓ 确定性的流程控制
✓ 高性能和低延迟
✓ 复杂的 DAG 结构
✓ 精细的状态管理
✓ 成本优化

// 选择多Agent方式，如果你需要：
✓ 智能的意图识别
✓ 快速开发和迭代
✓ 简单的层级结构
✓ 开放式对话场景
✓ 易于理解和维护
```

### 11.3 最后建议

> **没有最好的方式，只有最合适的方式。**

- **初学者**：从多Agent方式开始，理解基本概念
- **性能优先**：选择子图方式，获得最佳性能
- **快速原型**：使用多Agent方式，快速验证想法
- **复杂系统**：混合使用，在合适的层次使用合适的方式

**记住**：Eino 框架的设计哲学是**分层抽象**，ADK 层（多Agent）是对 Compose 层（子图）的高级封装。理解这一点，你就能更好地选择和组合这两种方式。

---

## 📝 附录：代码对比速查表

### A.1 创建流程

```go
// 子图方式
g := compose.NewGraph[string, string]()
g.AddGraphNode("agent1", subGraph1)
g.AddBranch("agent1", compose.NewGraphBranch(routeFunc, outMap))
r, _ := g.Compile(ctx)

// 多Agent方式
agent1 := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{...})
agent2 := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{...})
sv, _ := supervisor.New(ctx, &supervisor.Config{
    Supervisor: supervisor,
    SubAgents:  []adk.Agent{agent1, agent2},
})
```

### A.2 状态管理

```go
// 子图方式
type State struct { ... }
compose.ProcessState[*State](ctx, func(ctx context.Context, state *State) error {
    state.Field = value
    return nil
})

// 多Agent方式
adk.AddSessionValue(ctx, "key", value)
value, _ := adk.GetSessionValue(ctx, "key")
```

### A.3 流程控制

```go
// 子图方式
func routeFunc(ctx context.Context, input string) (string, error) {
    if condition {
        return "agent2", nil
    }
    return compose.END, nil
}

// 多Agent方式
// LLM 自动决策，调用 TransferTool("agent2") 或 ExitTool()
```

---

**版权所有 © 2025 CloudWeGo Authors**

