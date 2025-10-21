# Parallel Workflow - 旅游规划 Agent

## 概述

这个示例展示了如何使用 Eino ADK 的 **Parallel Workflow** 模式构建一个旅游规划 Agent。该 Agent 通过并发执行多个专家 Agent，为用户提供全面的旅行建议。

## 架构设计

```
                    ┌─────────────────────────┐
                    │  TravelPlanningAgent    │
                    │   (Parallel Workflow)   │
                    └──────────┬──────────────┘
                               │
              ┌────────────────┼────────────────┐
              │                │                │
              v                v                v
    ┌─────────────────┐ ┌─────────────────┐ ┌─────────────────┐
    │Transportation   │ │ Accommodation   │ │     Food        │
    │    Expert       │ │     Expert      │ │    Expert       │
    │                 │ │                 │ │                 │
    │ - 交通方式      │ │ - 住宿区域      │ │ - 特色美食      │
    │ - 本地交通      │ │ - 酒店类型      │ │ - 推荐餐厅      │
    │ - 预算估算      │ │ - 预算估算      │ │ - 美食预算      │
    │ - 出行时间      │ │ - 预订建议      │ │ - 饮食礼仪      │
    └─────────────────┘ └─────────────────┘ └─────────────────┘
              │                │                │
              └────────────────┼────────────────┘
                               │
                               v
                     汇总所有专家建议
```

## 核心特性

### 1. 并行执行
- 三个专家 Agent 同时工作，大幅提升响应速度
- 每个专家独立分析用户需求，提供专业建议

### 2. 专家分工
- **交通专家 (TransportationExpert)**: 负责交通规划
  - 如何到达目的地
  - 本地交通选项
  - 交通预算和时间规划
  
- **住宿专家 (AccommodationExpert)**: 负责住宿建议
  - 推荐住宿区域
  - 不同类型住宿对比
  - 预算分层建议
  
- **美食专家 (FoodExpert)**: 负责美食推荐
  - 当地特色菜肴
  - 餐厅和街头美食
  - 饮食文化和礼仪

### 3. 输出隔离
每个专家的输出通过 `OutputKey` 机制独立保存：
- `Transportation`: 交通建议
- `Accommodation`: 住宿建议
- `Food`: 美食建议

## 运行示例

```bash
cd /Users/wwxq/workProject/go/eino-examples/adk/intro/workflow/parallel
go run parallel.go
```

## 示例输出

```
=== 旅游规划 Agent 演示 ===
问题: 帮我规划一次为期7天的日本东京之旅

[TransportationExpert] 输出:
  交通建议...
  
[AccommodationExpert] 输出:
  住宿建议...
  
[FoodExpert] 输出:
  美食建议...

=== 演示完成 ===
```

## 与其他 Workflow 的对比

| Workflow 类型 | 执行方式 | 适用场景 | 示例 |
|--------------|---------|---------|------|
| **Sequential** | 顺序执行 | 需要前后依赖的任务 | 先规划后执行 |
| **Loop** | 循环执行 | 需要反复优化的任务 | 迭代式改进 |
| **Parallel** | 并发执行 | 独立任务并行处理 | 多专家咨询 |

## 扩展建议

你可以轻松添加更多专家 Agent，例如：
- **景点专家**: 推荐必游景点和路线
- **购物专家**: 推荐购物地点和特产
- **文化专家**: 介绍当地文化和习俗
- **预算专家**: 汇总所有预算并优化

只需在 `subagents/chatmodel.go` 中添加新的 Agent 函数，并在 `parallel.go` 中注册即可。

## 技术细节

- **runContext 隔离**: 每个并行 Agent 拥有独立的执行上下文
- **事件转发**: 所有 Agent 的事件通过线程安全的 `AsyncGenerator` 转发
- **中断恢复**: 支持分支级别的中断和恢复（通过 `ParallelInterruptInfo`）

详见: [Parallel设计原理.md](../Parallel设计原理.md)

