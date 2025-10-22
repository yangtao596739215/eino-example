# Researcher 执行逻辑分析

## 📖 概述

Researcher 是 deer-go 系统中负责**执行研究任务**的核心 Agent。它接收来自 ResearchTeam 调度的研究任务，通过 React Agent 框架调用各种工具（搜索、爬虫、MCP 工具等）完成信息收集和分析，最后将结果保存回执行计划中。

### 核心特点

- 🔧 **工具驱动**：使用 React Agent 框架，支持工具链式调用
- 🌐 **MCP 集成**：动态加载 MCP (Model Context Protocol) 工具
- 📊 **智能优化**：消息长度裁剪、流式检测等优化机制
- 🔄 **循环执行**：与 ResearchTeam 形成闭环，支持多步骤研究

---

## 🏗️ 整体架构

### 子图结构

```
┌─────────────────────────────────────────────┐
│         Researcher 子图 (Graph)              │
├─────────────────────────────────────────────┤
│                                             │
│  START                                      │
│    ↓                                        │
│  ┌──────────┐                              │
│  │  load    │ ← 加载当前步骤信息              │
│  └────┬─────┘                              │
│       ↓ []*schema.Message                  │
│  ┌──────────┐                              │
│  │  agent   │ ← React Agent 执行研究          │
│  └────┬─────┘                              │
│       ↓ *schema.Message                    │
│  ┌──────────┐                              │
│  │  router  │ ← 保存结果并返回调度中心         │
│  └────┬─────┘                              │
│       ↓ string                             │
│  END                                        │
│                                             │
└─────────────────────────────────────────────┘
```

### 代码结构

```go
func NewResearcher[I, O any](ctx context.Context) *compose.Graph[I, O] {
    cag := compose.NewGraph[I, O]()
    
    // 1. 加载 MCP 工具
    researchTools := []tool.BaseTool{}
    for _, cli := range infra.MCPServer {
        ts, err := mcp.GetTools(ctx, &mcp.Config{Cli: cli})
        researchTools = append(researchTools, ts...)
    }
    
    // 2. 创建 React Agent
    agent, err := react.NewAgent(ctx, &react.AgentConfig{
        MaxStep:               40,
        ToolCallingModel:      infra.ChatModel,
        ToolsConfig:           compose.ToolsNodeConfig{Tools: researchTools},
        MessageModifier:       modifyInputfunc,       // 消息优化
        StreamToolCallChecker: toolCallChecker,       // 流式检测
    })
    
    // 3. 包装为 Lambda
    agentLambda, _ := compose.AnyLambda(agent.Generate, agent.Stream, nil, nil)
    
    // 4. 构建节点链路
    _ = cag.AddLambdaNode("load", compose.InvokableLambdaWithOption(loadResearcherMsg))
    _ = cag.AddLambdaNode("agent", agentLambda)
    _ = cag.AddLambdaNode("router", compose.InvokableLambdaWithOption(routerResearcher))
    
    _ = cag.AddEdge(compose.START, "load")
    _ = cag.AddEdge("load", "agent")
    _ = cag.AddEdge("agent", "router")
    _ = cag.AddEdge("router", compose.END)
    
    return cag
}
```

---

## 🔍 节点详解

### 节点 1: load - 加载步骤信息

#### 功能

从全局 State 中提取当前需要执行的研究步骤，并构建 React Agent 所需的提示词。

#### 代码逻辑

