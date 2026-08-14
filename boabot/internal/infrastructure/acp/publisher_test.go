package acp

import (
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

func TestExtractChannelID_FindsUUIDInContextBlock(t *testing.T) {
	instruction := "[Context]\nScope: channel\nChannel: general (#3a6a69fc-05a5-55cb-a601-0e12afc77c07)\nDescription: General."
	got, ok := extractChannelID(instruction)
	if !ok {
		t.Fatal("expected a channel id to be found")
	}
	if got != "3a6a69fc-05a5-55cb-a601-0e12afc77c07" {
		t.Errorf("unexpected channel id: %q", got)
	}
}

func TestExtractChannelID_NoMatchReturnsFalse(t *testing.T) {
	_, ok := extractChannelID("[Context]\nScope: dm\nNo channel here.")
	if ok {
		t.Error("expected no channel id to be found")
	}
}

func TestCalledPublish_TrueWhenBuzzMessagesSendWasCalled(t *testing.T) {
	calls := []domain.ToolCall{
		{Name: "list_dir", Args: map[string]any{"path": "."}},
		{Name: "run_shell", Args: map[string]any{"command": `buzz messages send --channel abc --content "hi"`}},
	}
	if !calledPublish(calls) {
		t.Error("expected calledPublish to detect the buzz messages send call")
	}
}

func TestCalledPublish_FalseWhenNoMatchingCall(t *testing.T) {
	calls := []domain.ToolCall{
		{Name: "run_shell", Args: map[string]any{"command": "buzz messages get --channel abc"}},
		{Name: "read_file", Args: map[string]any{"path": "/tmp/x"}},
	}
	if calledPublish(calls) {
		t.Error("expected calledPublish to be false — no send call present")
	}
}

func TestCalledPublish_FalseWhenNoCallsAtAll(t *testing.T) {
	if calledPublish(nil) {
		t.Error("expected calledPublish to be false for an empty call list")
	}
}
