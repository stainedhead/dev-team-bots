package acp

import (
	"context"
	"errors"
	"testing"
	"time"

	sdk "github.com/coder/acp-go-sdk"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

func TestAgent_ImplementsSDKInterface(t *testing.T) {
	var _ sdk.Agent = (*Agent)(nil)
}

func TestAgent_WithKeepAliveInterval_Overrides(t *testing.T) {
	a := New(&fakeWorkerFactory{}, "", WithKeepAliveInterval(5*time.Millisecond))
	if a.keepAliveInterval != 5*time.Millisecond {
		t.Errorf("keepAliveInterval = %v, want 5ms", a.keepAliveInterval)
	}
}

func TestAgent_Initialize(t *testing.T) {
	a := New(&fakeWorkerFactory{}, "")

	resp, err := a.Initialize(context.Background(), sdk.InitializeRequest{ProtocolVersion: sdk.ProtocolVersionNumber})
	if err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}
	if resp.ProtocolVersion != sdk.ProtocolVersionNumber {
		t.Errorf("ProtocolVersion = %v, want %v", resp.ProtocolVersion, sdk.ProtocolVersionNumber)
	}
	if len(resp.AuthMethods) != 0 {
		t.Errorf("AuthMethods = %v, want empty (ACP mode requires no separate auth)", resp.AuthMethods)
	}
}

func TestAgent_NewSession_ReturnsUniqueIDs(t *testing.T) {
	a := New(&fakeWorkerFactory{}, "")

	first, err := a.NewSession(context.Background(), sdk.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}
	if first.SessionId == "" {
		t.Fatal("SessionId is empty")
	}

	second, err := a.NewSession(context.Background(), sdk.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}
	if second.SessionId == first.SessionId {
		t.Fatal("NewSession returned the same SessionId twice")
	}
}

func TestAgent_Authenticate_IsANoOpSuccess(t *testing.T) {
	a := New(&fakeWorkerFactory{}, "")
	if _, err := a.Authenticate(context.Background(), sdk.AuthenticateRequest{}); err != nil {
		t.Errorf("Authenticate returned error: %v", err)
	}
}

func TestAgent_SetSessionConfigOption_IsANoOpSuccess(t *testing.T) {
	// buzz-acp calls this for its `mode` (permission-mode) config option on
	// every session by default -- research.md. Must not error.
	a := New(&fakeWorkerFactory{}, "")
	if _, err := a.SetSessionConfigOption(context.Background(), sdk.SetSessionConfigOptionRequest{}); err != nil {
		t.Errorf("SetSessionConfigOption returned error: %v", err)
	}
}

func TestAgent_SetSessionMode_IsANoOpSuccess(t *testing.T) {
	a := New(&fakeWorkerFactory{}, "")
	if _, err := a.SetSessionMode(context.Background(), sdk.SetSessionModeRequest{}); err != nil {
		t.Errorf("SetSessionMode returned error: %v", err)
	}
}

func TestAgent_UnsupportedMethods_ReturnMethodNotFound(t *testing.T) {
	a := New(&fakeWorkerFactory{}, "")

	if _, err := a.Logout(context.Background(), sdk.LogoutRequest{}); !isMethodNotFound(err) {
		t.Errorf("Logout error = %v, want MethodNotFound", err)
	}
	if _, err := a.ListSessions(context.Background(), sdk.ListSessionsRequest{}); !isMethodNotFound(err) {
		t.Errorf("ListSessions error = %v, want MethodNotFound", err)
	}
	if _, err := a.ResumeSession(context.Background(), sdk.ResumeSessionRequest{}); !isMethodNotFound(err) {
		t.Errorf("ResumeSession error = %v, want MethodNotFound", err)
	}
}

func TestAgent_Initialize_AdvertisesSessionClose(t *testing.T) {
	// RT5/FR-006 (auto-review): CloseSession is now a real capability, not a
	// MethodNotFound stub, so it must be advertised.
	a := New(&fakeWorkerFactory{}, "")
	resp, err := a.Initialize(context.Background(), sdk.InitializeRequest{ProtocolVersion: sdk.ProtocolVersionNumber})
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if resp.AgentCapabilities.SessionCapabilities.Close == nil {
		t.Error("AgentCapabilities.SessionCapabilities.Close is nil, want advertised support")
	}
}

func TestAgent_CloseSession_RemovesTheSession(t *testing.T) {
	a := New(&fakeWorkerFactory{}, "")
	sid := newSessionForTest(t, a)

	if _, err := a.CloseSession(context.Background(), sdk.CloseSessionRequest{SessionId: sid}); err != nil {
		t.Fatalf("CloseSession: %v", err)
	}

	if _, err := a.Prompt(context.Background(), sdk.PromptRequest{SessionId: sid}); err == nil {
		t.Error("Prompt succeeded against a closed session, want an unknown-session error")
	}
}

func TestAgent_CloseSession_CancelsInFlightTurn(t *testing.T) {
	fw := &fakeWorker{delay: time.Second, result: domain.TaskResult{Output: "unused", Success: true}}
	a := New(&fakeWorkerFactory{worker: fw}, "")
	a.setUpdater(&fakeConn{})
	sid := newSessionForTest(t, a)

	done := make(chan sdk.PromptResponse, 1)
	go func() {
		resp, _ := a.Prompt(context.Background(), sdk.PromptRequest{SessionId: sid, Prompt: []sdk.ContentBlock{sdk.TextBlock("q")}})
		done <- resp
	}()
	time.Sleep(20 * time.Millisecond)

	if _, err := a.CloseSession(context.Background(), sdk.CloseSessionRequest{SessionId: sid}); err != nil {
		t.Fatalf("CloseSession: %v", err)
	}

	select {
	case resp := <-done:
		if resp.StopReason != sdk.StopReasonCancelled {
			t.Errorf("StopReason = %v, want Cancelled", resp.StopReason)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Prompt did not return promptly after CloseSession")
	}
}

func TestAgent_SessionMap_BoundedEviction(t *testing.T) {
	// RT5/FR-006 (auto-review): the session map must not grow without
	// bound for a long-lived, pooled process that never receives
	// session/close calls.
	a := New(&fakeWorkerFactory{}, "", WithMaxSessions(3))

	var ids []sdk.SessionId
	for range 5 {
		ids = append(ids, newSessionForTest(t, a))
	}

	// The two oldest should have been evicted; the three newest survive.
	if _, err := a.Prompt(context.Background(), sdk.PromptRequest{SessionId: ids[0]}); err == nil {
		t.Error("oldest session was not evicted")
	}
	if _, err := a.Prompt(context.Background(), sdk.PromptRequest{SessionId: ids[1]}); err == nil {
		t.Error("second-oldest session was not evicted")
	}
}

func isMethodNotFound(err error) bool {
	var re *sdk.RequestError
	return errors.As(err, &re) && re.Code == sdk.NewMethodNotFound("x").Code
}
