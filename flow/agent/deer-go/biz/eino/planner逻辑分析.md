# Planner（规划师）逻辑分析

## 一、概述

`planner.go` 实现了 **Planner（规划师）** 子图，它是整个 deer-go 系统的**核心决策 Agent**，负责将用户任务分解为具体的、可执行的研究计划。

### 在系统中的位置

```
Coordinator → BackgroundInvestigator → Planner → Human/ResearchTeam
                     (可选)              ↑ ↓
                                   (可能需要多次迭代)
```

### 核心职责

1. **任务分解**：将复杂任务拆分为多个可执行步骤
2. **上下文评估**：判断是否有足够信息制定计划
3. **步骤分类**：区分研究类（research）和处理类（processing）步骤
4. **计划优化**：结合背景调查结果优化计划质量

---

## 二、核心组件分析

### 2.1 `loadPlannerMsg` 函数（35-68行）

**作用**：加载 Planner 的 Prompt 模板，并根据是否有背景调查结果构造不同的输入

#### 实现逻辑

```go
func loadPlannerMsg(ctx context.Context, name string, opts ...any) (output []*schema.Message, err error) {
    err = compose.ProcessState[*model.State](ctx, func(_ context.Context, state *model.State) error {
        // 步骤1: 获取 Prompt 模板
        sysPrompt, err := infra.GetPromptTemplate(ctx, name)
        if err != nil {
            ilog.EventInfo(ctx, "get prompt template fail")
            return err
        }
        
        // 步骤2: 根据是否有背景调查结果构造不同的 Prompt
        var promptTemp *prompt.DefaultChatTemplate
        if state.EnableBackgroundInvestigation && len(state.BackgroundInvestigationResults) > 0 {
            // 情况A: 有背景调查结果
            promptTemp = prompt.FromMessages(schema.Jinja2,
                schema.SystemMessage(sysPrompt),
                schema.MessagesPlaceholder("user_input", true),
                schema.UserMessage(fmt.Sprintf(
                    "background investigation results of user query: \n %s", 
                    state.BackgroundInvestigationResults  // 👈 注入背景信息
                )),
            )
        } else {
            // 情况B: 无背景调查结果
            promptTemp = prompt.FromMessages(schema.Jinja2,
                schema.SystemMessage(sysPrompt),
                schema.MessagesPlaceholder("user_input", true),
            )
        }
        
        // 步骤3: 准备变量
        variables := map[string]any{
            "locale":              state.Locale,              // 用户语言
            "max_step_num":        state.MaxStepNum,          // 最大步骤数
            "max_plan_iterations": state.MaxPlanIterations,   // 最大迭代次数
            "CURRENT_TIME":        time.Now().Format("2006-01-02 15:04:05"),
            "user_input":          state.Messages,            // 用户消息历史
        }
        
        // 步骤4: 格式化 Prompt
        output, err = promptTemp.Format(ctx, variables)
        return err
    })
    return output, err
}
```

#### 关键特性

1. **动态 Prompt 增强**

   **无背景信息时**：
   ```
   [System Message]: "You are a planner. Create a research plan..."
   [User Message]: "What are the AI trends in 2025?"
   ```

   **有背景信息时**：
   ```
   [System Message]: "You are a planner. Create a research plan..."
   [User Message]: "What are the AI trends in 2025?"
   [User Message]: "background investigation results of user query:
                     Top AI trends include multimodal models, AGI progress..."
   ```

2. **配置参数传递**

   ```go
   variables := map[string]any{
       "max_step_num":        5,    // LLM 知道最多创建 5 个步骤
       "max_plan_iterations": 3,    // LLM 知道最多可以重新规划 3 次
       "locale":              "zh-CN",  // LLM 生成中文计划
   }
   ```

3. **时间感知**

   ```go
   "CURRENT_TIME": "2025-10-22 14:30:00"
   ```
   - 帮助 LLM 理解时效性问题
   - 例如："最新的AI趋势" → LLM知道参考 2025年的数据

