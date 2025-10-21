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

	"github.com/cloudwego/eino/compose"

	"github.com/cloudwego/eino-examples/internal/logs"
)

func main() {
	ctx := context.Background()

	// 创建旅游规划的多专家并行处理
	chain := createTravelPlanningChain(ctx)

	// 编译链
	runnable, err := chain.Compile(ctx)
	if err != nil {
		log.Fatalf("Failed to compile chain: %v", err)
	}

	// 执行旅游规划
	input := map[string]any{
		"destination": "东京",
		"duration":    7,
		"budget":      5000,
		"travelers":   2,
	}

	logs.Infof("开始旅游规划，输入: %+v", input)

	result, err := runnable.Invoke(ctx, input)
	if err != nil {
		log.Fatalf("Failed to invoke: %v", err)
	}

	logs.Infof("旅游规划完成，结果: %s", result)
}

// createTravelPlanningChain 创建旅游规划链
func createTravelPlanningChain(ctx context.Context) *compose.Chain[map[string]any, string] {
	chain := compose.NewChain[map[string]any, string]()

	// 1. 输入预处理节点
	chain.AppendLambda(compose.InvokableLambda(func(ctx context.Context, input map[string]any) (map[string]any, error) {
		logs.Infof("输入预处理: %+v", input)
		return input, nil
	}))

	// 2. 并行专家处理
	parallel := compose.NewParallel()

	// 交通专家
	parallel.AddLambda("transportation", compose.InvokableLambda(func(ctx context.Context, kvs map[string]any) (string, error) {
		destination := kvs["destination"].(string)
		budget := kvs["budget"].(int)

		logs.Infof("交通专家开始工作，目的地: %s, 预算: %d", destination, budget)

		var advice string
		// 基于目的地和预算提供交通建议
		switch destination {
		case "东京":
			if budget > 3000 {
				advice = "建议购买 JR Pass 7日券，可无限次乘坐新干线"
			} else {
				advice = "建议使用地铁一日券，经济实惠"
			}
		case "大阪":
			advice = "建议购买关西周游券，覆盖关西地区主要交通"
		default:
			advice = "建议根据具体目的地选择合适的交通方式"
		}

		logs.Infof("交通专家完成工作: %s", advice)
		return advice, nil
	}))

	// 住宿专家
	parallel.AddLambda("accommodation", compose.InvokableLambda(func(ctx context.Context, kvs map[string]any) (string, error) {
		destination := kvs["destination"].(string)
		budget := kvs["budget"].(int)
		travelers := kvs["travelers"].(int)
		duration := kvs["duration"].(int)

		logs.Infof("住宿专家开始工作，目的地: %s, 预算: %d, 人数: %d", destination, budget, travelers)

		// 基于预算和人数提供住宿建议
		perPersonBudget := budget / travelers / duration
		var advice string
		if perPersonBudget > 200 {
			advice = "推荐五星级酒店，享受豪华体验"
		} else if perPersonBudget > 100 {
			advice = "推荐商务酒店，性价比高"
		} else {
			advice = "推荐经济型酒店或民宿，节省预算"
		}

		logs.Infof("住宿专家完成工作: %s", advice)
		return advice, nil
	}))

	// 美食专家
	parallel.AddLambda("food", compose.InvokableLambda(func(ctx context.Context, kvs map[string]any) (string, error) {
		destination := kvs["destination"].(string)
		budget := kvs["budget"].(int)

		logs.Infof("美食专家开始工作，目的地: %s, 预算: %d", destination, budget)

		var advice string
		// 基于目的地提供美食建议
		switch destination {
		case "东京":
			advice = "推荐：寿司、拉面、天妇罗、和牛料理，预算充足可尝试米其林餐厅"
		case "大阪":
			advice = "推荐：章鱼烧、大阪烧、串炸、河豚料理，体验关西美食文化"
		default:
			advice = "推荐当地特色美食，体验地道风味"
		}

		logs.Infof("美食专家完成工作: %s", advice)
		return advice, nil
	}))

	// 景点专家
	parallel.AddLambda("attraction", compose.InvokableLambda(func(ctx context.Context, kvs map[string]any) (string, error) {
		destination := kvs["destination"].(string)
		duration := kvs["duration"].(int)

		logs.Infof("景点专家开始工作，目的地: %s, 天数: %d", destination, duration)

		var advice string
		// 基于目的地和天数提供景点建议
		switch destination {
		case "东京":
			if duration >= 7 {
				advice = "推荐：浅草寺、东京塔、上野公园、新宿御苑、明治神宫、涩谷、原宿、台场"
			} else {
				advice = "推荐：浅草寺、东京塔、上野公园、新宿御苑（精选必游景点）"
			}
		case "大阪":
			advice = "推荐：大阪城、通天阁、道顿堀、环球影城、天守阁"
		default:
			advice = "推荐当地著名景点，合理安排行程"
		}

		logs.Infof("景点专家完成工作: %s", advice)
		return advice, nil
	}))

	// 添加并行处理到链中
	chain.AppendParallel(parallel)

	// 3. 协调汇总节点
	chain.AppendLambda(compose.InvokableLambda(func(ctx context.Context, parallelResults map[string]any) (string, error) {
		logs.Infof("开始协调汇总，并行结果: %+v", parallelResults)

		// 从输入中获取基本信息
		destination := "未知目的地"
		duration := 0
		budget := 0
		travelers := 0

		// 从并行结果中提取建议
		transportationAdvice := parallelResults["transportation"].(string)
		accommodationAdvice := parallelResults["accommodation"].(string)
		foodAdvice := parallelResults["food"].(string)
		attractionAdvice := parallelResults["attraction"].(string)

		// 生成最终旅游规划
		finalResult := fmt.Sprintf(`
=== %s %d日游规划 ===

💰 预算: ¥%d (人均¥%d/天)
👥 人数: %d人

🚗 交通建议:
%s

🏨 住宿建议:
%s

🍜 美食推荐:
%s

🎯 景点推荐:
%s

=== 规划完成 ===
`,
			destination,
			duration,
			budget,
			budget/travelers/duration,
			travelers,
			transportationAdvice,
			accommodationAdvice,
			foodAdvice,
			attractionAdvice,
		)

		logs.Infof("协调汇总完成")
		return finalResult, nil
	}))

	return chain
}
