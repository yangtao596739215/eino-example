# Coordinator（协调器）逻辑分析

## 一、概述

`coordinator.go` 实现了 **Coordinator（协调器）** 子图，它是整个 deer-go 系统的**入口 Agent**，负责接收用户问题、分析任务性质、检测语言，并决定下一步的流程走向。

### 在系统中的位置

```
用户问题 → START → Coordinator → BackgroundInvestigator/Planner
                        ↑
                    (系统入口)
```

### 核心职责

1. **任务理解**：分析用户问题的意图和复杂度
2. **语言检测**：识别用户使用的语言（locale）
3. **流程决策**：决定是否需要背景调查
4. **状态初始化**：设置全局状态的初始值

---

## 二、核心组件分析

### 2.1 `loadMsg` 函数（34-58行）

**作用**：加载 System Prompt 并构造 Coordinator 的初始消息

#### 实现逻辑

```go
func loadMsg(ctx context.Context, name string, opts ...any) (output []*schema.Message, err error) {
    err = compose.ProcessState[*model.State](ctx, func(_ context.Context, state *model.State) error {
        // 步骤1: 从配置中获取 Prompt 模板
        sysPrompt, err := infra.GetPromptTemplate(ctx, name)
        if err != nil {
            ilog.EventInfo(ctx, "get prompt template fail")
            return err
        }
        
        // 步骤2: 构建 Prompt 模板
        promptTemp := prompt.FromMessages(schema.Jinja2,
            schema.SystemMessage(sysPrompt),              // System 指令
            schema.MessagesPlaceholder("user_input", true), // 用户输入占位符
        )
        
        // 步骤3: 准备变量
        variables := map[string]any{
            "locale":              state.Locale,
            "max_step_num":        state.MaxStepNum,
            "max_plan_iterations": state.MaxPlanIterations,
            "CURRENT_TIME":        time.Now().Format("2006-01-02 15:04:05"),
            "user_input":          state.Messages,  // 👈 用户的原始消息
        }
        
        // 步骤4: 格式化 Prompt
        output, err = promptTemp.Format(ctx, variables)
        return err
    })
    return output, err
}
```

#### 关键特性

1. **动态 Prompt 加载**
   - 通过 `infra.GetPromptTemplate(ctx, name)` 获取
   - `name` 参数通常是 "coordinator"
   - Prompt 存储在配置文件或数据库中（如 `biz/prompts/coordinator.md`）

2. **变量注入**
   - `user_input`：用户的原始消息历史
   - `CURRENT_TIME`：当前时间，帮助 LLM 理解时效性问题
   - `locale`、`max_step_num`、`max_plan_iterations`：系统配置参数

3. **Jinja2 模板**
   - 支持条件渲染、循环等高级模板语法
   - 占位符 `MessagesPlaceholder` 用于插入消息列表

#### 输出示例

```
[
  {
    "role": "system",
    "content": "You are a task coordinator. Analyze the user's question and decide whether to hand it off to the planner. Detect the user's language and provide the locale (e.g., en-US, zh-CN)."
  },
  {
    "role": "user",
    "content": "What are the latest AI trends in 2025?"
  }
]
```

---

### 2.2 `router` 函数（60-79行）

**作用**：解析 LLM 的输出（Tool Call），提取语言信息，决定下一步路由

#### 实现逻辑

```go
func router(ctx context.Context, input *schema.Message, opts ...any) (output string, err error) {
    err = compose.ProcessState[*model.State](ctx, func(_ context.Context, state *model.State) error {
        defer func() {
            output = state.Goto  // 👈 返回路由目标
        }()
        
        state.Goto = compose.END  // 默认值：结束流程
        
        // 检查 LLM 是否调用了 hand_to_planner 工具
        if len(input.ToolCalls) > 0 && 
           input.ToolCalls[0].Function.Name == "hand_to_planner" {
            
            // 解析工具参数
            argMap := map[string]string{}
            _ = json.Unmarshal([]byte(input.ToolCalls[0].Function.Arguments), &argMap)
            
            // 提取 locale 并保存到状态
            state.Locale, _ = argMap["locale"]
            
            // 决定下一步：背景调查 or 直接规划
            if state.EnableBackgroundInvestigation {
                state.Goto = consts.BackgroundInvestigator  // 👈 去背景调查
            } else {
                state.Goto = consts.Planner  // 👈 直接规划
            }
        }
        return nil
    })
    return output, nil
}
```