---

### 2.2 `routerPlanner` 函数（70-98行）

**作用**：解析 LLM 生成的 Plan，决定下一步路由

#### 实现逻辑

```go
func routerPlanner(ctx context.Context, input *schema.Message, opts ...any) (output string, err error) {
    err = compose.ProcessState[*model.State](ctx, func(_ context.Context, state *model.State) error {
        defer func() {
            output = state.Goto  // 返回路由目标
        }()
        
        state.Goto = compose.END  // 默认值
        state.CurrentPlan = &model.Plan{}
        
        // 步骤1: 尝试解析 LLM 输出为 Plan 结构体
        err = json.Unmarshal([]byte(input.Content), state.CurrentPlan)
        
        if err != nil {  // ❌ 解析失败
            ilog.EventInfo(ctx, "gen_plan_fail", "input.Content", input.Content, "err", err)
            
            if state.PlanIterations > 0 {
                // 已经重试过 → 直接生成报告
                state.Goto = consts.Reporter
                return nil
            }
            // 首次失败 → 终止流程
            return nil  // state.Goto = END
        }
        
        // ✅ 解析成功
        ilog.EventInfo(ctx, "gen_plan_ok", "plan", state.CurrentPlan)
        state.PlanIterations++  // 计数器 +1
        
        // 步骤2: 判断是否有足够上下文
        if state.CurrentPlan.HasEnoughContext {
            // 信息充足 → 直接进入执行阶段（跳过 Human）
            state.Goto = consts.Reporter
            return nil
        }
        
        // 步骤3: 信息不足 → 请求人工确认
        state.Goto = consts.Human  // TODO: 改成 human_feedback
        return nil
    })
    return output, nil
}
```

#### 路由决策树

```
                    ┌─────────────────────┐
                    │  routerPlanner      │
                    └──────────┬──────────┘
                               │
                    ┌──────────▼──────────┐
                    │ JSON 解析 Plan      │
                    └──────────┬──────────┘
                               │
                ┌──────────────┴──────────────┐
                │                             │
          ❌ 解析失败                      ✅ 解析成功
                │                             │
    ┌───────────▼──────────┐      ┌──────────▼──────────┐
    │ PlanIterations > 0?  │      │ PlanIterations++    │
    └───────────┬──────────┘      └──────────┬──────────┘
                │                             │
        ┌───────┴───────┐         ┌──────────▼──────────┐
        │               │         │ HasEnoughContext?   │
       Yes             No         └──────────┬──────────┘
        │               │                     │
        ↓               ↓             ┌───────┴───────┐
   [Reporter]        [END]           Yes             No
                                      │               │
                                      ↓               ↓
                                 [Reporter]        [Human]
                                (直接执行)      (人工确认)
```

#### 关键特性

1. **容错机制**

   ```go
   if err != nil {  // Plan 解析失败
       if state.PlanIterations > 0 {
           state.Goto = consts.Reporter  // 已重试 → 降级处理
       } else {
           state.Goto = compose.END      // 首次失败 → 终止
       }
   }
   ```

   **场景**：
   - LLM 输出格式错误（未严格遵循 JSON schema）
   - LLM 返回纯文本而非 JSON
   - 降级策略：如果已经迭代过，使用之前的部分结果

2. **自适应流程**

   ```go
   if state.CurrentPlan.HasEnoughContext {
       state.Goto = consts.Reporter  // 跳过 Human 和 ResearchTeam
   } else {
       state.Goto = consts.Human  // 需要人工确认
   }
   ```

   **`HasEnoughContext` 的判断**：
   - LLM 自我评估：是否有足够信息制定可执行计划
   - `true`：任务明确，不需要更多澄清 → 直接执行
   - `false`：任务模糊或缺少关键信息 → 请求用户反馈

3. **迭代计数**

   ```go
   state.PlanIterations++
   ```
   - 记录 Planner 被调用的次数
   - 用于限制重新规划的次数（避免无限循环）
   - 与 `max_plan_iterations` 配合使用

