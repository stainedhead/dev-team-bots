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

// --- SubTeamManager ---

type SubTeamManager struct {
	SpawnFn       func(ctx context.Context, botType, name, workDir string) (*domain.SpawnedAgent, error)
	TerminateFn   func(ctx context.Context, name string) error
	HeartbeatFn   func(ctx context.Context) error
	ListAgentsFn  func(ctx context.Context) ([]*domain.SpawnedAgent, error)
	TearDownAllFn func(ctx context.Context) error

	mu         sync.Mutex
	SpawnCalls []SpawnCall
}

type SpawnCall struct {
	BotType string
	Name    string
	WorkDir string
}

func (m *SubTeamManager) Spawn(ctx context.Context, botType, name, workDir string) (*domain.SpawnedAgent, error) {
	m.mu.Lock()
	m.SpawnCalls = append(m.SpawnCalls, SpawnCall{BotType: botType, Name: name, WorkDir: workDir})
	fn := m.SpawnFn
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, botType, name, workDir)
	}
	return &domain.SpawnedAgent{Name: name, BotType: botType, Status: domain.AgentStatusIdle}, nil
}

func (m *SubTeamManager) Terminate(ctx context.Context, name string) error {
	if m.TerminateFn != nil {
		return m.TerminateFn(ctx, name)
	}
	return nil
}

func (m *SubTeamManager) SendHeartbeat(ctx context.Context) error {
	if m.HeartbeatFn != nil {
		return m.HeartbeatFn(ctx)
	}
	return nil
}

func (m *SubTeamManager) ListAgents(ctx context.Context) ([]*domain.SpawnedAgent, error) {
	if m.ListAgentsFn != nil {
		return m.ListAgentsFn(ctx)
	}
	return nil, nil
}

func (m *SubTeamManager) TearDownAll(ctx context.Context) error {
	if m.TearDownAllFn != nil {
		return m.TearDownAllFn(ctx)
	}
	return nil
}

// RulesTracker is a test double for domain.RulesTracker.
type RulesTracker struct {
	UpdateForDirFn func(ctx context.Context, dir string) domain.RulesUpdate
	ResetFn        func()

	mu     sync.Mutex
	Dirs   []string // dirs passed to UpdateForDir in order
	Resets int      // number of Reset calls
}

func (m *RulesTracker) UpdateForDir(ctx context.Context, dir string) domain.RulesUpdate {
	m.mu.Lock()
	m.Dirs = append(m.Dirs, dir)
	fn := m.UpdateForDirFn
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, dir)
	}
	return domain.RulesUpdate{}
}

