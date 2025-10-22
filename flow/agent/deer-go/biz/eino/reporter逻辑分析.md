# Reporter（报告生成器）逻辑分析

## 一、概述

`reporter.go` 实现了 **Reporter（报告生成器）** 子图，它是整个 deer-go 系统的**最终输出节点**，负责汇总所有研究步骤的结果，生成格式规范、内容丰富的最终研究报告。

### 在系统中的位置

```
ResearchTeam (所有步骤完成) → Reporter → END
       ↑                                  
  (所有结果汇总)                    (最终报告)
```

### 核心职责

1. **结果汇总**：收集所有步骤的 `ExecutionRes`
2. **报告生成**：调用 LLM 生成结构化报告
3. **格式规范**：确保报告符合特定格式要求
4. **流程结束**：设置 `state.Goto = END`

---

## 二、核心组件分析

### 2.1 `loadReporterMsg` 函数（33-65行）

**作用**：构造 Reporter 的 Prompt，注入所有研究步骤的结果

#### 实现逻辑

```go
func loadReporterMsg(ctx context.Context, name string, opts ...any) (output []*schema.Message, err error) {
    err = compose.ProcessState[*model.State](ctx, func(_ context.Context, state *model.State) error {
        // 步骤1: 获取 Prompt 模板
        sysPrompt, err := infra.GetPromptTemplate(ctx, name)
        if err != nil {
            ilog.EventInfo(ctx, "get prompt template fail")
            return err
        }
        
        // 步骤2: 构造 Prompt 模板
        promptTemp := prompt.FromMessages(schema.Jinja2,
            schema.SystemMessage(sysPrompt),
            schema.MessagesPlaceholder("user_input", true),
        )
        
        // 步骤3: 构造消息列表
        msg := []*schema.Message{}
        
        // 添加任务概述
        msg = append(msg,
            schema.UserMessage(fmt.Sprintf(
                "# Research Requirements\n\n## Task\n\n %v \n\n## Description\n\n %v", 
                state.CurrentPlan.Title,    // 任务标题
                state.CurrentPlan.Thought,  // 任务描述/思路
            )),
            // 添加格式要求（硬编码在代码中）
            schema.SystemMessage("IMPORTANT: Structure your report according to the format in the prompt. Remember to include:\n\n1. Key Points - A bulleted list of the most important findings\n2. Overview - A brief introduction to the topic\n3. Detailed Analysis - Organized into logical sections\n4. Survey Note (optional) - For more comprehensive reports\n5. Key Citations - List all references at the end\n\nFor citations, DO NOT include inline citations in the text. Instead, place all citations in the 'Key Citations' section at the end using the format: `- [Source Title](URL)`. Include an empty line between each citation for better readability.\n\nPRIORITIZE USING MARKDOWN TABLES for data presentation and comparison. Use tables whenever presenting comparative data, statistics, features, or options. Structure tables with clear headers and aligned columns. Example table format:\n\n| Feature | Description | Pros | Cons |\n|---------|-------------|------|------|\n| Feature 1 | Description 1 | Pros 1 | Cons 1 |\n| Feature 2 | Description 2 | Pros 2 | Cons 2 |"),
        )
        
        // 步骤4: 添加所有步骤的执行结果
        for _, step := range state.CurrentPlan.Steps {
            msg = append(msg, schema.UserMessage(fmt.Sprintf(
                "Below are some observations for the research task:\n\n %v", 
                *step.ExecutionRes,  // 👈 每个步骤的结果
            )))
        }
        
        // 步骤5: 准备变量并格式化
        variables := map[string]any{
            "locale":              state.Locale,
            "max_step_num":        state.MaxStepNum,
            "max_plan_iterations": state.MaxPlanIterations,
            "CURRENT_TIME":        time.Now().Format("2006-01-02 15:04:05"),
            "user_input":          msg,  // 👈 包含所有观察结果
        }
        output, err = promptTemp.Format(ctx, variables)
        return err
    })
    return output, err
}
```

#### 关键特性