---

### 2.3 `NewPlanner` 函数（100-115行）

**作用**：构建 Planner 子图

#### 子图结构

```
START → load → agent → router → END
```

#### 实现代码

```go
func NewPlanner[I, O any](ctx context.Context) *compose.Graph[I, O] {
    cag := compose.NewGraph[I, O]()
    
    // 添加三个节点
    _ = cag.AddLambdaNode("load", compose.InvokableLambdaWithOption(loadPlannerMsg))
    _ = cag.AddChatModelNode("agent", infra.PlanModel)  // 👈 使用专门的 PlanModel
    _ = cag.AddLambdaNode("router", compose.InvokableLambdaWithOption(routerPlanner))
    
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
| `load` | LambdaNode | `string` | `[]*schema.Message` | 构造增强的 Prompt（可能包含背景信息） |
| `agent` | ChatModelNode | `[]*schema.Message` | `*schema.Message` | LLM 生成结构化 Plan（JSON） |
| `router` | LambdaNode | `*schema.Message` | `string` | 解析 Plan，决定路由 |

#### 特殊配置

```go
_ = cag.AddChatModelNode("agent", infra.PlanModel)
```

**`PlanModel` vs. `ChatModel`**：
- 可能使用不同的模型（如 GPT-4 for planning, GPT-3.5 for chat）
- 可能使用不同的参数（temperature, top_p）
- 专门优化用于结构化输出

---

## 三、Plan 数据结构

### 3.1 Plan 结构体定义

```go
type Plan struct {
    Locale           string `json:"locale" validate:"required"`
    HasEnoughContext bool   `json:"has_enough_context" validate:"required"`  // 👈 关键字段
    Thought          string `json:"thought" validate:"required"`
    Title            string `json:"title" validate:"required"`
    Steps            []Step `json:"steps"`
}

type Step struct {
    NeedWebSearch bool     `json:"need_web_search" validate:"required"`
    Title         string   `json:"title" validate:"required"`
    Description   string   `json:"description" validate:"required"`
    StepType      StepType `json:"step_type" validate:"required"`  // "research" or "processing"
    ExecutionRes  *string  `json:"execution_res,omitempty"`        // 执行结果（初始为 nil）
}
```

### 3.2 字段含义

| 字段 | 类型 | 作用 |
|------|------|------|
| `Locale` | `string` | 计划的语言（继承自 Coordinator） |
| `HasEnoughContext` | `bool` | **是否有足够上下文**（决定流程走向） |
| `Thought` | `string` | LLM 的思考过程（为什么这样规划） |
| `Title` | `string` | 任务标题 |
| `Steps` | `[]Step` | 具体步骤列表 |

**Step 字段**：

| 字段 | 类型 | 作用 |
|------|------|------|
| `NeedWebSearch` | `bool` | 是否需要联网搜索 |
| `Title` | `string` | 步骤标题 |
| `Description` | `string` | 详细描述 |
| `StepType` | `"research"`/`"processing"` | 步骤类型（决定路由到 Researcher 或 Coder） |
| `ExecutionRes` | `*string` | 执行结果（初始 `nil`，执行后填充） |

### 3.3 Plan 示例

```json
{
  "locale": "en-US",
  "has_enough_context": true,
  "thought": "The user wants to know about AI trends in 2025. Based on the background investigation results, I have enough information to create a comprehensive research plan.",
  "title": "AI Trends Research 2025",
  "steps": [
    {
      "need_web_search": true,
      "title": "Research Multimodal AI Models",
      "description": "Investigate the latest developments in multimodal AI, including GPT-4V, Gemini, and their applications.",
      "step_type": "research",
      "execution_res": null
    },
    {
      "need_web_search": true,
      "title": "Analyze AGI Progress",
      "description": "Examine recent progress toward Artificial General Intelligence and key milestones.",
      "step_type": "research",
      "execution_res": null
    },
    {
      "need_web_search": false,
      "title": "Generate Comparison Charts",
      "description": "Create charts comparing different AI models' capabilities using Python matplotlib.",
      "step_type": "processing",
      "execution_res": null
    }
  ]
}
```

---

## 四、完整执行流程

### 场景1：有背景信息，信息充足

```
用户问题: "What are the latest AI trends in 2025?"
背景调查结果: "Top AI trends include multimodal models, AGI progress..."
```

#### 执行步骤

```
1️⃣ load 节点
   ├─ 检查: state.BackgroundInvestigationResults 不为空
   ├─ 构造 Prompt:
   │  - System: "You are a planner..."
   │  - User: "What are the latest AI trends in 2025?"
   │  - User: "background investigation results: ..." 👈 额外信息
   ├─ 变量:
   │  - locale: "en-US"
   │  - max_step_num: 5
   │  - max_plan_iterations: 3
   └─ 输出: [System Message, 2x User Messages]

