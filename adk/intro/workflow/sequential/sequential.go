/*
 * Copyright 2025 CloudWeGo Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/cloudwego/eino-examples/adk/common/prints"
	"github.com/cloudwego/eino-examples/adk/intro/workflow/sequential/subagents"
	ccb "github.com/cloudwego/eino-ext/callbacks/cozeloop"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/callbacks"
	"github.com/coze-dev/cozeloop-go"
)

func main() {
	ctx := context.Background()
	InitCozeLoopTracing()

	a, err := adk.NewSequentialAgent(ctx, &adk.SequentialAgentConfig{
		Name:        "ResearchAgent",
		Description: "A sequential workflow for planning and writing a research report.",
		SubAgents:   []adk.Agent{subagents.NewPlanAgent(), subagents.NewWriterAgent()},
	})
	if err != nil {
		log.Fatal(err)
	}

	runner := adk.NewRunner(ctx, adk.RunnerConfig{
		EnableStreaming: true, // you can disable streaming here
		Agent:           a,
	})

	iter := runner.Query(ctx, "The history of Large Language Models")
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			fmt.Printf("Error: %v\n", event.Err)
			break
		}

		prints.Event(event)
	}
}

func InitCozeLoopTracing() {
	cozeloopApiToken := os.Getenv("COZELOOP_API_TOKEN")
	cozeloopWorkspaceID := os.Getenv("COZELOOP_WORKSPACE_ID") // use cozeloop trace, from https://loop.coze.cn/open/docs/cozeloop/go-sdk#4a8c980e

	fmt.Println("cozeloopApiToken", cozeloopApiToken)
	fmt.Println("cozeloopWorkspaceID", cozeloopWorkspaceID)
	if cozeloopApiToken == "" || cozeloopWorkspaceID == "" {
		return
	}
	client, err := cozeloop.NewClient(
		cozeloop.WithAPIToken(cozeloopApiToken),
		cozeloop.WithWorkspaceID(cozeloopWorkspaceID),
	)
	if err != nil {
		panic(err)
	}
	cozeloop.SetDefaultClient(client)
	callbacks.AppendGlobalHandlers(ccb.NewLoopHandler(client))
}
