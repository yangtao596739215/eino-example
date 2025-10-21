# Transfer 设计哲学和实现原理

## 概述

Eino ADK 的 Transfer 机制是一个优雅的多智能体协作方案，通过**分层架构**和**职责分离**实现了智能体间的无缝控制流转。

## 设计哲学

### 1. 职责分离（Separation of Concerns）

Transfer 机制采用双层架构，将"业务逻辑"与"控制流编排"完全解耦：

```
┌─────────────────────────────────────────────────────┐
│              用户调用 Runner.Query()                 │
└──────────────────┬──────────────────────────────────┘
                   │
                   v
         ┌─────────────────────┐
         │   flowAgent (Root)  │  ← 控制流编排层
         │  - 管理 RunPath     │     职责：流程控制
         │  - 维护 Session     │
         │  - 拦截 Action      │
         │  - 执行 Transfer    │
         └─────────┬───────────┘
                   │ wraps
                   v
         ┌─────────────────────┐
         │  ChatModelAgent     │  ← 业务逻辑层
         │  - 调用模型         │     职责：AI 推理
         │  - 执行工具         │
         │  - 生成 Action      │
         │  - 产生事件流       │
         └─────────────────────┘
```

**核心原则**：
- **底层 Agent**（如 `ChatModelAgent`）：专注 AI 能力，只负责生成"意图"（Action）
- **flowAgent**：专注流程控制，负责执行"决策"（流转、历史管理、路径追踪）

### 2. 单一工具 + 动态指令（Single Tool with Dynamic Instruction）

与"每个子 Agent 一个工具"的直观设计不同，ADK 采用**只有一个 `transfer_to_agent` 工具 + 动态生成可用 Agent 列表**的方案：

```go
// 工具定义：只有一个 transfer_to_agent
toolInfoTransferToAgent = &schema.ToolInfo{
    Name: "transfer_to_agent",
    Desc: "Transfer the question to another agent.",
    ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
        "agent_name": {  // ← 参数是目标 Agent 的名字
            Desc:     "the name of the agent to transfer to",
            Required: true,
            Type:     schema.String,
        },
    }),
}

// 动态指令：告诉模型有哪些 Agent 可用
const TransferToAgentInstruction = `Available other agents: %s

Decision rule:
- If you're best suited for the question according to your description: ANSWER
- If another agent is better according its description: CALL 'transfer_to_agent' function with their agent name

When transferring: OUTPUT ONLY THE FUNCTION CALL`
```

**优势**：
- 工具数量不随 Agent 数量膨胀（始终只有 1 个）
- 模型看到完整的 Agent 列表和描述，做出更明智的路由决策
- 支持双向 transfer（子→父，父→子），通过动态构建 `transferToAgents` 列表控制

### 3. 声明式关系管理（Declarative Relationship Management）

通过 `SetSubAgents` API 声明父子关系，框架自动完成：
- 双向引用建立（`parentAgent` ↔ `subAgents`）
- Transfer 工具自动注入
- Transfer 指令自动生成

```go
// 用户代码：声明式
routerAgent := subagents.NewRouterAgent()
weatherAgent := subagents.NewWeatherAgent()
chatAgent := subagents.NewChatAgent()

agent, _ := adk.SetSubAgents(ctx, routerAgent, []adk.Agent{chatAgent, weatherAgent})

// 框架自动完成：
// 1. routerAgent.subAgents = [chatAgent, weatherAgent]
// 2. chatAgent.parentAgent = routerAgent
// 3. weatherAgent.parentAgent = routerAgent
// 4. routerAgent 自动获得 transfer_to_agent 工具
// 5. routerAgent 的 Instruction 自动包含子 Agent 列表
```

## 实现原理

### 1. 工具注入时机（Tool Injection）

工具注入发生在 `ChatModelAgent.buildRunFunc()` 的 `once.Do` 中：

```go
// chatmodel.go:496-513
func (a *ChatModelAgent) buildRunFunc(ctx context.Context) runFunc {
    a.once.Do(func() {
        instruction := a.instruction
        toolsNodeConf := a.toolsConfig.ToolsNodeConfig
        returnDirectly := copyMap(a.toolsConfig.ReturnDirectly)

        // 1. 收集可 transfer 的 Agent（子 Agent + 父 Agent）
        transferToAgents := a.subAgents
        if a.parentAgent != nil && !a.disallowTransferToParent {
            transferToAgents = append(transferToAgents, a.parentAgent)
        }

        if len(transferToAgents) > 0 {
            // 2. 生成 transfer 指令（包含所有可 transfer 的 Agent 名字和描述）
            transferInstruction := genTransferToAgentInstruction(ctx, transferToAgents)
            instruction = concatInstructions(instruction, transferInstruction)

            // 3. 添加唯一的 transfer_to_agent 工具
            toolsNodeConf.Tools = append(toolsNodeConf.Tools, &transferToAgent{})
            returnDirectly[TransferToAgentToolName] = true  // 立即返回，不继续推理
        }

        // ... 构建 React Agent
    })
}
```