#### 关键特性

1. **Tool Call 解析**
   ```go
   if len(input.ToolCalls) > 0 && 
      input.ToolCalls[0].Function.Name == "hand_to_planner"
   ```
   - LLM 决定交接给 Planner 时会调用 `hand_to_planner` 工具
   - 工具参数包含 `task_title` 和 `locale`

2. **语言检测**
   ```go
   state.Locale, _ = argMap["locale"]
   ```
   - LLM 从用户消息中推断语言
   - 典型值：`"en-US"`, `"zh-CN"`, `"ja-JP"` 等
   - 后续所有 Agent 都会使用这个 locale 生成对应语言的输出

3. **路由决策**
   ```go
   if state.EnableBackgroundInvestigation {
       state.Goto = consts.BackgroundInvestigator
   } else {
       state.Goto = consts.Planner
   }
   ```
   - **启用背景调查**：先搜索相关信息，再制定计划
   - **禁用背景调查**：直接进入规划阶段

4. **默认行为**
   ```go
   state.Goto = compose.END
   ```
   - 如果 LLM 没有调用工具（判断任务不适合处理），流程直接结束
   - 例如：用户输入不是有效的任务请求

#### 执行示例

**输入**（LLM 的输出）：
```json
{
  "role": "assistant",
  "content": "",
  "tool_calls": [
    {
      "function": {
        "name": "hand_to_planner",
        "arguments": "{\"task_title\": \"AI trends research\", \"locale\": \"en-US\"}"
      }
    }
  ]
}
```

**执行结果**：
```go
state.Locale = "en-US"
state.Goto = "background_investigator"  // (如果启用了背景调查)
output = "background_investigator"
```

---

### 2.3 `NewCAgent` 函数（82-113行）

**作用**：构建 Coordinator 子图

#### 子图结构

```
START → load → agent → router → END
```

#### 实现代码

```go
func NewCAgent[I, O any](ctx context.Context) *compose.Graph[I, O] {
    cag := compose.NewGraph[I, O]()
    
    // 定义 hand_to_planner 工具
    hand_to_planner := &schema.ToolInfo{
        Name: "hand_to_planner",
        Desc: "Handoff to planner agent to do plan.",
        ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
            "task_title": {
                Type:     schema.String,
                Desc:     "The title of the task to be handed off.",
                Required: true,
            },
            "locale": {
                Type:     schema.String,
                Desc:     "The user's detected language locale (e.g., en-US, zh-CN).",
                Required: true,
            },
        }),
    }
    
    // 创建带工具的 Chat Model
    coorModel, _ := infra.ChatModel.WithTools([]*schema.ToolInfo{hand_to_planner})
    
    // 添加三个节点
    _ = cag.AddLambdaNode("load", compose.InvokableLambdaWithOption(loadMsg))
    _ = cag.AddChatModelNode("agent", coorModel)
    _ = cag.AddLambdaNode("router", compose.InvokableLambdaWithOption(router))
    
    // 线性连接
    _ = cag.AddEdge(compose.START, "load")
    _ = cag.AddEdge("load", "agent")
    _ = cag.AddEdge("agent", "router")
    _ = cag.AddEdge("router", compose.END)
    
    return cag
}
```

#### 节点说明

| 节点名 | 类型 | 输入 | 输出 | 作用 |
|--------|------|------|------|------|
| `load` | LambdaNode | `string` | `[]*schema.Message` | 加载 Prompt，构造 LLM 输入 |
| `agent` | ChatModelNode | `[]*schema.Message` | `*schema.Message` | 调用 LLM 分析任务 |
| `router` | LambdaNode | `*schema.Message` | `string` | 解析 Tool Call，设置路由 |

#### Tool 定义：`hand_to_planner`

