# BackgroundInvestigator（背景调查员）逻辑分析

## 一、概述

`investigator.go` 实现了 **BackgroundInvestigator（背景调查员）** 子图，这是一个**可选的预搜索优化组件**，在用户任务开始时预先搜索相关背景信息，为后续的 Planner 制定计划提供更全面的上下文。

### 整体流程

```
Coordinator → BackgroundInvestigator → Planner
             (可选，基于 EnableBackgroundInvestigation 配置)
```

---

## 二、核心组件分析

### 2.1 `search` 函数（34-72行）

**作用**：从 MCP 工具中动态查找搜索工具并执行背景调查搜索

#### 实现逻辑

```go
func search(ctx context.Context, name string, opts ...any) (output string, err error) {
    // 步骤1: 遍历所有 MCP Server，找到名字包含 "search" 的工具
    var searchTool tool.InvokableTool
    for _, cli := range infra.MCPServer {
        ts, err := mcp.GetTools(ctx, &mcp.Config{Cli: cli})
        for _, t := range ts {
            info, _ := t.Info(ctx)
            if strings.HasSuffix(info.Name, "search") {
                searchTool, _ = t.(tool.InvokableTool)
                break
            }
        }
    }
    
    // 步骤2: 使用最后一条消息（用户问题）作为搜索查询
    compose.ProcessState[*model.State](ctx, func(_ context.Context, state *model.State) error {
        args := map[string]any{
            "query": state.Messages[len(state.Messages)-1].Content,  // 用户的原始问题
        }
        argsBytes, _ := json.Marshal(args)
        result, err := searchTool.InvokableRun(ctx, string(argsBytes))
        
        // 步骤3: 将搜索结果保存到全局 state
        state.BackgroundInvestigationResults = result
        return nil
    })
}
```

#### 关键特性

1. **动态工具查找**
   - 遍历所有 MCP Server
   - 查找名字以 "search" 结尾的工具
   - 支持 Brave Search、Google Search 等多种搜索引擎

2. **搜索内容**
   - 直接使用用户的原始问题 `state.Messages[len(state.Messages)-1].Content`
   - 不经过 LLM 加工，保持问题原始语义

3. **结果存储**
   - 保存到 `state.BackgroundInvestigationResults`
   - 全局共享，Planner 可以直接访问

#### 执行示例

```
用户问题: "What's the latest news about AI in 2025?"

执行流程:
1. 从 MCP 找到 brave_search 工具
2. 调用 brave_search.InvokableRun(ctx, '{"query": "What\'s the latest news about AI in 2025?"}')
3. 获取搜索结果 (包含最新 AI 新闻摘要)
4. state.BackgroundInvestigationResults = "AI news summary: ..."
```

---

### 2.2 `bIRouter` 函数（74-83行）

**作用**：背景调查完成后的路由函数，固定返回 Planner

#### 实现逻辑

```go
func bIRouter(ctx context.Context, input string, opts ...any) (output string, err error) {
    err = compose.ProcessState[*model.State](ctx, func(_ context.Context, state *model.State) error {
        defer func() {
            output = state.Goto
        }()
        state.Goto = consts.Planner  // 固定路由到 Planner
        return nil
    })
    return output, nil
}
```

#### 特点

- **固定路由**：始终返回 `consts.Planner`，无条件判断
- **简单明确**：背景调查的唯一目的就是为 Planner 提供信息
- **状态驱动**：通过设置 `state.Goto` 实现动态路由

---

### 2.3 `NewBAgent` 函数（85-95行）

**作用**：构建 BackgroundInvestigator 子图

#### 子图结构

```
START → search → router → END
```

#### 实现代码

```go
func NewBAgent[I, O any](ctx context.Context) *compose.Graph[I, O] {
    cag := compose.NewGraph[I, O]()
    
    // 添加两个 Lambda 节点
    _ = cag.AddLambdaNode("search", compose.InvokableLambdaWithOption(search))
    _ = cag.AddLambdaNode("router", compose.InvokableLambdaWithOption(bIRouter))
    
    // 线性流程（无分支）
    _ = cag.AddEdge(compose.START, "search")
    _ = cag.AddEdge("search", "router")
    _ = cag.AddEdge("router", compose.END)
    
    return cag
}
```

#### 节点说明

| 节点名 | 类型 | 功能 |
|--------|------|------|
| `search` | LambdaNode | 执行背景搜索，保存结果到 state |
| `router` | LambdaNode | 设置下一步路由为 Planner |

#### 泛型说明

- `I, O` 为泛型参数，但实际节点内部使用固定类型（`string`）
- 这与 `coordinator.go`、`researcher.go` 等子图保持一致的接口风格
- 实际上这些泛型参数在节点实现中并未使用，是框架统一性的要求

---

## 三、在整体架构中的位置

