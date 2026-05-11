package domain_test

import (
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// helpers

func timeOnDay(t time.Time, h, m int) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), h, m, 0, 0, t.Location())
}

func tod(h, m int) time.Duration {
	return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute
}

// ---- RecurrenceRule.NextAfter — daily ----

func TestRecurrenceRule_NextAfter_Daily_SameDayWhenTimeOfDayInFuture(t *testing.T) {
	t.Parallel()
	rule := domain.RecurrenceRule{
		Frequency: domain.RecurrenceFrequencyDaily,
		TimeOfDay: tod(14, 0),
	}
	now := timeOnDay(time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC), 9, 0)
	got := rule.NextAfter(now)
	want := timeOnDay(now, 14, 0)
	if !got.Equal(want) {
		t.Errorf("NextAfter daily same-day: got %v, want %v", got, want)
	}
}

func TestRecurrenceRule_NextAfter_Daily_NextDayWhenTimeOfDayPast(t *testing.T) {
	t.Parallel()
	rule := domain.RecurrenceRule{
		Frequency: domain.RecurrenceFrequencyDaily,
		TimeOfDay: tod(8, 0),
	}
	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	got := rule.NextAfter(now)
	want := timeOnDay(now.AddDate(0, 0, 1), 8, 0)
	if !got.Equal(want) {
		t.Errorf("NextAfter daily next-day: got %v, want %v", got, want)
	}
}

func TestRecurrenceRule_NextAfter_Daily_ExactlyAtTimeOfDay_AdvancesToNextDay(t *testing.T) {
	t.Parallel()
	rule := domain.RecurrenceRule{
		Frequency: domain.RecurrenceFrequencyDaily,
		TimeOfDay: tod(10, 0),
	}
	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	got := rule.NextAfter(now)
	want := timeOnDay(now.AddDate(0, 0, 1), 10, 0)
	if !got.Equal(want) {
		t.Errorf("NextAfter daily exact: got %v, want %v", got, want)
	}
}

// ---- RecurrenceRule.NextAfter — weekly ----

func TestRecurrenceRule_NextAfter_Weekly_MonWed_ForwardToNextDay(t *testing.T) {
	t.Parallel()
	// bit1=Mon, bit3=Wed
	rule := domain.RecurrenceRule{
		Frequency: domain.RecurrenceFrequencyWeekly,
		DaysMask:  (1 << 1) | (1 << 3),
		TimeOfDay: tod(9, 0),
	}
	// 2026-05-10 is a Sunday
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	got := rule.NextAfter(now)
	// Next should be Monday 2026-05-11 at 09:00
	want := time.Date(2026, 5, 11, 9, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("NextAfter weekly Mon+Wed from Sun: got %v, want %v", got, want)
	}
}

func TestRecurrenceRule_NextAfter_Weekly_MonWed_FromMondayBeforeTime(t *testing.T) {
	t.Parallel()
	rule := domain.RecurrenceRule{
		Frequency: domain.RecurrenceFrequencyWeekly,
		DaysMask:  (1 << 1) | (1 << 3),
		TimeOfDay: tod(9, 0),
	}
	// Monday 2026-05-11, before fire time
	now := time.Date(2026, 5, 11, 8, 0, 0, 0, time.UTC)
	got := rule.NextAfter(now)
	want := time.Date(2026, 5, 11, 9, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("NextAfter weekly Mon+Wed from Mon early: got %v, want %v", got, want)
	}
}

func TestRecurrenceRule_NextAfter_Weekly_MonWed_FromMondayAfterTime(t *testing.T) {
	t.Parallel()
	rule := domain.RecurrenceRule{
		Frequency: domain.RecurrenceFrequencyWeekly,
		DaysMask:  (1 << 1) | (1 << 3),
		TimeOfDay: tod(9, 0),
	}
	// Monday 2026-05-11 after fire time → should move to Wednesday
	now := time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC)
	got := rule.NextAfter(now)
	want := time.Date(2026, 5, 13, 9, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("NextAfter weekly Mon+Wed from Mon late: got %v, want %v", got, want)
	}
}

func TestRecurrenceRule_NextAfter_Weekly_WrapAcrossWeek(t *testing.T) {
	t.Parallel()
	// Only Wednesday set
	rule := domain.RecurrenceRule{
		Frequency: domain.RecurrenceFrequencyWeekly,
		DaysMask:  1 << 3,
		TimeOfDay: tod(9, 0),
	}
	// Wednesday after fire time → next Wednesday
	now := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	got := rule.NextAfter(now)
	want := time.Date(2026, 5, 20, 9, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("NextAfter weekly wrap: got %v, want %v", got, want)
	}
}

func TestRecurrenceRule_NextAfter_Weekly_NoDaysSet_ReturnsUnchanged(t *testing.T) {
	t.Parallel()
	rule := domain.RecurrenceRule{
		Frequency: domain.RecurrenceFrequencyWeekly,
		DaysMask:  0,
		TimeOfDay: tod(9, 0),
	}
	now := time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC)
	got := rule.NextAfter(now)
	if !got.Equal(now) {
		t.Errorf("NextAfter weekly empty mask: got %v, want %v (unchanged)", got, now)
	}
}

