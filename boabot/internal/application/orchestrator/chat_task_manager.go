package orchestrator

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// ScheduledTaskDispatcher dispatches a task with an explicit schedule.
// Defined here in the application layer to avoid importing the http package
// (which would create a circular dependency).
type ScheduledTaskDispatcher interface {
	DispatchWithSchedule(ctx context.Context, botName, instruction string, schedule domain.Schedule, source domain.DirectTaskSource, threadID, workDir, title string) (domain.DirectTask, error)
}

// ChatTaskIntent holds the parsed intent from a task-management chat message.
type ChatTaskIntent struct {
	BotName     string
	Instruction string
	Schedule    domain.Schedule
	NLText      string // original natural-language schedule description
}

// ChatTaskManager detects task-management intent in chat messages and drives
// the confirmation flow.  It is safe for concurrent use.
type ChatTaskManager struct {
	dispatcher ScheduledTaskDispatcher
	pendingMap sync.Map // threadID (string) -> *ChatTaskIntent
}

// NewChatTaskManager constructs a ChatTaskManager.
func NewChatTaskManager(dispatcher ScheduledTaskDispatcher) *ChatTaskManager {
	return &ChatTaskManager{dispatcher: dispatcher}
}

// DetectAndHandle checks whether msg represents a task-management request or a
// confirmation/cancellation of a pending intent.
//
//   - If msg looks like a task request: stores the parsed intent, returns the
//     confirmation prompt, and sets handled=true.
//   - If msg looks like a confirmation and a pending intent exists for threadID:
//     dispatches the task, clears the pending intent, returns a success string,
//     and sets handled=true.
//   - If msg is a cancellation and a pending intent exists: clears the pending
//     intent, returns a cancellation string, and sets handled=true.
//   - Otherwise: returns "", false, nil — the caller should handle the message
//     normally.
func (m *ChatTaskManager) DetectAndHandle(ctx context.Context, threadID, msg string) (response string, handled bool, err error) {
	lower := strings.ToLower(strings.TrimSpace(msg))

	// Check confirmation / cancellation first so these short words are not
	// misidentified as task requests.
	if pending, ok := m.loadPending(threadID); ok {
		if isConfirmation(lower) {
			m.pendingMap.Delete(threadID)
			task, dispErr := m.dispatcher.DispatchWithSchedule(
				ctx,
				pending.BotName,
				pending.Instruction,
				pending.Schedule,
				domain.DirectTaskSourceChat,
				threadID, "", pending.Instruction,
			)
			if dispErr != nil {
				return "", true, fmt.Errorf("dispatch task: %w", dispErr)
			}
			return formatSuccess(pending.BotName, task), true, nil
		}
		if isCancellation(lower) {
			m.pendingMap.Delete(threadID)
			return "Cancelled. No task was created.", true, nil
		}
	}

	// Detect task request.
	if intent, ok := detectIntent(msg); ok {
		m.pendingMap.Store(threadID, intent)
		return formatConfirmationPrompt(intent), true, nil
	}

	return "", false, nil
}

// --- pending helpers ---------------------------------------------------------

func (m *ChatTaskManager) loadPending(threadID string) (*ChatTaskIntent, bool) {
	v, ok := m.pendingMap.Load(threadID)
	if !ok {
		return nil, false
	}
	intent, ok := v.(*ChatTaskIntent)
	return intent, ok
}

// --- intent detection --------------------------------------------------------

// Known bot names / keywords used for bot detection.
var knownBotKeywords = []string{
	"architect", "tech-lead", "tech lead", "reviewer", "maintainer", "implementer",
}

// detectIntent returns a ChatTaskIntent if msg matches the detection heuristic,
// or (nil, false) otherwise.  Two of the three keyword groups must match.
func detectIntent(msg string) (*ChatTaskIntent, bool) {
	lower := strings.ToLower(msg)

	actionScore := scoreAction(lower)
	timeScore := scoreTime(lower)
	botScore, botName := scoreBotName(lower)

	hits := 0
	if actionScore {
		hits++
	}
	if timeScore {
		hits++
	}
	if botScore {
		hits++
	}
	if hits < 2 {
		return nil, false
	}

	// Extract bot name — fall back to first word after "for" when not matched.
	if botName == "" {
		botName = extractBotNameFromFor(lower)
	}

	// Build the instruction: the full message minus schedule boilerplate.
	instruction := extractInstruction(msg, lower)

	schedule := ParseScheduleNL(lower)

	return &ChatTaskIntent{
		BotName:     botName,
		Instruction: instruction,
		Schedule:    schedule,
		NLText:      extractScheduleText(lower),
	}, true
}

