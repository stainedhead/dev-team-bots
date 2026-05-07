package domain

import (
	"context"
	"time"
)

// Skill is a modular capability package (SKILL.md + optional scripts) stored in S3.
type Skill struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Summary    string      `json:"summary,omitempty"`
	BotType    string      `json:"bot_type"`
	Status     SkillStatus `json:"status"`
	UploadedAt time.Time   `json:"uploaded_at"`
	ApprovedAt time.Time   `json:"approved_at,omitempty"`
}

type SkillStatus string

const (
	SkillStatusStaged SkillStatus = "staged" // uploaded, pending Admin approval
	SkillStatusActive SkillStatus = "active" // approved and available to the bot
)

// SkillRegistry lists and manages the lifecycle of skills for a bot.
type SkillRegistry interface {
	List(ctx context.Context, botType string, status SkillStatus) ([]Skill, error)
	Get(ctx context.Context, id string) (Skill, error)
	// Stage stores skill files and creates a staged Skill record.
	// files is a map of relative path → content.
	Stage(ctx context.Context, name, botType string, files map[string][]byte) (Skill, error)
	Approve(ctx context.Context, id string) error
	Reject(ctx context.Context, id string) error
	Revoke(ctx context.Context, id string) error
}
