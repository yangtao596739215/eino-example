# HumanFeedback（人工反馈节点）逻辑分析

## 一、概述

`human_feedback.go` 实现了 **HumanFeedback（人工反馈节点）** 子图，它是整个 deer-go 系统的**人机交互枢纽**，负责在计划不够明确时，中断流程并等待用户反馈，实现**人在回路（Human-in-the-Loop）**的协作模式。

### 在系统中的位置

```
Planner (has_enough_context=false) → Human → Planner/ResearchTeam
                                      ↑ ↓
                                  (等待用户反馈)
```

### 核心职责

1. **流程中断**：暂停自动执行，等待用户输入
2. **反馈处理**：解析用户的反馈决策（接受/编辑计划）
3. **路由决策**：根据反馈决定下一步（执行/重新规划）
4. **自动模式支持**：可配置为自动接受计划，跳过人工确认

---

## 二、核心组件分析

### 2.1 `routerHuman` 函数（28-50行）

**作用**：处理用户反馈，决定下一步流程

#### 实现逻辑

```go
func routerHuman(ctx context.Context, input string, opts ...any) (output string, err error) {
    err = compose.ProcessState[*model.State](ctx, func(_ context.Context, state *model.State) error {
        defer func() {
            output = state.Goto
            state.InterruptFeedback = ""  // 👈 清空反馈，避免影响下次
        }()
        
        state.Goto = consts.ResearchTeam  // 默认值：进入执行阶段
        
        // 检查是否启用自动模式
        if !state.AutoAcceptedPlan {
            // 手动模式：需要用户反馈
            switch state.InterruptFeedback {
            case consts.AcceptPlan:
                // 用户接受计划 → 执行
                return nil  // state.Goto = ResearchTeam
                
            case consts.EditPlan:
                // 用户要求修改计划 → 重新规划
                state.Goto = consts.Planner
                return nil
                
            default:
                // 没有反馈或反馈无效 → 中断并等待
                return compose.InterruptAndRerun  // 👈 关键：触发中断
            }
        }
        
        // 自动模式：直接进入执行
        state.Goto = consts.ResearchTeam
        return nil
    })
    return output, err
}
```

#### 关键特性

1. **两种工作模式**

   **模式A：自动模式**
   ```go
   if state.AutoAcceptedPlan = true:
       state.Goto = consts.ResearchTeam  // 👈 直接执行，不等待
   ```

   **模式B：手动模式**
   ```go
   if state.AutoAcceptedPlan = false:
       根据 state.InterruptFeedback 决定:
         - AcceptPlan → ResearchTeam
         - EditPlan → Planner
         - 其他 → InterruptAndRerun (中断)
   ```

2. **中断机制**

   ```go
   return compose.InterruptAndRerun
   ```

   **作用**：
   - 暂停当前图的执行
   - 保存当前状态到 CheckPoint
   - 等待外部输入（用户反馈）
   - 可以从中断点恢复执行

3. **反馈选项**

   ```go
   const (
       EditPlan   = "edit_plan"   // 用户要求修改计划
       AcceptPlan = "accepted"     // 用户接受计划
   )
   ```

   **流程图**：
   ```
   Human 节点执行
         │
         ↓
   检查 AutoAcceptedPlan
         │
    ┌────┴────┐
    │         │
   Yes       No (手动模式)
    │         │
    │    检查 InterruptFeedback
    │         │
    │    ┌────┴────┬────────┐
    │    │         │        │
    │ AcceptPlan EditPlan  其他
    │    │         │        │
    ↓    ↓         ↓        ↓
   Research    Planner   中断等待
   Team                  (InterruptAndRerun)
   ```

4. **状态清理**

   ```go
   defer func() {
       output = state.Goto
       state.InterruptFeedback = ""  // 👈 清空反馈
   }()
   ```

   **原因**：
   - 避免反馈被重复使用
   - 下次进入 Human 节点时，需要新的反馈
   - 确保每次决策都是基于最新的用户输入

---

### 2.2 `NewHumanNode` 函数（52-60行）

**作用**：构建 HumanFeedback 子图

#### 子图结构

```
START → router → END
```

#### 实现代码

