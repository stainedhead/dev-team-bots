package acp

import (
	"context"
	"errors"
	"testing"

	sdk "github.com/coder/acp-go-sdk"
)

func TestAgent_ImplementsSDKInterface(t *testing.T) {
	var _ sdk.Agent = (*Agent)(nil)
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
	if _, err := a.CloseSession(context.Background(), sdk.CloseSessionRequest{}); !isMethodNotFound(err) {
		t.Errorf("CloseSession error = %v, want MethodNotFound", err)
	}
	if _, err := a.ListSessions(context.Background(), sdk.ListSessionsRequest{}); !isMethodNotFound(err) {
		t.Errorf("ListSessions error = %v, want MethodNotFound", err)
	}
	if _, err := a.ResumeSession(context.Background(), sdk.ResumeSessionRequest{}); !isMethodNotFound(err) {
		t.Errorf("ResumeSession error = %v, want MethodNotFound", err)
	}
}

func isMethodNotFound(err error) bool {
	var re *sdk.RequestError
	return errors.As(err, &re) && re.Code == sdk.NewMethodNotFound("x").Code
}