```go
func loadResearcherMsg(ctx context.Context, name string, opts ...any) (output []*schema.Message, err error) {
    err = compose.ProcessState[*model.State](ctx, func(_ context.Context, state *model.State) error {
        // 1. 获取系统提示词模板
        sysPrompt, err := infra.GetPromptTemplate(ctx, name)
        if err != nil {
            return err
        }
        
        // 2. 创建提示词模板（系统提示 + 用户输入占位符）
        promptTemp := prompt.FromMessages(schema.Jinja2,
            schema.SystemMessage(sysPrompt),
            schema.MessagesPlaceholder("user_input", true),
        )
        
        // 3. 找到当前需要执行的步骤（第一个 ExecutionRes == nil 的）
        var curStep *model.Step
        for i := range state.CurrentPlan.Steps {
            if state.CurrentPlan.Steps[i].ExecutionRes == nil {
                curStep = &state.CurrentPlan.Steps[i]
                break
            }
        }
        
        if curStep == nil {
            panic("no step found")  // 不应该发生
        }
        
        // 4. 构建用户消息（包含任务信息）
        msg := []*schema.Message{}
        msg = append(msg,
            schema.UserMessage(fmt.Sprintf(
                "#Task\n\n##title\n\n %v \n\n##description\n\n %v \n\n##locale\n\n %v",
                curStep.Title, curStep.Description, state.Locale,
            )),
            schema.SystemMessage("IMPORTANT: DO NOT include inline citations..."),
        )
        
        // 5. 填充提示词变量
        variables := map[string]any{
            "locale":              state.Locale,
            "max_step_num":        state.MaxStepNum,
            "max_plan_iterations": state.MaxPlanIterations,
            "CURRENT_TIME":        time.Now().Format("2006-01-02 15:04:05"),
            "user_input":          msg,
        }
        
        // 6. 生成最终的消息列表
        output, err = promptTemp.Format(ctx, variables)
        return err
    })
    return output, err
}
```

#### 输出示例

```
[
  {
    "Role": "system",
    "Content": "You are `researcher` agent...\n\nCURRENT_TIME: 2025-01-15 14:30:00\n..."
  },
  {
    "Role": "user",
    "Content": "#Task\n\n##title\n\n 搜索 Go 1.23 新特性\n\n##description\n\n 调研 Go 语言最新版本的新功能\n\n##locale\n\n zh-CN"
  },
  {
    "Role": "system",
    "Content": "IMPORTANT: DO NOT include inline citations in the text..."
  }
]
```

#### 关键点

- ✅ **任务信息提取**：从 `state.CurrentPlan.Steps` 中找到当前步骤
- ✅ **提示词注入**：系统提示词 + 任务信息 + 格式要求
- ✅ **上下文信息**：包含 locale、时间等全局信息

---

### 节点 2: agent - React Agent 执行

#### 功能

使用 React Agent 框架，通过**思考-行动-观察**的循环模式执行研究任务。

#### React Agent 配置

```go
agent, err := react.NewAgent(ctx, &react.AgentConfig{
    MaxStep:               40,                      // 最大步骤数
    ToolCallingModel:      infra.ChatModel,         // 支持工具调用的模型
    ToolsConfig:           compose.ToolsNodeConfig{
        Tools: researchTools,                       // 可用工具列表
    },
    MessageModifier:       modifyInputfunc,         // 消息长度优化
    StreamToolCallChecker: toolCallChecker,         // 流式工具调用检测
})
```

#### 工具加载机制

```go
// 从 MCP 服务器动态加载工具
researchTools := []tool.BaseTool{}
for _, cli := range infra.MCPServer {
    ts, err := mcp.GetTools(ctx, &mcp.Config{Cli: cli})
    if err != nil {
        ilog.EventError(ctx, err, "builder_error")
    }
    researchTools = append(researchTools, ts...)
}
```

**可用工具类型：**
- 🔍 **web_search_tool**: 网络搜索
- 🌐 **crawl_tool**: URL 内容抓取
- 🛠️ **动态 MCP 工具**: GitHub、Google Maps、数据库等

#### React 执行循环

```
Step 1: Thought
  LLM 分析任务：需要搜索 Go 1.23 新特性
  ↓
Step 2: Action
  调用 web_search_tool("Go 1.23 new features")
  ↓
Step 3: Observation
  获取搜索结果（链接、摘要等）
  ↓
Step 4: Thought
  分析结果：需要获取详细内容
  ↓
Step 5: Action
  调用 crawl_tool("https://go.dev/blog/go1.23")
  ↓
Step 6: Observation
  获取完整文章内容
  ↓
Step 7: Thought
  信息足够，可以总结
  ↓
Step 8: Final Answer
  返回研究报告
```

#### 消息优化器

