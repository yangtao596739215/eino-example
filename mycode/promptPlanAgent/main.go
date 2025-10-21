package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	modelopenai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
)

func main() {
	ctx := context.Background()

	// 1) 构造一个 Chat Model（OpenAI，需环境变量 OPENAI_API_KEY 和 OPENAI_MODEL）
	chatModel, err := modelopenai.NewChatModel(ctx, &modelopenai.ChatModelConfig{
		Model:   os.Getenv("OPENAI_MODEL"),
		APIKey:  os.Getenv("OPENAI_API_KEY"),
		BaseURL: os.Getenv("OPENAI_BASE_URL"),
	})
	if err != nil {
		log.Fatalf("create chat model failed: %v", err)
	}

	// 2) 构建 ReactAgent，不注册任何工具，仅通过提示词引导
	cb := callbacks.NewHandlerBuilder().
		OnStartFn(func(ctx context.Context, info *callbacks.RunInfo, input callbacks.CallbackInput) context.Context {
			fmt.Printf("\n[AgentStart] %s\n", info.Name)
			return ctx
		}).
		OnEndFn(func(ctx context.Context, info *callbacks.RunInfo, output callbacks.CallbackOutput) context.Context {
			fmt.Printf("[AgentEnd] %s\n", info.Name)
			return ctx
		}).
		Build()

	reactAgent, err := react.NewAgent(ctx, &react.AgentConfig{
		MaxStep:          10, // 减少步数，因为不需要工具调用
		ToolCallingModel: chatModel,
		MessageModifier: func(_ context.Context, msgs []*schema.Message) []*schema.Message {
			const planningPrompt = `你是一个专业的项目规划助手。请按照以下步骤来分析和规划用户提出的项目或任务：

## 第一步：思维树分析
请先进行思维树分析，从多个角度思考这个项目：
- 项目背景和现状分析
- 核心问题和挑战识别
- 可能的解决方案和路径
- 关键成功因素
- 潜在风险和障碍

## 第二步：目标树规划
基于思维树的分析，构建目标树：
- 总体目标（最终要达到的状态）
- 主要目标（支撑总体目标的关键目标）
- 具体目标（可量化的、具体的目标）
- 目标之间的依赖关系

## 第三步：任务列表制定
根据目标树，制定详细的任务列表：
- 按优先级排序的任务
- 每个任务的描述和预期结果
- 任务的时间估算
- 任务之间的依赖关系
- 资源需求

请确保每一步都输出清晰的结构化内容，使用适当的标题和格式。`

			if len(msgs) > 0 {
				msgs[0].Content = fmt.Sprintf("%s\n\n用户需求：%s", planningPrompt, msgs[0].Content)
			}
			return msgs
		},
	})
	if err != nil {
		log.Fatalf("create react agent failed: %v", err)
	}

	// 3) 终端交互：读取用户输入，调用 ReactAgent，打印结果
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("=== 基于提示词的项目规划助手 ===")
	fmt.Println("请输入您的项目愿景或复杂任务（回车发送，Ctrl+C 退出）：")
	fmt.Println("系统将按照 思维树 -> 目标树 -> 任务列表 的流程为您规划")
	fmt.Println()

	for {
		fmt.Print("> ")
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		msgs := []*schema.Message{schema.UserMessage(line)}
		out, err := reactAgent.Generate(ctx, msgs, agent.WithComposeOptions(compose.WithCallbacks(cb)))
		if err != nil {
			fmt.Println("Error:", err)
			continue
		}
		fmt.Println()
		fmt.Println("====================")
		fmt.Println("规划结果")
		fmt.Println("--------------------")
		fmt.Println(strings.TrimSpace(out.Content))
		fmt.Println()
		fmt.Println("====================")
		fmt.Println()
	}
}