// ---- RecurrenceRule.NextAfter — monthly ----

func TestRecurrenceRule_NextAfter_Monthly_SameMonthWhenDayInFuture(t *testing.T) {
	t.Parallel()
	rule := domain.RecurrenceRule{
		Frequency: domain.RecurrenceFrequencyMonthly,
		MonthDay:  15,
		TimeOfDay: tod(10, 0),
	}
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	got := rule.NextAfter(now)
	want := time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("NextAfter monthly same month: got %v, want %v", got, want)
	}
}

func TestRecurrenceRule_NextAfter_Monthly_NextMonthWhenDayPast(t *testing.T) {
	t.Parallel()
	rule := domain.RecurrenceRule{
		Frequency: domain.RecurrenceFrequencyMonthly,
		MonthDay:  5,
		TimeOfDay: tod(10, 0),
	}
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	got := rule.NextAfter(now)
	want := time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("NextAfter monthly next month: got %v, want %v", got, want)
	}
}

func TestRecurrenceRule_NextAfter_Monthly_Day31_February_ClampsToNextMonth(t *testing.T) {
	t.Parallel()
	rule := domain.RecurrenceRule{
		Frequency: domain.RecurrenceFrequencyMonthly,
		MonthDay:  31,
		TimeOfDay: tod(10, 0),
	}
	// January 15 → should fire Jan 31
	now := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	got := rule.NextAfter(now)
	want := time.Date(2026, 1, 31, 10, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("NextAfter monthly day31 from Jan: got %v, want %v", got, want)
	}

	// Jan 31 after fire time → next attempt is Feb 31 which overflows into Mar 3 (2026 non-leap)
	now2 := time.Date(2026, 1, 31, 11, 0, 0, 0, time.UTC)
	got2 := rule.NextAfter(now2)
	// time.Date normalises Feb 31 → Mar 3
	want2 := time.Date(2026, 3, 3, 10, 0, 0, 0, time.UTC)
	if !got2.Equal(want2) {
		t.Errorf("NextAfter monthly day31 overflow Feb: got %v, want %v", got2, want2)
	}
}

// ---- RecurrenceRule.Validate ----

func TestRecurrenceRule_Validate_WeeklyNoDays_ReturnsError(t *testing.T) {
	t.Parallel()
	rule := domain.RecurrenceRule{
		Frequency: domain.RecurrenceFrequencyWeekly,
		DaysMask:  0,
	}
	if err := rule.Validate(); err == nil {
		t.Error("expected error for weekly with DaysMask==0, got nil")
	}
}

func TestRecurrenceRule_Validate_MonthlyNoDay_ReturnsError(t *testing.T) {
	t.Parallel()
	rule := domain.RecurrenceRule{
		Frequency: domain.RecurrenceFrequencyMonthly,
		MonthDay:  0,
	}
	if err := rule.Validate(); err == nil {
		t.Error("expected error for monthly with MonthDay==0, got nil")
	}
}

func TestRecurrenceRule_Validate_MonthlyValidDay_NoError(t *testing.T) {
	t.Parallel()
	rule := domain.RecurrenceRule{
		Frequency: domain.RecurrenceFrequencyMonthly,
		MonthDay:  15,
	}
	if err := rule.Validate(); err != nil {
		t.Errorf("unexpected error for valid monthly rule: %v", err)
	}
}

func TestRecurrenceRule_Validate_WeeklyWithDays_NoError(t *testing.T) {
	t.Parallel()
	rule := domain.RecurrenceRule{
		Frequency: domain.RecurrenceFrequencyWeekly,
		DaysMask:  1 << 1,
	}
	if err := rule.Validate(); err != nil {
		t.Errorf("unexpected error for valid weekly rule: %v", err)
	}
}

func TestRecurrenceRule_Validate_DailyAlwaysValid(t *testing.T) {
	t.Parallel()
	rule := domain.RecurrenceRule{
		Frequency: domain.RecurrenceFrequencyDaily,
	}
	if err := rule.Validate(); err != nil {
		t.Errorf("unexpected error for daily rule: %v", err)
	}
}

// ---- Schedule.Validate ----

func TestSchedule_Validate_ASAPAlwaysValid(t *testing.T) {
	t.Parallel()
	s := domain.Schedule{Mode: domain.ScheduleModeASAP}
	if err := s.Validate(); err != nil {
		t.Errorf("unexpected error for ASAP schedule: %v", err)
	}
}

func TestSchedule_Validate_FutureWithNilRunAt_ReturnsError(t *testing.T) {
	t.Parallel()
	s := domain.Schedule{Mode: domain.ScheduleModeFuture, RunAt: nil}
	if err := s.Validate(); err == nil {
		t.Error("expected error for Future with nil RunAt, got nil")
	}
}