### 3.1 如何被触发

在 `coordinator.go` 的 `router` 函数中：

```go
func router(ctx context.Context, input *schema.Message, opts ...any) (output string, err error) {
    compose.ProcessState[*model.State](ctx, func(_ context.Context, state *model.State) error {
        // ... 解析 Coordinator 的输出 ...
        
        if state.EnableBackgroundInvestigation {
            state.Goto = consts.BackgroundInvestigator  // 👈 触发背景调查
        } else {
            state.Goto = consts.Planner  // 直接进入规划
        }
        return nil
    })
}
```

**触发条件**：
- `state.EnableBackgroundInvestigation = true`
- 通常在用户初次提问时，Coordinator 决定是否需要背景调查

### 3.2 搜索结果的使用

在 `planner.go` 的 `load` 函数中：

```go
func load(ctx context.Context, name string, opts ...any) (output []*schema.Message, err error) {
    compose.ProcessState[*model.State](ctx, func(_ context.Context, state *model.State) error {
        var promptTemp *prompt.DefaultChatTemplate
        
        // 👇 使用背景调查结果
        if state.EnableBackgroundInvestigation && len(state.BackgroundInvestigationResults) > 0 {
            promptTemp = prompt.FromMessages(schema.Jinja2,
                schema.SystemMessage(sysPrompt),
                schema.MessagesPlaceholder("user_input", true),
                schema.UserMessage(fmt.Sprintf(
                    "background investigation results of user query: \n %s", 
                    state.BackgroundInvestigationResults  // 👈 注入到 Prompt
                )),
            )
        } else {
            promptTemp = prompt.FromMessages(schema.Jinja2,
                schema.SystemMessage(sysPrompt),
                schema.MessagesPlaceholder("user_input", true),
            )
        }
        
        output, _ = promptTemp.Format(ctx, schema.ChatModelInput{
            Messages: state.Messages,
        })
        return nil
    })
}
```

**使用方式**：
- 将搜索结果作为额外的 UserMessage 注入到 Planner 的 Prompt 中
- Planner LLM 可以基于这些背景信息制定更准确的计划

---

## 四、完整执行流程示例

### 场景：用户询问最新技术趋势

```
用户问题: "What are the emerging AI trends in 2025?"
配置: state.EnableBackgroundInvestigation = true
```

### 执行步骤

```
1️⃣ Coordinator 决策
   ├─ agent 节点: LLM 判断需要背景调查
   └─ router 节点: state.Goto = "background_investigator"

2️⃣ BackgroundInvestigator 执行
   ├─ search 节点:
   │  ├─ 从 MCP 找到 brave_search 工具
   │  ├─ 搜索 query="What are the emerging AI trends in 2025?"
   │  ├─ 获取结果: "Top AI trends include: multimodal models, ..."
   │  └─ state.BackgroundInvestigationResults = "Top AI trends include: ..."
   │
   └─ router 节点:
      └─ state.Goto = "planner"

3️⃣ Planner 制定计划
   ├─ load 节点:
   │  └─ Prompt 包含:
   │     - System: "You are a planner..."
   │     - User: 原始问题
   │     - User: "background investigation results: Top AI trends include: ..."  👈 关键
   │
   ├─ agent 节点:
   │  └─ LLM 基于背景信息制定详细计划:
   │     {
   │       "title": "AI Trends Research Report 2025",
   │       "steps": [
   │         {"title": "Deep dive into multimodal models", "step_type": "research"},
   │         {"title": "Analyze market impact", "step_type": "research"},
   │         {"title": "Synthesize findings", "step_type": "processing"}
   │       ]
   │     }
   │
   └─ router 节点: state.Goto = "research_team"

4️⃣ ResearchTeam 执行计划
   └─ (按计划执行各个步骤...)
```

---

## 五、设计模式分析

### 5.1 预搜索优化（Pre-Search Optimization）

**原理**：在制定详细计划之前，先获取必要的背景信息

**优势**：
- ✅ **信息增强**：Planner 拥有更多上下文，计划更准确
- ✅ **时间优化**：并行进行背景调查，而不是等到执行阶段才搜索
- ✅ **减少迭代**：减少因信息不足导致的重新规划次数

**类比**：类似于人类工作方式
```
普通方式: 直接制定计划 → 执行时发现信息不足 → 重新规划 ❌
优化方式: 先快速调研 → 基于背景信息制定计划 → 高效执行 ✅
```

### 5.2 与 Researcher 的对比

