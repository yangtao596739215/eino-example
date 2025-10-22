# ResearchTeam（研究团队调度器）逻辑分析

## 一、概述

`research_team.go` 实现了 **ResearchTeam（研究团队调度器）** 子图，它是整个 deer-go 系统的**任务调度中心**，负责遍历 Plan 中的所有步骤，动态分配给不同的执行 Agent（Researcher 或 Coder），形成一个迭代式的执行循环。

### 在系统中的位置

```
Planner → Human → ResearchTeam ⇄ Researcher/Coder
                       ↓
                    Reporter
```

### 核心职责

1. **步骤遍历**：按顺序遍历 `state.CurrentPlan.Steps`
2. **动态分发**：根据步骤类型（research/processing）路由到对应 Agent
3. **进度管理**：跟踪哪些步骤已完成（`ExecutionRes != nil`）
4. **完成判断**：所有步骤完成后路由到 Reporter

---

## 二、核心组件分析

### 2.1 `loadResearchTeamMsg` 函数（29-36行）

**作用**：简单的占位函数，返回空字符串

#### 实现逻辑

```go
func loadResearchTeamMsg(ctx context.Context, name string, opts ...any) (output string, err error) {
    err = compose.ProcessState[*model.State](ctx, func(_ context.Context, state *model.State) error {
        output = ""  // 👈 不需要加载任何消息
        return nil
    })
    return output, err
}
```

#### 设计说明

**为什么是空字符串？**

ResearchTeam 不需要调用 LLM 或加载 Prompt，它的逻辑是**纯粹的调度逻辑**：
- 不生成内容
- 不做推理
- 只是根据 `state.CurrentPlan` 的状态做路由决策

**作用**：
- 保持子图结构的一致性（load → agent → router）
- 占位节点，符合框架的三节点模式
- 未来可以扩展为加载调度配置等

---

### 2.2 `routerResearchTeam` 函数（38-64行）

**作用**：ResearchTeam 的核心逻辑，遍历步骤并动态路由

#### 实现逻辑

```go
func routerResearchTeam(ctx context.Context, input string, opts ...any) (output string, err error) {
    err = compose.ProcessState[*model.State](ctx, func(_ context.Context, state *model.State) error {
        defer func() {
            output = state.Goto  // 返回路由目标
        }()
        
        // 默认值：返回 Planner（重新规划）
        state.Goto = consts.Planner
        
        // 检查是否有计划
        if state.CurrentPlan == nil {
            return nil  // 无计划 → 返回 Planner
        }
        
        // 遍历所有步骤，找到第一个未完成的步骤
        for i, step := range state.CurrentPlan.Steps {
            if step.ExecutionRes == nil {  // 👈 未执行
                continue  // 跳过，继续找
            }
            
            ilog.EventInfo(ctx, "research_team_step", "step", step, "index", i)
            
            // 根据步骤类型路由
            switch step.StepType {
            case model.Research:
                state.Goto = consts.Researcher  // 👈 研究类步骤
                return nil
            case model.Processing:
                state.Goto = consts.Coder  // 👈 处理类步骤
                return nil
            }
        }
        
        // 所有步骤都执行完成，检查是否需要重新规划
        if state.PlanIterations >= state.MaxPlanIterations {
            state.Goto = consts.Reporter  // 👈 达到最大迭代次数，生成报告
            return nil
        }
        
        // 未达到最大迭代次数，返回 Planner 重新规划
        return nil  // state.Goto = Planner
    })
    return output, nil
}
```

#### 关键逻辑

**1. 查找未完成步骤**

```go
for i, step := range state.CurrentPlan.Steps {
    if step.ExecutionRes == nil {  // 未完成
        continue
    }
    // 找到第一个未完成的步骤...
}
```

**等等，这里逻辑有问题！** 

让我重新审视代码：

```go
if step.ExecutionRes == nil {
    continue  // 👈 这里应该是找未完成的，但却 continue 了
}
```

**正确理解**：这段代码应该是找**第一个已完成但未被处理的步骤**，或者是逻辑错误。

