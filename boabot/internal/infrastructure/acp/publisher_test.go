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

// TestExtractHumanMessage_IsolatesRealMessageFromBuzzACPPromptBoilerplate is
// a direct regression test for a live production bug: buzz-acp assembles a
// multi-thousand-character prompt (platform instructions, tool docs,
// replayed conversation context) ending in a "[Buzz event: ...]" block
// carrying the actual human message. Scoring or recording the ENTIRE
// assembled prompt as "the message" -- instead of isolating the real one --
// caused ChatTaskManager's NL scheduling-intent heuristic to false-positive
// on nearly every turn (the boilerplate itself contains words like
// "schedule"/"every"/"bot"), confirmed live: a genuine question ("what
// scheduled tasks do you have coming up? what about the backlog in the
// kanban board?") was misclassified as a request to create a new scheduled
// task for a bot named "package".
func TestExtractHumanMessage_IsolatesRealMessageFromBuzzACPPromptBoilerplate(t *testing.T) {
	instruction := "[Base]\nYou are operating inside the Buzz platform. Reply promptly. Every mention sends a notification. Schedule your work carefully. This is the orchestrator agent within baobot.\n\n" +
		"[Conversation Context (2 of 2 messages)]\n[1] stainedhead (abc) (2026-08-16T02:10:25+00:00): hello\n\n" +
		"[Buzz event: @mention]\n" +
		"Event ID: d04e43023e2167ab0fcd04255ceec2ddd1c233c01ca749696453f8d094b9cd78\n" +
		"Channel: DM (#f9043d9d-dbb0-400c-a072-0c74ef737835)\n" +
		"Kind: 9\n" +
		"From: stainedhead (npub: npub18r8, hex: 38cf3d46)\n" +
		"Time: 2026-08-17T05:13:06+00:00\n" +
		"Content: what scheduled tasks do you have coming up?  what about the backlog in the kanban board?\n" +
		"Tags: [[\"h\",\"f9043d9d-dbb0-400c-a072-0c74ef737835\"],[\"p\",\"d5b57891\"]]"

	got := extractHumanMessage(instruction)
	want := "what scheduled tasks do you have coming up?  what about the backlog in the kanban board?"
	if got != want {
		t.Errorf("extractHumanMessage() = %q, want %q", got, want)
	}
}

// TestExtractHumanMessage_MultilineContent verifies the extraction handles a
// message containing literal newlines (buzz-acp's own docs describe
// multiline message content via stdin), not just single-line messages.
func TestExtractHumanMessage_MultilineContent(t *testing.T) {
	instruction := "[Buzz event: @mention]\nEvent ID: abc\nContent: line one\nline two\nTags: []"
	got := extractHumanMessage(instruction)
	want := "line one\nline two"
	if got != want {
		t.Errorf("extractHumanMessage() = %q, want %q", got, want)
	}
}

// TestExtractHumanMessage_NoBuzzEventBlock_FallsBackToWholeInstruction
// guards a bare/non-buzz-acp ACP client sending a short instruction
// directly with no wrapping context -- must behave exactly as before this
// fix (NFR-Correctness).
func TestExtractHumanMessage_NoBuzzEventBlock_FallsBackToWholeInstruction(t *testing.T) {
	got := extractHumanMessage("hello, what is 2+2?")
	if got != "hello, what is 2+2?" {
		t.Errorf("extractHumanMessage() = %q, want the instruction unchanged", got)
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
