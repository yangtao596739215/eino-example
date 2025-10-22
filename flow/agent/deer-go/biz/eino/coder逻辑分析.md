# Coder（代码执行器）逻辑分析

## 一、概述

`coder.go` 实现了 **Coder（代码执行器）** 子图，专门负责执行 Plan 中类型为 `Processing` 的步骤，主要用于**数据处理、代码执行、图表生成**等需要编程能力的任务。

### 在系统中的位置

```
ResearchTeam → Coder → ResearchTeam
                ↓ (所有步骤完成)
             Reporter
```

### 核心职责

1. **执行处理步骤**：执行 `step_type == "processing"` 的步骤
2. **代码生成与运行**：使用 Python MCP 工具执行代码
3. **结果保存**：将执行结果保存到 `step.ExecutionRes`
4. **返回调度中心**：完成后返回 ResearchTeam

---

## 二、核心组件分析

### 2.1 `loadCoderMsg` 函数（38-78行）

**作用**：构造 Coder 的 Prompt，注入当前需要处理的步骤信息

#### 实现逻辑

```go
func loadCoderMsg(ctx context.Context, name string, opts ...any) (output []*schema.Message, err error) {
    err = compose.ProcessState[*model.State](ctx, func(_ context.Context, state *model.State) error {
        // 步骤1: 获取 Prompt 模板
        sysPrompt, err := infra.GetPromptTemplate(ctx, name)
        if err != nil {
            ilog.EventError(ctx, err, "get prompt template error")
            return err
        }
        
        // 步骤2: 构造 Prompt 模板
        promptTemp := prompt.FromMessages(schema.Jinja2,
            schema.SystemMessage(sysPrompt),
            schema.MessagesPlaceholder("user_input", true),
        )
        
        // 步骤3: 找到当前需要执行的步骤
        var curStep *model.Step
        for i := range state.CurrentPlan.Steps {
            if state.CurrentPlan.Steps[i].ExecutionRes == nil {  // 👈 未执行
                curStep = &state.CurrentPlan.Steps[i]
                break
            }
        }
        
        if curStep == nil {
            panic("no step found")  // 不应该发生
        }
        
        // 步骤4: 构造用户消息（包含步骤详情）
        msg := []*schema.Message{}
        msg = append(msg,
            schema.UserMessage(fmt.Sprintf(
                "#Task\n\n##title\n\n %v \n\n##description\n\n %v \n\n##locale\n\n %v", 
                curStep.Title, 
                curStep.Description, 
                state.Locale,
            )),
        )
        
        // 步骤5: 准备变量并格式化
        variables := map[string]any{
            "locale":              state.Locale,
            "max_step_num":        state.MaxStepNum,
            "max_plan_iterations": state.MaxPlanIterations,
            "CURRENT_TIME":        time.Now().Format("2006-01-02 15:04:05"),
            "user_input":          msg,  // 👈 注入步骤信息
        }
        output, err = promptTemp.Format(ctx, variables)
        return err
    })
    return output, err
}
```

#### 关键特性

1. **查找当前步骤**

   ```go
   for i := range state.CurrentPlan.Steps {
       if state.CurrentPlan.Steps[i].ExecutionRes == nil {
           curStep = &state.CurrentPlan.Steps[i]
           break
       }
   }
   ```

   - 找到第一个未执行的步骤（`ExecutionRes == nil`）
   - 假设 ResearchTeam 已经正确路由，这应该是一个 `Processing` 类型的步骤

2. **步骤信息注入**

   ```go
   UserMessage(fmt.Sprintf(
       "#Task\n\n##title\n\n %v \n\n##description\n\n %v \n\n##locale\n\n %v", 
       curStep.Title,       // "Generate comparison charts"
       curStep.Description, // "Create charts comparing AI models using Python matplotlib"
       state.Locale,        // "en-US"
   ))
   ```

   **生成的用户消息示例**：
   ```
   #Task

   ##title

   Generate comparison charts

   ##description

   Create charts comparing different AI models' capabilities using Python matplotlib.

   ##locale

   en-US
   ```