让我重新分析（基于实际的运行逻辑）：

实际上，这段代码的逻辑应该是：
- `ExecutionRes == nil` 表示步骤**还未执行**
- 代码遍历找到**第一个未执行**的步骤
- 然后根据类型路由到相应的 Agent

**修正后的理解**：

```go
for i, step := range state.CurrentPlan.Steps {
    if step.ExecutionRes != nil {  // 👈 应该是 != nil（已完成，跳过）
        continue
    }
    
    // 找到第一个未完成的步骤
    switch step.StepType {
    case model.Research:
        state.Goto = consts.Researcher
        return nil
    case model.Processing:
        state.Goto = consts.Coder
        return nil
    }
}
```

**但原代码写的是 `== nil`，让我重新理解原意：**

仔细看原代码：
```go
if step.ExecutionRes == nil {
    continue  // 跳过未执行的
}
// 这里是已执行的步骤...
```

这说明代码在找**第一个已执行的步骤**，然后根据类型路由。这个逻辑似乎不太合理。

**让我查看 Researcher 和 Coder 的逻辑来理解**：

根据之前看到的 `researcher.go` 和 `coder.go`，它们的 `router` 函数会：
1. 找到第一个 `ExecutionRes == nil` 的步骤
2. 执行后设置 `ExecutionRes = result`
3. 返回 `ResearchTeam`

所以 **ResearchTeam 的逻辑应该是**：
- 找到第一个 `ExecutionRes == nil`（未执行）的步骤
- 路由到对应的 Agent 执行

**代码可能有bug，或者我理解有误。让我基于合理的逻辑来分析：**

---

### 2.2（修正版）`routerResearchTeam` 逻辑分析

**合理的逻辑应该是**：

```go
func routerResearchTeam(ctx context.Context, input string, opts ...any) (output string, err error) {
    err = compose.ProcessState[*model.State](ctx, func(_ context.Context, state *model.State) error {
        defer func() {
            output = state.Goto
        }()
        
        state.Goto = consts.Planner  // 默认：重新规划
        
        if state.CurrentPlan == nil {
            return nil
        }
        
        // 遍历步骤，找到第一个未执行的
        for i, step := range state.CurrentPlan.Steps {
            if step.ExecutionRes != nil {  // 👈 已执行，跳过
                continue
            }
            
            ilog.EventInfo(ctx, "research_team_step", "step", step, "index", i)
            
            // 找到未执行的步骤，根据类型路由
            switch step.StepType {
            case model.Research:
                state.Goto = consts.Researcher
                return nil
            case model.Processing:
                state.Goto = consts.Coder
                return nil
            }
        }
        
        // 所有步骤都完成了
        if state.PlanIterations >= state.MaxPlanIterations {
            state.Goto = consts.Reporter  // 生成报告
            return nil
        }
        
        // 可能需要更多迭代，返回 Planner
        return nil
    })
    return output, nil
}
```

**执行流程**：

```
1. 检查 state.CurrentPlan 是否存在
2. 遍历 steps，找到第一个 ExecutionRes == nil 的步骤
3. 根据 step.StepType 路由：
   - Research → Researcher
   - Processing → Coder
4. 如果所有步骤都完成：
   - 检查迭代次数
   - >= MaxPlanIterations → Reporter
   - < MaxPlanIterations → Planner（可能重新规划）
```

---

### 2.3 `NewResearchTeamNode` 函数（66-76行）

**作用**：构建 ResearchTeam 子图

#### 子图结构

```
START → load → router → END
```

#### 实现代码

```go
func NewResearchTeamNode[I, O any](ctx context.Context) *compose.Graph[I, O] {
    cag := compose.NewGraph[I, O]()
    
    _ = cag.AddLambdaNode("load", compose.InvokableLambdaWithOption(loadResearchTeamMsg))
    _ = cag.AddLambdaNode("router", compose.InvokableLambdaWithOption(routerResearchTeam))
    
    _ = cag.AddEdge(compose.START, "load")
    _ = cag.AddEdge("load", "router")
    _ = cag.AddEdge("router", compose.END)
    
    return cag
}
```