// scoreAction returns true if the message contains an action keyword.
func scoreAction(lower string) bool {
	actions := []string{"schedule", "create task", "add task", "set up", " run "}
	for _, a := range actions {
		if strings.Contains(lower, a) {
			return true
		}
	}
	return false
}

// scoreTime returns true if the message contains a time/recurrence keyword.
func scoreTime(lower string) bool {
	timeWords := []string{
		"every", "daily", "weekly", "monthly",
		"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday",
	}
	for _, w := range timeWords {
		if strings.Contains(lower, w) {
			return true
		}
	}
	// "at HH" / "at Xam" / "at Xpm"
	if idx := strings.Index(lower, " at "); idx >= 0 {
		rest := strings.TrimSpace(lower[idx+4:])
		if len(rest) > 0 && (unicode.IsDigit(rune(rest[0]))) {
			return true
		}
	}
	return false
}

// scoreBotName returns (true, botName) if a known bot keyword is found.
func scoreBotName(lower string) (bool, string) {
	if strings.Contains(lower, "bot") {
		return true, extractBotNameFromFor(lower)
	}
	for _, kw := range knownBotKeywords {
		if strings.Contains(lower, kw) {
			return true, kw
		}
	}
	return false, ""
}

// extractBotNameFromFor extracts the word after "for the" or "for" in lower.
func extractBotNameFromFor(lower string) string {
	if idx := strings.Index(lower, "for the "); idx >= 0 {
		rest := strings.TrimSpace(lower[idx+8:])
		return firstWord(rest)
	}
	if idx := strings.Index(lower, " for "); idx >= 0 {
		rest := strings.TrimSpace(lower[idx+5:])
		return firstWord(rest)
	}
	return ""
}

func firstWord(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && r != '-' })
	if len(parts) == 0 {
		return s
	}
	return parts[0]
}

// extractInstruction builds a human-readable instruction from the original msg.
func extractInstruction(msg, _ string) string {
	// Use the original message as the instruction for now — simple and lossless.
	return strings.TrimSpace(msg)
}

// extractScheduleText returns a short NL description of the schedule from lower.
func extractScheduleText(lower string) string {
	// Look for text from "every"/"daily"/"weekly"/"monthly" onward.
	scheduleStarters := []string{"every ", "daily", "weekly", "monthly"}
	for _, s := range scheduleStarters {
		if idx := strings.Index(lower, s); idx >= 0 {
			text := lower[idx:]
			// Trim trailing " for ..." clause.
			if fi := strings.Index(text, " for "); fi >= 0 {
				text = text[:fi]
			}
			return strings.TrimSpace(text)
		}
	}
	return lower
}

// --- confirmation / cancellation detection -----------------------------------

var confirmationWords = map[string]bool{
	"yes":      true,
	"y":        true,
	"confirm":  true,
	"ok":       true,
	"sure":     true,
	"do it":    true,
	"go ahead": true,
}

var cancellationWords = map[string]bool{
	"no":        true,
	"cancel":    true,
	"nevermind": true,
	"stop":      true,
}

func isConfirmation(lower string) bool { return confirmationWords[lower] }
func isCancellation(lower string) bool { return cancellationWords[lower] }

// --- schedule parsing --------------------------------------------------------

// dayNameToBit maps lowercase day names to the DaysMask bit value.
var dayNameToBit = map[string]uint8{
	"sunday":    1 << uint(time.Sunday),
	"monday":    1 << uint(time.Monday),
	"tuesday":   1 << uint(time.Tuesday),
	"wednesday": 1 << uint(time.Wednesday),
	"thursday":  1 << uint(time.Thursday),
	"friday":    1 << uint(time.Friday),
	"saturday":  1 << uint(time.Saturday),
}

