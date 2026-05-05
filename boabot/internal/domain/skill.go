package domain

import (
	"context"
	"time"
)

// Skill is a modular capability package (SKILL.md + optional scripts) stored in S3.
type Skill struct {
	ID         string
	Name       string
	Summary    string
	BotType    string
	Status     SkillStatus
	UploadedAt time.Time
	ApprovedAt time.Time
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
	Approve(ctx context.Context, id string) error
	Reject(ctx context.Context, id string) error
	Revoke(ctx context.Context, id string) error
}
