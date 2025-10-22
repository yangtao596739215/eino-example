# Workflow 设计原理和特性

## 概述

Eino Compose 层的 **Workflow** 是基于 Graph 的高级封装，专门为复杂的数据流编排而设计。它通过声明式的依赖关系和精确的字段映射，提供了比传统 Graph 更灵活、更强大的数据流控制能力。

## 核心设计理念

### 1. Graph 包装器设计

```go
// Workflow 是 Graph 的包装器
type Workflow[I, O any] struct {
    g                *graph                    // 底层 Graph 实例
    workflowNodes    map[string]*WorkflowNode  // Workflow 节点管理
    workflowBranches []*WorkflowBranch         // 分支管理
    dependencies     map[string]map[string]dependencyType  // 依赖关系管理
}
```

**设计要点**：
- Workflow 是 Graph 的包装器，底层使用 `NodeTriggerMode(AllPredecessor)` 模式
- 不支持循环（cycles），确保执行的有向无环图特性
- 通过声明式 API 替代 Graph 的边连接方式

### 2. 声明式依赖管理

#### 传统 Graph 方式（命令式）
```go
// Graph 使用 AddEdge 进行一对一连接
graph.AddEdge(compose.START, "ChatTemplate")
graph.AddEdge("ChatTemplate", "ChatModel")
graph.AddEdge("ToolsNode", "ChatModel")
```

#### Workflow 方式（声明式）
```go
// Workflow 使用 AddInput 支持多前驱节点
wf.End().
    AddInput("c1", compose.ToField("content_count")).      // 前驱节点 1
    AddInput("c2", compose.ToField("reasoning_content_count")) // 前驱节点 2
```

## 核心特性

### 1. 精确字段映射

Workflow 支持精确的字段映射，允许从复杂的数据结构中提取特定字段：

```go
// 字段映射示例
wf.AddLambdaNode("c1", compose.InvokableLambda(wordCounter)).
    AddInput(compose.START, 
        compose.MapFields("SubStr", "SubStr"),                    // 直接字段映射
        compose.MapFieldPaths([]string{"Message", "Content"}, []string{"FullStr"})  // 嵌套字段映射
    )
```

**支持的映射类型**：
- `MapFields()`: 直接字段映射
- `MapFieldPaths()`: 嵌套字段路径映射
- `ToField()`: 输出到指定字段
- `FromField()`: 从指定字段读取

### 2. 多前驱节点支持

Workflow 允许一个节点从多个前驱节点获取数据：

```go
// END 节点从多个前驱节点获取数据
wf.End().
    AddInput("c1", compose.ToField("content_count")).      // 从 c1 获取数据
    AddInput("c2", compose.ToField("reasoning_content_count"))  // 从 c2 获取数据
```

**优势**：
- 支持数据聚合场景
- 避免中间节点的数据传递复杂性
- 提供更灵活的数据流控制

### 3. 依赖关系分离

Workflow 将控制依赖和数据依赖分离，提供更精细的控制：

#### 控制依赖（AddDependency）
```go
// 纯控制依赖，无数据传递
wf.AddLambdaNode("b2", compose.InvokableLambda(bidder)).
    AddDependency("b1")  // b2 在 b1 后执行，但不接收 b1 的数据
```

#### 数据依赖（WithNoDirectDependency）
```go
// 纯数据依赖，无控制依赖
wf.AddLambdaNode("mul", compose.InvokableLambda(multiplier)).
    AddInputWithOptions(compose.START, 
        []*compose.FieldMapping{compose.MapFields("Multiply", "B")},
        compose.WithNoDirectDependency()  // 只有数据依赖
    )
```

#### 控制+数据依赖（AddInput）
```go
// 同时建立控制依赖和数据依赖
wf.AddLambdaNode("adder", compose.InvokableLambda(adder)).
    AddInput(compose.START, compose.FromField("Add"))
```

### 4. 静态值设置

Workflow 支持为节点设置静态值，在编译时确定：

```go
wf.AddLambdaNode("c1", compose.InvokableLambda(wordCounter)).
    AddInput(compose.START, compose.MapFields("Content", "FullStr")).
    SetStaticValue([]string{"SubStr"}, "o")  // 设置静态值
```

**应用场景**：
- 配置参数设置
- 常量值传递
- 减少运行时计算

### 5. 分支支持

Workflow 支持条件分支，类似 Graph 的分支功能：

```go
// 添加分支
wf.AddBranch("b1", compose.NewGraphBranch(func(ctx context.Context, in float64) (string, error) {
    if in > 5.0 {
        return compose.END, nil
    }
    return "b2", nil
}, map[string]bool{compose.END: true, "b2": true}))
```

## 依赖类型详解

### 1. 普通依赖（normalDependency）

```go
// 同时建立控制依赖和数据依赖
node.AddInput("predecessor", mappings...)
```

