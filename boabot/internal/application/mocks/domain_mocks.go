// Package mocks provides hand-written test doubles for core domain interfaces
// used by the application layer.
package mocks

import (
	"context"
	"sync"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// --- MessageQueue ---

type SendCall struct {
	QueueURL string
	Message  domain.Message
}

type MessageQueue struct {
	SendFn    func(ctx context.Context, queueURL string, msg domain.Message) error
	ReceiveFn func(ctx context.Context) ([]domain.ReceivedMessage, error)
	DeleteFn  func(ctx context.Context, receiptHandle string) error

	mu          sync.Mutex
	SendCalls   []SendCall
	DeleteCalls []string
}

func (m *MessageQueue) Send(ctx context.Context, queueURL string, msg domain.Message) error {
	m.mu.Lock()
	m.SendCalls = append(m.SendCalls, SendCall{QueueURL: queueURL, Message: msg})
	fn := m.SendFn
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, queueURL, msg)
	}
	return nil
}

func (m *MessageQueue) Receive(ctx context.Context) ([]domain.ReceivedMessage, error) {
	if m.ReceiveFn != nil {
		return m.ReceiveFn(ctx)
	}
	return nil, nil
}

func (m *MessageQueue) Delete(ctx context.Context, receiptHandle string) error {
	m.mu.Lock()
	m.DeleteCalls = append(m.DeleteCalls, receiptHandle)
	fn := m.DeleteFn
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, receiptHandle)
	}
	return nil
}

func (m *MessageQueue) GetSendCalls() []SendCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]SendCall, len(m.SendCalls))
	copy(out, m.SendCalls)
	return out
}

// --- Broadcaster ---

type Broadcaster struct {
	BroadcastFn    func(ctx context.Context, msg domain.Message) error
	BroadcastCalls []domain.Message
}

func (m *Broadcaster) Broadcast(ctx context.Context, msg domain.Message) error {
	m.BroadcastCalls = append(m.BroadcastCalls, msg)
	if m.BroadcastFn != nil {
		return m.BroadcastFn(ctx, msg)
	}
	return nil
}

// --- ChannelMonitor ---

type ChannelMonitor struct {
	StartFn    func(ctx context.Context) error
	StopFn     func(ctx context.Context) error
	StartCalls int
	StopCalls  int
}

func (m *ChannelMonitor) Start(ctx context.Context) error {
	m.StartCalls++
	if m.StartFn != nil {
		return m.StartFn(ctx)
	}
	return nil
}

func (m *ChannelMonitor) Stop(ctx context.Context) error {
	m.StopCalls++
	if m.StopFn != nil {
		return m.StopFn(ctx)
	}
	return nil
}

// --- Worker ---

type Worker struct {
	ExecuteFn    func(ctx context.Context, task domain.Task) (domain.TaskResult, error)
	ExecuteCalls []domain.Task
	mu           sync.Mutex
}

func (m *Worker) Execute(ctx context.Context, task domain.Task) (domain.TaskResult, error) {
	m.mu.Lock()
	m.ExecuteCalls = append(m.ExecuteCalls, task)
	fn := m.ExecuteFn
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, task)
	}
	return domain.TaskResult{TaskID: task.ID, Success: true}, nil
}

// --- WorkerFactory ---

type WorkerFactory struct {
	Worker *Worker
}

func (m *WorkerFactory) New() domain.Worker {
	return m.Worker
}

// --- ModelProvider ---

type ModelProvider struct {
	InvokeFn    func(ctx context.Context, req domain.InvokeRequest) (domain.InvokeResponse, error)
	InvokeCalls []domain.InvokeRequest
}

func (m *ModelProvider) Invoke(ctx context.Context, req domain.InvokeRequest) (domain.InvokeResponse, error) {
	m.InvokeCalls = append(m.InvokeCalls, req)
	if m.InvokeFn != nil {
		return m.InvokeFn(ctx, req)
	}
	return domain.InvokeResponse{Content: "mock response", StopReason: "end_turn"}, nil
}

// --- MCPClient ---

type MCPClient struct {
	ListToolsFn func(ctx context.Context) ([]domain.MCPTool, error)
	CallToolFn  func(ctx context.Context, name string, args map[string]any) (domain.MCPToolResult, error)
}

func (m *MCPClient) ListTools(ctx context.Context) ([]domain.MCPTool, error) {
	if m.ListToolsFn != nil {
		return m.ListToolsFn(ctx)
	}
	return nil, nil
}

func (m *MCPClient) CallTool(ctx context.Context, name string, args map[string]any) (domain.MCPToolResult, error) {
	if m.CallToolFn != nil {
		return m.CallToolFn(ctx, name, args)
	}
	return domain.MCPToolResult{}, nil
}

// --- MemoryStore ---

type MemoryStore struct {
	WriteFn  func(ctx context.Context, key string, value []byte) error
	ReadFn   func(ctx context.Context, key string) ([]byte, error)
	DeleteFn func(ctx context.Context, key string) error
}

func (m *MemoryStore) Write(ctx context.Context, key string, value []byte) error {
	if m.WriteFn != nil {
		return m.WriteFn(ctx, key, value)
	}
	return nil
}

func (m *MemoryStore) Read(ctx context.Context, key string) ([]byte, error) {
	if m.ReadFn != nil {
		return m.ReadFn(ctx, key)
	}
	return nil, nil
}

func (m *MemoryStore) Delete(ctx context.Context, key string) error {
	if m.DeleteFn != nil {
		return m.DeleteFn(ctx, key)
	}
	return nil
}

// --- VectorStore ---

type VectorStore struct {
	UpsertFn func(ctx context.Context, key string, vector []float32, metadata map[string]string) error
	SearchFn func(ctx context.Context, query []float32, limit int) ([]domain.VectorResult, error)
}

func (m *VectorStore) Upsert(ctx context.Context, key string, vector []float32, metadata map[string]string) error {
	if m.UpsertFn != nil {
		return m.UpsertFn(ctx, key, vector, metadata)
	}
	return nil
}

func (m *VectorStore) Search(ctx context.Context, query []float32, limit int) ([]domain.VectorResult, error) {
	if m.SearchFn != nil {
		return m.SearchFn(ctx, query, limit)
	}
	return nil, nil
}

// --- Embedder ---

type Embedder struct {
	EmbedFn    func(ctx context.Context, text string) ([]float32, error)
	EmbedCalls []string
}

func (m *Embedder) Embed(ctx context.Context, text string) ([]float32, error) {
	m.EmbedCalls = append(m.EmbedCalls, text)
	if m.EmbedFn != nil {
		return m.EmbedFn(ctx, text)
	}
	return []float32{0.1, 0.2, 0.3}, nil
}