**作用**：让 LLM 决定是否交接给 Planner

**参数**：
- `task_title`（必需）：任务标题
- `locale`（必需）：用户语言

**LLM 的决策过程**：
```
用户输入: "帮我研究一下 2025 年的 AI 趋势"

LLM 思考:
  1. 这是一个研究任务 ✓
  2. 用户使用中文 → locale = "zh-CN"
  3. 任务标题: "AI 趋势研究"

LLM 输出:
  调用 hand_to_planner({
    "task_title": "AI 趋势研究",
    "locale": "zh-CN"
  })
```

---

## 三、完整执行流程

### 场景1：英文问题，启用背景调查

```
用户输入: "What are the latest AI trends in 2025?"
配置: state.EnableBackgroundInvestigation = true
```

#### 执行步骤

```
1️⃣ load 节点
   ├─ 读取 state.Messages = [{"role": "user", "content": "What are..."}]
   ├─ 加载 Prompt 模板: "biz/prompts/coordinator.md"
   ├─ 注入变量: user_input, CURRENT_TIME, locale, ...
   └─ 输出: [
         {"role": "system", "content": "You are a coordinator..."},
         {"role": "user", "content": "What are the latest AI trends in 2025?"}
       ]

2️⃣ agent 节点 (ChatModel)
   ├─ 输入: [System Message, User Message]
   ├─ LLM 分析:
   │  - 这是一个研究任务
   │  - 需要时效性信息（2025 年）
   │  - 语言: 英文 (en-US)
   ├─ 决策: 调用 hand_to_planner 工具
   └─ 输出: {
         "role": "assistant",
         "tool_calls": [{
           "function": {
             "name": "hand_to_planner",
             "arguments": "{\"task_title\": \"AI Trends 2025 Research\", \"locale\": \"en-US\"}"
           }
         }]
       }

3️⃣ router 节点
   ├─ 解析 ToolCall: hand_to_planner
   ├─ 提取参数:
   │  - task_title = "AI Trends 2025 Research"
   │  - locale = "en-US"
   ├─ 保存到状态: state.Locale = "en-US"
   ├─ 检查配置: state.EnableBackgroundInvestigation = true
   └─ 设置路由: state.Goto = "background_investigator"

4️⃣ 返回主图
   └─ agentHandOff 读取 state.Goto
      └─ 路由到 BackgroundInvestigator
```

### 场景2：中文问题，禁用背景调查

```
用户输入: "帮我分析一下 Go 语言的优势"
配置: state.EnableBackgroundInvestigation = false
```

#### 执行步骤

```
1️⃣ load 节点
   └─ 输出: [System Message, User Message("帮我分析...")]

2️⃣ agent 节点
   ├─ LLM 分析:
   │  - 任务: Go 语言优势分析
   │  - 语言: 中文 (zh-CN)
   ├─ 调用工具: hand_to_planner({
         "task_title": "Go语言优势分析",
         "locale": "zh-CN"
       })
   └─ 输出: Message with ToolCall

3️⃣ router 节点
   ├─ state.Locale = "zh-CN"
   ├─ state.EnableBackgroundInvestigation = false
   └─ state.Goto = "planner"  // 👈 直接规划

4️⃣ 返回主图
   └─ 路由到 Planner
```

### 场景3：非任务输入（闲聊）

```
用户输入: "你好，今天天气怎么样？"
```

#### 执行步骤

```
1️⃣ load 节点
   └─ 输出: [System Message, User Message]

2️⃣ agent 节点
   ├─ LLM 分析:
   │  - 这不是研究任务
   │  - 无需交接给 Planner
   ├─ 决策: 不调用工具
   └─ 输出: {
         "role": "assistant",
         "content": "抱歉，我是专注于研究任务的助手，无法回答天气问题。"
       }

3️⃣ router 节点
   ├─ 检查: len(input.ToolCalls) == 0
   └─ state.Goto = compose.END  // 👈 保持默认值

4️⃣ 返回主图
   └─ 流程结束
```

---

## 四、设计模式分析

### 4.1 门面模式（Facade Pattern）