```go
func modifyInputfunc(ctx context.Context, input []*schema.Message) []*schema.Message {
    sum := 0
    maxLimit := 50000  // 单条消息最大长度
    
    for i := range input {
        if input[i] == nil {
            ilog.EventWarn(ctx, "modify_inputfunc_nil", "input", input[i])
            continue
        }
        
        l := len(input[i].Content)
        if l > maxLimit {
            // 裁剪过长的消息（保留后部）
            ilog.EventWarn(ctx, "modify_inputfunc_clip", "raw_len", l)
            input[i].Content = input[i].Content[l-maxLimit:]
        }
        sum += len(input[i].Content)
    }
    
    ilog.EventInfo(ctx, "modify_inputfunc", "sum", sum, "input_len", len(input))
    return input
}
```

**优化目的：**
- ⚡ 避免超长消息导致 API 调用失败
- 💰 减少 token 消耗
- 🎯 保留最相关的信息（保留后部）

#### 流式工具调用检测器

```go
func toolCallChecker(_ context.Context, sr *schema.StreamReader[*schema.Message]) (bool, error) {
    defer sr.Close()
    
    for {
        msg, err := sr.Recv()
        if err == io.EOF {
            return false, nil  // 没有工具调用
        }
        if err != nil {
            return false, err
        }
        
        if len(msg.ToolCalls) > 0 {
            return true, nil  // 检测到工具调用
        }
    }
}
```

**作用：**
- 🔄 流式响应中检测是否有工具调用
- ⚡ 提前中断，避免等待完整响应

---

### 节点 3: router - 保存结果并返回

#### 功能

将 React Agent 的研究结果保存到当前执行步骤中，并返回到 ResearchTeam 调度中心。

#### 代码逻辑

```go
func routerResearcher(ctx context.Context, input *schema.Message, opts ...any) (output string, err error) {
    last := input  // React Agent 的输出
    
    err = compose.ProcessState[*model.State](ctx, func(_ context.Context, state *model.State) error {
        defer func() {
            output = state.Goto  // 返回下一个节点名称
        }()
        
        // 找到当前正在执行的步骤（第一个 ExecutionRes == nil 的）
        for i, step := range state.CurrentPlan.Steps {
            if step.ExecutionRes == nil {
                // 保存研究结果
                str := strings.Clone(last.Content)
                state.CurrentPlan.Steps[i].ExecutionRes = &str
                break  // 只保存一次
            }
        }
        
        ilog.EventInfo(ctx, "researcher_end", "plan", state.CurrentPlan)
        
        // 返回到调度中心
        state.Goto = consts.ResearchTeam
        return nil
    })
    
    return output, nil
}
```

#### 状态变化

**执行前：**
```json
{
  "Steps": [
    {
      "Title": "搜索 Go 1.23 新特性",
      "ExecutionRes": null  // ← 未完成
    }
  ]
}
```

**执行后：**
```json
{
  "Steps": [
    {
      "Title": "搜索 Go 1.23 新特性",
      "ExecutionRes": "经过搜索，Go 1.23 的主要新特性包括：\n1. 改进的泛型支持...\n\nReferences:\n- [Go 1.23 Release Notes](https://go.dev/doc/go1.23)"
    }
  ]
}
```

#### 关键点

- ✅ **精确保存**：只更新第一个未完成的步骤
- ✅ **内存安全**：使用 `strings.Clone` 避免共享内存
- ✅ **循环回归**：设置 `state.Goto = ResearchTeam`

---

## 🔄 完整执行流程

### 场景示例

用户查询："研究 Go 1.23 的新特性和性能改进"

Planner 生成的计划：
```json
{
  "Steps": [
    {
      "StepType": "Research",
      "Title": "Go 1.23 新特性调研",
      "Description": "搜索并总结 Go 1.23 的主要新功能",
      "ExecutionRes": null
    },
    {
      "StepType": "Research",
      "Title": "性能改进分析",
      "Description": "对比 Go 1.22 和 1.23 的性能差异",
      "ExecutionRes": null
    }
  ]
}
```

### 执行流程

