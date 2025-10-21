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
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"sync"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"

	"github.com/cloudwego/eino-examples/adk/common/prints"
	"github.com/cloudwego/eino-examples/adk/intro/chatmodel/subagents"
)

func main() {
	ctx := context.Background()
	a := subagents.NewBookRecommendAgent()
	store := newInMemoryStore()
	runner := adk.NewRunner(ctx, adk.RunnerConfig{
		EnableStreaming: true, // you can disable streaming here
		Agent:           a,
		CheckPointStore: store,
	})
	iter := runner.Query(ctx, "recommend a book to me", adk.WithCheckPointID("1"))
	var hasInterrupt bool
	for {
		event, ok := iter.Next()
		if !ok {
			//中断虽然是异步保存的，但是只要这个迭代器完成，checkpoint就保存完成了，因为提示词没强制使用中断，所以，有时候，不一定会中断
			break
		}
		if event.Err != nil {
			log.Fatal(event.Err)
		}

		// 检查是否有中断事件
		if event.Action != nil && event.Action.Interrupted != nil {
			hasInterrupt = true
			fmt.Printf("DEBUG: Interrupt event detected!\n")
		}

		// 检查是否有工具调用
		if event.Output != nil && event.Output.MessageOutput != nil && event.Output.MessageOutput.Message != nil {
			if msg := event.Output.MessageOutput.Message; msg.ToolCalls != nil && len(msg.ToolCalls) > 0 {
				fmt.Printf("DEBUG: Tool calls detected: %v\n", msg.ToolCalls)
			}
		}

		prints.Event(event)
	}

	fmt.Printf("DEBUG: Iterator completed, hasInterrupt=%v\n", hasInterrupt)

	// 只有当发生中断时，checkpoint才会被保存
	if !hasInterrupt {
		fmt.Println("No interrupt occurred, no checkpoint was saved. This is normal behavior.")
		fmt.Println("The agent completed without needing user input.")
		return
	}

	// 发生中断时，checkpoint已经保存完成（因为我们在AsyncIterator完成后才到这里）

	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("\nyour input here: ")
	scanner.Scan()
	fmt.Println()
	nInput := scanner.Text()

	fmt.Println("get checkpoint 1")
	data, ok, errGet := store.Get(ctx, "1")
	if !ok {
		fmt.Printf("DEBUG: Checkpoint not found! hasInterrupt was %v\n", hasInterrupt)
		log.Fatal("checkpoint not found")
	}
	if errGet != nil {
		log.Fatal(errGet)
	}
	fmt.Println(string(data))
	store.Set(ctx, "2", data)

	iter, err := runner.Resume(ctx, "2", adk.WithToolOptions([]tool.Option{subagents.WithNewInput(nInput)}))
	if err != nil {
		log.Fatal(err)
	}
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}

		if event.Err != nil {
			log.Fatal(event.Err)
		}

		prints.Event(event)
	}
}

func newInMemoryStore() compose.CheckPointStore {
	return &inMemoryStore{
		mem: map[string][]byte{},
	}
}

type inMemoryStore struct {
	mu  sync.RWMutex
	mem map[string][]byte
}

func (i *inMemoryStore) Set(ctx context.Context, key string, value []byte) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.mem[key] = value
	return nil
}

func (i *inMemoryStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	v, ok := i.mem[key]
	return v, ok, nil
}