**特点**：
- 前驱节点必须完成执行
- 前驱节点的输出传递给当前节点
- 最常用的依赖类型

### 2. 无直接依赖（noDirectDependency）

```go
// 只有数据依赖，无控制依赖
node.AddInputWithOptions("predecessor", mappings, compose.WithNoDirectDependency())
```

**特点**：
- 只有数据传递，无执行顺序控制
- 适用于跨分支数据访问
- 需要确保存在其他路径到达当前节点

### 3. 纯控制依赖（branchDependency）

```go
// 只有控制依赖，无数据传递
node.AddDependency("predecessor")
```

**特点**：
- 只有执行顺序控制，无数据传递
- 适用于初始化、清理等场景
- 确保前驱节点完成后再执行

## 与 Graph 的对比

| 特性 | Graph | Workflow |
|------|-------|------------|
| **连接方式** | 一对一（AddEdge） | 多对一（AddInput） |
| **数据传递** | 完整对象传递 | 精确字段映射 |
| **依赖控制** | 控制+数据耦合 | 控制+数据分离 |
| **多前驱** | 不支持 | 支持 |
| **静态值** | 不支持 | 支持 |
| **字段映射** | 不支持 | 支持 |
| **循环支持** | 支持 | 不支持 |
| **中断支持** | 支持 | 不支持 |

## 适用场景

### 1. 数据聚合场景

```go
// 多个数据源聚合到最终结果
wf.End().
    AddInput("source1", compose.ToField("result1")).
    AddInput("source2", compose.ToField("result2")).
    AddInput("source3", compose.ToField("result3"))
```

### 2. 复杂数据流控制

```go
// 精确控制数据流
wf.AddLambdaNode("processor", processor).
    AddInput("input1", compose.MapFields("field1", "input1")).
    AddInput("input2", compose.MapFields("field2", "input2")).
    SetStaticValue([]string{"config"}, "static_config")
```

### 3. 跨分支数据访问

```go
// 分支间数据访问
wf.AddLambdaNode("branchNode", branchProcessor).
    AddInputWithOptions("crossBranchData", mappings, compose.WithNoDirectDependency())
```

## 最佳实践

### 1. 合理设计字段映射

```go
// ✅ 好的设计：语义清晰的字段映射
wf.AddLambdaNode("processor", processor).
    AddInput(compose.START, 
        compose.MapFields("UserID", "UserID"),
        compose.MapFieldPaths([]string{"Profile", "Name"}, []string{"UserName"})
    )

// ❌ 不好的设计：模糊的字段映射
wf.AddLambdaNode("processor", processor).
    AddInput(compose.START)  // 传递整个对象，失去精确控制
```

### 2. 合理使用依赖类型

```go
// ✅ 好的设计：明确依赖类型
wf.AddLambdaNode("processor", processor).
    AddInput("dataSource", mappings).                    // 需要数据
    AddDependency("initializer").                      // 需要初始化
    AddInputWithOptions("config", configMappings,       // 只需要配置
        compose.WithNoDirectDependency())
```

### 3. 利用静态值减少复杂度

```go
// ✅ 好的设计：使用静态值
wf.AddLambdaNode("processor", processor).
    SetStaticValue([]string{"version"}, "v1.0").
    SetStaticValue([]string{"timeout"}, 30)
```

## 限制和注意事项

### 1. 不支持循环

```go
// ❌ 不支持：循环依赖
wf.AddLambdaNode("A", lambdaA).AddInput("B")
wf.AddLambdaNode("B", lambdaB).AddInput("A")  // 错误：循环依赖
```

### 2. 不支持中断

```go
// ❌ 不支持：中断功能
// Workflow 不支持类似 Graph 的 WithInterruptBeforeNodes
```

### 3. 依赖路径要求

使用 `WithNoDirectDependency()` 时，必须确保存在其他路径到达当前节点：

```go
// ✅ 正确：存在其他路径
wf.AddLambdaNode("A", lambdaA).AddInput(compose.START)
wf.AddLambdaNode("B", lambdaB).AddInput("A")
wf.AddLambdaNode("C", lambdaC).
    AddInput("A").  // 直接路径
    AddInputWithOptions("B", mappings, compose.WithNoDirectDependency())  // 数据路径
```

## 总结

Workflow 作为 Graph 的高级封装，通过以下设计实现了更强大的数据流编排能力：

1. **声明式 API**：通过 `AddInput` 替代 `AddEdge`，支持多前驱节点
2. **精确字段映射**：支持复杂数据结构的精确字段控制
3. **依赖关系分离**：将控制依赖和数据依赖分离，提供更精细的控制
4. **静态值支持**：在编译时设置常量值，减少运行时复杂度
5. **多前驱支持**：支持数据聚合和复杂的数据流控制

Workflow 特别适用于需要精确数据流控制、多数据源聚合、复杂字段映射的场景，是构建高级数据编排系统的理想选择。