// ParseScheduleNL parses a natural-language schedule string and returns a
// domain.Schedule.  Exported so that unit tests can call it directly.
//
// Rules:
//   - "every day" | "daily" → Recurring/Daily
//   - "every week" | "weekly" | "every <dayname>" → Recurring/Weekly
//   - "every month" | "monthly" | "every <N>th" → Recurring/Monthly
//   - "at <H>am" / "at <H>pm" / "at <HH:MM>" → sets TimeOfDay
//   - Otherwise → ASAP
func ParseScheduleNL(lower string) domain.Schedule {
	lower = strings.ToLower(lower)

	// --- Determine frequency -------------------------------------------------

	isDaily := strings.Contains(lower, "every day") || strings.Contains(lower, "daily")
	isMonthly := strings.Contains(lower, "every month") || strings.Contains(lower, "monthly")

	// Day-name matching for weekly.
	var daysMask uint8
	for day, bit := range dayNameToBit {
		if strings.Contains(lower, day) {
			daysMask |= bit
		}
	}

	// Month-day extraction: "on the Nth" or "on the Nst/rd/th".
	monthDay := extractMonthDay(lower)

	// Decide frequency.
	var freq domain.RecurrenceFrequency
	switch {
	case isMonthly || monthDay > 0:
		freq = domain.RecurrenceFrequencyMonthly
	case isDaily:
		freq = domain.RecurrenceFrequencyDaily
	case daysMask > 0 || strings.Contains(lower, "every week") || strings.Contains(lower, "weekly"):
		freq = domain.RecurrenceFrequencyWeekly
	default:
		return domain.Schedule{Mode: domain.ScheduleModeASAP}
	}

	// --- Parse time of day ---------------------------------------------------
	tod := parseTimeOfDay(lower)

	rule := &domain.RecurrenceRule{
		Frequency: freq,
		DaysMask:  daysMask,
		TimeOfDay: tod,
		MonthDay:  monthDay,
	}

	return domain.Schedule{
		Mode: domain.ScheduleModeRecurring,
		Rule: rule,
	}
}

// parseTimeOfDay extracts a time.Duration from an "at Xam", "at Xpm", or
// "at HH:MM" pattern in lower.  Returns 0 if no time is found.
func parseTimeOfDay(lower string) time.Duration {
	idx := strings.Index(lower, " at ")
	if idx < 0 {
		return 0
	}
	rest := strings.TrimSpace(lower[idx+4:])

	// Try "HH:MM".
	if colon := strings.Index(rest, ":"); colon > 0 {
		hStr := rest[:colon]
		mRest := rest[colon+1:]
		h, errH := strconv.Atoi(hStr)
		if errH == nil {
			// Extract minutes (digits only).
			mStr := strings.TrimFunc(mRest, func(r rune) bool { return !unicode.IsDigit(r) })
			if len(mStr) >= 2 {
				mStr = mStr[:2]
			}
			m, errM := strconv.Atoi(mStr)
			if errM == nil {
				return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute
			}
		}
	}

	// Try "Xam" / "Xpm".
	digits := strings.TrimFunc(rest, func(r rune) bool { return !unicode.IsDigit(r) })
	// Extract leading digits.
	numStr := ""
	for _, ch := range rest {
		if unicode.IsDigit(ch) {
			numStr += string(ch)
		} else {
			break
		}
	}
	if numStr == "" {
		_ = digits
		return 0
	}
	h, err := strconv.Atoi(numStr)
	if err != nil {
		return 0
	}
	after := strings.TrimSpace(rest[len(numStr):])
	if strings.HasPrefix(after, "pm") && h < 12 {
		h += 12
	}
	return time.Duration(h) * time.Hour
}

// extractMonthDay extracts the day number from patterns like "on the 15th",
// "on the 1st", "on the 2nd", "on the 3rd", "every 15th".  Returns 0 if none.
func extractMonthDay(lower string) int {
	// Patterns: "on the Nth", "every Nth"
	prefixes := []string{"on the ", "every "}
	for _, p := range prefixes {
		idx := strings.Index(lower, p)
		if idx < 0 {
			continue
		}
		rest := lower[idx+len(p):]
		numStr := ""
		for _, ch := range rest {
			if unicode.IsDigit(ch) {
				numStr += string(ch)
			} else {
				break
			}
		}
		if numStr == "" {
			continue
		}
		n, err := strconv.Atoi(numStr)
		if err != nil {
			continue
		}
		if n >= 1 && n <= 31 {
			// Confirm there's an ordinal suffix or nothing suspicious after.
			after := rest[len(numStr):]
			if strings.HasPrefix(after, "st") || strings.HasPrefix(after, "nd") ||
				strings.HasPrefix(after, "rd") || strings.HasPrefix(after, "th") ||
				after == "" || after[0] == ' ' {
				return n
			}
		}
	}
	return 0
}

// --- formatting helpers ------------------------------------------------------

func formatConfirmationPrompt(intent *ChatTaskIntent) string {
	scheduleDesc := intent.NLText
	if scheduleDesc == "" {
		scheduleDesc = string(intent.Schedule.Mode)
	}
	return fmt.Sprintf(
		"I'll create a task for %s:\n  Instruction: %s\n  Schedule: %s\n\nConfirm? (yes / no)",
		intent.BotName, intent.Instruction, scheduleDesc,
	)
}

func formatSuccess(botName string, task domain.DirectTask) string {
	if task.NextRunAt != nil {
		return fmt.Sprintf("Task created for %s — next run: %s.", botName, task.NextRunAt.Format(time.RFC3339))
	}
	return fmt.Sprintf("Task created for %s — next run: ASAP.", botName)
}