2️⃣ agent 节点 (PlanModel)
   ├─ 输入: 增强的 Prompt（含背景信息）
   ├─ LLM 分析:
   │  - 基于背景信息，已经了解主要趋势
   │  - 有足够上下文制定计划
   │  - 生成 3 个步骤：2x research + 1x processing
   ├─ 决策: has_enough_context = true
   └─ 输出: {
         "role": "assistant",
         "content": "{\"locale\": \"en-US\", \"has_enough_context\": true, ...}"
       }

3️⃣ router 节点
   ├─ 解析 JSON: ✅ 成功
   ├─ state.CurrentPlan = {...}
   ├─ state.PlanIterations = 1
   ├─ 检查: has_enough_context = true
   └─ state.Goto = "reporter"  👈 跳过 Human，直接执行

4️⃣ 返回主图
   └─ agentHandOff → Reporter
```

**特点**：
- 背景信息帮助 LLM 快速决策
- 无需人工介入，自动进入执行
- 效率最高的流程

### 场景2：无背景信息，信息不足

```
用户问题: "帮我研究一下那个项目"
背景调查: 未启用
```

#### 执行步骤

```
1️⃣ load 节点
   ├─ 检查: state.BackgroundInvestigationResults 为空
   ├─ 构造 Prompt:
   │  - System: "You are a planner..."
   │  - User: "帮我研究一下那个项目"
   └─ 输出: [System Message, 1x User Message]

2️⃣ agent 节点
   ├─ LLM 分析:
   │  - "那个项目"指的是什么？缺少关键信息
   │  - 无法制定具体的研究计划
   │  - has_enough_context = false
   ├─ 生成 Plan:
   │  {
   │    "has_enough_context": false,
   │    "thought": "The user mentioned '那个项目' but didn't specify which project. Need clarification.",
   │    "title": "项目研究",
   │    "steps": []  // 可能为空或只有占位符步骤
   │  }
   └─ 输出: JSON Plan

3️⃣ router 节点
   ├─ 解析: ✅ 成功
   ├─ state.CurrentPlan = {...}
   ├─ state.PlanIterations = 1
   ├─ 检查: has_enough_context = false  👈 关键
   └─ state.Goto = "human_feedback"

4️⃣ 返回主图
   └─ agentHandOff → Human
      ├─ 等待用户澄清："那个项目"是什么？
      └─ 用户反馈后:
         - 选项A: 编辑计划 → 返回 Planner
         - 选项B: 接受计划 → 进入 ResearchTeam
```

### 场景3：Plan 解析失败，重试

```
首次 Planner 执行: LLM 输出格式错误
state.PlanIterations = 1
```

#### 执行步骤

```
1️⃣ router 节点（首次）
   ├─ 解析 JSON: ❌ 失败
   ├─ 检查: state.PlanIterations = 1 > 0
   └─ state.Goto = "reporter"  // 降级：用部分结果生成报告