| 特性 | BackgroundInvestigator | Researcher |
|------|----------------------|------------|
| **执行时机** | Planner **之前** | Planner **之后** |
| **目的** | 为制定计划提供背景信息 | 执行计划中的具体研究任务 |
| **输入来源** | 用户原始问题 | 计划中的具体步骤描述 |
| **搜索深度** | **浅**（单次快速搜索） | **深**（多轮推理 + 多次工具调用） |
| **使用工具** | 简单搜索工具（MCP） | ReAct Agent + 多种工具 |
| **复杂度** | **低**（2个节点，线性流程） | **高**（3个节点，包含 react.Agent） |
| **输出形式** | 搜索结果文本 | 结构化研究报告 |
| **可选性** | 可选（基于配置） | 必选（计划中的步骤） |

### 5.3 状态管理模式

**共享状态字段**：

```go
type State struct {
    // ... 其他字段 ...
    
    // BackgroundInvestigator 相关
    EnableBackgroundInvestigation  bool   `json:"enable_background_investigation"`
    BackgroundInvestigationResults string `json:"background_investigation_results"`
}
```

**状态流转**：

```
Coordinator:
  └─ 设置: state.EnableBackgroundInvestigation (基于 LLM 判断)
  └─ 设置: state.Goto = "background_investigator"

BackgroundInvestigator:
  └─ 写入: state.BackgroundInvestigationResults (搜索结果)
  └─ 设置: state.Goto = "planner"

Planner:
  └─ 读取: state.BackgroundInvestigationResults (用于 Prompt 增强)
```

---

## 六、代码设计特点

### 6.1 动态工具发现

```go
// 不硬编码具体搜索工具，而是动态查找
for _, cli := range infra.MCPServer {
    ts, err := mcp.GetTools(ctx, &mcp.Config{Cli: cli})
    for _, t := range ts {
        info, _ := t.Info(ctx)
        if strings.HasSuffix(info.Name, "search") {  // 👈 基于命名约定
            searchTool, _ = t.(tool.InvokableTool)
            break
        }
    }
}
```

**优点**：
- ✅ 灵活性：支持任何符合命名约定的搜索工具
- ✅ 可扩展：新增 MCP 搜索工具无需修改代码
- ✅ 降耦合：不依赖具体搜索引擎实现

### 6.2 简洁的子图结构

```
START → search → router → END
```

**特点**：
- 无分支逻辑，纯线性流程
- 职责单一：搜索 + 路由
- 易于理解和维护

### 6.3 状态驱动的路由

```go
// 不通过返回值，而是通过修改 state.Goto 实现路由
state.Goto = consts.Planner
```

**优势**：
- 与整体架构的动态路由机制一致
- 通过 `agentHandOff` 函数统一处理路由逻辑
- 支持灵活的流程控制

---

## 七、性能与优化考虑

### 7.1 何时启用背景调查

**建议启用场景**：
- ✅ 用户问题涉及时效性信息（新闻、趋势）
- ✅ 用户问题需要事实查证
- ✅ 任务复杂度高，需要充分的背景信息

**建议禁用场景**：
- ❌ 用户问题是纯计算、逻辑推理任务
- ❌ 用户提供了足够的上下文信息
- ❌ 对响应速度要求极高

### 7.2 搜索结果质量控制

当前实现的潜在问题：
```go
// 问题：没有对搜索结果进行质量检查
state.BackgroundInvestigationResults = result
```

**改进建议**：
```go
// 可以添加结果过滤和摘要
if len(result) > 5000 {
    // 使用 LLM 对长文本进行摘要
    result = summarize(ctx, result)
}
state.BackgroundInvestigationResults = result
```

### 7.3 错误处理

当前实现：
```go
if err != nil {
    ilog.EventError(ctx, err, "search_result_error")
}
// 即使出错也继续执行
```

**改进空间**：
- 搜索失败时可以降级到无背景信息模式
- 添加重试逻辑
- 记录失败指标用于监控

---

## 八、总结

### 核心价值

BackgroundInvestigator 实现了一个**轻量级的预搜索优化**机制：

1. **提升计划质量**：为 Planner 提供及时的背景信息
2. **优化执行效率**：提前获取信息，减少后续重新规划
3. **灵活可配置**：可根据任务类型动态启用/禁用
4. **工具抽象良好**：动态发现 MCP 工具，易于扩展

### 设计亮点

- ✅ **职责单一**：只做背景搜索，不做深度研究
- ✅ **接口简洁**：仅 2 个节点，线性流程
- ✅ **状态共享**：通过全局 state 传递搜索结果
- ✅ **动态路由**：与整体架构的路由机制完美融合

### 与其他子图的协作

```
Coordinator (决策者)
    ↓
BackgroundInvestigator (背景调查员) ← 【本文档】
    ↓ [提供背景信息]
Planner (规划师)
    ↓ [制定计划]
ResearchTeam (任务调度)
    ↓
Researcher (深度研究员) ← 执行具体研究任务
```

这种**分层设计**体现了"快速预搜 + 深度研究"的两阶段信息获取策略，是多智能体协作的优秀实践！

