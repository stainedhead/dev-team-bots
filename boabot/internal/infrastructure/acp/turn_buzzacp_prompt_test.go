package acp

import (
	"context"
	"strings"
	"testing"

	sdk "github.com/coder/acp-go-sdk"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// buzzACPStylePrompt builds a prompt shaped like the real prompt buzz-acp
// assembles in production: several paragraphs of platform/tool-doc
// boilerplate (deliberately containing words the NL scheduling heuristic
// scores on -- "schedule", "every", "bot" -- exactly like the real
// boilerplate does), ending in the "[Buzz event: ...]" block carrying the
// actual human message. This is what a live production bug looked like:
// scoring/recording the ENTIRE prompt as "the message" caused a genuine
// question to be misclassified as a scheduling request.
func buzzACPStylePrompt(humanText string) string {
	var sb strings.Builder
	sb.WriteString("[Base]\nYou are operating inside the Buzz platform. ")
	sb.WriteString("Respond promptly. Every mention sends a notification; a mention nobody needs to act on is a false alarm. ")
	sb.WriteString("Schedule your work carefully and track every deliverable. ")
	sb.WriteString("This is the orchestrator agent within baobot, he can make things happen.\n\n")
	sb.WriteString("[Conversation Context (1 of 1 messages)]\n[1] stainedhead (abc) (2026-08-16T02:10:25+00:00): hello\n\n")
	sb.WriteString("[Buzz event: @mention]\n")
	sb.WriteString("Event ID: d04e43023e2167ab0fcd04255ceec2ddd1c233c01ca749696453f8d094b9cd78\n")
	sb.WriteString("Channel: DM (#f9043d9d-dbb0-400c-a072-0c74ef737835)\n")
	sb.WriteString("Kind: 9\n")
	sb.WriteString("From: stainedhead (npub: npub18r8, hex: 38cf3d46)\n")
	sb.WriteString("Time: 2026-08-17T05:13:06+00:00\n")
	sb.WriteString("Content: " + humanText + "\n")
	sb.WriteString("Tags: [[\"h\",\"f9043d9d-dbb0-400c-a072-0c74ef737835\"],[\"p\",\"d5b57891\"]]")
	return sb.String()
}

// TestAgent_Prompt_BuzzACPBoilerplate_DoesNotFalsePositiveScheduling is the
// direct end-to-end regression test for the live production bug: a genuine
// question wrapped in buzz-acp's real prompt shape must reach worker.Execute
// normally, not get intercepted as a false scheduling confirmation just
// because the surrounding boilerplate contains scheduling-heuristic
// keywords.
func TestAgent_Prompt_BuzzACPBoilerplate_DoesNotFalsePositiveScheduling(t *testing.T) {
	fw := &fakeWorker{result: domain.TaskResult{Output: "The board has 6 items.", Success: true}}
	a, _, taskStore, _ := newChatStateAgent(t, fw)
	sid := newSessionForTest(t, a)

	prompt := buzzACPStylePrompt("what scheduled tasks do you have coming up?  what about the backlog in the kanban board?")
	resp, err := a.Prompt(context.Background(), sdk.PromptRequest{SessionId: sid, Prompt: []sdk.ContentBlock{sdk.TextBlock(prompt)}})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if resp.StopReason != sdk.StopReasonEndTurn {
		t.Errorf("StopReason = %v, want %v", resp.StopReason, sdk.StopReasonEndTurn)
	}
	if fw.receivedTask.ID == "" {
		t.Fatal("expected worker.Execute to be called for a genuine question, but it was intercepted as a scheduling request")
	}

	tasks, _ := taskStore.ListAll(context.Background())
	scheduled := 0
	for _, task := range tasks {
		if task.Schedule.Mode == domain.ScheduleModeRecurring || task.Schedule.Mode == domain.ScheduleModeFuture {
			scheduled++
		}
	}
	if scheduled != 0 {
		t.Errorf("expected no scheduled tasks created from a genuine question, got %d", scheduled)
	}
}

// TestAgent_Prompt_BuzzACPBoilerplate_ChatStoreRecordsCleanMessageOnly
// verifies FR-503's history doesn't get polluted with buzz-acp's own
// multi-thousand-character injected prompt -- only the real human message
// is recorded, so history replay stays small across many turns instead of
// compounding unboundedly.
func TestAgent_Prompt_BuzzACPBoilerplate_ChatStoreRecordsCleanMessageOnly(t *testing.T) {
	fw := &fakeWorker{result: domain.TaskResult{Output: "done", Success: true}}
	a, chatStore, _, _ := newChatStateAgent(t, fw)
	sid := newSessionForTest(t, a)

	prompt := buzzACPStylePrompt("what is on the kanban board?")
	if _, err := a.Prompt(context.Background(), sdk.PromptRequest{SessionId: sid, Prompt: []sdk.ContentBlock{sdk.TextBlock(prompt)}}); err != nil {
		t.Fatalf("Prompt: %v", err)
	}

	history, err := chatStore.List(context.Background(), "f9043d9d-dbb0-400c-a072-0c74ef737835")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	outbound := ""
	for _, m := range history {
		if m.Direction == domain.ChatDirectionOutbound {
			outbound = m.Content
		}
	}
	if outbound != "what is on the kanban board?" {
		t.Errorf("expected ChatStore to record only the clean human message, got %q (len=%d)", outbound, len(outbound))
	}
	if len(outbound) > 200 {
		t.Errorf("recorded outbound message is %d bytes -- looks like the full boilerplate prompt was recorded instead of the isolated message", len(outbound))
	}
}

// TestAgent_Prompt_BuzzACPBoilerplate_SecondTurnHistoryStaysSmall is the
// compounding-bloat regression test: across two turns, the replayed "Prior
// conversation context" block must reflect the small human messages, not
// buzz-acp's own large injected prompt from the first turn.
func TestAgent_Prompt_BuzzACPBoilerplate_SecondTurnHistoryStaysSmall(t *testing.T) {
	fw := &fakeWorker{result: domain.TaskResult{Output: "ok", Success: true}}
	a, _, _, _ := newChatStateAgent(t, fw)
	sid := newSessionForTest(t, a)

	first := buzzACPStylePrompt("what is on the kanban board?")
	if _, err := a.Prompt(context.Background(), sdk.PromptRequest{SessionId: sid, Prompt: []sdk.ContentBlock{sdk.TextBlock(first)}}); err != nil {
		t.Fatalf("first Prompt: %v", err)
	}

	second := buzzACPStylePrompt("and what about tomorrow?")
	if _, err := a.Prompt(context.Background(), sdk.PromptRequest{SessionId: sid, Prompt: []sdk.ContentBlock{sdk.TextBlock(second)}}); err != nil {
		t.Fatalf("second Prompt: %v", err)
	}

	// fw.receivedTask.Instruction is the second turn's full instruction:
	// buzz-acp's own fresh boilerplate for turn 2, plus a prepended history
	// block. The history block must contain the SMALL first message, not
	// the first turn's own multi-KB boilerplate repeated.
	if !strings.Contains(fw.receivedTask.Instruction, "what is on the kanban board?") {
		t.Error("expected the second turn's history to include the first turn's clean message")
	}
	// Count occurrences of a boilerplate-only phrase -- it must appear
	// exactly once (from turn 2's own fresh prompt), not twice (which would
	// mean turn 1's full boilerplate was replayed as "history").
	count := strings.Count(fw.receivedTask.Instruction, "Respond promptly")
	if count != 1 {
		t.Errorf("expected buzz-acp's boilerplate phrase to appear exactly once (this turn's own prompt), got %d -- history replay is duplicating boilerplate", count)
	}
}
