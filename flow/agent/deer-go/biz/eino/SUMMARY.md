# Deer-Go 多智能体系统架构总结

## 一、系统概览

Deer-Go 是一个基于 **Eino 框架**构建的**多智能体研究系统**，通过多个专业化的 Agent 协同工作，将用户的研究需求自动分解、执行并生成高质量的研究报告。

### 核心特性

- 🤖 **8 个专业化 Agent**：各司其职，形成完整的研究工作流
- 🔄 **动态路由机制**：基于状态驱动的智能流程控制
- 👤 **人在回路**：支持人工介入的质量把关机制
- 💾 **中断与恢复**：基于 CheckPoint 的状态持久化
- 🌍 **多语言支持**：自动检测并适配用户语言
- 🔧 **工具集成**：MCP 工具动态加载（搜索、代码执行等）

---

## 二、系统架构图

### 2.1 整体流程图

```
┌─────────────────────────────────────────────────────────────────┐
│                          用户问题                                 │
└────────────────────────────┬────────────────────────────────────┘
                             │
                             ↓
                    ┌────────────────┐
                    │  Coordinator   │  1️⃣ 任务理解 & 语言检测
                    │   (协调器)      │
                    └────────┬───────┘
                             │
                    ┌────────▼────────┐
                    │ Background      │  2️⃣ 预搜索背景信息 (可选)
                    │ Investigator    │
                    └────────┬────────┘
                             │
                    ┌────────▼────────┐
                    │    Planner      │  3️⃣ 制定研究计划
                    │   (规划师)       │
                    └────────┬────────┘
                             │
                  ┌──────────▼──────────┐
                  │ has_enough_context? │
                  └──────────┬──────────┘
                             │
                    ┌────────┴────────┐
                   No                Yes
                    │                 │
           ┌────────▼────────┐        │
           │     Human       │  4️⃣ 人工确认 (条件触发)
           │  (人工反馈)      │        │
           └────────┬────────┘        │
                    │                 │
        ┌───────────┴─────────┬───────┘
       Edit                Accept
        │                     │
        ↓                     ↓
    [返回Planner]     ┌────────────────┐
                      │ ResearchTeam   │  5️⃣ 任务调度
                      │   (调度器)      │
                      └───────┬────────┘
                              │
                     ┌────────┴────────┐
                     │ 遍历 Plan.Steps │
                     └────────┬────────┘
                              │
                 ┌────────────┴────────────┐
                 │                         │
        ┌────────▼────────┐       ┌───────▼────────┐
        │   Researcher    │  6️⃣   │     Coder      │  6️⃣
        │  (研究员-ReAct) │       │ (代码执行器)    │
        └────────┬────────┘       └───────┬────────┘
                 │                         │
                 └────────────┬────────────┘
                              │
                     ┌────────▼────────┐
                     │ 所有步骤完成？   │
                     └────────┬────────┘
                              │
                             Yes
                              │
                     ┌────────▼────────┐
                     │    Reporter     │  7️⃣ 生成最终报告
                     │   (报告生成器)   │
                     └────────┬────────┘
                              │
                              ↓
                    ┌────────────────┐
                    │   最终报告      │
                    └────────────────┘
```

### 2.2 子图层次结构

```
                    ┌──────────────────────┐
                    │    Main Graph        │
                    │    (EinoDeer)        │
                    └──────────┬───────────┘
                               │
        ┌──────────────────────┼──────────────────────┐
        │                      │                      │
   ┌────▼────┐           ┌────▼────┐           ┌────▼────┐
   │Subgraph │           │Subgraph │           │Subgraph │
   │  (8个)   │           │  (8个)   │           │  (8个)   │
   └─────────┘           └─────────┘           └─────────┘

每个 Subgraph 内部结构 (大部分):
    START → load → agent → router → END
```

---

## 三、子模块功能详解

### 3.1 Builder (图构建器)

**职责**：系统的总装配线

**功能**：
- ✅ 初始化所有 8 个子图
- ✅ 建立动态路由 (`agentHandOff` + `AddBranch`)
- ✅ 配置全局状态管理 (`WithGenLocalState`)
- ✅ 编译可执行图 (`Compile`)
- ✅ 支持循环图 (`WithNodeTriggerMode(AnyPredecessor)`)

**关键设计**：
```go
// 动态路由函数：读取 state.Goto 决定下一个 Agent
func agentHandOff(ctx context.Context, input string) (next string, err error) {
    compose.ProcessState[*model.State](ctx, func(_, state *model.State) error {
        next = state.Goto  // 👈 状态驱动路由
        return nil
    })
    return next, nil
}
```