#### 特点

**只有 2 个有效节点**：
- `load`：占位节点（返回空字符串）
- `router`：核心调度逻辑

**没有 agent 节点**：
- 不需要调用 LLM
- 纯粹的逻辑路由
- 比其他子图更简单

---

## 三、执行流程分析

### 3.1 场景：3 个步骤的计划

```
Plan:
  Step 0: Research - "Research AI trends" → ExecutionRes = null
  Step 1: Research - "Analyze adoption" → ExecutionRes = null  
  Step 2: Processing - "Generate charts" → ExecutionRes = null
```

#### 执行循环

```
═══════════════════════════════════════════════════════════
第 1 轮：ResearchTeam 执行
═══════════════════════════════════════════════════════════

1️⃣ load 节点
   └─ 输出: ""

2️⃣ router 节点
   ├─ 遍历 steps:
   │  ├─ Step 0: ExecutionRes == null → 找到！
   │  └─ StepType = Research
   ├─ 决策: state.Goto = "researcher"
   └─ 输出: "researcher"

3️⃣ 返回主图 → Researcher 执行
   ├─ Researcher 执行 Step 0
   ├─ 完成后: Step 0.ExecutionRes = "AI trends research result..."
   └─ Researcher.router: state.Goto = "research_team"

═══════════════════════════════════════════════════════════
第 2 轮：ResearchTeam 执行
═══════════════════════════════════════════════════════════

1️⃣ router 节点
   ├─ 遍历 steps:
   │  ├─ Step 0: ExecutionRes != null → 跳过
   │  ├─ Step 1: ExecutionRes == null → 找到！
   │  └─ StepType = Research
   ├─ 决策: state.Goto = "researcher"
   └─ 输出: "researcher"

2️⃣ 返回主图 → Researcher 执行
   ├─ Researcher 执行 Step 1
   ├─ 完成后: Step 1.ExecutionRes = "Adoption analysis result..."
   └─ Researcher.router: state.Goto = "research_team"

═══════════════════════════════════════════════════════════
第 3 轮：ResearchTeam 执行
═══════════════════════════════════════════════════════════

1️⃣ router 节点
   ├─ 遍历 steps:
   │  ├─ Step 0: ExecutionRes != null → 跳过
   │  ├─ Step 1: ExecutionRes != null → 跳过
   │  ├─ Step 2: ExecutionRes == null → 找到！
   │  └─ StepType = Processing
   ├─ 决策: state.Goto = "coder"
   └─ 输出: "coder"

2️⃣ 返回主图 → Coder 执行
   ├─ Coder 执行 Step 2 (运行 Python 代码生成图表)
   ├─ 完成后: Step 2.ExecutionRes = "Charts generated..."
   └─ Coder.router: state.Goto = "research_team"

═══════════════════════════════════════════════════════════
第 4 轮：ResearchTeam 执行
═══════════════════════════════════════════════════════════

1️⃣ router 节点
   ├─ 遍历 steps:
   │  ├─ Step 0: ExecutionRes != null → 跳过
   │  ├─ Step 1: ExecutionRes != null → 跳过
   │  └─ Step 2: ExecutionRes != null → 跳过
   ├─ 所有步骤完成！
   ├─ 检查: state.PlanIterations = 1 < state.MaxPlanIterations = 3
   └─ 决策: state.Goto = "planner"  // 👈 可能需要重新规划？

2️⃣ 返回主图 → Planner
   └─ （实际上，通常在所有步骤完成后应该去 Reporter）

═══════════════════════════════════════════════════════════
注：这里的逻辑可能需要调整，通常应该是：
  - 所有步骤完成 → Reporter（生成最终报告）
  - 而不是回到 Planner
═══════════════════════════════════════════════════════════
```

### 3.2 优化后的逻辑

**建议的 router 逻辑**：

