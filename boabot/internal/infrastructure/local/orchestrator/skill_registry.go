package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// LocalSkillRegistry stores skills on the local filesystem under baseDir.
// Skills are stored as files in baseDir/<botType>/<skillID>/
// with a metadata.json file and the skill content files.
type LocalSkillRegistry struct {
	mu      sync.RWMutex
	baseDir string
	skills  map[string]domain.Skill
}

// NewLocalSkillRegistry creates a LocalSkillRegistry backed by baseDir.
func NewLocalSkillRegistry(baseDir string) (*LocalSkillRegistry, error) {
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("skill registry: create base dir: %w", err)
	}
	r := &LocalSkillRegistry{
		baseDir: baseDir,
		skills:  make(map[string]domain.Skill),
	}
	r.loadFromDisk()
	return r, nil
}

func (r *LocalSkillRegistry) loadFromDisk() {
	// Walk baseDir for metadata.json files.
	_ = filepath.Walk(r.baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.Name() != "metadata.json" {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		var s domain.Skill
		if json.Unmarshal(data, &s) == nil {
			r.skills[s.ID] = s
		}
		return nil
	})
}

func (r *LocalSkillRegistry) saveMeta(s domain.Skill) {
	dir := filepath.Join(r.baseDir, s.BotType, s.ID)
	_ = os.MkdirAll(dir, 0o755)
	data, _ := json.Marshal(s)
	_ = os.WriteFile(filepath.Join(dir, "metadata.json"), data, 0o644)
}

// Stage stores skill files and creates a staged Skill record.
// files is a map of relative path → content.
func (r *LocalSkillRegistry) Stage(_ context.Context, name, botType string, files map[string][]byte) (domain.Skill, error) {
	id, err := newID()
	if err != nil {
		return domain.Skill{}, err
	}
	dir := filepath.Join(r.baseDir, botType, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return domain.Skill{}, fmt.Errorf("skill stage: mkdir: %w", err)
	}
	for relPath, content := range files {
		dest := filepath.Join(dir, relPath)
		if mkErr := os.MkdirAll(filepath.Dir(dest), 0o755); mkErr != nil {
			return domain.Skill{}, fmt.Errorf("skill stage: mkdir parent: %w", mkErr)
		}
		if writeErr := os.WriteFile(dest, content, 0o644); writeErr != nil {
			return domain.Skill{}, fmt.Errorf("skill stage: write file: %w", writeErr)
		}
	}
	s := domain.Skill{
		ID:         id,
		Name:       name,
		BotType:    botType,
		Status:     domain.SkillStatusStaged,
		UploadedAt: time.Now().UTC(),
	}
	r.mu.Lock()
	r.skills[id] = s
	r.saveMeta(s)
	r.mu.Unlock()
	return s, nil
}

// List returns skills filtered by botType (empty = all) and status (empty = all).
func (r *LocalSkillRegistry) List(_ context.Context, botType string, status domain.SkillStatus) ([]domain.Skill, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []domain.Skill
	for _, s := range r.skills {
		if botType != "" && s.BotType != botType {
			continue
		}
		if status != "" && s.Status != status {
			continue
		}
		result = append(result, s)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].UploadedAt.After(result[j].UploadedAt)
	})
	return result, nil
}

// Get returns a skill by ID.
func (r *LocalSkillRegistry) Get(_ context.Context, id string) (domain.Skill, error) {
	r.mu.RLock()
	s, ok := r.skills[id]
	r.mu.RUnlock()
	if !ok {
		return domain.Skill{}, ErrSkillNotFound
	}
	return s, nil
}

// Approve marks a staged skill as active.
func (r *LocalSkillRegistry) Approve(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.skills[id]
	if !ok {
		return ErrSkillNotFound
	}
	s.Status = domain.SkillStatusActive
	s.ApprovedAt = time.Now().UTC()
	r.skills[id] = s
	r.saveMeta(s)
	return nil
}

// Reject removes a staged skill.
func (r *LocalSkillRegistry) Reject(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.skills[id]
	if !ok {
		return ErrSkillNotFound
	}
	delete(r.skills, id)
	_ = os.RemoveAll(filepath.Join(r.baseDir, s.BotType, id))
	return nil
}

// Revoke removes an active skill.
func (r *LocalSkillRegistry) Revoke(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.skills[id]
	if !ok {
		return ErrSkillNotFound
	}
	delete(r.skills, id)
	_ = os.RemoveAll(filepath.Join(r.baseDir, s.BotType, id))
	return nil
}