**特点**：
- 中心化路由模式
- 所有 Agent 通过统一的 `agentHandOff` 连接
- 易于扩展（新增 Agent 只需 4 步）

---

### 3.2 Coordinator (协调器)

**职责**：系统入口，任务理解与语言检测

**流程**：
```
用户问题 → load (构造Prompt) 
         → agent (LLM分析) 
         → router (解析工具调用)
```

**核心功能**：
1. **任务分析**：判断是否是有效的研究任务
2. **语言检测**：自动识别用户语言 (en-US, zh-CN, etc.)
3. **流程决策**：
   - 启用背景调查 → BackgroundInvestigator
   - 禁用背景调查 → Planner
   - 无效任务 → END

**关键输出**：
- `state.Locale`：用户语言（全局使用）
- `state.Goto`：下一步路由目标

**Tool**：
```go
hand_to_planner({
    "task_title": "任务标题",
    "locale": "zh-CN"
})
```

---

### 3.3 BackgroundInvestigator (背景调查员)

**职责**：预搜索背景信息，增强 Planner 上下文

**流程**：
```
START → search (执行搜索) → router (返回Planner) → END
```

**核心功能**：
1. **动态工具发现**：从 MCP 中查找名字包含 "search" 的工具
2. **快速搜索**：使用用户原始问题进行搜索
3. **结果保存**：`state.BackgroundInvestigationResults`

**特点**：
- 轻量级（单次搜索，无深度推理）
- 可选功能（基于 `EnableBackgroundInvestigation` 配置）
- 为 Planner 提供时效性信息

**vs. Researcher**：
| 特性 | BackgroundInvestigator | Researcher |
|------|----------------------|------------|
| 时机 | Planner **之前** | Planner **之后** |
| 深度 | 浅（单次搜索） | 深（多轮 ReAct） |
| 目的 | 辅助规划 | 执行具体步骤 |

---

### 3.4 Planner (规划师)

**职责**：核心决策 Agent，将任务分解为可执行计划

**流程**：
```
START → load (注入背景信息) 
      → agent (生成Plan-JSON) 
      → router (解析+路由)
      → END
```

**核心功能**：
1. **任务分解**：生成 `Plan{Title, Thought, Steps[]}`
2. **上下文评估**：判断 `has_enough_context`
3. **步骤分类**：区分 `research` 和 `processing` 类型
4. **迭代支持**：跟踪 `PlanIterations`

**关键数据结构**：
```go
type Plan struct {
    Locale           string  // 语言
    HasEnoughContext bool    // 👈 决定流程走向
    Thought          string  // 思考过程
    Title            string  // 任务标题
    Steps            []Step  // 具体步骤
}

type Step struct {
    NeedWebSearch bool
    Title         string
    Description   string
    StepType      StepType     // "research" or "processing"
    ExecutionRes  *string      // 执行结果 (初始 nil)
}
```

**路由决策**：
```
has_enough_context = true  → Reporter (跳过 Human，直接执行)
has_enough_context = false → Human (需要人工确认)
```

---

### 3.5 Human (人工反馈节点)

**职责**：人在回路（Human-in-the-Loop），质量把关

**流程**：
```
START → router (处理反馈) → END
```

**核心功能**：
1. **流程中断**：`InterruptAndRerun` 暂停执行
2. **等待反馈**：用户选择 Accept/Edit
3. **状态恢复**：从 CheckPoint 继续执行
4. **自动模式**：支持跳过人工确认

**工作模式**：
```go
if state.AutoAcceptedPlan {
    // 自动模式：直接执行
    state.Goto = ResearchTeam
} else {
    // 手动模式
    switch state.InterruptFeedback {
    case "accepted":
        state.Goto = ResearchTeam  // 执行
    case "edit_plan":
        state.Goto = Planner       // 重新规划
    default:
        return InterruptAndRerun   // 等待用户输入
    }
}
```

**应用场景**：
- ✅ 任务模糊需要澄清
- ✅ 敏感操作需要授权
- ✅ 资源消耗大需要确认

---

### 3.6 ResearchTeam (研究团队调度器)

**职责**：任务调度中心，遍历步骤并分发

**流程**：
```
START → load (占位) → router (查找未完成步骤) → END
```

**核心功能**：
1. **步骤遍历**：按顺序查找 `ExecutionRes == nil` 的步骤
2. **类型路由**：
   - `step_type = "research"` → Researcher
   - `step_type = "processing"` → Coder