```go
func routerResearchTeam(ctx context.Context, input string, opts ...any) (output string, err error) {
    err = compose.ProcessState[*model.State](ctx, func(_ context.Context, state *model.State) error {
        defer func() {
            output = state.Goto
        }()
        
        if state.CurrentPlan == nil {
            state.Goto = compose.END
            return nil
        }
        
        // 找到第一个未完成的步骤
        for i, step := range state.CurrentPlan.Steps {
            if step.ExecutionRes != nil {
                continue  // 已完成，跳过
            }
            
            // 找到未完成的步骤
            ilog.EventInfo(ctx, "research_team_dispatch", "step", step, "index", i)
            
            switch step.StepType {
            case model.Research:
                state.Goto = consts.Researcher
                return nil
            case model.Processing:
                state.Goto = consts.Coder
                return nil
            }
        }
        
        // 所有步骤都完成，直接生成报告
        state.Goto = consts.Reporter
        return nil
    })
    return output, nil
}
```

---

## 四、设计模式分析

### 4.1 迭代器模式（Iterator Pattern）

**ResearchTeam 作为步骤迭代器**：

```go
// 伪代码表示
type StepIterator struct {
    steps   []Step
    current int
}

func (it *StepIterator) Next() *Step {
    for it.current < len(it.steps) {
        step := &it.steps[it.current]
        it.current++
        if step.ExecutionRes == nil {
            return step  // 返回未完成的步骤
        }
    }
    return nil  // 所有步骤完成
}
```

**实际实现**：
```go
for i, step := range state.CurrentPlan.Steps {
    if step.ExecutionRes != nil {
        continue
    }
    // 处理当前步骤...
    return
}
```

### 4.2 策略模式（Strategy Pattern）

**根据步骤类型选择执行策略**：

```go
switch step.StepType {
case model.Research:
    // 策略A: 使用 Researcher（ReAct Agent + 搜索工具）
    state.Goto = consts.Researcher
case model.Processing:
    // 策略B: 使用 Coder（ReAct Agent + Python MCP）
    state.Goto = consts.Coder
}
```

### 4.3 责任链模式（Chain of Responsibility）

**ResearchTeam ⇄ Researcher/Coder 循环**：

```
ResearchTeam:
  ├─ 职责: 分发未完成的步骤
  └─ 传递: 将步骤交给执行者

Researcher/Coder:
  ├─ 职责: 执行具体步骤
  └─ 返回: 将控制权返回 ResearchTeam

ResearchTeam:
  ├─ 检查: 是否还有未完成的步骤
  └─ 决策: 继续分发 / 完成汇总
```

---

## 五、与其他 Agent 的协作

### 5.1 ResearchTeam ← Researcher

**Researcher 的返回逻辑**：

```go
// researcher.go
func routerResearcher(ctx context.Context, input *schema.Message, opts ...any) (output string, err error) {
    compose.ProcessState[*model.State](ctx, func(_, state *model.State) error {
        // 找到当前执行的步骤
        for i, step := range state.CurrentPlan.Steps {
            if step.ExecutionRes == nil {
                // 保存执行结果
                state.CurrentPlan.Steps[i].ExecutionRes = &input.Content
                break
            }
        }
        
        state.Goto = consts.ResearchTeam  // 👈 返回 ResearchTeam
        return nil
    })
}
```

**协作流程**：

```
ResearchTeam:
  └─ 分发: state.Goto = "researcher"

Researcher:
  ├─ 执行: ReAct Agent 进行研究
  ├─ 保存: step.ExecutionRes = result
  └─ 返回: state.Goto = "research_team"

ResearchTeam:
  └─ 继续分发下一个步骤...
```

### 5.2 ResearchTeam ← Coder

**Coder 的逻辑类似**：

```go
// coder.go
func routerCoder(ctx context.Context, input *schema.Message, opts ...any) (output string, err error) {
    compose.ProcessState[*model.State](ctx, func(_, state *model.State) error {
        for i, step := range state.CurrentPlan.Steps {
            if step.ExecutionRes == nil {
                state.CurrentPlan.Steps[i].ExecutionRes = &input.Content
                break
            }
        }
        
        state.Goto = consts.ResearchTeam  // 👈 返回 ResearchTeam
        return nil
    })
}
```