```
┌─────────────────────────────────────────────────────────┐
│ 第 1 轮：执行 Step 0                                     │
└─────────────────────────────────────────────────────────┘

ResearchTeam (调度)
  → 检查 Steps[0].ExecutionRes == null
  → state.Goto = "Researcher"
  ↓

Researcher.load (节点 1)
  → 从 state 读取 Steps[0]
  → 构建提示词：
      System: "You are researcher agent..."
      User: "Task: Go 1.23 新特性调研..."
  → 输出 []*schema.Message
  ↓

Researcher.agent (节点 2 - React Loop)
  → Step 1: Thought
      "需要搜索 Go 1.23 的新特性"
  
  → Step 2: Action
      web_search_tool(query="Go 1.23 new features")
  
  → Step 3: Observation
      搜索结果：
      - https://go.dev/blog/go1.23
      - https://tip.golang.org/doc/go1.23
  
  → Step 4: Thought
      "需要获取详细内容"
  
  → Step 5: Action
      crawl_tool(url="https://go.dev/blog/go1.23")
  
  → Step 6: Observation
      文章内容：
      "Go 1.23 introduces several improvements..."
  
  → Step 7: Thought
      "信息足够，可以总结了"
  
  → Step 8: Final Answer
      "# Go 1.23 新特性调研
      
      ## 主要新特性
      1. **泛型改进**：新增 min/max 内置函数
      2. **性能优化**：编译速度提升 15%
      3. **标准库增强**：slices、maps 包新增函数
      
      ## 详细说明
      ...
      
      ## References
      - [Go 1.23 Release Notes](https://go.dev/doc/go1.23)
      - [Go Blog: Go 1.23](https://go.dev/blog/go1.23)"
  
  → 输出 *schema.Message (Content = 上述报告)
  ↓

Researcher.router (节点 3)
  → 读取 agent 输出的 Message
  → 找到 Steps[0] (ExecutionRes == null)
  → 保存结果：Steps[0].ExecutionRes = Message.Content
  → 设置 state.Goto = "ResearchTeam"
  → 返回 "ResearchTeam"
  ↓

┌─────────────────────────────────────────────────────────┐
│ 第 2 轮：执行 Step 1                                     │
└─────────────────────────────────────────────────────────┘

ResearchTeam (调度)
  → 检查 Steps[0].ExecutionRes != null (已完成)
  → 检查 Steps[1].ExecutionRes == null (未完成)
  → state.Goto = "Researcher"
  ↓

Researcher.load
  → 从 state 读取 Steps[1]
  → 构建提示词：
      User: "Task: 性能改进分析..."
  ↓

Researcher.agent (React Loop)
  → 执行搜索、爬取、分析...
  → 输出性能对比报告
  ↓

Researcher.router
  → 保存结果：Steps[1].ExecutionRes = 报告内容
  → 返回 "ResearchTeam"
  ↓

ResearchTeam (调度)
  → 检查所有 Steps 都有 ExecutionRes
  → 所有步骤完成！
  → state.Goto = "Reporter"
```

---

## 📊 数据流图

```
┌──────────────┐
│    State     │ (全局状态)
│  CurrentPlan │
│   - Steps[0] │
│   - Steps[1] │
└──────┬───────┘
       │
       │ 读取当前步骤
       ↓
┌──────────────┐
│     load     │
│ (提取步骤信息) │
└──────┬───────┘
       │
       │ []*schema.Message
       │ (系统提示 + 任务描述)
       ↓
┌──────────────┐
│    agent     │
│ (React Agent)│
│  ┌────────┐  │
│  │ Thought│  │
│  │ Action │  │
│  │Observ. │  │ ← 工具：web_search, crawl, MCP...
│  │  ...   │  │
│  │Final   │  │
│  └────────┘  │
└──────┬───────┘
       │
       │ *schema.Message
       │ (研究报告)
       ↓
┌──────────────┐
│    router    │
│  (保存结果)   │
└──────┬───────┘
       │
       │ 写入 Steps[i].ExecutionRes
       ↓
┌──────────────┐
│    State     │ (更新后)
│  CurrentPlan │
│   - Steps[0] │ ✓ ExecutionRes: "报告内容"
│   - Steps[1] │
└──────────────┘
```