**关键点**：
- 延迟初始化：只在第一次 `Run()` 时构建，确保 `SetSubAgents` 已完成
- 双向支持：子 Agent 也可以 transfer 回父 Agent
- 自动返回：`returnDirectly[TransferToAgentToolName] = true` 确保 transfer 后立即返回，避免继续推理

### 2. 动态指令生成（Dynamic Instruction Generation）

```go
// instruction.go:35-43
func genTransferToAgentInstruction(ctx context.Context, agents []Agent) string {
    var sb strings.Builder
    for _, agent := range agents {
        sb.WriteString(fmt.Sprintf("\n- Agent name: %s\n  Agent description: %s",
            agent.Name(ctx), agent.Description(ctx)))
    }
    
    return fmt.Sprintf(TransferToAgentInstruction, sb.String(), TransferToAgentToolName)
}
```

**生成的实际指令示例**：
```
Available other agents: 
- Agent name: WeatherAgent
  Agent description: This agent can get the current weather for a given city.
- Agent name: ChatAgent
  Agent description: A general-purpose agent for handling conversational chat.

Decision rule:
- If you're best suited for the question according to your description: ANSWER
- If another agent is better according its description: CALL 'transfer_to_agent' function with their agent name

When transferring: OUTPUT ONLY THE FUNCTION CALL
```

### 3. Action 生成（Action Generation）

底层 Agent 的工具执行时，生成 `TransferToAgentAction`：

```go
// chatmodel.go:300-317
type transferToAgent struct{}

func (tta transferToAgent) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
    type transferParams struct {
        AgentName string `json:"agent_name"`
    }

    params := &transferParams{}
    err := sonic.UnmarshalString(argumentsInJSON, params)  // 解析出目标 Agent 名字
    if err != nil {
        return "", err
    }

    // ← 关键：发送 TransferToAgentAction 到上下文
    err = SendToolGenAction(ctx, TransferToAgentToolName, NewTransferToAgentAction(params.AgentName))
    if err != nil {
        return "", err
    }

    return transferToAgentToolOutput(params.AgentName), nil  // 返回确认消息
}
```

**职责边界**：
- `transferToAgent` 工具：只负责生成 Action，不执行任何跳转
- 工具返回值：模拟真实工具执行结果，供模型继续对话

### 4. 控制流转（Control Flow Transfer）

`flowAgent.run()` 拦截 Action 并执行实际跳转：

```go
// flow.go:365-437
func (a *flowAgent) run(
    ctx context.Context,
    runCtx *runContext,
    aIter *AsyncIterator[*AgentEvent],
    generator *AsyncGenerator[*AgentEvent],
    opts ...AgentRunOption) {
    
    var lastAction *AgentAction
    
    // 1. 收集底层 Agent 产生的所有事件
    for {
        event, ok := aIter.Next()
        if !ok {
            break
        }

        event.AgentName = a.Name(ctx)      // ← 注入当前 Agent 名
        event.RunPath = runCtx.RunPath     // ← 维护运行路径
        runCtx.Session.addEvent(event)     // ← 记录历史
        lastAction = event.Action          // ← 保存最后一个 Action
        generator.Send(event)
    }
    
    // 2. 根据 Action 类型决定控制流
    if lastAction != nil {
        if lastAction.Interrupted != nil {
            appendInterruptRunCtx(ctx, runCtx)
            return  // 中断，等待恢复
        }
        if lastAction.Exit {
            return  // 退出
        }
    }
    
    var destName string
    if lastAction != nil && lastAction.TransferToAgent != nil {
        destName = lastAction.TransferToAgent.DestAgentName
    }

    // 3. Transfer：查找并递归调用目标 Agent
    if destName != "" {
        agentToRun := a.getAgent(ctx, destName)  // ← 在父子关系中查找
        if agentToRun == nil {
            e := errors.New(fmt.Sprintf(
                "transfer failed: agent '%s' not found when transferring from '%s'",
                destName, a.Name(ctx)))
            generator.Send(&AgentEvent{Err: e})
            return
        }

        // ← 递归调用目标 Agent，继续流转
        subAIter := agentToRun.Run(ctx, nil /*subagents get input from runCtx*/, opts...)
        for {
            subEvent, ok_ := subAIter.Next()
            if !ok_ {
                break
            }

            setAutomaticClose(subEvent)
            generator.Send(subEvent)  // ← 透传子 Agent 事件
        }
    }
}
```