3. **完成判断**：所有步骤完成 → Reporter
4. **迭代控制**：基于 `PlanIterations` 和 `MaxPlanIterations`

**迭代模式**：
```
ResearchTeam → Researcher → ResearchTeam  ┐
            ↘ Coder → ResearchTeam         ├─ 循环
                      ↓                    ┘
                (所有步骤完成)
                      ↓
                   Reporter
```

**特点**：
- 纯逻辑路由（无 LLM 调用）
- 简单高效
- 支持任意数量的步骤

---

### 3.7 Researcher (研究员)

**职责**：执行 `research` 类型步骤，深度研究

**流程**：
```
START → load (注入步骤信息) 
      → agent (ReAct Agent + Web Search) 
      → router (保存结果)
      → END
```

**核心功能**：
1. **ReAct 推理**：多轮思考-行动-观察循环
2. **工具使用**：Web Search, Wikipedia, etc.
3. **结果保存**：`step.ExecutionRes = LLM的最终输出`
4. **返回调度**：完成后返回 ResearchTeam

**ReAct 循环示例**：
```
Round 1: 思考 → 调用 search("AI trends") → 观察结果
Round 2: 思考 → 调用 wikipedia("GPT-4") → 观察结果
Round 3: 思考 → 生成最终答案
```

**工具加载**：
```go
// 加载所有名字包含 "search" 的工具
if strings.HasSuffix(info.Name, "search") {
    researchTools = append(researchTools, t)
}
```

---

### 3.8 Coder (代码执行器)

**职责**：执行 `processing` 类型步骤，代码生成与运行

**流程**：
```
START → load (注入步骤信息) 
      → agent (ReAct Agent + Python MCP) 
      → router (保存结果)
      → END
```

**核心功能**：
1. **代码生成**：根据步骤描述生成 Python 代码
2. **代码执行**：通过 Python MCP 执行代码
3. **迭代调试**：ReAct 模式自动修复错误
4. **消息修剪**：`modifyCoderfunc` 防止上下文爆炸

**工具加载**：
```go
// 只加载 Python 相关工具
if strings.HasPrefix(mcpName, "python") {
    coderTools = append(coderTools, ts...)
}
```

**典型任务**：
- 数据处理（Pandas）
- 图表生成（Matplotlib）
- 数学计算（NumPy）
- 文件操作

**vs. Researcher**：
| 特性 | Researcher | Coder |
|------|-----------|-------|
| 工具 | Web Search | Python MCP |
| 输出 | 文字总结 | 代码、图表 |
| MaxStep | ~20 | 40 |

---

### 3.9 Reporter (报告生成器)

**职责**：最终输出节点，生成结构化报告

**流程**：
```
START → load (汇总所有结果) 
      → agent (生成Markdown报告) 
      → router (记录+结束)
      → END
```

**核心功能**：
1. **结果汇总**：收集所有 `step.ExecutionRes`
2. **报告生成**：调用 LLM 生成结构化 Markdown
3. **格式规范**：确保包含必需部分
4. **流程终止**：`state.Goto = END`

**报告结构**：
```markdown
# [标题]

## Key Points
- 要点 1
- 要点 2

## Overview
概述...

## Detailed Analysis
详细分析...

| 对比项 | 数据1 | 数据2 |
|--------|-------|-------|
| ...    | ...   | ...   |

## Key Citations
- [来源1](URL)
- [来源2](URL)
```

**格式要求**：
- 优先使用 Markdown 表格
- 引用集中在末尾
- 包含所有必需部分

---

## 四、核心设计机制

### 4.1 状态管理

**共享状态**：
```go
type State struct {
    // 用户输入
    Messages []*schema.Message
    
    // 路由控制
    Goto string  // 👈 核心：决定下一个 Agent
    
    // 语言与配置
    Locale       string
    MaxStepNum   int
    MaxPlanIterations int
    
    // 计划与执行
    CurrentPlan   *Plan
    PlanIterations int
    
    // 背景调查
    EnableBackgroundInvestigation  bool
    BackgroundInvestigationResults string
    
    // 人工反馈
    AutoAcceptedPlan  bool
    InterruptFeedback string
}
```

**状态传递**：
- 通过 `context.Context` 隐式传递
- `compose.ProcessState` 确保并发安全
- 所有 Agent 共享同一个 State 实例

### 4.2 动态路由机制

