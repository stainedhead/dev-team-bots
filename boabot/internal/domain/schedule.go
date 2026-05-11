package domain

import (
	"errors"
	"time"
)

// ScheduleMode distinguishes how a task's execution time is determined.
type ScheduleMode string

const (
	ScheduleModeASAP      ScheduleMode = "asap"
	ScheduleModeFuture    ScheduleMode = "future"
	ScheduleModeRecurring ScheduleMode = "recurring"
)

// RecurrenceFrequency is the base cadence of a recurring rule.
type RecurrenceFrequency string

const (
	RecurrenceFrequencyDaily   RecurrenceFrequency = "daily"
	RecurrenceFrequencyWeekly  RecurrenceFrequency = "weekly"
	RecurrenceFrequencyMonthly RecurrenceFrequency = "monthly"
)

// RecurrenceRule describes a repeating schedule.
// DaysMask is a bitmask where bit 0 = Sunday, bit 1 = Monday, …, bit 6 = Saturday.
// TimeOfDay is the offset from midnight on a fire day.
// MonthDay is the day-of-month for monthly rules (1–31); 0 means not set.
type RecurrenceRule struct {
	Frequency RecurrenceFrequency
	DaysMask  uint8
	TimeOfDay time.Duration
	MonthDay  int
}

// NextAfter returns the next fire time strictly after t.
// For weekly rules with an empty DaysMask it returns t unchanged — callers must
// validate the rule before calling NextAfter.
func (r RecurrenceRule) NextAfter(t time.Time) time.Time {
	switch r.Frequency {
	case RecurrenceFrequencyDaily:
		return r.nextAfterDaily(t)
	case RecurrenceFrequencyWeekly:
		return r.nextAfterWeekly(t)
	case RecurrenceFrequencyMonthly:
		return r.nextAfterMonthly(t)
	default:
		return r.nextAfterDaily(t)
	}
}

func midnight(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func (r RecurrenceRule) nextAfterDaily(t time.Time) time.Time {
	candidate := midnight(t).Add(r.TimeOfDay)
	if candidate.After(t) {
		return candidate
	}
	return midnight(t.AddDate(0, 0, 1)).Add(r.TimeOfDay)
}

func (r RecurrenceRule) nextAfterWeekly(t time.Time) time.Time {
	if r.DaysMask == 0 {
		return t
	}
	// Check today first, then the next 6 days — 7 candidates covers a full week.
	for i := 0; i <= 6; i++ {
		candidate := midnight(t.AddDate(0, 0, i)).Add(r.TimeOfDay)
		wd := candidate.Weekday() // time.Weekday: Sunday=0 … Saturday=6
		if r.DaysMask&(1<<uint(wd)) != 0 && candidate.After(t) {
			return candidate
		}
	}
	// No match in this week window — advance by 7 days and try again once.
	for i := 1; i <= 7; i++ {
		candidate := midnight(t.AddDate(0, 0, i)).Add(r.TimeOfDay)
		wd := candidate.Weekday()
		if r.DaysMask&(1<<uint(wd)) != 0 {
			return candidate
		}
	}
	return t
}

func (r RecurrenceRule) nextAfterMonthly(t time.Time) time.Time {
	// Try same month
	candidate := time.Date(t.Year(), t.Month(), r.MonthDay, 0, 0, 0, 0, t.Location()).Add(r.TimeOfDay)
	if candidate.After(t) {
		return candidate
	}
	// Next month (time.Date normalises overflow, e.g. Feb 31 → Mar 3)
	return time.Date(t.Year(), t.Month()+1, r.MonthDay, 0, 0, 0, 0, t.Location()).Add(r.TimeOfDay)
}

// Validate returns an error if the rule cannot produce any future runs.
func (r RecurrenceRule) Validate() error {
	switch r.Frequency {
	case RecurrenceFrequencyDaily:
		// daily rules are always valid
	case RecurrenceFrequencyWeekly:
		if r.DaysMask == 0 {
			return errors.New("schedule: weekly rule must have at least one day set in DaysMask")
		}
	case RecurrenceFrequencyMonthly:
		if r.MonthDay < 1 || r.MonthDay > 31 {
			return errors.New("schedule: monthly rule must have MonthDay in range 1–31")
		}
	default:
		return errors.New("schedule: unknown frequency (want daily, weekly, or monthly)")
	}
	return nil
}

// Schedule determines when a task should run.
type Schedule struct {
	Mode  ScheduleMode    `json:"mode"`
	RunAt *time.Time      `json:"run_at,omitempty"`
	Rule  *RecurrenceRule `json:"rule,omitempty"`
}

// NextRunAt returns the next scheduled execution time given now.
// Returns nil for ASAP mode. Returns RunAt for Future mode. Delegates to
// Rule.NextAfter for Recurring mode.
func (s Schedule) NextRunAt(now time.Time) *time.Time {
	switch s.Mode {
	case ScheduleModeFuture:
		return s.RunAt
	case ScheduleModeRecurring:
		if s.Rule == nil {
			return nil
		}
		next := s.Rule.NextAfter(now)
		return &next
	default:
		return nil
	}
}

// Validate returns an error if the schedule is internally inconsistent.
func (s Schedule) Validate() error {
	switch s.Mode {
	case ScheduleModeFuture:
		if s.RunAt == nil {
			return errors.New("schedule: Future mode requires RunAt to be set")
		}
	case ScheduleModeRecurring:
		if s.Rule == nil {
			return errors.New("schedule: Recurring mode requires Rule to be set")
		}
		if err := s.Rule.Validate(); err != nil {
			return err
		}
	}
	return nil
}
