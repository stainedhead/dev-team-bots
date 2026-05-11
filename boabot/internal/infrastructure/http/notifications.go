package httpserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	appnotifications "github.com/stainedhead/dev-team-bots/boabot/internal/application/notifications"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// ── Notification handlers ─────────────────────────────────────────────────────

func (s *Server) handleNotificationList(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Notifications == nil {
		writeError(w, http.StatusNotImplemented, "notifications not available")
		return
	}
	filter := domain.AgentNotificationFilter{
		BotName: r.URL.Query().Get("bot"),
		Status:  domain.AgentNotificationStatus(r.URL.Query().Get("status")),
		Search:  r.URL.Query().Get("search"),
	}
	notifs, err := s.cfg.Notifications.List(r.Context(), filter)
	if err != nil {
		writeInternalError(w, "notification list", err)
		return
	}
	if notifs == nil {
		notifs = []domain.AgentNotification{}
	}
	writeJSON(w, http.StatusOK, notifs)
}

func (s *Server) handleNotificationCount(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Notifications == nil {
		writeError(w, http.StatusNotImplemented, "notifications not available")
		return
	}
	count, err := s.cfg.Notifications.UnreadCount(r.Context())
	if err != nil {
		writeInternalError(w, "notification count", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"unread": count})
}

func (s *Server) handleNotificationDiscuss(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Notifications == nil {
		writeError(w, http.StatusNotImplemented, "notifications not available")
		return
	}
	id := r.PathValue("id")
	var req struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Message == "" {
		writeError(w, http.StatusBadRequest, "message must not be empty")
		return
	}
	if err := s.cfg.Notifications.AppendDiscuss(r.Context(), id, "user", req.Message); err != nil {
		writeInternalError(w, "notification discuss", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleNotificationRequeue(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Notifications == nil {
		writeError(w, http.StatusNotImplemented, "notifications not available")
		return
	}
	id := r.PathValue("id")
	if err := s.cfg.Notifications.RequeueTask(r.Context(), id); err != nil {
		if errors.Is(err, appnotifications.ErrRequeueConflict) {
			writeError(w, http.StatusConflict, "task is currently running")
			return
		}
		writeInternalError(w, "notification requeue", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleNotificationDelete(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Notifications == nil {
		writeError(w, http.StatusNotImplemented, "notifications not available")
		return
	}
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.cfg.Notifications.Delete(r.Context(), req.IDs); err != nil {
		writeInternalError(w, "notification delete", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ── Schedule parsing helpers ──────────────────────────────────────────────────

// scheduleRequest is the JSON shape accepted in POST /api/v1/bots/:bot/tasks.
type scheduleRequest struct {
	Mode       string             `json:"mode"`
	RunAt      *time.Time         `json:"run_at,omitempty"`
	Recurrence *recurrenceRequest `json:"recurrence,omitempty"`
}

// recurrenceRequest is the JSON shape for the recurrence sub-object.
type recurrenceRequest struct {
	Frequency string   `json:"frequency"`
	Days      []string `json:"days,omitempty"`
	Time      string   `json:"time,omitempty"` // "HH:MM"
	MonthDay  int      `json:"month_day,omitempty"`
}

// dayNameToWeekday maps lowercase day names to time.Weekday values.
var dayNameToWeekday = map[string]time.Weekday{
	"sunday":    time.Sunday,
	"monday":    time.Monday,
	"tuesday":   time.Tuesday,
	"wednesday": time.Wednesday,
	"thursday":  time.Thursday,
	"friday":    time.Friday,
	"saturday":  time.Saturday,
}

// parseScheduleRequest converts a scheduleRequest into a domain.Schedule.
// Returns an error if the mode or recurrence fields are invalid.
func parseScheduleRequest(req *scheduleRequest) (domain.Schedule, error) {
	if req == nil || req.Mode == "" {
		return domain.Schedule{Mode: domain.ScheduleModeASAP}, nil
	}

	switch domain.ScheduleMode(req.Mode) {
	case domain.ScheduleModeASAP:
		return domain.Schedule{Mode: domain.ScheduleModeASAP}, nil

	case domain.ScheduleModeFuture:
		s := domain.Schedule{Mode: domain.ScheduleModeFuture, RunAt: req.RunAt}
		if err := s.Validate(); err != nil {
			return domain.Schedule{}, err
		}
		return s, nil

	case domain.ScheduleModeRecurring:
		if req.Recurrence == nil {
			return domain.Schedule{}, errors.New("schedule: recurring mode requires recurrence object")
		}
		rule, err := parseRecurrenceRequest(req.Recurrence)
		if err != nil {
			return domain.Schedule{}, err
		}
		s := domain.Schedule{Mode: domain.ScheduleModeRecurring, Rule: &rule}
		if err := s.Validate(); err != nil {
			return domain.Schedule{}, err
		}
		return s, nil

	default:
		return domain.Schedule{}, fmt.Errorf("schedule: unknown mode %q (want asap, future, or recurring)", req.Mode)
	}
}

// parseRecurrenceRequest converts a recurrenceRequest into a domain.RecurrenceRule.
func parseRecurrenceRequest(req *recurrenceRequest) (domain.RecurrenceRule, error) {
	rule := domain.RecurrenceRule{
		Frequency: domain.RecurrenceFrequency(req.Frequency),
		MonthDay:  req.MonthDay,
	}

	// Parse DaysMask from day names.
	for _, day := range req.Days {
		wd, ok := dayNameToWeekday[strings.ToLower(day)]
		if !ok {
			return domain.RecurrenceRule{}, fmt.Errorf("schedule: unknown day name %q", day)
		}
		rule.DaysMask |= 1 << uint(wd)
	}

	// Parse TimeOfDay from "HH:MM".
	if req.Time != "" {
		var h, m int
		if _, err := fmt.Sscanf(req.Time, "%d:%d", &h, &m); err != nil {
			return domain.RecurrenceRule{}, fmt.Errorf("schedule: invalid time format %q (want HH:MM)", req.Time)
		}
		rule.TimeOfDay = time.Duration(h)*time.Hour + time.Duration(m)*time.Minute
	}

	return rule, nil
}
