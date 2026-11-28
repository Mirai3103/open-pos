package hooks

import (
	"context"
	"fmt"
	"time"

	"github.com/Mirai3103/open-pos/internal/platform/database/sqlc"
	"github.com/google/uuid"
)

// UnitRef names a freshly inserted Preparation Unit and its Order Item.
type UnitRef struct {
	PreparationUnitID uuid.UUID
	OrderItemID       uuid.UUID
}

// PublishCreatedUnits resolves refs to the catalog identities their Order
// Items were sold as and publishes PreparationUnitsCreated. It reads nothing
// when no subscriber listens, so a build without extensions pays nothing.
func (b *Bus) PublishCreatedUnits(ctx context.Context, q *sqlc.Queries, origin string,
	refs []UnitRef, at time.Time,
) error {
	if !b.hasCreatedSubscribers() || len(refs) == 0 {
		return nil
	}
	seen := make(map[uuid.UUID]struct{}, len(refs))
	ids := make([]uuid.UUID, 0, len(refs))
	for _, ref := range refs {
		if _, ok := seen[ref.OrderItemID]; !ok {
			seen[ref.OrderItemID] = struct{}{}
			ids = append(ids, ref.OrderItemID)
		}
	}
	rows, err := q.ListPreparationUnitSources(ctx, ids)
	if err != nil {
		return fmt.Errorf("list preparation unit sources: %w", err)
	}
	sources := groupUnitSources(rows)

	units := make([]CreatedUnit, 0, len(refs))
	for _, ref := range refs {
		src, ok := sources[ref.OrderItemID]
		if !ok {
			return fmt.Errorf("preparation unit source for order item %s not found", ref.OrderItemID)
		}
		units = append(units, CreatedUnit{
			PreparationUnitID: ref.PreparationUnitID,
			MenuItemID:        src.menuItemID,
			SizeID:            src.sizeID,
			ModifierOptionIDs: append([]uuid.UUID{}, src.optionIDs...),
		})
	}
	return b.PublishPreparationUnitsCreated(ctx, q, PreparationUnitsCreated{
		Origin: origin, Units: units, At: at,
	})
}

type unitSource struct {
	menuItemID uuid.UUID
	sizeID     *uuid.UUID
	optionIDs  []uuid.UUID
}

func groupUnitSources(rows []sqlc.ListPreparationUnitSourcesRow) map[uuid.UUID]unitSource {
	out := make(map[uuid.UUID]unitSource, len(rows))
	for _, row := range rows {
		src, ok := out[row.OrderItemID]
		if !ok {
			src = unitSource{menuItemID: row.MenuItemID, optionIDs: []uuid.UUID{}}
			if row.SizeID.Valid {
				id := row.SizeID.UUID
				src.sizeID = &id
			}
		}
		if row.ModifierOptionID.Valid {
			src.optionIDs = append(src.optionIDs, row.ModifierOptionID.UUID)
		}
		out[row.OrderItemID] = src
	}
	return out
}
