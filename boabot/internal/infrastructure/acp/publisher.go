package acp

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// Publisher publishes text as a real Buzz channel message. Used as a
// fallback safety net when a turn produces a substantive reply but the
// model never actually called the buzz CLI to publish it itself -- a known
// function-calling-model failure mode (the model judges a casual-seeming
// reply doesn't need publishing and just answers conversationally instead),
// not a wiring bug. Without this, that reply is only ever visible as an
// ephemeral ACP session/update -- ACP has no concept of a persisted channel
// message, so the harness can only surface it as a transient notification.
type Publisher interface {
	Publish(ctx context.Context, channelID, content string) error
}

// cliPublisher shells out to the buzz CLI exactly as a persona's own
// run_shell tool call would -- same binary resolution (PATH) and same
// inherited process environment, so it succeeds or fails under the
// identical conditions a manual "buzz messages send" call would.
type cliPublisher struct{}

func (cliPublisher) Publish(ctx context.Context, channelID, content string) error {
	cmd := exec.CommandContext(ctx, "buzz", "messages", "send", "--channel", channelID, "--content", content)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("buzz messages send: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// channelIDPattern matches the channel UUID embedded in the "[Context]"
// block buzz-acp prepends to every prompt, e.g. "Channel: general
// (#3a6a69fc-05a5-55cb-a601-0e12afc77c07)". extractText treats the rest of
// that block as opaque text (research.md); this is the one piece of it the
// fallback publisher needs to parse back out.
var channelIDPattern = regexp.MustCompile(`\(#([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})\)`)

// extractChannelID pulls the channel UUID out of a turn's raw instruction
// text. Returns false if no channel UUID is present -- e.g. a DM-scoped
// turn, which buzz-acp's own [Context] block formats without a channel.
func extractChannelID(instruction string) (string, bool) {
	m := channelIDPattern.FindStringSubmatch(instruction)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// humanMessagePattern matches the actual human-authored message text inside
// buzz-acp's assembled prompt's trailing "[Buzz event: ...]" block --
// everything else in the prompt (platform instructions, tool docs, replayed
// conversation context) is buzz-acp's own injected boilerplate, not
// something a human typed this turn. (?s) lets Content span multiple lines,
// since buzz-acp's own docs describe multiline message content sent via
// stdin. Matches greedily to the LAST "Content: ...\nTags:" occurrence (via
// FindAllStringSubmatch in extractHumanMessage) since that block is always
// the final section of the assembled prompt.
var humanMessagePattern = regexp.MustCompile(`(?s)\nContent: (.*?)(?:\nTags:|\z)`)

// extractHumanMessage isolates the real human-authored message from
// buzz-acp's assembled prompt (see humanMessagePattern's doc comment).
// Confirmed live production bug this fixes: scoring or recording buzz-acp's
// ENTIRE assembled prompt (several thousand characters of platform
// instructions/tool docs, which routinely contain words like
// "schedule"/"every"/"bot") as if it were "the message" caused
// ChatTaskManager's short-message NL heuristic to false-positive a
// scheduling intent on nearly every turn, and would have compounded
// unboundedly across turns once replayed as chat history. Falls back to
// returning instruction unchanged if the expected block isn't found -- e.g.
// a bare/non-buzz-acp ACP client sending a short instruction directly with
// no wrapping context, which must behave exactly as before this fix.
func extractHumanMessage(instruction string) string {
	matches := humanMessagePattern.FindAllStringSubmatch(instruction, -1)
	if len(matches) == 0 {
		return instruction
	}
	text := strings.TrimSpace(matches[len(matches)-1][1])
	if text == "" {
		return instruction
	}
	return text
}

// calledPublish reports whether any tool call executed during the turn was
// a run_shell invocation of "buzz messages send". A plain substring check
// on the shell command, not a full parse -- run_shell takes an opaque shell
// string rather than structured args, and the system prompt's own
// documented example is exactly this substring, so this catches the common
// case without needing to parse flags or quoting.
func calledPublish(calls []domain.ToolCall) bool {
	for _, c := range calls {
		if c.Name != "run_shell" {
			continue
		}
		cmd, _ := c.Args["command"].(string)
		if strings.Contains(cmd, "messages send") {
			return true
		}
	}
	return false
}