**作用**：Coordinator 作为系统的统一入口，隐藏后续复杂的多 Agent 协作

```
用户 → Coordinator → [复杂的多Agent系统]
       (简单接口)    (内部复杂性被隐藏)
```

**优势**：
- ✅ 用户只需关注问题本身，无需理解系统架构
- ✅ 可以在 Coordinator 层做统一的验证和预处理
- ✅ 易于添加通用功能（如访问控制、日志、限流）

### 4.2 策略模式（Strategy Pattern）

**路由策略**：根据配置动态选择路由目标

```go
if state.EnableBackgroundInvestigation {
    state.Goto = consts.BackgroundInvestigator  // 策略A
} else {
    state.Goto = consts.Planner  // 策略B
}
```

**扩展**：未来可以添加更多策略
```go
if state.EnableFactChecking {
    state.Goto = consts.FactChecker
} else if state.EnableBackgroundInvestigation {
    state.Goto = consts.BackgroundInvestigator
} else {
    state.Goto = consts.Planner
}
```

### 4.3 责任链模式（Chain of Responsibility）

**流程**：Coordinator → BackgroundInvestigator → Planner

```
Coordinator:
  ├─ 职责: 任务理解、语言检测
  └─ 传递: 将任务交给下一个处理者

BackgroundInvestigator:
  ├─ 职责: 收集背景信息
  └─ 传递: 将增强的上下文交给 Planner

Planner:
  ├─ 职责: 制定详细计划
  └─ 传递: 将计划交给执行团队
```

---

## 五、Locale（语言）管理

### 5.1 语言检测机制

**LLM 自动检测**：
```
用户输入: "What are the AI trends?"
LLM 输出: locale = "en-US"

用户输入: "最新的 AI 趋势是什么？"
LLM 输出: locale = "zh-CN"

用户输入: "AIのトレンドは何ですか？"
LLM 输出: locale = "ja-JP"
```

**优势**：
- ✅ 无需用户手动选择语言
- ✅ 支持多语言混合输入
- ✅ 自动适配输出语言

### 5.2 Locale 的传播

```go
// Coordinator 设置
state.Locale = "zh-CN"

// Planner 使用
variables := map[string]any{
    "locale": state.Locale,  // "zh-CN"
    ...
}

// Researcher 使用
msg := []*schema.Message{
    schema.UserMessage(fmt.Sprintf("##locale\n\n %v", state.Locale)),
}

// Reporter 使用
// 根据 locale 生成对应语言的报告
```

**全局一致性**：
- 所有 Agent 使用相同的 `state.Locale`
- 确保输入和输出语言一致
- 提升用户体验

---

## 六、Tool Call 机制详解

### 6.1 Tool 定义

```go
hand_to_planner := &schema.ToolInfo{
    Name: "hand_to_planner",
    Desc: "Handoff to planner agent to do plan.",
    ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
        "task_title": {
            Type:     schema.String,
            Desc:     "The title of the task to be handed off.",
            Required: true,
        },
        "locale": {
            Type:     schema.String,
            Desc:     "The user's detected language locale (e.g., en-US, zh-CN).",
            Required: true,
        },
    }),
}
```

### 6.2 LLM 如何使用工具

**发送给 LLM 的 Prompt 包含**：
1. System Message: 你的角色和任务
2. User Message: 用户的问题
3. Tool Definition: hand_to_planner 的描述和参数

**LLM 的决策过程**：
```
1. 理解用户问题
2. 判断是否是研究任务
3. 如果是 → 调用 hand_to_planner
4. 如果不是 → 直接回复或拒绝
```

### 6.3 Tool Call 的解析

```go
// LLM 返回的数据结构
type Message struct {
    Role      string      `json:"role"`
    Content   string      `json:"content"`
    ToolCalls []ToolCall  `json:"tool_calls,omitempty"`
}

type ToolCall struct {
    Function struct {
        Name      string `json:"name"`       // "hand_to_planner"
        Arguments string `json:"arguments"`  // JSON 字符串
    } `json:"function"`
}

// 解析参数
argMap := map[string]string{}
json.Unmarshal([]byte(input.ToolCalls[0].Function.Arguments), &argMap)
locale := argMap["locale"]  // "zh-CN"
```

