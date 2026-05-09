package domain

import "strings"

const taskOutcomePrefix = "TASK_OUTCOME:"

// ParseTaskOutcome scans output line-by-line for a TASK_OUTCOME marker and
// returns the corresponding DirectTaskStatus. Returns an empty string when no
// recognised marker is present, meaning the caller should treat the task as
// succeeded.
func ParseTaskOutcome(output string) DirectTaskStatus {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, taskOutcomePrefix) {
			continue
		}
		val := strings.TrimSpace(strings.TrimPrefix(line, taskOutcomePrefix))
		switch val {
		case string(DirectTaskStatusBlocked):
			return DirectTaskStatusBlocked
		case string(DirectTaskStatusErrored):
			return DirectTaskStatusErrored
		}
	}
	return ""
}

// TaskOutcomeInstructions is appended to every bot's system prompt so the bot
// knows how to signal a non-success outcome.
const TaskOutcomeInstructions = `
## Signalling task outcomes

When you complete a task, your response is treated as succeeded by default.
If the task cannot proceed and requires operator intervention, append this
exact line at the end of your response:

  TASK_OUTCOME: blocked

Use blocked when:
- A required resource, directory, or tool is missing but the operator can
  fix it and re-run the task (e.g. missing git repo, wrong work directory,
  prerequisite not installed).
- A human decision or approval is needed before you can continue.

If the task failed in a way that is unclear whether it can be recovered, or
that is definitely unrecoverable, append:

  TASK_OUTCOME: errored

Only append one marker, on its own line, at the very end of your response.
Do not append a marker when the task succeeds.`