---

## 🛠️ 工具集成详解

### MCP 工具动态加载

```go
// 配置文件: deer-go.yaml
mcp_servers:
  - name: "brave-search"
    command: "npx"
    args: ["-y", "@modelcontextprotocol/server-brave-search"]
    env:
      BRAVE_API_KEY: "your-api-key"
  
  - name: "github"
    command: "npx"
    args: ["-y", "@modelcontextprotocol/server-github"]
    env:
      GITHUB_PERSONAL_ACCESS_TOKEN: "your-token"

// 加载过程
for _, cli := range infra.MCPServer {
    // cli 是已连接的 MCP 客户端
    ts, err := mcp.GetTools(ctx, &mcp.Config{Cli: cli})
    // ts = [brave_web_search, github_search_repositories, ...]
    researchTools = append(researchTools, ts...)
}
```

### 内置工具 vs MCP 工具

| 类型 | 示例 | 加载方式 | 特点 |
|------|------|---------|------|
| **内置工具** | web_search_tool, crawl_tool | 代码内置 | 固定、可靠 |
| **MCP 工具** | brave_search, github_search | 动态加载 | 灵活、可扩展 |

### React Agent 工具调用流程

```
LLM 输出：
{
  "ToolCalls": [
    {
      "Function": {
        "Name": "web_search_tool",
        "Arguments": '{"query": "Go 1.23 features"}'
      }
    }
  ]
}
  ↓
ToolsNode 执行工具
  ↓
工具返回结果：
{
  "Role": "tool",
  "Content": '[{"title": "Go 1.23...", "url": "..."}]'
}
  ↓
再次输入 LLM (包含工具结果)
  ↓
LLM 继续思考 / 调用更多工具 / 返回最终答案
```

---

## ⚡ 性能优化策略

### 1. 消息长度裁剪

```go
// 问题：历史消息可能包含大量工具结果，导致超长
// 解决：裁剪单条消息到 50000 字符

if len(input[i].Content) > 50000 {
    input[i].Content = input[i].Content[len-50000:]  // 保留后部
}
```

**优势：**
- ✅ 避免 API 调用失败（超过 token 限制）
- ✅ 减少成本（少发送无关内容）
- ✅ 保留最相关信息（最新的对话和工具结果）

### 2. 流式工具调用检测

```go
// 问题：流式响应需要等待完整输出才知道是否有工具调用
// 解决：提前检测 ToolCalls 字段

if len(msg.ToolCalls) > 0 {
    return true, nil  // 立即返回，无需等待完整响应
}
```

**优势：**
- ⚡ 减少等待时间
- 🔄 更快的 React 循环迭代

### 3. 最大步骤数限制

```go
MaxStep: 40  // React Agent 最多执行 40 步
```

**优势：**
- 🛡️ 防止无限循环
- 💰 控制成本
- ⏱️ 保证响应时间

---

## 🎯 最佳实践

### 1. 提示词设计

```markdown
系统提示词要点：
✓ 明确角色定位 (researcher agent)
✓ 说明可用工具类型
✓ 强调工具使用规则（何时用、如何用）
✓ 输出格式要求（Markdown、章节结构）
✓ 引用规范（References section）
✓ 语言要求（locale）
```

### 2. 步骤粒度控制

```go
// ✅ 好的步骤划分
Steps: [
  {Title: "搜索基础信息", Description: "搜索 Go 1.23 官方文档"},
  {Title: "性能测试", Description: "查找性能对比数据"},
  {Title: "社区反馈", Description: "搜索开发者评价"},
]

// ❌ 步骤过于粗糙
Steps: [
  {Title: "完整调研", Description: "调研 Go 1.23 的所有信息"},
]
```

**原因：**
- 细粒度步骤更容易完成
- React Agent 的 40 步限制更合理
- 结果更结构化

### 3. 工具选择策略