2️⃣ 返回主图
   └─ agentHandOff → Reporter
      └─ Reporter 基于已有信息生成报告
```

**降级策略**：
- 避免因格式错误导致整个流程失败
- 如果已经迭代过，说明可能已有部分有用信息
- 直接生成报告，而不是完全放弃

---

## 五、背景信息的作用

### 5.1 对比：有/无背景信息

| 特性 | 无背景信息 | 有背景信息 |
|------|----------|----------|
| **Prompt 长度** | 短 | 长（+背景摘要） |
| **上下文丰富度** | 低 | 高 |
| **has_enough_context** | 更可能为 `false` | 更可能为 `true` |
| **需要人工确认** | 概率高 | 概率低 |
| **计划质量** | 可能较泛 | 更具体、更针对性 |
| **步骤数量** | 可能较少 | 可能更多、更详细 |

### 5.2 背景信息的传递链

```
BackgroundInvestigator:
  └─ 搜索 "AI trends 2025"
  └─ 保存: state.BackgroundInvestigationResults = "Multimodal AI, AGI, ..."

Planner:
  └─ load 节点读取: state.BackgroundInvestigationResults
  └─ 注入到 Prompt:
      UserMessage("background investigation results: Multimodal AI, AGI, ...")
  └─ LLM 基于背景生成更准确的计划

Reporter:
  └─ （间接受益）Planner 生成的步骤更具体
  └─ Researcher 执行更有针对性
```

---

## 六、Planner 与 Human 的交互

### 6.1 触发 Human 的条件

```go
if state.CurrentPlan.HasEnoughContext {
    state.Goto = consts.Reporter  // 跳过 Human
} else {
    state.Goto = consts.Human  // 进入人工确认
}
```

### 6.2 Human 的两种响应

**响应A：接受计划**
```go
state.InterruptFeedback = consts.AcceptPlan
state.Goto = consts.ResearchTeam  // 开始执行
```

**响应B：编辑计划**
```go
state.InterruptFeedback = consts.EditPlan
state.Goto = consts.Planner  // 👈 返回 Planner 重新规划
```

### 6.3 迭代循环

```
Planner (第1次)
  └─ has_enough_context = false
     └─ Human: "请添加关于AI安全的研究"
        └─ Planner (第2次)
           ├─ 结合用户反馈
           ├─ 重新生成计划
           └─ has_enough_context = true
              └─ Reporter (开始执行)
```

**循环终止**：
- `has_enough_context = true`
- `state.PlanIterations >= state.MaxPlanIterations`

---

## 七、设计模式分析

### 7.1 模板方法模式（Template Method）

**Planner 的固定流程**：
```
1. 加载 Prompt（load）
2. 调用 LLM（agent）
3. 解析结果并路由（router）
```

**变化点**：
- Prompt 内容（是否包含背景信息）
- 路由目标（Reporter / Human）

### 7.2 策略模式（Strategy Pattern）

**路由策略**：
```go
// 策略1: 信息充足 → 直接执行
if has_enough_context { state.Goto = Reporter }

// 策略2: 信息不足 → 人工介入
else { state.Goto = Human }

// 策略3: 解析失败 → 降级处理
if parseError && iterations > 0 { state.Goto = Reporter }
```

### 7.3 状态模式（State Pattern）

**Plan 的状态变化**：
```
状态1: Plan = nil          (未规划)
  └─ Planner 执行
     └─ 状态2: Plan = {...}, ExecutionRes = nil  (已规划，未执行)
        └─ ResearchTeam/Researcher 执行
           └─ 状态3: Plan = {...}, ExecutionRes = "..."  (已执行)
              └─ Reporter 汇总
                 └─ 状态4: 最终报告生成