```go
func NewHumanNode[I, O any](ctx context.Context) *compose.Graph[I, O] {
    cag := compose.NewGraph[I, O]()
    
    // 只有一个节点
    _ = cag.AddLambdaNode("router", compose.InvokableLambdaWithOption(routerHuman))
    
    // 最简单的流程
    _ = cag.AddEdge(compose.START, "router")
    _ = cag.AddEdge("router", compose.END)
    
    return cag
}
```

#### 特点

**最简子图**：
- 没有 `load` 节点（不需要加载 Prompt）
- 没有 `agent` 节点（不需要调用 LLM）
- 只有 `router` 节点（纯逻辑处理）

**为什么这么简单？**
- Human 节点不生成内容，只处理用户输入
- 不需要复杂的 Prompt 构建
- 逻辑清晰：检查反馈 → 决定路由

---

## 三、完整执行流程

### 场景1：手动模式 - 用户接受计划

```
状态:
  AutoAcceptedPlan = false
  InterruptFeedback = ""  (初始为空)
  CurrentPlan = {Title: "AI Trends", Steps: [...]}
```

#### 执行步骤

```
═══════════════════════════════════════════════════════════
第 1 次进入 Human 节点
═══════════════════════════════════════════════════════════

1️⃣ router 节点
   ├─ 检查: state.AutoAcceptedPlan = false  (手动模式)
   ├─ 检查: state.InterruptFeedback = ""  (无反馈)
   ├─ 匹配: default 分支
   └─ 返回: compose.InterruptAndRerun  // 👈 中断！

2️⃣ 主图引擎
   ├─ 捕获中断信号
   ├─ 保存当前状态到 CheckPoint:
   │  - CurrentPlan: {...}
   │  - PlanIterations: 1
   │  - Locale: "en-US"
   │  - 当前节点: Human
   ├─ 暂停执行
   └─ 等待外部输入...

3️⃣ 用户看到界面
   ┌────────────────────────────────────────┐
   │ Plan Generated:                        │
   │ Title: AI Trends Research 2025         │
   │ Steps:                                 │
   │   1. Research Multimodal AI            │
   │   2. Research AGI Progress             │
   │   3. Generate Comparison Charts        │
   │                                        │
   │ Do you want to:                        │
   │ [Accept] [Edit Plan]                   │
   └────────────────────────────────────────┘

4️⃣ 用户点击 [Accept]
   └─ 设置: state.InterruptFeedback = "accepted"
      └─ 调用: Runnable.Generate(ctx, checkpointID)

═══════════════════════════════════════════════════════════
第 2 次进入 Human 节点 (恢复执行)
═══════════════════════════════════════════════════════════

1️⃣ router 节点
   ├─ 从 CheckPoint 恢复状态
   ├─ 检查: state.AutoAcceptedPlan = false
   ├─ 检查: state.InterruptFeedback = "accepted"  // 👈 有反馈了！
   ├─ 匹配: case consts.AcceptPlan
   ├─ 决策: state.Goto = consts.ResearchTeam
   └─ 清空: state.InterruptFeedback = ""

2️⃣ 返回主图
   └─ agentHandOff → ResearchTeam
      └─ 开始执行计划...
```

### 场景2：手动模式 - 用户编辑计划

```
用户点击 [Edit Plan]
  └─ state.InterruptFeedback = "edit_plan"
     └─ 恢复执行

1️⃣ router 节点
   ├─ 匹配: case consts.EditPlan
   ├─ 决策: state.Goto = consts.Planner  // 👈 返回 Planner
   └─ 清空: state.InterruptFeedback = ""

2️⃣ 返回主图 → Planner
   ├─ Planner 重新执行
   ├─ 可能结合用户的修改意见（如果有额外输入）
   └─ 生成新的 Plan
      └─ 可能再次进入 Human 节点...
```

### 场景3：自动模式 - 跳过人工确认

```
状态:
  AutoAcceptedPlan = true  // 👈 启用自动模式
```

#### 执行步骤

```
1️⃣ router 节点
   ├─ 检查: state.AutoAcceptedPlan = true
   ├─ 跳过反馈检查
   ├─ 决策: state.Goto = consts.ResearchTeam  // 👈 直接执行
   └─ 清空: state.InterruptFeedback = ""

2️⃣ 返回主图 → ResearchTeam
   └─ 无需用户介入，自动执行
```