1. **任务概述**

   ```go
   schema.UserMessage(fmt.Sprintf(
       "# Research Requirements\n\n## Task\n\n %v \n\n## Description\n\n %v", 
       state.CurrentPlan.Title,    // "AI Trends Research 2025"
       state.CurrentPlan.Thought,  // "Comprehensive analysis of..."
   ))
   ```

   **作用**：
   - 提醒 LLM 报告的主题和目标
   - 确保报告聚焦于原始任务

2. **格式要求（硬编码）**

   ```go
   schema.SystemMessage("IMPORTANT: Structure your report according to the format in the prompt...")
   ```

   **要求的报告结构**：
   ```
   1. Key Points (关键要点)
   2. Overview (概述)
   3. Detailed Analysis (详细分析)
   4. Survey Note (可选)
   5. Key Citations (引用列表)
   ```

   **特殊要求**：
   - **引用格式**：`- [Source Title](URL)`，每个引用间空一行
   - **优先使用表格**：对比数据、统计信息、特性列表
   - **表格格式示例**：
     ```markdown
     | Feature | Description | Pros | Cons |
     |---------|-------------|------|------|
     | ...     | ...         | ...  | ...  |
     ```

3. **注入所有观察结果**

   ```go
   for _, step := range state.CurrentPlan.Steps {
       msg = append(msg, schema.UserMessage(fmt.Sprintf(
           "Below are some observations for the research task:\n\n %v", 
           *step.ExecutionRes,
       )))
   }
   ```

   **生成的消息列表示例**：
   ```
   Message 1 (User):
     # Research Requirements
     ## Task
     AI Trends Research 2025
     ## Description
     Comprehensive analysis of emerging AI trends...
   
   Message 2 (System):
     IMPORTANT: Structure your report... (格式要求)
   
   Message 3 (User):
     Below are some observations for the research task:
     
     [Step 0 的研究结果：关于 Multimodal AI 的详细信息...]
   
   Message 4 (User):
     Below are some observations for the research task:
     
     [Step 1 的研究结果：关于 AGI Progress 的详细信息...]
   
   Message 5 (User):
     Below are some observations for the research task:
     
     [Step 2 的处理结果：生成的图表和分析...]
   ```

---

### 2.2 `routerReporter` 函数（67-77行）

**作用**：记录最终报告，结束整个流程

#### 实现逻辑

```go
func routerReporter(ctx context.Context, input *schema.Message, opts ...any) (output string, err error) {
    err = compose.ProcessState[*model.State](ctx, func(_ context.Context, state *model.State) error {
        defer func() {
            output = state.Goto
        }()
        
        // 记录最终报告
        ilog.EventInfo(ctx, "report_end", "report", input.Content)
        
        // 结束流程
        state.Goto = compose.END  // 👈 终点
        return nil
    })
    return output, nil
}
```

#### 关键特性

1. **日志记录**

   ```go
   ilog.EventInfo(ctx, "report_end", "report", input.Content)
   ```

   - 记录完整的报告内容
   - 用于调试、归档、质量评估
   - `input.Content` 是 LLM 生成的 Markdown 报告

2. **流程终止**

   ```go
   state.Goto = compose.END
   ```

   - 设置路由目标为 `END`
   - 主图的 `agentHandOff` 读取后，流程结束
   - 报告作为最终输出返回给用户

---

### 2.3 `NewReporter` 函数（79-94行）

**作用**：构建 Reporter 子图

#### 子图结构

```
START → load → agent → router → END
```

#### 实现代码