**核心原理**：
```
Agent 内部:
  router 节点设置 → state.Goto = "next_agent"

主图:
  AddBranch(..., agentHandOff, ...)

agentHandOff:
  读取 state.Goto → 返回下一个节点名

主图引擎:
  根据返回值路由到对应 Agent
```

**优势**：
- ✅ 灵活：支持任意 Agent 间的跳转
- ✅ 可扩展：新增 Agent 无需修改路由逻辑
- ✅ 可追踪：所有路由都经过 `agentHandOff`，便于日志

### 4.3 中断与恢复

**CheckPoint 机制**：
```go
// 配置
compose.WithCheckPointStore(model.NewDeerCheckPoint(ctx))

// 触发中断
return compose.InterruptAndRerun

// 恢复执行
runnable.Generate(ctx, compose.WithCheckPointID(checkpointID))
```

**存储内容**：
- 全局 State
- 各节点输入
- 当前执行位置

**应用**：
- Human 节点等待用户反馈
- 系统崩溃后恢复
- 长时间任务的分段执行

### 4.4 工具动态加载

**MCP 工具集成**：
```go
// 从多个 MCP Server 加载工具
for mcpName, cli := range infra.MCPServer {
    ts, err := mcp.GetTools(ctx, &mcp.Config{Cli: cli})
    
    // 根据命名约定过滤
    for _, t := range ts {
        info, _ := t.Info(ctx)
        
        // Researcher: 搜索工具
        if strings.HasSuffix(info.Name, "search") {
            researchTools = append(researchTools, t)
        }
        
        // Coder: Python 工具
        if strings.HasPrefix(mcpName, "python") {
            coderTools = append(coderTools, t)
        }
    }
}
```

**优势**：
- 动态发现工具
- 无需硬编码
- 易于扩展新工具

---

## 五、完整执行流程示例

### 场景：研究 2025 年 AI 趋势