---

## 七、错误处理与边界情况

### 7.1 Prompt 加载失败

```go
sysPrompt, err := infra.GetPromptTemplate(ctx, name)
if err != nil {
    ilog.EventInfo(ctx, "get prompt template fail")
    return err  // 👈 子图执行失败
}
```

**影响**：
- 子图执行中断
- 主图捕获错误，流程终止
- 建议：添加默认 Prompt 作为 fallback

### 7.2 LLM 未调用工具

```go
if len(input.ToolCalls) > 0 && ... {
    // 处理工具调用
} else {
    state.Goto = compose.END  // 👈 默认行为：结束流程
}
```

**场景**：
- 用户输入是闲聊或无效请求
- LLM 判断任务不适合处理
- 结果：流程正常结束，返回 LLM 的回复（如果有）

### 7.3 Tool 参数解析失败

```go
argMap := map[string]string{}
_ = json.Unmarshal(...)  // 👈 忽略错误
state.Locale, _ = argMap["locale"]  // 👈 可能为空字符串
```

**潜在问题**：
- 如果 JSON 格式错误，`argMap` 为空
- `state.Locale` 会是空字符串
- 后续 Agent 可能使用默认语言

**改进建议**：
```go
if err := json.Unmarshal(...); err != nil {
    ilog.EventError(ctx, err, "parse_tool_args_fail")
    state.Goto = compose.END
    return nil
}
if state.Locale == "" {
    state.Locale = "en-US"  // 默认语言
}
```

---

## 八、性能与优化

### 8.1 潜在优化点

**1. 缓存 Prompt 模板**
```go
// 当前实现：每次都加载
sysPrompt, err := infra.GetPromptTemplate(ctx, name)

// 优化方案：启动时加载到内存
var coordinatorPrompt string
func init() {
    coordinatorPrompt, _ = loadPromptFromFile("coordinator.md")
}
```

**2. 并行处理（如果有多个工具）**
```go
// 当前只有一个工具，未来可以并行解析
var wg sync.WaitGroup
for _, toolCall := range input.ToolCalls {
    wg.Add(1)
    go func(tc ToolCall) {
        defer wg.Done()
        // 处理工具调用
    }(toolCall)
}
wg.Wait()
```

### 8.2 监控指标

**建议监控**：
- `hand_to_planner` 调用成功率
- `locale` 检测准确率（通过人工采样）
- Coordinator 执行延迟
- Prompt 加载失败率

---

## 九、总结

### 核心价值

Coordinator 实现了一个**智能的任务入口网关**：

1. **任务理解**：通过 LLM 分析用户意图
2. **语言适配**：自动检测并设置全局语言
3. **流程决策**：根据配置选择最优路径
4. **状态初始化**：为后续 Agent 准备必要的上下文

### 设计亮点

- ✅ **Tool-based 路由**：通过工具调用实现智能决策
- ✅ **语言自动检测**：无需用户手动选择
- ✅ **配置驱动**：通过 `EnableBackgroundInvestigation` 灵活控制
- ✅ **统一入口**：隐藏系统复杂性

### 架构图

```
                ┌──────────────────────────────────┐
                │         Coordinator              │
                └─────────────┬────────────────────┘
                              │
        ┌─────────────────────┼─────────────────────┐
        │                     │                     │
    ┌───▼───┐            ┌────▼────┐           ┌────▼────┐
    │ load  │───────────▶│ agent   │──────────▶│ router  │
    │       │            │(+tool)  │           │         │
    └───────┘            └─────────┘           └─────────┘
        │                     │                     │
        │                     │                     │
    [加载Prompt]         [LLM分析]            [解析ToolCall]
    [注入变量]           [调用工具]           [设置locale]
                         [检测语言]           [决定路由]
                                                   │
                                      ┌────────────┴───────────┐
                                      │                        │
                                 [Background-          [Planner]
                                  Investigator]
```

Coordinator 是整个系统的**智能前台**，确保每个任务都能被正确理解并分配到合适的处理流程！