**核心能力**：
- **事件流透传**：子 Agent 的所有事件都会透传给上层
- **递归调用**：支持无限层级嵌套（如 Layered Supervisor）
- **历史管理**：`runCtx.Session` 记录跨 Agent 的对话历史
- **路径追踪**：`event.RunPath` 记录完整调用链

### 5. 父子关系查找（Parent-Child Lookup）

```go
// flow.go:150-162
func (a *flowAgent) getAgent(ctx context.Context, name string) *flowAgent {
    // 1. 在子 Agent 中查找
    for _, subAgent := range a.subAgents {
        if subAgent.Name(ctx) == name {
            return subAgent
        }
    }

    // 2. 检查是否是父 Agent
    if a.parentAgent != nil && a.parentAgent.Name(ctx) == name {
        return a.parentAgent
    }

    return nil
}
```

**双向查找**：
- 支持向下 transfer（父 → 子）
- 支持向上 transfer（子 → 父）
- 可通过 `WithDisallowTransferToParent()` 禁用向上 transfer

### 6. 历史记录改写（History Rewriting）

当控制转给子 Agent 时，父 Agent 的消息会被改写，避免角色混淆：

```go
// flow.go:178-198
func rewriteMessage(msg Message, agentName string) Message {
    var sb strings.Builder
    sb.WriteString("For context:")
    if msg.Role == schema.Assistant {
        if msg.Content != "" {
            sb.WriteString(fmt.Sprintf(" [%s] said: %s.", agentName, msg.Content))
        }
        if len(msg.ToolCalls) > 0 {
            for i := range msg.ToolCalls {
                f := msg.ToolCalls[i].Function
                sb.WriteString(fmt.Sprintf(" [%s] called tool: `%s` with arguments: %s.",
                    agentName, f.Name, f.Arguments))
            }
        }
    } else if msg.Role == schema.Tool && msg.Content != "" {
        sb.WriteString(fmt.Sprintf(" [%s] `%s` tool returned result: %s.",
            agentName, msg.ToolName, msg.Content))
    }

    return schema.UserMessage(sb.String())
}
```

**改写示例**：
- 原始消息（RouterAgent）：`Assistant: "Let me transfer you to WeatherAgent"`
- 改写后（WeatherAgent 看到）：`User: "For context: [RouterAgent] said: Let me transfer you to WeatherAgent."`

## 完整流程示例

### 场景：查询北京天气

```go
routerAgent := NewRouterAgent()
weatherAgent := NewWeatherAgent()
chatAgent := NewChatAgent()

agent, _ := adk.SetSubAgents(ctx, routerAgent, []adk.Agent{chatAgent, weatherAgent})
runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent})

iter := runner.Query(ctx, "What's the weather in Beijing?")
```

### 执行流程

```
1. User Input
   └─> "What's the weather in Beijing?"

2. flowAgent(RouterAgent).run()
   ├─> ChatModelAgent(RouterAgent).Run()
   │   ├─> Model sees instruction:
   │   │   "Available other agents: WeatherAgent, ChatAgent
   │   │    Decision rule: If another agent is better, CALL 'transfer_to_agent'"
   │   │
   │   ├─> Model decides: Need WeatherAgent
   │   ├─> Model calls: transfer_to_agent(agent_name="WeatherAgent")
   │   │
   │   └─> transferToAgent.InvokableRun()
   │       └─> SendToolGenAction(ctx, TransferToAgentAction{DestAgentName: "WeatherAgent"})
   │
   ├─> flowAgent intercepts: lastAction.TransferToAgent != nil
   ├─> destName = "WeatherAgent"
   ├─> agentToRun = a.getAgent(ctx, "WeatherAgent")
   │
   └─> agentToRun.Run(ctx, nil, opts...)  // ← 递归调用

3. flowAgent(WeatherAgent).run()
   ├─> ChatModelAgent(WeatherAgent).Run()
   │   ├─> Model sees history (rewritten):
   │   │   "User: What's the weather in Beijing?"
   │   │   "User: For context: [RouterAgent] called tool: transfer_to_agent with arguments: {agent_name: WeatherAgent}"
   │   │
   │   ├─> Model sees tools: [get_weather]
   │   ├─> Model calls: get_weather(city="Beijing")
   │   │
   │   └─> get_weather returns: "the temperature in Beijing is 25°C"
   │
   └─> No transfer action, flowAgent returns

4. flowAgent(RouterAgent) receives WeatherAgent's events
   └─> Transparently forward to user

5. User receives final result
   └─> "the temperature in Beijing is 25°C"
```

### 运行路径（RunPath）

```
Event 1: AgentName=RouterAgent, RunPath=[]
Event 2: AgentName=WeatherAgent, RunPath=[RouterAgent]
Event 3: AgentName=WeatherAgent, RunPath=[RouterAgent]
```

