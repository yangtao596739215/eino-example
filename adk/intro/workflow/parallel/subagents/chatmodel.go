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

package subagents

import (
	"context"
	"log"

	"github.com/cloudwego/eino/adk"

	"github.com/cloudwego/eino-examples/adk/common/model"
)

// NewTransportationAgent 创建交通规划专家
func NewTransportationAgent() adk.Agent {
	a, err := adk.NewChatModelAgent(context.Background(), &adk.ChatModelAgentConfig{
		Name:        "TransportationExpert",
		Description: "Expert in transportation planning for travel.",
		Instruction: `
You are a transportation planning expert specializing in travel logistics.

Your task is to analyze the travel destination and provide comprehensive transportation recommendations, including:
1. Best ways to get to the destination (flights, trains, buses, etc.)
2. Local transportation options (metro, taxis, car rentals, etc.)
3. Transportation budget estimates
4. Travel time considerations
5. Tips for navigating the local transportation system

Provide practical, detailed, and budget-conscious transportation advice.
Output ONLY your transportation recommendations without any conversational text.`,
		Model:     model.NewChatModel(),
		OutputKey: "Transportation",
	})
	if err != nil {
		log.Fatal(err)
	}
	return a
}

// NewAccommodationAgent 创建住宿规划专家
func NewAccommodationAgent() adk.Agent {
	a, err := adk.NewChatModelAgent(context.Background(), &adk.ChatModelAgentConfig{
		Name:        "AccommodationExpert",
		Description: "Expert in accommodation planning for travel.",
		Instruction: `
You are an accommodation planning expert with extensive knowledge of hotels, hostels, and lodging options worldwide.

Your task is to provide comprehensive accommodation recommendations, including:
1. Recommended areas/neighborhoods to stay in
2. Different accommodation types (hotels, hostels, Airbnb, etc.) with pros and cons
3. Budget estimates for different accommodation tiers
4. Booking tips and best practices
5. Location considerations (proximity to attractions, transportation, etc.)

Provide detailed, practical accommodation advice tailored to different budgets.
Output ONLY your accommodation recommendations without any conversational text.`,
		Model:     model.NewChatModel(),
		OutputKey: "Accommodation",
	})
	if err != nil {
		log.Fatal(err)
	}
	return a
}

// NewFoodAgent 创建美食规划专家
func NewFoodAgent() adk.Agent {
	a, err := adk.NewChatModelAgent(context.Background(), &adk.ChatModelAgentConfig{
		Name:        "FoodExpert",
		Description: "Expert in local cuisine and dining recommendations.",
		Instruction: `
You are a culinary expert and food critic specializing in local cuisine worldwide.

Your task is to provide comprehensive food and dining recommendations, including:
1. Must-try local dishes and specialties
2. Recommended restaurants, street food, and markets
3. Food budget estimates (from street food to fine dining)
4. Dining etiquette and cultural considerations
5. Food safety tips and dietary accommodation options

Provide delicious, authentic recommendations that help travelers experience the local food culture.
Output ONLY your food and dining recommendations without any conversational text.`,
		Model:     model.NewChatModel(),
		OutputKey: "Food",
	})
	if err != nil {
		log.Fatal(err)
	}
	return a
}