**适用场景**：
- Demo 演示（无需等待用户）
- 自动化测试
- 批量处理任务
- 信任度高的场景（Planner 很少出错）

---

## 四、中断与恢复机制

### 4.1 InterruptAndRerun 的工作原理

**触发中断**：
```go
return compose.InterruptAndRerun
```

**框架层的处理**：
1. 捕获特殊错误 `InterruptAndRerun`
2. 保存当前状态到 CheckPointStore
3. 暂停图的执行
4. 返回中断信息给调用方

**CheckPoint 存储的内容**：
```go
type CheckPoint struct {
    GraphID       string           // "EinoDeer"
    ThreadID      string           // 会话ID
    NodeInputs    map[string]any   // 各节点的输入
    State         *model.State     // 共享状态
    CurrentNode   string           // "human_feedback"
    Timestamp     time.Time
}
```

### 4.2 恢复执行的流程

**恢复调用**：
```go
// 用户设置反馈
state.InterruptFeedback = "accepted"

// 从 CheckPoint 恢复
runnable.Generate(ctx, 
    compose.WithCheckPointID(checkpointID),  // 👈 指定恢复点
)
```

**框架层的处理**：
1. 加载 CheckPoint
2. 恢复 State
3. 从中断的节点（Human）重新开始
4. Human.router 检测到反馈，继续执行

---

## 五、设计模式分析

### 5.1 守卫模式（Guard Pattern）

**Human 作为守卫节点**：

```
Planner → [Human Guard] → ResearchTeam
            ↑
          (检查：计划是否可接受)
            │
       ┌────┴────┐
      Yes       No
       │         │
    [通过]   [返回Planner]
```

**守卫条件**：
```go
if has_enough_context {
    bypass Human  // 直接通过
} else {
    enter Human → wait for approval  // 需要审核
}
```

### 5.2 状态机模式（State Machine）

**Human 节点的状态转换**：

```
初始状态 (NoFeedback)
    │
    ↓ (InterruptAndRerun)
等待状态 (Waiting)
    │
    ├─ state.InterruptFeedback = "accepted"
    │  └─> 执行状态 (Approved) → ResearchTeam
    │
    ├─ state.InterruptFeedback = "edit_plan"
    │  └─> 修改状态 (Edit) → Planner
    │
    └─ state.AutoAcceptedPlan = true
       └─> 自动通过 (AutoApproved) → ResearchTeam
```

### 5.3 策略模式（Strategy Pattern）

**两种反馈处理策略**：

```go
// 策略A: 自动策略
if state.AutoAcceptedPlan {
    return AutoApproveStrategy()  // 无需等待
}

// 策略B: 手动策略
else {
    return ManualApproveStrategy()  // 等待用户
}
```

---

## 六、与其他 Agent 的协作

### 6.1 与 Planner 的交互

**流程图**：

```
Planner (第1次)
  ├─ 生成 Plan
  └─ has_enough_context = false
     └─ state.Goto = "human_feedback"

Human
  ├─ 用户查看计划
  ├─ 反馈: "edit_plan"
  └─ state.Goto = "planner"

Planner (第2次)
  ├─ 重新生成 Plan (可能结合用户建议)
  └─ has_enough_context = true
     └─ state.Goto = "reporter"  // 跳过 Human
```

**迭代终止条件**：
- `has_enough_context = true`
- `PlanIterations >= MaxPlanIterations`
- 用户接受计划（`AcceptPlan`）

### 6.2 与 ResearchTeam 的交互

**流程图**：

```
Human
  ├─ 用户接受计划
  └─ state.Goto = "research_team"

ResearchTeam
  ├─ 开始执行步骤
  └─ 分发到 Researcher/Coder
```

**数据传递**：
- Human 不修改 `CurrentPlan`
- 只是**批准**现有计划的执行
- ResearchTeam 接收的是 Planner 生成的原始计划

---

## 七、实际应用场景

### 7.1 需要人工确认的情况

**场景1：任务模糊**
```
用户: "帮我研究一下那个项目"
Planner: has_enough_context = false
Human: 等待用户澄清 → 用户: "我指的是 OpenAI 的 GPT-5 项目"
```