## 设计优势

### 1. 可扩展性（Extensibility）
- 新增 Action 类型（如 `Parallel`、`Loop`）只需修改 `flowAgent.run`
- 底层 Agent 无需改动
- 支持自定义 Agent 实现（只需实现 `Agent` 接口）

### 2. 可组合性（Composability）
- 任何 `Agent` 都可被 `flowAgent` 包装
- 支持嵌套多层（Layered Supervisor、Plan-Execute-Replan）
- 可动态添加/移除子 Agent

### 3. 可观测性（Observability）
- `RunPath` 记录完整调用链
- `Session.events` 记录所有历史事件
- 每个 `AgentEvent` 带 `AgentName` 和 `Action`
- 支持流式输出，实时观察 Agent 思考过程

### 4. 灵活性（Flexibility）
- 支持双向 transfer（父 ↔ 子）
- 支持禁用向上 transfer（`WithDisallowTransferToParent`）
- 支持自定义历史改写（`WithHistoryRewriter`）

### 5. 安全性（Safety）
- Transfer 失败会返回明确错误，不会静默失败
- `returnDirectly` 确保 transfer 后立即返回，避免死循环
- 父子关系在 `SetSubAgents` 时建立，运行时不可变

## 设计模式

Transfer 机制综合运用了多种设计模式：

1. **策略模式（Strategy Pattern）**：底层 Agent 生成"意图"（Action），上层执行"决策"（流转）
2. **责任链模式（Chain of Responsibility）**：事件沿 Agent 链向上传播
3. **装饰器模式（Decorator Pattern）**：`flowAgent` 包装 `ChatModelAgent`，增强控制流能力
4. **命令模式（Command Pattern）**：`Action` 封装控制流指令
5. **观察者模式（Observer Pattern）**：`AsyncIterator` 流式发送事件

## 与手动实现的对比

| 方面 | 手动实现（如 deer-go） | ADK Transfer |
|------|----------------------|--------------|
| 工具定义 | 显式定义 `hand_to_planner` | 自动注入 `transfer_to_agent` |
| 工具数量 | 每个目标一个工具 | 始终只有 1 个工具 |
| 路由逻辑 | 手写 `router` lambda 解析 ToolCall | 框架自动拦截 Action |
| 历史管理 | 手动维护 `state.Messages` | 框架自动改写和管理 |
| 控制流 | 手动设置 `state.Goto` | 框架递归调用 `agentToRun.Run()` |
| 路径追踪 | 需自行实现 | 框架自动维护 `RunPath` |
| 可扩展性 | 每次新增需手写逻辑 | 声明式添加子 Agent |

## 最佳实践

### 1. 合理设计 Agent Description
Description 会出现在 Transfer 指令中，是模型做路由决策的关键依据：

```go
// ❌ 不好：描述不清晰
Description: "This is an agent."

// ✅ 好：清晰说明职责和能力
Description: "This agent can get the current weather for a given city."
```

### 2. 避免循环 Transfer
虽然框架不会死循环（因为 `returnDirectly`），但应避免设计导致 Agent 互相 transfer：

```go
// ❌ 不好：AgentA 和 AgentB 互为子 Agent
adk.SetSubAgents(ctx, agentA, []adk.Agent{agentB})
adk.SetSubAgents(ctx, agentB, []adk.Agent{agentA})

// ✅ 好：清晰的层级关系
adk.SetSubAgents(ctx, supervisor, []adk.Agent{agentA, agentB})
```

### 3. 使用 Exit 工具明确结束
RouterAgent 应该配置 `Exit` 工具，明确告知模型何时结束对话：

```go
agent, _ := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
    Name:        "RouterAgent",
    Description: "A router that transfers tasks to other agents.",
    Exit:        &adk.ExitTool{},  // ← 添加 exit 工具
})
```

### 4. 利用 RunPath 追踪调用链
在生产环境中，记录 `event.RunPath` 用于调试和分析：

```go
for {
    event, ok := iter.Next()
    if !ok {
        break
    }
    
    // 记录完整调用链
    log.Printf("Agent: %s, RunPath: %v, Action: %v", 
        event.AgentName, event.RunPath, event.Action)
}
```

## 总结

Eino ADK 的 Transfer 机制通过**单一工具 + 动态指令 + 分层架构**实现了优雅的多智能体协作：

- **简洁**：用户只需 `SetSubAgents`，框架自动完成工具注入、指令生成、控制流转
- **灵活**：支持任意层级嵌套、双向 transfer、自定义历史改写
- **高效**：工具数量不随 Agent 数量增长，模型看到完整信息做出更好决策
- **可靠**：职责分离、事件流透传、完整的错误处理

这是一个值得借鉴的"框架自动化 > 用户手动实现"的优秀设计！🎯

