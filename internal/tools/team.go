package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/qiangli/ycode/internal/runtime/team"
)

// RegisterTeamHandlers registers Team and Cron tool handlers.
func RegisterTeamHandlers(r *Registry, teamReg *team.Registry, cronReg *team.CronRegistry) {
	registerTeamCreate(r, teamReg)
	registerTeamDelete(r, teamReg)
	registerCronCreate(r, cronReg)
	registerCronDelete(r, cronReg)
	registerCronList(r, cronReg)
}

func registerTeamCreate(r *Registry, teamReg *team.Registry) {
	spec, ok := r.Get("TeamCreate")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse TeamCreate input: %w", err)
		}
		t := teamReg.Create(uuid.New().String(), params.Name)
		data, _ := json.Marshal(t)
		return string(data), nil
	}
}

func registerTeamDelete(r *Registry, teamReg *team.Registry) {
	spec, ok := r.Get("TeamDelete")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse TeamDelete input: %w", err)
		}
		if err := teamReg.Delete(params.ID); err != nil {
			return "", err
		}
		return fmt.Sprintf("Team %s deleted", params.ID), nil
	}
}

func registerCronCreate(r *Registry, cronReg *team.CronRegistry) {
	spec, ok := r.Get("CronCreate")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			Name     string `json:"name"`
			Schedule string `json:"schedule"`
			Command  string `json:"command"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse CronCreate input: %w", err)
		}
		entry, err := cronReg.Create(uuid.New().String(), params.Name, params.Schedule, params.Command)
		if err != nil {
			return "", err
		}
		data, _ := json.Marshal(entry)
		return string(data), nil
	}
}

func registerCronDelete(r *Registry, cronReg *team.CronRegistry) {
	spec, ok := r.Get("CronDelete")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse CronDelete input: %w", err)
		}
		if err := cronReg.Delete(params.ID); err != nil {
			return "", err
		}
		return fmt.Sprintf("Cron %s deleted", params.ID), nil
	}
}

func registerCronList(r *Registry, cronReg *team.CronRegistry) {
	spec, ok := r.Get("CronList")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		entries := cronReg.List()
		data, _ := json.Marshal(entries)
		return string(data), nil
	}
}