```

---

## 八、错误处理与优化

### 8.1 JSON 解析容错

**当前实现**：
```go
err = json.Unmarshal([]byte(input.Content), state.CurrentPlan)
if err != nil {
    // 处理错误
}
```

**潜在问题**：
- LLM 可能输出 Markdown 包裹的 JSON（` ```json ... ``` `）
- LLM 可能包含额外的解释文本

**改进建议**：
```go
// 提取 JSON 部分
content := input.Content
if strings.Contains(content, "```json") {
    start := strings.Index(content, "```json") + 7
    end := strings.Index(content[start:], "```")
    content = content[start : start+end]
}
content = strings.TrimSpace(content)

// 再解析
err = json.Unmarshal([]byte(content), state.CurrentPlan)
```

### 8.2 迭代次数限制

**建议在 routerPlanner 中添加**：
```go
if state.PlanIterations >= state.MaxPlanIterations {
    // 达到上限，强制进入下一步
    if state.CurrentPlan != nil && len(state.CurrentPlan.Steps) > 0 {
        state.Goto = consts.ResearchTeam  // 使用当前计划
    } else {
        state.Goto = compose.END  // 没有可用计划，终止
    }
    return nil
}
```

### 8.3 Prompt 优化

**建议增强 System Prompt**：
```
IMPORTANT: Your response MUST be a valid JSON object. Do NOT include any explanatory text before or after the JSON. Do NOT wrap the JSON in markdown code blocks.

Output format:
{
  "locale": "en-US",
  "has_enough_context": true/false,
  "thought": "your reasoning...",
  "title": "task title",
  "steps": [...]
}
```

---

## 九、性能监控指标

### 9.1 关键指标

| 指标 | 含义 | 目标值 |
|------|------|--------|
| **JSON 解析成功率** | 成功解析 Plan 的比例 | > 95% |
| **has_enough_context = true 比例** | 无需人工介入的比例 | > 80% |
| **平均步骤数** | 每个 Plan 的平均步骤数 | 2-5 |
| **平均迭代次数** | 从 Planner 到执行的平均循环次数 | < 1.5 |
| **Planner 执行延迟** | load + agent + router 总时间 | < 10s |

### 9.2 质量评估

**Plan 质量维度**：
1. **步骤合理性**：步骤是否逻辑清晰、覆盖全面
2. **可执行性**：描述是否足够详细，Researcher 能否理解
3. **语言一致性**：所有字段的语言是否与 `locale` 一致
4. **类型准确性**：`step_type` 分类是否合理

---

## 十、总结

### 核心价值

Planner 实现了一个**智能的任务分解引擎**：

1. **上下文增强**：利用背景调查结果优化计划质量
2. **自适应流程**：根据信息充足度决定是否需要人工介入
3. **结构化输出**：生成标准化的、可执行的研究计划
4. **容错机制**：解析失败时的降级策略

### 设计亮点

- ✅ **动态 Prompt 增强**：根据背景信息调整输入
- ✅ **自我评估机制**：LLM 判断 `has_enough_context`
- ✅ **迭代支持**：支持人工反馈后重新规划
- ✅ **降级容错**：解析失败时不会完全崩溃

### 架构图

```
                ┌──────────────────────────────────┐
                │          Planner                 │
                └─────────────┬────────────────────┘
                              │
        ┌─────────────────────┼─────────────────────┐
        │                     │                     │
    ┌───▼───┐            ┌────▼────┐           ┌────▼────┐
    │ load  │───────────▶│ agent   │──────────▶│ router  │
    │       │            │(PlanMod)│           │         │
    └───┬───┘            └─────────┘           └────┬────┘
        │                     │                     │
        │                     │                     │
    [加载Prompt]         [生成Plan]            [解析JSON]
    [注入背景]           [JSON输出]            [设置路由]
    [变量替换]                                       │
                                         ┌───────────┴────────────┐
                                         │                        │
                                   [has_enough               [has_enough
                                    _context=true]            _context=false]
                                         │                        │
                                         ↓                        ↓
                                    [Reporter]                [Human]
                                   (直接执行)              (人工确认)
```

Planner 是整个系统的**战略制定中心**，将模糊的用户需求转化为清晰的行动计划！