func TestSchedule_Validate_FutureWithRunAt_NoError(t *testing.T) {
	t.Parallel()
	at := time.Now().Add(time.Hour)
	s := domain.Schedule{Mode: domain.ScheduleModeFuture, RunAt: &at}
	if err := s.Validate(); err != nil {
		t.Errorf("unexpected error for Future with RunAt set: %v", err)
	}
}

func TestSchedule_Validate_RecurringWithNilRule_ReturnsError(t *testing.T) {
	t.Parallel()
	s := domain.Schedule{Mode: domain.ScheduleModeRecurring, Rule: nil}
	if err := s.Validate(); err == nil {
		t.Error("expected error for Recurring with nil Rule, got nil")
	}
}

func TestSchedule_Validate_RecurringWithInvalidRule_ReturnsError(t *testing.T) {
	t.Parallel()
	rule := domain.RecurrenceRule{
		Frequency: domain.RecurrenceFrequencyWeekly,
		DaysMask:  0,
	}
	s := domain.Schedule{Mode: domain.ScheduleModeRecurring, Rule: &rule}
	if err := s.Validate(); err == nil {
		t.Error("expected error for Recurring with invalid rule, got nil")
	}
}

func TestSchedule_Validate_RecurringWithValidRule_NoError(t *testing.T) {
	t.Parallel()
	rule := domain.RecurrenceRule{
		Frequency: domain.RecurrenceFrequencyDaily,
		TimeOfDay: tod(9, 0),
	}
	s := domain.Schedule{Mode: domain.ScheduleModeRecurring, Rule: &rule}
	if err := s.Validate(); err != nil {
		t.Errorf("unexpected error for valid Recurring schedule: %v", err)
	}
}

// FR-003: RecurrenceRule.Validate frequency check.

func TestRecurrenceRule_Validate_EmptyFrequency_ReturnsError(t *testing.T) {
	t.Parallel()
	rule := domain.RecurrenceRule{
		Frequency: "",
	}
	if err := rule.Validate(); err == nil {
		t.Error("expected error for empty frequency, got nil")
	}
}

func TestRecurrenceRule_Validate_UnknownFrequency_ReturnsError(t *testing.T) {
	t.Parallel()
	rule := domain.RecurrenceRule{
		Frequency: "hourly",
	}
	if err := rule.Validate(); err == nil {
		t.Error("expected error for unknown frequency 'hourly', got nil")
	}
}

// FR-014: MonthDay range validation.

func TestRecurrenceRule_Validate_MonthDay32_ReturnsError(t *testing.T) {
	t.Parallel()
	rule := domain.RecurrenceRule{
		Frequency: domain.RecurrenceFrequencyMonthly,
		MonthDay:  32,
	}
	if err := rule.Validate(); err == nil {
		t.Error("expected error for MonthDay=32, got nil")
	}
}

func TestRecurrenceRule_Validate_MonthDayNegative_ReturnsError(t *testing.T) {
	t.Parallel()
	rule := domain.RecurrenceRule{
		Frequency: domain.RecurrenceFrequencyMonthly,
		MonthDay:  -1,
	}
	if err := rule.Validate(); err == nil {
		t.Error("expected error for MonthDay=-1, got nil")
	}
}

func TestRecurrenceRule_Validate_MonthDay31_NoError(t *testing.T) {
	t.Parallel()
	rule := domain.RecurrenceRule{
		Frequency: domain.RecurrenceFrequencyMonthly,
		MonthDay:  31,
	}
	if err := rule.Validate(); err != nil {
		t.Errorf("unexpected error for MonthDay=31: %v", err)
	}
}

// ---- Schedule.NextRunAt ----

func TestSchedule_NextRunAt_ASAP_ReturnsNil(t *testing.T) {
	t.Parallel()
	s := domain.Schedule{Mode: domain.ScheduleModeASAP}
	if got := s.NextRunAt(time.Now()); got != nil {
		t.Errorf("expected nil for ASAP, got %v", got)
	}
}

func TestSchedule_NextRunAt_Future_ReturnsRunAt(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	s := domain.Schedule{Mode: domain.ScheduleModeFuture, RunAt: &at}
	got := s.NextRunAt(time.Now())
	if got == nil || !got.Equal(at) {
		t.Errorf("expected RunAt=%v, got %v", at, got)
	}
}

func TestSchedule_NextRunAt_Recurring_DelegatesToRule(t *testing.T) {
	t.Parallel()
	rule := domain.RecurrenceRule{
		Frequency: domain.RecurrenceFrequencyDaily,
		TimeOfDay: tod(14, 0),
	}
	s := domain.Schedule{Mode: domain.ScheduleModeRecurring, Rule: &rule}
	now := time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC)
	got := s.NextRunAt(now)
	want := rule.NextAfter(now)
	if got == nil || !got.Equal(want) {
		t.Errorf("NextRunAt recurring: got %v, want %v", got, want)
	}
}
