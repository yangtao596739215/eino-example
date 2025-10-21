package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	modelopenai "github.com/cloudwego/eino-ext/components/model/openai"
	mcpwrap "github.com/cloudwego/eino-ext/components/tool/mcp"
	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

func main() {
	ctx := context.Background()

	// 1) 启动并连接我们刚写的 Go MCP Server（思维树/目标树/任务列表）
	// Start the prebuilt server binary to avoid 'go run' stdio interference.
	mcpCli, err := client.NewStdioMCPClient(
		"/Users/yangtao/WorkProject/personal/eino-example/flow/agent/deer-go/biz/mcps/thinking_planning_go/thinking-planning-go",
		nil,
	)
	if err != nil {
		log.Fatalf("create MCP client failed: %v", err)
	}
	defer func() { _ = mcpCli.Close() }()

	if err := mcpCli.Start(ctx); err != nil {
		log.Fatalf("start MCP stdio client failed: %v", err)
	}

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: "react-agent-cli", Version: "0.1.0"}
	initReq.Params.Capabilities = mcp.ClientCapabilities{}
	if _, err := mcpCli.Initialize(ctx, initReq); err != nil {
		log.Fatalf("initialize MCP failed: %v", err)
	}

	// 2) 通过 eino-ext 的 mcp 工具封装，把 server 暴露的工具注册为可调用工具
	mcpTools, err := mcpwrap.GetTools(ctx, &mcpwrap.Config{Cli: mcpCli})
	if err != nil {
		log.Fatalf("load MCP tools failed: %v", err)
	}

	// 3) 构造一个 Chat Model（OpenAI，需环境变量 OPENAI_API_KEY 和 OPENAI_MODEL）
	chatModel, err := modelopenai.NewChatModel(ctx, &modelopenai.ChatModelConfig{
		Model:   os.Getenv("OPENAI_MODEL"),
		APIKey:  os.Getenv("OPENAI_API_KEY"),
		BaseURL: os.Getenv("OPENAI_BASE_URL"),
	})
	if err != nil {
		log.Fatalf("create chat model failed: %v", err)
	}

	// 4) 构建 ReactAgent，并注册 MCP 工具
	cb := callbacks.NewHandlerBuilder().
		OnStartFn(func(ctx context.Context, info *callbacks.RunInfo, input callbacks.CallbackInput) context.Context {
			if info.Component == components.ComponentOfTool {
				ci := tool.ConvCallbackInput(input)
				pretty := ci.ArgumentsInJSON
				var v any
				if err := json.Unmarshal([]byte(ci.ArgumentsInJSON), &v); err == nil {
					if b, err2 := json.MarshalIndent(v, "  ", "  "); err2 == nil {
						pretty = string(b)
					}
				}
				fmt.Printf("\n[ToolStart] %s args:\n%s\n", info.Name, pretty)
			}
			return ctx
		}).
		OnEndFn(func(ctx context.Context, info *callbacks.RunInfo, output callbacks.CallbackOutput) context.Context {
			if info.Component == components.ComponentOfTool {
				co := tool.ConvCallbackOutput(output)
				pretty := co.Response
				var v any
				if err := json.Unmarshal([]byte(co.Response), &v); err == nil {
					if b, err2 := json.MarshalIndent(v, "  ", "  "); err2 == nil {
						pretty = string(b)
					}
				}
				fmt.Printf("[ToolEnd] %s result:\n%s\n", info.Name, pretty)
			}
			return ctx
		}).
		Build()

	reactAgent, err := react.NewAgent(ctx, &react.AgentConfig{
		MaxStep:          1000,
		ToolCallingModel: chatModel,
		ToolsConfig:      compose.ToolsNodeConfig{Tools: toBaseTools(mcpTools)},
		MessageModifier: func(_ context.Context, msgs []*schema.Message) []*schema.Message {
			const hint = "请先用思维树(thought_tree)探索方案，再用目标树(goal_tree)规划分解，最后用任务列表(task_list)管理执行，并在每一步输出清晰的路径。"
			if len(msgs) > 0 {
				msgs[0].Content = fmt.Sprintf("%s\n\n%s", hint, msgs[0].Content)
			}
			return msgs
		},
		// StreamToolCallChecker: toolCallLogger,
	})
	if err != nil {
		log.Fatalf("create react agent failed: %v", err)
	}

	// 5) 终端交互：读取用户输入，调用 ReactAgent，打印结果
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("请输入项目愿景或复杂任务（回车发送，Ctrl+C 退出）：")
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
		fmt.Println("Agent 输出")
		fmt.Println("--------------------")
		fmt.Println(strings.TrimSpace(out.Content))
		fmt.Println()
	}
}

func toBaseTools(ts []tool.BaseTool) []tool.BaseTool { return ts }