```go
// 在 Prompt 中引导工具选择
"1. Use web_search_tool for general searches
 2. Use crawl_tool only when detailed content is needed
 3. Use specialized MCP tools when available"
```

### 4. 错误处理

```go
// 工具加载失败不应中断整个流程
for _, cli := range infra.MCPServer {
    ts, err := mcp.GetTools(ctx, &mcp.Config{Cli: cli})
    if err != nil {
        ilog.EventError(ctx, err, "builder_error")  // 记录但继续
        continue
    }
    researchTools = append(researchTools, ts...)
}
```

---

## 🔧 调试技巧

### 1. 查看执行日志

```go
ilog.EventInfo(ctx, "researcher_end", "plan", state.CurrentPlan)
ilog.EventDebug(ctx, "researcher_end", "research_tools", len(researchTools))
```

### 2. 监控消息长度

```go
ilog.EventInfo(ctx, "modify_inputfunc", "sum", sum, "input_len", len(input))
ilog.EventWarn(ctx, "modify_inputfunc_clip", "raw_len", l)
```

### 3. React 循环追踪

在 React Agent 配置中启用详细日志：
```go
// 查看每一步的 Thought、Action、Observation
```

---

## 📊 性能指标

### 典型执行时间

| 场景 | 步骤数 | React 迭代 | 总时间 |
|------|--------|-----------|--------|
| 简单搜索 | 1 | 3-5 | 10-20s |
| 深度调研 | 1 | 10-15 | 30-60s |
| 多步骤研究 | 3 | 每步 5-10 | 60-180s |

### Token 消耗估算

```
单次 Researcher 执行：
- 系统提示词：~2000 tokens
- 任务描述：~200 tokens
- React 循环（10 步）：
  - 每步 LLM 调用：~1000 tokens
  - 工具结果：~500 tokens/次
  - 小计：10 * 1500 = 15000 tokens
- 最终输出：~2000 tokens

总计：~19000 tokens (输入 + 输出)
```

---

## 🔗 与其他组件的交互

### 上游：ResearchTeam

```go
// ResearchTeam 决定调用 Researcher
state.Goto = consts.Researcher
```

### 下游：返回 ResearchTeam

```go
// Researcher 完成后返回
state.Goto = consts.ResearchTeam
```

### 数据交互

```go
// 读取
curStep := state.CurrentPlan.Steps[i]  // 获取任务

// 写入
state.CurrentPlan.Steps[i].ExecutionRes = &result  // 保存结果
```

---

## 🚀 扩展建议

### 1. 增加缓存机制

```go
// 缓存已搜索过的内容，避免重复调用
type ResearchCache struct {
    queries map[string]*schema.Message
}
```

### 2. 支持并行研究

```go
// 如果多个步骤相互独立，可以并行执行
// 使用 compose.Parallel 或 goroutine
```

### 3. 结果质量评估

```go
// 添加质量检查节点
_ = cag.AddLambdaNode("quality_check", qualityCheckFunc)
_ = cag.AddBranch("quality_check", compose.NewGraphBranch(func(...) {
    if quality < threshold {
        return "agent", nil  // 重新研究
    }
    return "router", nil
}, ...))
```

---

## 📖 总结

**Researcher 的核心价值：**

1. 🎯 **智能研究执行器**：使用 React Agent 框架自主完成信息收集
2. 🔧 **工具集成中心**：支持内置工具和动态 MCP 工具
3. 📊 **结果标准化**：输出结构化的 Markdown 研究报告
4. 🔄 **循环协作**：与 ResearchTeam 形成完美的任务执行闭环
5. ⚡ **性能优化**：消息裁剪、流式检测等优化机制

**设计亮点：**

- ✅ **模块化**：三个节点职责清晰（加载、执行、保存）
- ✅ **可扩展**：通过 MCP 动态加载新工具
- ✅ **健壮性**：消息长度限制、最大步骤数控制
- ✅ **可观测**：完善的日志记录

Researcher 是 deer-go 系统中最核心的执行单元，通过精妙的设计将 LLM 的推理能力和工具的信息获取能力完美结合！🎉

---

**版权所有 © 2025 CloudWeGo Authors**