3. **与 Researcher 的区别**

   | 特性 | Researcher | Coder |
   |------|-----------|-------|
   | **注入内容** | 步骤信息 | 步骤信息（相同） |
   | **可用工具** | Web Search, Wikipedia, etc. | Python MCP (代码执行) |
   | **主要任务** | 信息检索、研究 | 数据处理、代码执行 |

---

### 2.2 `routerCoder` 函数（80-99行）

**作用**：保存 Coder 的执行结果，并路由回 ResearchTeam

#### 实现逻辑

```go
func routerCoder(ctx context.Context, input *schema.Message, opts ...any) (output string, err error) {
    last := input  // ReAct Agent 的最终输出
    
    err = compose.ProcessState[*model.State](ctx, func(_ context.Context, state *model.State) error {
        defer func() {
            output = state.Goto
        }()
        
        // 遍历步骤，找到当前执行的步骤（第一个未完成的）
        for i, step := range state.CurrentPlan.Steps {
            if step.ExecutionRes == nil {  // 👈 找到未执行的步骤
                // 保存执行结果
                str := strings.Clone(last.Content)
                state.CurrentPlan.Steps[i].ExecutionRes = &str
                break
            }
        }
        
        ilog.EventInfo(ctx, "coder_end", "plan", state.CurrentPlan)
        
        // 返回 ResearchTeam 继续调度
        state.Goto = consts.ResearchTeam
        return nil
    })
    return output, nil
}
```

#### 关键特性

1. **结果保存**

   ```go
   str := strings.Clone(last.Content)
   state.CurrentPlan.Steps[i].ExecutionRes = &str
   ```

   - `last.Content` 是 ReAct Agent 的最终输出（通常包含思考过程和最终答案）
   - 使用 `strings.Clone` 避免潜在的内存共享问题
   - 保存为指针，标记步骤已完成

2. **固定路由**

   ```go
   state.Goto = consts.ResearchTeam
   ```

   - Coder 完成后**始终**返回 ResearchTeam
   - 由 ResearchTeam 决定下一步（继续下一个步骤 / 完成所有步骤）

---

### 2.3 `modifyCoderfunc` 函数（101-118行）

**作用**：消息修剪器，防止上下文过长导致超出 LLM 限制

#### 实现逻辑

```go
func modifyCoderfunc(ctx context.Context, input []*schema.Message) []*schema.Message {
    sum := 0
    maxLimit := 50000  // 👈 单条消息最大长度（字符）
    
    for i := range input {
        if input[i] == nil {
            ilog.EventWarn(ctx, "modify_inputfunc_nil", "input", input[i])
            continue
        }
        
        l := len(input[i].Content)
        
        // 如果消息过长，截取后半部分
        if l > maxLimit {
            ilog.EventWarn(ctx, "modify_inputfunc_clip", "raw_len", l)
            input[i].Content = input[i].Content[l-maxLimit:]  // 👈 保留最后 50000 字符
        }
        
        sum += len(input[i].Content)
    }
    
    ilog.EventInfo(ctx, "modify_inputfunc", "sum", sum, "input_len", input)
    return input
}
```

#### 关键特性

1. **后半部分保留策略**

   ```go
   input[i].Content = input[i].Content[l-maxLimit:]
   ```

   **示例**：
   ```
   原始内容 (70000 字符):
   "...previous research results...latest findings about AI..."
   
   截取后 (50000 字符):
   "...latest findings about AI..."  // 保留后半部分
   ```

   **原理**：
   - ReAct Agent 的历史消息通常越往后越重要（最新的观察和思考）
   - 早期的消息可能是初步尝试，不如最新消息关键