```go
func NewReporter[I, O any](ctx context.Context) *compose.Graph[I, O] {
    cag := compose.NewGraph[I, O]()
    
    // 添加三个节点
    _ = cag.AddLambdaNode("load", compose.InvokableLambdaWithOption(loadReporterMsg))
    _ = cag.AddChatModelNode("agent", infra.ChatModel)  // 👈 使用通用 ChatModel
    _ = cag.AddLambdaNode("router", compose.InvokableLambdaWithOption(routerReporter))
    
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
| `load` | LambdaNode | `string` | `[]*schema.Message` | 汇总所有观察结果，构造 Prompt |
| `agent` | ChatModelNode | `[]*schema.Message` | `*schema.Message` | LLM 生成结构化报告（Markdown） |
| `router` | LambdaNode | `*schema.Message` | `string` | 记录报告，结束流程 |

#### 特点

**使用通用 ChatModel**：
```go
_ = cag.AddChatModelNode("agent", infra.ChatModel)
```

- 不是专门的 `ReportModel`
- 可能与 Coordinator 使用同一个模型
- 但 Prompt 不同（专注于报告生成）

---

## 三、完整执行流程

### 场景：生成 AI 趋势研究报告

```
Plan:
  Title: "AI Trends Research 2025"
  Thought: "Analyze emerging AI technologies..."
  Steps:
    - Step 0 (Research): ExecutionRes = "Multimodal AI is..."
    - Step 1 (Research): ExecutionRes = "AGI progress shows..."
    - Step 2 (Processing): ExecutionRes = "Chart generated..."
```

#### 执行步骤

```
═══════════════════════════════════════════════════════════
Reporter 子图执行
═══════════════════════════════════════════════════════════

1️⃣ load 节点
   ├─ 构造消息列表:
   │  
   │  [Message 1 - User]:
   │  "# Research Requirements
   │   ## Task
   │   AI Trends Research 2025
   │   ## Description
   │   Analyze emerging AI technologies..."
   │  
   │  [Message 2 - System]:
   │  "IMPORTANT: Structure your report... (格式要求)"
   │  
   │  [Message 3 - User]:
   │  "Below are some observations for the research task:
   │   
   │   Multimodal AI is rapidly advancing. GPT-4V and Gemini 
   │   demonstrate strong vision-language capabilities..."
   │  
   │  [Message 4 - User]:
   │  "Below are some observations for the research task:
   │   
   │   AGI progress shows significant milestones. OpenAI's 
   │   research indicates..."
   │  
   │  [Message 5 - User]:
   │  "Below are some observations for the research task:
   │   
   │   Chart generated: comparison.png (25KB). Shows GPT-4 
   │   leading at 95 score..."
   │
   └─ 输出: [5 条消息]