func (m *RulesTracker) Reset() {
	m.mu.Lock()
	m.Resets++
	fn := m.ResetFn
	m.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// --- PluginStore ---

// PluginStore is a hand-written mock for domain.PluginStore.
type PluginStore struct {
	ListFn    func(ctx context.Context) ([]domain.Plugin, error)
	GetFn     func(ctx context.Context, id string) (domain.Plugin, error)
	InstallFn func(ctx context.Context, manifest domain.PluginManifest, archive []byte, registry string, trusted bool) (domain.Plugin, error)
	ApproveFn func(ctx context.Context, id string) error
	RejectFn  func(ctx context.Context, id string) error
	DisableFn func(ctx context.Context, id string) error
	EnableFn  func(ctx context.Context, id string) error
	UpdateFn  func(ctx context.Context, id string, manifest domain.PluginManifest, archive []byte) error
	ReloadFn  func(ctx context.Context, id string) error
	RemoveFn  func(ctx context.Context, id string) error

	mu           sync.Mutex
	InstallCalls []struct {
		Manifest domain.PluginManifest
		Registry string
		Trusted  bool
	}
}

func (m *PluginStore) List(ctx context.Context) ([]domain.Plugin, error) {
	if m.ListFn != nil {
		return m.ListFn(ctx)
	}
	return nil, nil
}

func (m *PluginStore) Get(ctx context.Context, id string) (domain.Plugin, error) {
	if m.GetFn != nil {
		return m.GetFn(ctx, id)
	}
	return domain.Plugin{}, nil
}

func (m *PluginStore) Install(ctx context.Context, manifest domain.PluginManifest, archive []byte, registry string, trusted bool) (domain.Plugin, error) {
	m.mu.Lock()
	m.InstallCalls = append(m.InstallCalls, struct {
		Manifest domain.PluginManifest
		Registry string
		Trusted  bool
	}{Manifest: manifest, Registry: registry, Trusted: trusted})
	m.mu.Unlock()
	if m.InstallFn != nil {
		return m.InstallFn(ctx, manifest, archive, registry, trusted)
	}
	return domain.Plugin{}, nil
}

func (m *PluginStore) Approve(ctx context.Context, id string) error {
	if m.ApproveFn != nil {
		return m.ApproveFn(ctx, id)
	}
	return nil
}

func (m *PluginStore) Reject(ctx context.Context, id string) error {
	if m.RejectFn != nil {
		return m.RejectFn(ctx, id)
	}
	return nil
}

func (m *PluginStore) Disable(ctx context.Context, id string) error {
	if m.DisableFn != nil {
		return m.DisableFn(ctx, id)
	}
	return nil
}

func (m *PluginStore) Enable(ctx context.Context, id string) error {
	if m.EnableFn != nil {
		return m.EnableFn(ctx, id)
	}
	return nil
}

func (m *PluginStore) Update(ctx context.Context, id string, manifest domain.PluginManifest, archive []byte) error {
	if m.UpdateFn != nil {
		return m.UpdateFn(ctx, id, manifest, archive)
	}
	return nil
}

func (m *PluginStore) Reload(ctx context.Context, id string) error {
	if m.ReloadFn != nil {
		return m.ReloadFn(ctx, id)
	}
	return nil
}

func (m *PluginStore) Remove(ctx context.Context, id string) error {
	if m.RemoveFn != nil {
		return m.RemoveFn(ctx, id)
	}
	return nil
}

// --- RegistryManager ---

// RegistryManager is a hand-written mock for domain.RegistryManager.
type RegistryManager struct {
	ListFn          func(ctx context.Context) ([]domain.PluginRegistry, error)
	AddFn           func(ctx context.Context, reg domain.PluginRegistry) error
	RemoveFn        func(ctx context.Context, name string) error
	FetchIndexFn    func(ctx context.Context, registryURL string, force bool) (domain.RegistryIndex, error)
	FetchManifestFn func(ctx context.Context, manifestURL string) (domain.PluginManifest, error)
	FetchArchiveFn  func(ctx context.Context, downloadURL string) ([]byte, error)
}

func (m *RegistryManager) List(ctx context.Context) ([]domain.PluginRegistry, error) {
	if m.ListFn != nil {
		return m.ListFn(ctx)
	}
	return nil, nil
}

func (m *RegistryManager) Add(ctx context.Context, reg domain.PluginRegistry) error {
	if m.AddFn != nil {
		return m.AddFn(ctx, reg)
	}
	return nil
}

func (m *RegistryManager) Remove(ctx context.Context, name string) error {
	if m.RemoveFn != nil {
		return m.RemoveFn(ctx, name)
	}
	return nil
}

func (m *RegistryManager) FetchIndex(ctx context.Context, registryURL string, force bool) (domain.RegistryIndex, error) {
	if m.FetchIndexFn != nil {
		return m.FetchIndexFn(ctx, registryURL, force)
	}
	return domain.RegistryIndex{}, nil
}

func (m *RegistryManager) FetchManifest(ctx context.Context, manifestURL string) (domain.PluginManifest, error) {
	if m.FetchManifestFn != nil {
		return m.FetchManifestFn(ctx, manifestURL)
	}
	return domain.PluginManifest{}, nil
}

func (m *RegistryManager) FetchArchive(ctx context.Context, downloadURL string) ([]byte, error) {
	if m.FetchArchiveFn != nil {
		return m.FetchArchiveFn(ctx, downloadURL)
	}
	return nil, nil
}