---

## 六、状态跟踪机制

### 6.1 ExecutionRes 作为进度标记

```go
type Step struct {
    Title        string   `json:"title"`
    Description  string   `json:"description"`
    StepType     StepType `json:"step_type"`
    ExecutionRes *string  `json:"execution_res,omitempty"`  // 👈 关键字段
}
```

**状态变化**：

```
初始状态:
  ExecutionRes = nil  // 未执行

执行中:
  Researcher/Coder 处理步骤

执行完成:
  ExecutionRes = &"result content"  // 指针非空
```

**进度计算**：

```go
func calculateProgress(plan *model.Plan) (completed, total int) {
    total = len(plan.Steps)
    for _, step := range plan.Steps {
        if step.ExecutionRes != nil {
            completed++
        }
    }
    return
}

// 使用示例
completed, total := calculateProgress(state.CurrentPlan)
progress := float64(completed) / float64(total) * 100
// progress = 66.67% (2 out of 3 steps completed)
```

---

## 七、边界情况处理

### 7.1 无计划

```go
if state.CurrentPlan == nil {
    state.Goto = consts.Planner  // 返回 Planner 生成计划
    return nil
}
```

**场景**：
- 系统错误导致计划丢失
- 中断恢复时计划未正确恢复
- 建议：添加日志和告警

### 7.2 空步骤列表

```go
for i, step := range state.CurrentPlan.Steps {
    // 如果 Steps = []，循环不会执行
}
// 直接跳到后续逻辑
```

**当前行为**：
- 所有步骤"完成"（因为没有步骤）
- 可能路由到 Planner 或 Reporter

**建议处理**：
```go
if len(state.CurrentPlan.Steps) == 0 {
    ilog.EventWarn(ctx, "empty_plan_steps")
    state.Goto = compose.END  // 或返回 Planner
    return nil
}
```

### 7.3 未知步骤类型

```go
switch step.StepType {
case model.Research:
    state.Goto = consts.Researcher
case model.Processing:
    state.Goto = consts.Coder
// 缺少 default 分支
}
```

**潜在问题**：
- 如果 LLM 生成了新的 `step_type`（如 `"analysis"`）
- 没有匹配的路由
- 步骤会被跳过

**建议添加**：
```go
switch step.StepType {
case model.Research:
    state.Goto = consts.Researcher
    return nil
case model.Processing:
    state.Goto = consts.Coder
    return nil
default:
    ilog.EventError(ctx, fmt.Errorf("unknown step type: %s", step.StepType))
    state.Goto = consts.Researcher  // 默认当作研究步骤
    return nil
}
```

---

## 八、性能与优化

### 8.1 潜在优化：并行执行

**当前实现**：顺序执行所有步骤

**优化方案**：并行执行独立的步骤

```go
// 当前：顺序执行
Step 0 (Research) → Step 1 (Research) → Step 2 (Processing)
总时间 = T0 + T1 + T2

// 优化：并行执行
Step 0 (Research)  ┐
Step 1 (Research)  ├─ 并行
Step 2 (Processing)┘
总时间 = max(T0, T1, T2)
```

**实现挑战**：
- 需要分析步骤间的依赖关系
- 需要修改 Graph 结构支持并行节点
- 需要同步机制等待所有并行步骤完成

### 8.2 进度报告

**当前缺失**：用户不知道执行进度

**建议添加**：
```go
completed, total := 0, len(state.CurrentPlan.Steps)
for _, step := range state.CurrentPlan.Steps {
    if step.ExecutionRes != nil {
        completed++
    }
}

ilog.EventInfo(ctx, "research_team_progress", 
    "completed", completed, 
    "total", total, 
    "progress", float64(completed)/float64(total)*100)

// 如果支持流式输出，可以推送进度事件
emitProgressEvent(ctx, completed, total)
```

---

## 九、监控指标

### 9.1 关键指标