2. **为什么需要修剪？**

   **场景**：
   - Coder 使用 ReAct 模式，可能经历多轮推理
   - 每轮都会调用工具（如执行 Python 代码）
   - 工具返回的输出可能很长（如打印大量数据）
   - 累积的消息历史可能超出 LLM 的上下文窗口

   **示例流程**：
   ```
   Round 1: LLM 思考 → 调用 Python → 返回 10000 字符
   Round 2: LLM 思考 → 调用 Python → 返回 15000 字符
   Round 3: LLM 思考 → 调用 Python → 返回 20000 字符
   Round 4: LLM 思考 → 调用 Python → 返回 25000 字符
   总计: 70000 字符 → 超出限制！
   ```

3. **限制值选择**

   ```go
   maxLimit := 50000
   ```

   **考量**：
   - GPT-4: 8K-128K tokens context window
   - 1 token ≈ 0.75 words ≈ 4 characters (英文)
   - 50000 characters ≈ 12500 tokens
   - 为多轮对话预留足够空间

---

### 2.4 `NewCoder` 函数（120-157行）

**作用**：构建 Coder 子图

#### 子图结构

```
START → load → agent (ReAct Agent + Python MCP) → router → END
```

#### 实现代码

```go
func NewCoder[I, O any](ctx context.Context) *compose.Graph[I, O] {
    cag := compose.NewGraph[I, O]()
    
    // 步骤1: 加载 Python MCP 工具
    researchTools := []tool.BaseTool{}
    for mcpName, cli := range infra.MCPServer {
        ts, err := mcp.GetTools(ctx, &mcp.Config{Cli: cli})
        if err != nil {
            ilog.EventError(ctx, err, "builder_error")
        }
        
        // 只加载 Python 相关的工具
        if strings.HasPrefix(mcpName, "python") {  // 👈 关键过滤
            researchTools = append(researchTools, ts...)
        }
    }
    ilog.EventDebug(ctx, "coder_end", "coder_tools", researchTools)
    
    // 步骤2: 创建 ReAct Agent
    agent, err := react.NewAgent(ctx, &react.AgentConfig{
        MaxStep:               40,  // 最多 40 轮推理
        ToolCallingModel:      infra.ChatModel,
        ToolsConfig:           compose.ToolsNodeConfig{Tools: researchTools},
        MessageModifier:       modifyCoderfunc,  // 👈 注入消息修剪器
        StreamToolCallChecker: toolCallChecker,
    })
    
    // 步骤3: 将 Agent 包装为 Lambda
    agentLambda, err := compose.AnyLambda(agent.Generate, agent.Stream, nil, nil)
    if err != nil {
        panic(err)
    }
    
    // 步骤4: 添加节点
    _ = cag.AddLambdaNode("load", compose.InvokableLambdaWithOption(loadCoderMsg))
    _ = cag.AddLambdaNode("agent", agentLambda)
    _ = cag.AddLambdaNode("router", compose.InvokableLambdaWithOption(routerCoder))
    
    // 步骤5: 连接节点
    _ = cag.AddEdge(compose.START, "load")
    _ = cag.AddEdge("load", "agent")
    _ = cag.AddEdge("agent", "router")
    _ = cag.AddEdge("router", compose.END)
    
    return cag
}
```

#### 关键特性

1. **工具过滤：只加载 Python MCP**

   ```go
   if strings.HasPrefix(mcpName, "python") {
       researchTools = append(researchTools, ts...)
   }
   ```

   **原因**：
   - Coder 专注于代码执行，不需要搜索工具
   - 减少工具数量，提高 LLM 选择工具的准确性
   - Python MCP 提供的工具示例：
     - `python_execute`：执行 Python 代码
     - `python_install_package`：安装 Python 包
     - `python_read_file`：读取文件
     - `python_write_file`：写入文件

2. **ReAct Agent 配置**

   ```go
   &react.AgentConfig{
       MaxStep:               40,  // 👈 比 Researcher 可能更多（代码调试需要多轮）
       ToolCallingModel:      infra.ChatModel,
       ToolsConfig:           compose.ToolsNodeConfig{Tools: researchTools},
       MessageModifier:       modifyCoderfunc,  // 👈 关键：防止上下文爆炸
       StreamToolCallChecker: toolCallChecker,
   }
   ```

