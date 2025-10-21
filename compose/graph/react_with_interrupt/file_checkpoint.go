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
	"os"
	"path/filepath"
	"sync"

	"github.com/cloudwego/eino/compose"
)

// FileCheckPointStore 基于本地文件的checkpoint存储
// 支持跨会话的状态恢复
type FileCheckPointStore struct {
	baseDir    string
	filePrefix string
	mu         sync.RWMutex
}

// NewFileCheckPointStore 创建文件checkpoint存储
func NewFileCheckPointStore(ctx context.Context) (compose.CheckPointStore, error) {
	baseDir := os.Getenv("CHECKPOINT_DIR")
	if baseDir == "" {
		baseDir = "./checkpoints"
	}

	// 确保目录存在
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create checkpoint directory: %w", err)
	}

	return &FileCheckPointStore{
		baseDir:    baseDir,
		filePrefix: "eino_checkpoint_",
	}, nil
}

// Get 从文件获取checkpoint数据
func (f *FileCheckPointStore) Get(ctx context.Context, checkPointID string) ([]byte, bool, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	filePath := f.getFilePath(checkPointID)

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			// 文件不存在
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("failed to read checkpoint file: %w", err)
	}

	fmt.Printf("Get checkpoint %s from file, content: %s\n", checkPointID, string(data))
	return data, true, nil
}

// Set 将checkpoint数据保存到文件
func (f *FileCheckPointStore) Set(ctx context.Context, checkPointID string, checkPoint []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	filePath := f.getFilePath(checkPointID)

	// 创建临时文件，然后原子性重命名
	tempPath := filePath + ".tmp"
	if err := os.WriteFile(tempPath, checkPoint, 0644); err != nil {
		return fmt.Errorf("failed to write checkpoint file: %w", err)
	}

	if err := os.Rename(tempPath, filePath); err != nil {
		os.Remove(tempPath) // 清理临时文件
		return fmt.Errorf("failed to rename checkpoint file: %w", err)
	}

	fmt.Printf("Set checkpoint %s to file, content: %s\n", checkPointID, string(checkPoint))
	return nil
}

// Delete 删除checkpoint文件（可选，用于清理）
func (f *FileCheckPointStore) Delete(ctx context.Context, checkPointID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	filePath := f.getFilePath(checkPointID)
	return os.Remove(filePath)
}

// ListActiveCheckpoints 列出所有活跃的checkpoint（用于监控和清理）
func (f *FileCheckPointStore) ListActiveCheckpoints(ctx context.Context) ([]string, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	pattern := filepath.Join(f.baseDir, f.filePrefix+"*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to list checkpoint files: %w", err)
	}

	checkpointIDs := make([]string, 0, len(matches))
	for _, match := range matches {
		filename := filepath.Base(match)
		checkpointID := filename[len(f.filePrefix):]
		checkpointIDs = append(checkpointIDs, checkpointID)
	}

	return checkpointIDs, nil
}

// Close 关闭文件存储（无操作，但保持接口兼容）
func (f *FileCheckPointStore) Close() error {
	return nil
}

// getFilePath 获取checkpoint文件路径
func (f *FileCheckPointStore) getFilePath(checkPointID string) string {
	return filepath.Join(f.baseDir, f.filePrefix+checkPointID)
}

// 优雅关闭时的checkpoint保存机制
type GracefulShutdownStoreManager struct {
	store      *FileCheckPointStore
	checkpoint map[string][]byte // 当前内存中的checkpoint缓存
	mu         sync.RWMutex
}

func NewGracefulShutdownStoreManager(ctx context.Context) (*GracefulShutdownStoreManager, error) {
	// 自己创建FileCheckPointStore
	fileStore, err := NewFileCheckPointStore(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create file checkpoint store: %w", err)
	}

	return &GracefulShutdownStoreManager{
		store:      fileStore.(*FileCheckPointStore),
		checkpoint: make(map[string][]byte),
	}, nil
}

// 实现 compose.CheckPointStore 接口

// Get 从文件获取checkpoint数据
func (g *GracefulShutdownStoreManager) Get(ctx context.Context, checkPointID string) ([]byte, bool, error) {
	fmt.Printf("Get checkpoint %s from file\n", checkPointID)
	return g.store.Get(ctx, checkPointID)
}

// Set 将checkpoint数据保存到文件
func (g *GracefulShutdownStoreManager) Set(ctx context.Context, checkPointID string, checkPoint []byte) error {
	// 同时保存到内存缓存和文件
	g.mu.Lock()
	g.checkpoint[checkPointID] = checkPoint
	g.mu.Unlock()
	fmt.Printf("Set checkpoint %s to file, content: %s\n", checkPointID, string(checkPoint))
	return g.store.Set(ctx, checkPointID, checkPoint)
}

// SaveCheckpoint 保存checkpoint到内存缓存（内部方法）
func (g *GracefulShutdownStoreManager) SaveCheckpoint(checkPointID string, data []byte) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.checkpoint[checkPointID] = data
}

// FlushToFile 将所有缓存的checkpoint刷新到文件
func (g *GracefulShutdownStoreManager) FlushToFile(ctx context.Context) error {
	g.mu.RLock()
	defer g.mu.RUnlock()

	for checkPointID, data := range g.checkpoint {
		fmt.Printf("Flushing checkpoint %s to file, content: %s\n", checkPointID, string(data))
		if err := g.store.Set(ctx, checkPointID, data); err != nil {
			return fmt.Errorf("failed to flush checkpoint %s: %w", checkPointID, err)
		}
	}
	return nil
}

// GetCheckpointFromFile 从文件获取checkpoint数据
func (g *GracefulShutdownStoreManager) GetCheckpointFromFile(ctx context.Context, checkPointID string) ([]byte, bool, error) {
	return g.store.Get(ctx, checkPointID)
}

// ClearCache 清空内存中的checkpoint缓存
func (g *GracefulShutdownStoreManager) ClearCache() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.checkpoint = make(map[string][]byte)
}

// Close 关闭文件存储
func (g *GracefulShutdownStoreManager) Close() error {
	return g.store.Close()
}