2️⃣ agent 节点 (ChatModel)
   ├─ 输入: [任务概述, 格式要求, 3x 观察结果]
   ├─ LLM 思考:
   │  - 需要生成结构化报告
   │  - 包含: Key Points, Overview, Detailed Analysis, Citations
   │  - 使用 Markdown 格式
   │  - 数据对比使用表格
   ├─ 生成报告:
   │  "# AI Trends Research 2025
   │   
   │   ## Key Points
   │   
   │   - Multimodal AI models are leading the innovation wave
   │   - AGI research shows promising progress
   │   - GPT-4 currently leads in performance metrics
   │   
   │   ## Overview
   │   
   │   The AI landscape in 2025 is characterized by rapid 
   │   advancement in multimodal capabilities...
   │   
   │   ## Detailed Analysis
   │   
   │   ### Multimodal AI Evolution
   │   
   │   Recent developments in multimodal AI demonstrate...
   │   
   │   | Model    | Performance | Release | Key Features |
   │   |----------|-------------|---------|--------------|
   │   | GPT-4V   | 95          | 2023    | Vision+Text  |
   │   | Gemini   | 88          | 2024    | Multimodal   |
   │   | Claude 3 | 92          | 2024    | Long Context |
   │   
   │   ### AGI Progress
   │   
   │   The path toward Artificial General Intelligence...
   │   
   │   ## Key Citations
   │   
   │   - [OpenAI GPT-4 Technical Report](https://openai.com/research/gpt-4)
   │   
   │   - [Google Gemini Overview](https://deepmind.google/technologies/gemini/)
   │   
   │   - [Anthropic Claude 3 Announcement](https://www.anthropic.com/claude)
   │  "
   └─ 输出: Message with Content = (上述 Markdown 报告)

3️⃣ router 节点
   ├─ 接收报告: input.Content = "# AI Trends Research 2025\n\n..."
   ├─ 记录日志: "report_end", report: (完整内容)
   └─ 设置路由: state.Goto = compose.END

4️⃣ 返回主图
   └─ agentHandOff: next = "END"
      └─ 主图执行结束，返回最终报告
```

---

## 四、报告格式分析

### 4.1 标准报告结构

**层次结构**：
```markdown
# [报告标题]

## Key Points
- 要点 1
- 要点 2
- 要点 3

## Overview
简要介绍...

## Detailed Analysis

### 子主题 1
详细分析...

| 对比项 | 数据1 | 数据2 |
|--------|-------|-------|
| ...    | ...   | ...   |

### 子主题 2
详细分析...

## Survey Note (可选)
更深入的调研说明...

## Key Citations
- [来源 1](URL)

- [来源 2](URL)
```

### 4.2 格式要求详解

**1. 引用格式**

**要求**：
```
DO NOT include inline citations in the text. 
Instead, place all citations in the 'Key Citations' section.
```

**错误示例**（内联引用）：
```markdown
According to OpenAI's report[1], GPT-4 shows...

[1] https://openai.com/research/gpt-4
```

**正确示例**（集中引用）：
```markdown
According to OpenAI's report, GPT-4 shows...

## Key Citations

- [OpenAI GPT-4 Report](https://openai.com/research/gpt-4)
```

**2. 表格使用**

**要求**：
```
PRIORITIZE USING MARKDOWN TABLES for data presentation and comparison.
```

**适用场景**：
- 对比不同产品/技术
- 展示统计数据
- 列举特性/优缺点

**示例**：
```markdown
| AI Model | Performance | Context Window | Price |
|----------|-------------|----------------|-------|
| GPT-4    | 95/100      | 128K tokens    | High  |
| Claude 3 | 92/100      | 200K tokens    | Mid   |
| Gemini   | 88/100      | 32K tokens     | Low   |
```

---

## 五、设计模式分析

### 5.1 聚合模式（Aggregator Pattern）

**Reporter 作为聚合器**：

```
Researcher (Step 0) → ExecutionRes[0]  ┐
Researcher (Step 1) → ExecutionRes[1]  ├─→ Reporter → 汇总报告
Coder (Step 2)      → ExecutionRes[2]  ┘
```

**聚合逻辑**：
```go
for _, step := range state.CurrentPlan.Steps {
    msg = append(msg, schema.UserMessage(fmt.Sprintf(
        "Below are some observations for the research task:\n\n %v", 
        *step.ExecutionRes,
    )))
}
```

### 5.2 模板方法模式（Template Method）

**报告生成的固定流程**：
```
1. 收集所有结果（load）
2. 生成报告（agent）
3. 记录并结束（router）
```

**变化点**：
- 观察结果的内容（由前序步骤决定）
- 报告的风格（由 Prompt 模板决定）

---

## 六、质量控制机制

### 6.1 硬编码的格式要求

**优点**：
- ✅ 确保报告结构一致性
- ✅ 符合用户期望的格式
- ✅ 便于后续处理（如解析、展示）

**缺点**：
- ❌ 缺乏灵活性（所有报告都一样的结构）
- ❌ 难以适应不同类型的任务
- ❌ 修改格式需要改代码

**改进方案**：
```go
// 从配置或 Prompt 文件加载格式要求
formatRequirements, _ := infra.GetPromptTemplate(ctx, "report_format_requirements")
msg = append(msg, schema.SystemMessage(formatRequirements))
```

### 6.2 潜在的质量问题

**问题1：缺少结果验证**

```go
for _, step := range state.CurrentPlan.Steps {
    msg = append(msg, schema.UserMessage(fmt.Sprintf(
        "Below are some observations for the research task:\n\n %v", 
        *step.ExecutionRes,  // 👈 未检查是否为空或错误
    )))
}
```

**改进**：
```go
for i, step := range state.CurrentPlan.Steps {
    if step.ExecutionRes == nil {
        ilog.EventWarn(ctx, "missing_execution_result", "step_index", i)
        continue  // 跳过未完成的步骤
    }
    
    // 检查是否包含错误信息
    if strings.Contains(*step.ExecutionRes, "ERROR") || 
       strings.Contains(*step.ExecutionRes, "FAILED") {
        ilog.EventWarn(ctx, "step_execution_error", "step_index", i)
        // 可以添加错误标记到报告中
    }
    
    msg = append(msg, schema.UserMessage(fmt.Sprintf(
        "Below are some observations for the research task:\n\n %v", 
        *step.ExecutionRes,
    )))
}
```

**问题2：超长结果处理**

如果某个步骤的 `ExecutionRes` 非常长（如 Researcher 返回了大量文本），可能导致：
- 超出 LLM 上下文窗口
- 报告生成时间过长
- 成本过高

**改进**：
```go
for _, step := range state.CurrentPlan.Steps {
    result := *step.ExecutionRes
    
    // 截断超长结果
    maxLen := 10000
    if len(result) > maxLen {
        result = result[:maxLen] + "\n\n[... truncated ...]"
        ilog.EventWarn(ctx, "truncated_long_result", "original_len", len(*step.ExecutionRes))
    }
    
    msg = append(msg, schema.UserMessage(fmt.Sprintf(
        "Below are some observations for the research task:\n\n %v", 
        result,
    )))
}
```

---

## 七、与其他 Agent 的协作

### 7.1 数据流

```
Planner:
  └─ 创建: state.CurrentPlan (Title, Thought, Steps)

Researcher/Coder:
  └─ 填充: Steps[i].ExecutionRes = result

Reporter:
  └─ 读取: CurrentPlan.Title, Thought, Steps[*].ExecutionRes
  └─ 生成: 最终报告
```

### 7.2 完整数据流示例

```go
// Planner 阶段
state.CurrentPlan = &Plan{
    Title:   "AI Trends Research",
    Thought: "Comprehensive analysis...",
    Steps:   [
        {Title: "Research Multimodal", ExecutionRes: nil},
        {Title: "Research AGI", ExecutionRes: nil},
        {Title: "Generate Charts", ExecutionRes: nil},
    ],
}

// ResearchTeam → Researcher (Step 0)
state.CurrentPlan.Steps[0].ExecutionRes = &"Multimodal AI is..."

// ResearchTeam → Researcher (Step 1)
state.CurrentPlan.Steps[1].ExecutionRes = &"AGI progress shows..."

// ResearchTeam → Coder (Step 2)
state.CurrentPlan.Steps[2].ExecutionRes = &"Chart generated..."

// ResearchTeam → Reporter
// Reporter 读取所有 ExecutionRes，生成报告
```

---

## 八、性能与优化

### 8.1 潜在瓶颈

**1. 长文本处理**

如果所有步骤的结果加起来很长：
```
Step 0: 5000 字符
Step 1: 4000 字符
Step 2: 3000 字符
总计: 12000 字符 + 格式要求 + System Prompt
```

**影响**：
- LLM 处理时间增加
- Token 消耗增加
- 可能超出上下文窗口

**优化**：
- 限制每个步骤结果的长度
- 使用摘要模型先压缩结果
- 仅提取关键信息

**2. 报告生成时间**

Reporter 是最后一步，用户等待最久：
```
用户提问 → ... → Reporter (用户看到进度条卡在这里)
```

**优化**：
- 使用流式输出（`Stream` 模式）
- 逐步返回报告的各个部分
- 提供进度提示（"生成概述...""生成详细分析..."）

### 8.2 成本优化

**Token 消耗分析**：
```
Input Tokens:
  - System Prompt: ~1000 tokens
  - Format Requirements: ~500 tokens
  - Task Overview: ~200 tokens
  - 3x Observations: ~6000 tokens (假设每个2000)
  Total Input: ~7700 tokens

Output Tokens:
  - Final Report: ~3000 tokens (假设 2000 words)

Cost (GPT-4):
  Input: 7700 * $0.03/1K = $0.23
  Output: 3000 * $0.06/1K = $0.18
  Total: $0.41 per report
```

**优化建议**：
- 使用更便宜的模型（如 GPT-3.5）用于简单报告
- 压缩观察结果，去除冗余
- 缓存相似的报告（如果用户多次请求相同主题）

---

## 九、监控指标

### 9.1 关键指标

| 指标 | 含义 | 目标值 |
|------|------|--------|
| **报告生成成功率** | 成功生成有效报告的比例 | > 99% |
| **平均生成时间** | 从 load 到 router 的时间 | < 30s |
| **平均报告长度** | 生成报告的字符数 | 2000-5000 |
| **格式合规率** | 包含所有必需部分的报告比例 | > 95% |
| **引用准确率** | 引用格式正确的比例 | > 90% |

### 9.2 质量评估

**报告质量维度**：
1. **结构完整性**：是否包含所有必需部分（Key Points, Overview, etc.）
2. **内容准确性**：是否准确反映观察结果
3. **格式规范性**：Markdown 格式是否正确
4. **可读性**：逻辑是否清晰，语言是否流畅
5. **引用有效性**：链接是否有效，来源是否可靠

**自动化评估**：
```go
func evaluateReport(report string) (score float64, issues []string) {
    score = 100.0
    
    // 检查必需部分
    if !strings.Contains(report, "## Key Points") {
        score -= 20
        issues = append(issues, "Missing 'Key Points' section")
    }
    if !strings.Contains(report, "## Overview") {
        score -= 20
        issues = append(issues, "Missing 'Overview' section")
    }
    if !strings.Contains(report, "## Key Citations") {
        score -= 10
        issues = append(issues, "Missing 'Key Citations' section")
    }
    
    // 检查表格使用
    if !strings.Contains(report, "|") {
        score -= 5
        issues = append(issues, "No tables used")
    }
    
    // 检查引用格式
    citationPattern := regexp.MustCompile(`- \[.+\]\(.+\)`)
    if !citationPattern.MatchString(report) {
        score -= 10
        issues = append(issues, "Citations not in required format")
    }
    
    return score, issues
}
```

---

## 十、总结

### 核心价值

Reporter 实现了一个**智能的报告生成引擎**：

1. **结果汇总**：集成所有研究和处理步骤的成果
2. **格式规范**：确保报告符合专业标准
3. **内容组织**：将零散的观察结果组织成连贯的报告
4. **流程终结**：作为整个系统的最终输出节点

### 设计亮点

- ✅ **聚合模式**：统一汇总所有步骤结果
- ✅ **格式控制**：通过硬编码要求确保一致性
- ✅ **Markdown 输出**：易于阅读和进一步处理
- ✅ **引用管理**：规范的引用格式

### 架构图

```
                ┌──────────────────────────────────┐
                │          Reporter                │
                │       (报告生成器)                 │
                └─────────────┬────────────────────┘
                              │
        ┌─────────────────────┼─────────────────────┐
        │                     │                     │
    ┌───▼───┐            ┌────▼────┐           ┌────▼────┐
    │ load  │───────────▶│ agent   │──────────▶│ router  │
    │       │            │(ChatMod)│           │         │
    └───┬───┘            └─────────┘           └────┬────┘
        │                     │                     │
        │                     │                     │
    [汇总结果]            [生成报告]            [记录+结束]
    [格式要求]            [Markdown]            [Goto=END]
        │                     │                     │
        ↓                     ↓                     ↓
   ┌─────────┐          ┌──────────┐         ┌──────────┐
   │Plan Info│          │Structured│         │  Final   │
   │All Steps│          │  Report  │         │  Output  │
   │ Results │          │+ Tables  │         │          │
   └─────────┘          └──────────┘         └──────────┘
        │                     │
        └──────────┬──────────┘
                   │
              [完整报告]
                   │
                   ↓
            # AI Trends 2025
            
            ## Key Points
            - ...
            
            ## Overview
            ...
            
            ## Detailed Analysis
            ...
            
            ## Key Citations
            - [Source](URL)
```

Reporter 是整个系统的**最终输出节点**，将所有努力转化为结构化、专业化的研究报告！