**场景2：敏感操作**
```
用户: "帮我分析竞争对手的技术栈"
Planner: 生成包含网络抓取步骤的计划
Human: 等待用户确认合规性 → 用户: 接受
```

**场景3：资源消耗大**
```
用户: "分析过去10年的所有AI论文"
Planner: 生成20个步骤的计划
Human: 提示用户 "这将花费较长时间和成本，是否继续？"
```

### 7.2 自动模式的应用

**场景1：批量处理**
```go
// 处理100个相似任务
for _, task := range tasks {
    state := &model.State{
        AutoAcceptedPlan: true,  // 自动模式
        Messages: task.Messages,
    }
    runnable.Generate(ctx, compose.WithGenLocalState(func() *model.State {
        return state
    }))
}
```

**场景2：Demo 演示**
```go
// 演示模式：无需手动确认
state.AutoAcceptedPlan = true
```

---

## 八、错误处理与优化

### 8.1 超时机制

**当前缺失**：
- 用户可能永远不提供反馈
- 系统会一直等待

**建议添加**：
```go
func routerHuman(ctx context.Context, input string, opts ...any) (output string, err error) {
    err = compose.ProcessState[*model.State](ctx, func(_ context.Context, state *model.State) error {
        defer func() {
            output = state.Goto
            state.InterruptFeedback = ""
        }()
        
        state.Goto = consts.ResearchTeam
        
        if !state.AutoAcceptedPlan {
            // 检查超时
            if state.InterruptStartTime.IsZero() {
                state.InterruptStartTime = time.Now()
            } else if time.Since(state.InterruptStartTime) > 5*time.Minute {
                // 超时，自动接受或取消
                ilog.EventWarn(ctx, "human_feedback_timeout")
                state.Goto = compose.END  // 或者 ResearchTeam
                return nil
            }
            
            switch state.InterruptFeedback {
            // ... 现有逻辑
            }
        }
        
        return nil
    })
}
```

### 8.2 反馈验证

**当前问题**：
```go
case consts.AcceptPlan:
    return nil
```
- 没有验证反馈内容的合法性
- 如果 `InterruptFeedback` 被意外修改？

**建议添加**：
```go
// 定义允许的反馈值
var validFeedbacks = map[string]bool{
    consts.AcceptPlan: true,
    consts.EditPlan:   true,
}

func routerHuman(ctx context.Context, input string, opts ...any) (output string, err error) {
    // ...
    if !state.AutoAcceptedPlan {
        feedback := state.InterruptFeedback
        
        // 验证反馈
        if feedback != "" && !validFeedbacks[feedback] {
            ilog.EventWarn(ctx, "invalid_feedback", "value", feedback)
            state.InterruptFeedback = ""  // 清空无效反馈
            return compose.InterruptAndRerun  // 重新等待
        }
        
        switch feedback {
        // ...
        }
    }
    // ...
}
```

### 8.3 用户体验优化

**提供更多反馈选项**：

```go
const (
    AcceptPlan     = "accepted"
    EditPlan       = "edit_plan"
    CancelTask     = "cancel"      // 新增：取消任务
    AdjustSteps    = "adjust_steps" // 新增：调整步骤数量
    ChangeLanguage = "change_locale" // 新增：更改语言
)

switch state.InterruptFeedback {
case consts.AcceptPlan:
    return nil
case consts.EditPlan:
    state.Goto = consts.Planner
    return nil
case consts.CancelTask:
    state.Goto = compose.END
    return nil
case consts.AdjustSteps:
    // 允许用户修改 MaxStepNum
    state.Goto = consts.Planner
    return nil
// ...
}
```

---

## 九、监控指标

### 9.1 关键指标

| 指标 | 含义 | 目标值 |
|------|------|--------|
| **中断率** | 进入 Human 节点并中断的比例 | < 30% |
| **平均等待时间** | 从中断到用户反馈的平均时长 | < 2 分钟 |
| **自动通过率** | `AutoAcceptedPlan = true` 的比例 | 根据场景而定 |
| **编辑率** | 用户选择 `EditPlan` 的比例 | < 20% |
| **超时率** | 用户未在限定时间内反馈的比例 | < 5% |

### 9.2 质量评估