3. **与 Researcher 的对比**

   | 特性 | Researcher | Coder |
   |------|-----------|-------|
   | **工具类型** | Web Search, Wikipedia | Python MCP |
   | **MaxStep** | 通常较少（~20） | 较多（40） |
   | **MessageModifier** | 可能没有或不同策略 | `modifyCoderfunc`（截断） |
   | **典型任务** | 搜索、总结、分析 | 数据处理、图表生成、计算 |

---

## 三、完整执行流程

### 场景：生成 AI 模型对比图表

```
Plan Step:
  Title: "Generate AI Models Comparison Chart"
  Description: "Create a bar chart comparing GPT-4, Claude 3, and Gemini on key metrics"
  StepType: "processing"
  ExecutionRes: null
```

#### 执行步骤

```
═══════════════════════════════════════════════════════════
Coder 子图执行
═══════════════════════════════════════════════════════════

1️⃣ load 节点
   ├─ 查找当前步骤: ExecutionRes == null
   ├─ 构造 UserMessage:
   │  "#Task
   │   ##title
   │   Generate AI Models Comparison Chart
   │   
   │   ##description
   │   Create a bar chart comparing GPT-4, Claude 3, and Gemini on key metrics
   │   
   │   ##locale
   │   en-US"
   └─ 输出: [System Message, User Message]

2️⃣ agent 节点 (ReAct Agent)
   
   【Round 1】
   ├─ LLM 思考:
   │  "I need to create a bar chart. I'll use matplotlib. First, let me write the code."
   ├─ 调用工具: python_execute
   │  Arguments: {
   │    "code": "
   │      import matplotlib.pyplot as plt
   │      
   │      models = ['GPT-4', 'Claude 3', 'Gemini']
   │      scores = [95, 92, 88]
   │      
   │      plt.bar(models, scores)
   │      plt.title('AI Models Comparison')
   │      plt.ylabel('Performance Score')
   │      plt.savefig('comparison.png')
   │      print('Chart saved as comparison.png')
   │    "
   │  }
   └─ 工具返回:
      "Chart saved as comparison.png"
   
   【Round 2】
   ├─ LLM 思考:
   │  "Great! The chart is generated. Let me verify it was created correctly."
   ├─ 调用工具: python_execute
   │  Arguments: {
   │    "code": "
   │      import os
   │      if os.path.exists('comparison.png'):
   │          print('File exists. Size:', os.path.getsize('comparison.png'), 'bytes')
   │      else:
   │          print('File not found')
   │    "
   │  }
   └─ 工具返回:
      "File exists. Size: 25634 bytes"
   
   【Final】
   └─ LLM 总结:
      "I've successfully created a bar chart comparing GPT-4, Claude 3, and Gemini. 
       The chart is saved as 'comparison.png' (25KB). It shows GPT-4 with the highest 
       score at 95, followed by Claude 3 at 92, and Gemini at 88."

3️⃣ router 节点
   ├─ 接收 agent 输出: last.Content = "I've successfully created..."
   ├─ 查找当前步骤: Steps[i].ExecutionRes == null
   ├─ 保存结果: Steps[i].ExecutionRes = &"I've successfully created..."
   ├─ 日志: "coder_end", plan: {...}
   └─ 路由: state.Goto = "research_team"

4️⃣ 返回主图
   └─ agentHandOff → ResearchTeam
      └─ ResearchTeam 继续调度下一个步骤（如果有）
```

---

## 四、Python MCP 工具示例

### 4.1 python_execute

**功能**：执行 Python 代码

**输入**：
```json
{
  "code": "print('Hello, World!')"
}
```

**输出**：
```
Hello, World!
```

### 4.2 python_install_package

**功能**：安装 Python 包