```
═══════════════════════════════════════════════════════════
用户输入
═══════════════════════════════════════════════════════════
"What are the latest AI trends in 2025?"

配置:
  EnableBackgroundInvestigation = true
  AutoAcceptedPlan = false

═══════════════════════════════════════════════════════════
1️⃣ Coordinator
═══════════════════════════════════════════════════════════
load:   构造 Prompt (System + User Message)
agent:  LLM 分析任务，调用 hand_to_planner
        Arguments: {
          "task_title": "AI Trends 2025",
          "locale": "en-US"
        }
router: state.Locale = "en-US"
        state.Goto = "background_investigator"

═══════════════════════════════════════════════════════════
2️⃣ BackgroundInvestigator
═══════════════════════════════════════════════════════════
search: 查找 Brave Search 工具
        执行: search("What are the latest AI trends in 2025?")
        结果: "Multimodal AI, AGI progress, LLM improvements..."
        保存: state.BackgroundInvestigationResults = "..."
router: state.Goto = "planner"

═══════════════════════════════════════════════════════════
3️⃣ Planner
═══════════════════════════════════════════════════════════
load:   Prompt 包含背景调查结果
agent:  LLM 生成 Plan:
        {
          "locale": "en-US",
          "has_enough_context": true,
          "title": "AI Trends Research 2025",
          "steps": [
            {
              "title": "Research Multimodal AI",
              "step_type": "research",
              "execution_res": null
            },
            {
              "title": "Research AGI Progress",
              "step_type": "research",
              "execution_res": null
            },
            {
              "title": "Generate Comparison Charts",
              "step_type": "processing",
              "execution_res": null
            }
          ]
        }
router: state.CurrentPlan = {...}
        state.PlanIterations = 1
        has_enough_context = true
        state.Goto = "reporter"  // 👈 跳过 Human

[注: 如果 has_enough_context = false，会进入 Human 节点]

═══════════════════════════════════════════════════════════
4️⃣ ResearchTeam (第1轮)
═══════════════════════════════════════════════════════════
router: 遍历 steps，找到 Step 0 (ExecutionRes == null)
        StepType = "research"
        state.Goto = "researcher"

═══════════════════════════════════════════════════════════
5️⃣ Researcher (执行 Step 0)
═══════════════════════════════════════════════════════════
load:   注入步骤信息:
        "Task: Research Multimodal AI
         Description: Investigate multimodal AI..."
         
agent:  ReAct Agent 执行:
        Round 1: 思考 → search("multimodal AI 2025")
        Round 2: 思考 → search("GPT-4V capabilities")
        Round 3: 思考 → 生成总结
        最终输出: "Multimodal AI has made significant progress..."
        
router: 找到 Step 0，保存结果:
        Steps[0].ExecutionRes = &"Multimodal AI has made..."
        state.Goto = "research_team"

═══════════════════════════════════════════════════════════
6️⃣ ResearchTeam (第2轮)
═══════════════════════════════════════════════════════════
router: Step 0 已完成，找到 Step 1 (ExecutionRes == null)
        StepType = "research"
        state.Goto = "researcher"

═══════════════════════════════════════════════════════════
7️⃣ Researcher (执行 Step 1)
═══════════════════════════════════════════════════════════
[类似 Step 0 的流程]
router: Steps[1].ExecutionRes = &"AGI progress shows..."
        state.Goto = "research_team"

═══════════════════════════════════════════════════════════
8️⃣ ResearchTeam (第3轮)
═══════════════════════════════════════════════════════════
router: Step 0, 1 已完成，找到 Step 2
        StepType = "processing"
        state.Goto = "coder"

═══════════════════════════════════════════════════════════
9️⃣ Coder (执行 Step 2)
═══════════════════════════════════════════════════════════
load:   注入步骤信息:
        "Task: Generate Comparison Charts
         Description: Create charts comparing AI models..."
         
agent:  ReAct Agent 执行:
        Round 1: 思考 → python_execute(生成图表代码)
        Round 2: 思考 → python_execute(验证文件)
        最终输出: "Chart generated: comparison.png"
        
router: Steps[2].ExecutionRes = &"Chart generated..."
        state.Goto = "research_team"

═══════════════════════════════════════════════════════════
🔟 ResearchTeam (第4轮)
═══════════════════════════════════════════════════════════
router: 所有步骤完成 (ExecutionRes 都非空)
        state.Goto = "reporter"

═══════════════════════════════════════════════════════════
1️⃣1️⃣ Reporter
═══════════════════════════════════════════════════════════
load:   汇总所有结果:
        - Task: AI Trends Research 2025
        - Step 0 结果: "Multimodal AI has made..."
        - Step 1 结果: "AGI progress shows..."
        - Step 2 结果: "Chart generated..."
        
agent:  LLM 生成结构化报告:
        # AI Trends Research 2025
        
        ## Key Points
        - Multimodal AI is leading innovation
        - AGI shows significant progress
        - Performance comparison available
        
        ## Overview
        The AI landscape in 2025...
        
        ## Detailed Analysis
        ### Multimodal AI
        ...
        
        | Model | Score | Features |
        |-------|-------|----------|
        | GPT-4 | 95    | Vision   |
        | ...   | ...   | ...      |
        
        ## Key Citations
        - [OpenAI GPT-4](https://...)
        
router: 记录报告
        state.Goto = "END"

═══════════════════════════════════════════════════════════
最终输出
═══════════════════════════════════════════════════════════
完整的 Markdown 研究报告
```

---

## 六、关键设计特点

### 6.1 模块化设计

**每个 Agent 都是独立子图**：
- ✅ 可独立开发和测试
- ✅ 职责单一，易于维护
- ✅ 统一的三节点结构（load → agent → router）

### 6.2 状态驱动

**所有路由决策都基于共享状态**：
```go
// 不是硬编码的流程
Coordinator → Planner → ResearchTeam → Reporter

// 而是动态的状态驱动
每个 Agent: state.Goto = "next_agent"
主图: 根据 state.Goto 路由
```

**优势**：
- 灵活的流程控制
- 支持循环和条件跳转
- 易于调试和追踪

### 6.3 工具专用化

**不同 Agent 使用不同工具集**：
- Researcher → Web Search
- Coder → Python MCP
- BackgroundInvestigator → Search

**避免工具混乱**：
- LLM 选择工具更准确
- 减少无关工具干扰
- 提高执行效率

### 6.4 人机协作

**人在回路（Human-in-the-Loop）**：
- 关键决策点人工介入
- 支持自动和手动模式
- 基于 CheckPoint 的中断恢复

### 6.5 多语言支持

**全局语言一致性**：
```go
Coordinator: 检测 locale = "zh-CN"
所有 Agent: 使用 state.Locale 生成对应语言的输出
```

---

## 七、性能与可扩展性

### 7.1 性能考虑

**潜在瓶颈**：
- 状态锁竞争（所有 Agent 通过 `ProcessState` 访问）
- 长上下文处理（Reporter 汇总所有结果）
- 工具调用延迟（外部 API 依赖）