**用户满意度**：
- 计划接受率高 → Planner 质量好
- 编辑率高 → Planner 需要优化
- 取消率高 → 任务理解有问题

**建议监控**：
```go
func recordHumanFeedback(ctx context.Context, feedback string, plan *model.Plan) {
    metrics.RecordCounter("human_feedback", map[string]string{
        "action":     feedback,
        "plan_title": plan.Title,
        "step_count": strconv.Itoa(len(plan.Steps)),
    })
    
    if feedback == consts.EditPlan {
        // 记录需要编辑的原因（如果用户提供）
        ilog.EventInfo(ctx, "plan_needs_edit", "plan", plan)
    }
}
```

---

## 十、总结

### 核心价值

HumanFeedback 实现了一个**人在回路（Human-in-the-Loop）** 机制：

1. **质量保障**：人工审核确保计划的合理性
2. **灵活控制**：用户可以在关键点介入决策
3. **自适应**：支持自动和手动两种模式
4. **可恢复**：基于 CheckPoint 的中断与恢复

### 设计亮点

- ✅ **中断机制**：`InterruptAndRerun` 实现暂停和恢复
- ✅ **双模式**：自动/手动灵活切换
- ✅ **简洁实现**：只有一个 router 节点
- ✅ **状态清理**：自动清空反馈，避免重复使用

### 架构图

```
                ┌──────────────────────────────────┐
                │       HumanFeedback              │
                │      (人工反馈节点)                │
                └─────────────┬────────────────────┘
                              │
                   ┌──────────▼──────────┐
                   │  router (唯一节点)   │
                   └──────────┬──────────┘
                              │
              ┌───────────────┴───────────────┐
              │                               │
         检查 AutoAcceptedPlan           检查 InterruptFeedback
              │                               │
        ┌─────┴─────┐                 ┌───────┴────────┐
       Yes         No                 │                │
        │           │            AcceptPlan        EditPlan
        ↓           ↓                 │                │
   [自动通过]   [手动模式]              ↓                ↓
        │           │            [执行计划]        [重新规划]
        │     ┌─────┴─────┐          │                │
        │    有反馈      无反馈         ↓                ↓
        │     │           │      ResearchTeam       Planner
        │     ↓           ↓
        │  [处理]    [中断等待]
        │     │           │
        │     │      InterruptAndRerun
        │     │           │
        └─────┴───────────┴────────────────────────┐
                          │                        │
                          ↓                        ↓
                    ┌──────────┐            ┌──────────┐
                    │保存状态到 │            │ 等待用户  │
                    │CheckPoint│            │   输入   │
                    └──────────┘            └──────────┘
                          │                        │
                          └───────────┬────────────┘
                                      │
                                恢复执行 ↓
```

HumanFeedback 是整个系统的**质量把关节点**，确保关键决策由人类最终审核，实现人机协作的最佳平衡！

---

## 十一、高级应用

### 11.1 多轮交互

**场景**：用户需要多次修改计划

```
第1轮:
  Planner → Human → 用户: "添加关于AI安全的研究"
         → Planner → Human → 用户: "还要加上伦理分析"
                  → Planner → Human → 用户: 接受

实现:
  通过迭代实现，每次 EditPlan 返回 Planner
  Planner 可以保留之前的反馈记录
```

### 11.2 条件式自动模式

**根据任务复杂度决定是否需要人工确认**：

```go
func routerHuman(ctx context.Context, input string, opts ...any) (output string, err error) {
    err = compose.ProcessState[*model.State](ctx, func(_ context.Context, state *model.State) error {
        // 动态决定是否需要人工确认
        if shouldAutoAccept(state.CurrentPlan) {
            state.Goto = consts.ResearchTeam
            return nil
        }
        
        // 需要人工确认...
        // ...
    })
}

func shouldAutoAccept(plan *model.Plan) bool {
    // 简单任务自动接受
    if len(plan.Steps) <= 2 {
        return true
    }
    
    // 无敏感操作的任务自动接受
    for _, step := range plan.Steps {
        if containsSensitiveKeyword(step.Description) {
            return false
        }
    }
    
    return true
}
```

这样可以实现**智能化的人工介入**，在真正需要时才请求用户确认！