**输入**：
```json
{
  "package": "pandas"
}
```

**输出**：
```
Successfully installed pandas-2.0.0
```

### 4.3 典型使用场景

| 任务 | Python 代码示例 |
|------|----------------|
| **数据处理** | `import pandas as pd; df = pd.read_csv('data.csv'); df.describe()` |
| **图表生成** | `import matplotlib.pyplot as plt; plt.plot([1,2,3]); plt.savefig('chart.png')` |
| **数学计算** | `import numpy as np; result = np.linalg.solve(A, b)` |
| **文件操作** | `with open('results.txt', 'w') as f: f.write(summary)` |

---

## 五、设计模式分析

### 5.1 与 Researcher 的共同模式

**三节点结构**：
```
load → agent (ReAct) → router
```

**差异化配置**：
| 组件 | Researcher | Coder |
|------|-----------|-------|
| **load** | 注入步骤信息 | 注入步骤信息（相同） |
| **agent - 工具** | Web Search | Python MCP |
| **agent - MessageModifier** | 可能不同 | `modifyCoderfunc` |
| **router** | 保存结果 → ResearchTeam | 保存结果 → ResearchTeam（相同） |

### 5.2 策略模式（Tool Selection）

**工具选择策略**：

```go
// Researcher 策略
for mcpName, cli := range infra.MCPServer {
    if strings.HasSuffix(info.Name, "search") {  // 搜索工具
        tools = append(tools, t)
    }
}

// Coder 策略
for mcpName, cli := range infra.MCPServer {
    if strings.HasPrefix(mcpName, "python") {  // Python 工具
        tools = append(tools, t)
    }
}
```

### 5.3 装饰器模式（Message Modifier）

**`MessageModifier` 作为装饰器**：

```go
// 原始消息流
messages = [msg1, msg2, msg3]

// 经过 modifyCoderfunc 装饰
messages = modifyCoderfunc(ctx, messages)
// → [msg1 (截断), msg2 (截断), msg3 (截断)]

// 传递给 LLM
llm.Generate(ctx, messages)
```

---

## 六、错误处理与优化

### 6.1 当前的错误处理

**Panic 而非优雅降级**：

```go
if curStep == nil {
    panic("no step found")
}
```

**问题**：
- 系统崩溃，无法恢复
- 用户体验差

**建议改进**：
```go
if curStep == nil {
    ilog.EventError(ctx, fmt.Errorf("no pending step found"))
    // 返回空消息或默认任务
    return []*schema.Message{
        schema.UserMessage("No specific task. Please standby."),
    }, nil
}
```

### 6.2 Python 执行错误

**当前行为**：
- Python 代码执行失败时，MCP 返回错误信息
- ReAct Agent 会看到错误并尝试修复（重新生成代码）

**示例**：
```
Round 1: 执行代码 → 语法错误
Round 2: LLM 看到错误 → 修复代码 → 重新执行
Round 3: 成功执行
```

**优化建议**：
- 添加错误重试次数限制
- 记录失败的代码和错误，用于后续分析
- 提供代码模板/示例，减少错误率

### 6.3 超长输出处理

**`modifyCoderfunc` 的局限**：
- 只截断单条消息，不考虑总上下文长度
- 可能仍然超出 LLM 限制

**改进方案**：
```go
func modifyCoderfunc(ctx context.Context, input []*schema.Message) []*schema.Message {
    maxTotalTokens := 100000  // 总 token 限制
    maxSingleMessage := 50000  // 单条消息限制
    
    totalLen := 0
    for i := range input {
        // 截断单条消息
        if len(input[i].Content) > maxSingleMessage {
            input[i].Content = input[i].Content[len(input[i].Content)-maxSingleMessage:]
        }
        totalLen += len(input[i].Content)
    }
    
    // 如果总长度仍超限，移除早期消息
    if totalLen > maxTotalTokens {
        // 保留 System Message + 最近的 N 条消息
        keepCount := 10
        if len(input) > keepCount+1 {
            input = append(input[:1], input[len(input)-keepCount:]...)
        }
    }
    
    return input
}
```