| 指标 | 含义 | 用途 |
|------|------|------|
| **平均步骤数** | 每个 Plan 的平均步骤数 | 评估任务复杂度 |
| **Research vs Processing 比例** | 两类步骤的比例 | 资源分配优化 |
| **ResearchTeam 循环次数** | 从进入到所有步骤完成的循环次数 | 评估执行效率 |
| **单步骤平均执行时间** | Researcher/Coder 的平均执行时间 | 性能优化目标 |
| **步骤失败率** | ExecutionRes 包含错误的比例 | 质量监控 |

### 9.2 异常检测

**建议监控**：
- ResearchTeam 循环次数 > Plan.Steps 数量 * 2（可能陷入死循环）
- 单个步骤执行时间 > 5 分钟（可能卡住）
- 连续多个步骤失败（系统性问题）

---

## 十、总结

### 核心价值

ResearchTeam 实现了一个**轻量级的任务调度器**：

1. **顺序调度**：按 Plan 中的步骤顺序执行
2. **类型路由**：根据步骤类型分发到专业 Agent
3. **进度跟踪**：通过 `ExecutionRes` 跟踪执行状态
4. **循环控制**：完成所有步骤后路由到下一阶段

### 设计亮点

- ✅ **简单高效**：纯逻辑路由，无需 LLM
- ✅ **状态驱动**：基于 `ExecutionRes` 判断进度
- ✅ **类型分发**：Research → Researcher, Processing → Coder
- ✅ **迭代支持**：与 Researcher/Coder 形成循环

### 架构图

```
                ┌──────────────────────────────────┐
                │       ResearchTeam               │
                │      (任务调度中心)                │
                └─────────────┬────────────────────┘
                              │
                   ┌──────────▼──────────┐
                   │  遍历 Plan.Steps    │
                   │ 找未完成的步骤(*)    │
                   └──────────┬──────────┘
                              │
                ┌─────────────┴─────────────┐
                │                           │
         ┌──────▼──────┐           ┌───────▼──────┐
         │ Research 类型│           │Processing类型 │
         └──────┬──────┘           └───────┬──────┘
                │                           │
                ↓                           ↓
         ┌───────────┐              ┌────────────┐
         │Researcher │              │   Coder    │
         │(ReAct+Web)│              │(ReAct+Py)  │
         └─────┬─────┘              └──────┬─────┘
               │                           │
               │  ExecutionRes = result    │
               │                           │
               └───────────┬───────────────┘
                           │
                           ↓
                  ┌────────────────┐
                  │  返回 Research  │
                  │     Team       │
                  └────────────────┘
                           │
                   (循环，直到所有步骤完成)
                           │
                           ↓
                  ┌────────────────┐
                  │    Reporter    │
                  └────────────────┘

(*) ExecutionRes == nil
```

ResearchTeam 是整个系统的**任务调度枢纽**，确保计划中的每个步骤都被正确执行并汇总！

---

## 十一、代码改进建议

### 11.1 修复潜在的逻辑问题

**原代码**：
```go
for i, step := range state.CurrentPlan.Steps {
    if step.ExecutionRes == nil {
        continue  // 👈 可能有误
    }
    // ...
}
```

**建议修改**：
```go
for i, step := range state.CurrentPlan.Steps {
    if step.ExecutionRes != nil {
        continue  // 跳过已完成的
    }
    
    // 找到未完成的步骤，立即处理
    ilog.EventInfo(ctx, "dispatch_step", "index", i, "type", step.StepType)
    
    switch step.StepType {
    case model.Research:
        state.Goto = consts.Researcher
        return nil
    case model.Processing:
        state.Goto = consts.Coder
        return nil
    default:
        ilog.EventWarn(ctx, "unknown_step_type", "type", step.StepType)
        state.Goto = consts.Researcher  // 默认
        return nil
    }
}
```

### 11.2 添加完成判断

```go
// 所有步骤完成后的逻辑
allCompleted := true
for _, step := range state.CurrentPlan.Steps {
    if step.ExecutionRes == nil {
        allCompleted = false
        break
    }
}

if allCompleted {
    ilog.EventInfo(ctx, "all_steps_completed")
    state.Goto = consts.Reporter
    return nil
}
```

这样逻辑会更清晰！

