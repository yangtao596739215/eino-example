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

	"github.com/cloudwego/eino/adk"

	"github.com/cloudwego/eino-examples/adk/common/prints"
	"github.com/cloudwego/eino-examples/adk/intro/workflow/parallel/subagents"
)

func main() {
	ctx := context.Background()

	// 创建并行工作流 Agent，包含三个专家：交通、住宿、美食
	a, err := adk.NewParallelAgent(ctx, &adk.ParallelAgentConfig{
		Name:        "TravelPlanningAgent",
		Description: "A parallel workflow for comprehensive travel planning with multiple expert agents.",
		SubAgents: []adk.Agent{
			subagents.NewTransportationAgent(),
			subagents.NewAccommodationAgent(),
			subagents.NewFoodAgent(),
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	runner := adk.NewRunner(ctx, adk.RunnerConfig{
		EnableStreaming: true, // 启用流式输出
		Agent:           a,
	})

	// 查询示例：规划日本旅行
	fmt.Println("=== 旅游规划 Agent 演示 ===")
	fmt.Println("问题: 帮我规划一次为期7天的日本东京之旅")
	fmt.Println()

	iter := runner.Query(ctx, "帮我规划一次为期7天的日本东京之旅，预算中等")
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

	fmt.Println("\n=== 演示完成 ===")
}