---

## 七、性能监控

### 7.1 关键指标

| 指标 | 含义 | 目标值 |
|------|------|--------|
| **平均轮次** | ReAct Agent 的平均推理轮数 | < 10 |
| **代码执行成功率** | Python 代码首次执行成功的比例 | > 80% |
| **平均执行时间** | 从 load 到 router 的总时间 | < 60s |
| **消息截断率** | 触发 `modifyCoderfunc` 截断的比例 | < 10% |
| **工具调用次数** | 平均每个步骤的工具调用次数 | 2-5 |

### 7.2 质量评估

**代码质量维度**：
1. **语法正确性**：代码能否成功执行
2. **功能完整性**：是否完成了步骤描述的任务
3. **输出有效性**：生成的文件/数据是否有效
4. **效率**：代码是否优化（如使用向量化而非循环）

---

## 八、与 Researcher 的详细对比

### 8.1 职责分工

| 维度 | Researcher | Coder |
|------|-----------|-------|
| **主要任务** | 信息检索、文献调研 | 数据处理、代码执行 |
| **输入** | Research 类型步骤 | Processing 类型步骤 |
| **工具类型** | 搜索引擎、知识库 | Python 解释器 |
| **输出特点** | 文字总结、研究报告 | 代码、图表、计算结果 |
| **典型场景** | "研究最新 AI 趋势" | "生成对比图表" |

### 8.2 实现差异

| 组件 | Researcher | Coder |
|------|-----------|-------|
| **工具加载** | `strings.HasSuffix(name, "search")` | `strings.HasPrefix(mcpName, "python")` |
| **MessageModifier** | 可能没有 | `modifyCoderfunc`（截断） |
| **MaxStep** | ~20 | 40 |
| **主要挑战** | 信息筛选、去重 | 代码调试、错误修复 |

---

## 九、总结

### 核心价值

Coder 实现了一个**智能的代码执行引擎**：

1. **自动编程**：根据自然语言描述生成并执行代码
2. **迭代调试**：通过 ReAct 模式自动修复代码错误
3. **工具专用化**：只加载 Python MCP，提高工具选择准确性
4. **上下文管理**：通过消息修剪防止超出 LLM 限制

### 设计亮点

- ✅ **ReAct 模式**：支持多轮推理和错误修复
- ✅ **工具过滤**：只加载相关工具，提高效率
- ✅ **消息修剪**：防止上下文爆炸
- ✅ **与 Researcher 互补**：形成完整的研究+处理能力

### 架构图

```
                ┌──────────────────────────────────┐
                │           Coder                  │
                │      (代码执行器)                  │
                └─────────────┬────────────────────┘
                              │
        ┌─────────────────────┼─────────────────────┐
        │                     │                     │
    ┌───▼───┐            ┌────▼────┐           ┌────▼────┐
    │ load  │───────────▶│ agent   │──────────▶│ router  │
    │       │            │(ReAct)  │           │         │
    └───────┘            └────┬────┘           └─────────┘
        │                     │                     │
        │                     │                     │
    [查找步骤]            [Python MCP]          [保存结果]
    [注入信息]             [代码执行]           [返回Team]
                              │
                    ┌─────────┴─────────┐
                    │                   │
              ┌─────▼─────┐       ┌─────▼─────┐
              │python_    │       │python_    │
              │execute    │       │install_   │
              │           │       │package    │
              └───────────┘       └───────────┘
                    │                   │
              [执行代码]          [安装依赖]
              [返回结果]          [返回状态]
                    │                   │
                    └─────────┬─────────┘
                              │
                        [LLM 观察结果]
                              │
                     [决策: 继续/完成]
                              │
                        [Final Answer]
```

Coder 是整个系统的**代码执行引擎**，将自然语言任务转化为可执行的代码并生成结果！