**优化策略**：
- 消息修剪（`modifyCoderfunc`）
- 缓存 Prompt 模板
- 并行执行独立步骤（未来优化）

### 7.2 扩展性

**新增 Agent 的步骤**：
1. 创建子图构造函数 `NewMyAgent[I, O any](ctx)`
2. 在 `Builder` 中初始化: `myAgentGraph := NewMyAgent[I, O](ctx)`
3. 添加到 `outMap`: `consts.MyAgent: true`
4. 添加节点和分支:
   ```go
   g.AddGraphNode(consts.MyAgent, myAgentGraph, ...)
   g.AddBranch(consts.MyAgent, compose.NewGraphBranch(agentHandOff, outMap))
   ```

**无需修改**：
- `agentHandOff` 函数（通用路由）
- 其他 Agent 的代码

---

## 八、监控与调试

### 8.1 关键监控指标

| Agent | 关键指标 |
|-------|----------|
| **Coordinator** | hand_to_planner 调用成功率、locale 检测准确率 |
| **Planner** | JSON 解析成功率、has_enough_context=true 比例 |
| **Human** | 中断率、平均等待时间、编辑率 |
| **ResearchTeam** | ResearchTeam 循环次数、平均步骤数 |
| **Researcher** | 平均轮次、工具调用成功率 |
| **Coder** | 代码执行成功率、平均执行时间 |
| **Reporter** | 报告生成成功率、格式合规率 |

### 8.2 日志追踪

**关键日志点**：
```go
// agentHandOff
ilog.EventInfo(ctx, "agent_hand_off", "input", input, "next", next)

// 各 Agent 的 router
ilog.EventInfo(ctx, "xxx_end", "plan", state.CurrentPlan)
```

**调试流程**：
1. 查看 `agent_hand_off` 日志追踪路由链
2. 检查各 Agent 的输入输出
3. 分析状态变化（`state.Goto`, `state.CurrentPlan`）

---

## 九、使用场景

### 9.1 适用场景

✅ **研究型任务**
- 市场调研报告
- 技术趋势分析
- 竞品对比分析

✅ **数据处理 + 分析**
- 数据可视化
- 统计分析报告
- 自动化数据处理

✅ **知识整合**
- 多源信息汇总
- 文献综述
- 主题深度分析

### 9.2 不适用场景

❌ **实时对话**
- 系统设计为任务型，不适合闲聊
- 多步骤执行，延迟较高

❌ **简单问答**
- 过于复杂，杀鸡用牛刀
- 建议直接调用单个 LLM

❌ **创意生成**
- 流程固定，限制创造性
- 更适合结构化任务

---

## 十、总结

### 核心价值

Deer-Go 是一个**生产级的多智能体研究系统**，实现了：

1. **智能任务分解**：Planner 将复杂任务拆分为可执行步骤
2. **专业化执行**：Researcher（研究）+ Coder（处理）分工协作
3. **质量保障**：Human 节点的人工把关机制
4. **灵活路由**：状态驱动的动态流程控制
5. **工具集成**：MCP 工具的动态加载和专用化
6. **可靠性**：CheckPoint 机制的中断与恢复

### 设计亮点

- ✅ **8 个专业 Agent**：各司其职，模块化设计
- ✅ **动态路由**：`agentHandOff` + `state.Goto` 实现灵活流程
- ✅ **状态共享**：通过 `context.Context` 传递全局状态
- ✅ **人机协作**：Human-in-the-Loop 机制
- ✅ **工具专用化**：不同 Agent 使用不同工具集
- ✅ **多语言支持**：自动检测并适配

### 技术栈

- **框架**：Eino (Compose Graph)
- **LLM**：OpenAI/Anthropic/Google (可配置)
- **工具协议**：MCP (Model Context Protocol)
- **语言**：Go
- **特性**：ReAct、CheckPoint、流式输出

---

## 相关文档

- `builder逻辑分析.md` - 图构建器详解
- `coordinator逻辑分析.md` - 协调器详解
- `planner逻辑分析.md` - 规划师详解
- `research_team逻辑分析.md` - 调度器详解
- `Researcher执行逻辑分析.md` - 研究员详解
- `coder逻辑分析.md` - 代码执行器详解
- `reporter逻辑分析.md` - 报告生成器详解
- `investigator逻辑分析.md` - 背景调查员详解
- `human_feedback逻辑分析.md` - 人工反馈详解

---

**最后更新**: 2025-10-22

**版本**: v1.0

**作者**: Eino Deer-Go Team

