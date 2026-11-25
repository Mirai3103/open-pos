package features

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/Mirai3103/open-pos/internal/platform/command"
	"github.com/Mirai3103/open-pos/internal/platform/database/sqlc"
	"github.com/Mirai3103/open-pos/internal/response"
	"github.com/google/uuid"
)

// SetFeatureCommand turns one Store Feature on or off. Key comes from the route.
type SetFeatureCommand struct {
	RequestID uuid.UUID `json:"request_id"`
	Key       string    `json:"-"`
	Enabled   *bool     `json:"enabled"`
}

// FeatureResponse is one Store Feature's state.
type FeatureResponse struct {
	Key     string `json:"key"`
	Enabled bool   `json:"enabled"`
}

// SetFeatureHandler changes a Store Feature.
type SetFeatureHandler struct{ runner *command.Runner }

// NewSetFeatureHandler creates a SetFeatureHandler.
func NewSetFeatureHandler(runner *command.Runner) *SetFeatureHandler {
	return &SetFeatureHandler{runner: runner}
}

// Handle sets the feature. Setting the current value again is a no-op that
// writes no change row and no audit event.
func (h *SetFeatureHandler) Handle(ctx context.Context, actor command.Actor, cmd SetFeatureCommand,
) (int, FeatureResponse, error) {
	if cmd.Enabled == nil {
		return 0, FeatureResponse{}, fmt.Errorf("%w: enabled is required", response.ErrInvalid)
	}
	enabled := *cmd.Enabled
	spec := command.MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpSetFeature,
		Fingerprint: map[string]any{"key": cmd.Key, "enabled": enabled},
		Required:    []string{CapAdminister},
	}
	return command.ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc command.MutationContext) (int, FeatureResponse, command.AuditRecord, error) {
			var zero FeatureResponse
			row, err := mc.Queries.LockStoreFeature(ctx, cmd.Key)
			if errors.Is(err, sql.ErrNoRows) {
				return 0, zero, command.AuditRecord{}, fmt.Errorf("%w: %s", ErrUnknownFeature, cmd.Key)
			}
			if err != nil {
				return 0, zero, command.AuditRecord{}, fmt.Errorf("lock store feature: %w", err)
			}
			out := FeatureResponse{Key: row.Key, Enabled: enabled}
			if row.Enabled == enabled {
				return http.StatusOK, out, command.AuditRecord{}, nil
			}
			if err := mc.Queries.SetStoreFeatureEnabled(ctx, sqlc.SetStoreFeatureEnabledParams{
				Enabled: enabled, ActorID: mc.Actor.StaffID, Key: cmd.Key,
			}); err != nil {
				return 0, zero, command.AuditRecord{}, fmt.Errorf("set store feature: %w", err)
			}
			if err := mc.Queries.InsertStoreFeatureChange(ctx, sqlc.InsertStoreFeatureChangeParams{
				Key: cmd.Key, Enabled: enabled, ActorStaffIdentityID: mc.Actor.StaffID,
			}); err != nil {
				return 0, zero, command.AuditRecord{}, fmt.Errorf("insert store feature change: %w", err)
			}
			event := EventFeatureDisabled
			if enabled {
				event = EventFeatureEnabled
			}
			return http.StatusOK, out, command.AuditRecord{
				EventType: event,
				Details:   map[string]any{"key": cmd.Key, "enabled": enabled},
			}, nil
		})
}

// ListFeaturesHandler reads every Store Feature. Any authenticated staff
// member may read them, so clients can show or hide extension UI.
type ListFeaturesHandler struct{ runner *command.Runner }

// NewListFeaturesHandler creates a ListFeaturesHandler.
func NewListFeaturesHandler(runner *command.Runner) *ListFeaturesHandler {
	return &ListFeaturesHandler{runner: runner}
}

// Handle returns key → enabled.
func (h *ListFeaturesHandler) Handle(ctx context.Context, actor command.Actor) (map[string]bool, error) {
	return command.ExecuteRead(ctx, h.runner, actor, command.ReadSpec{},
		func(rc command.ReadContext) (map[string]bool, error) {
			rows, err := rc.Queries.ListStoreFeatures(ctx)
			if err != nil {
				return nil, fmt.Errorf("list store features: %w", err)
			}
			out := make(map[string]bool, len(rows))
			for _, r := range rows {
				out[r.Key] = r.Enabled
			}
			return out, nil
		})
}
