package inventory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Mirai3103/open-pos/internal/platform/database/sqlc"
	"github.com/Mirai3103/open-pos/internal/platform/features"
	"github.com/Mirai3103/open-pos/internal/platform/hooks"
	"github.com/google/uuid"
)

// Subscribe registers Inventory's reactions to Core facts. Both subscribers
// honour the hooks contract: they return only database errors.
func Subscribe(bus *hooks.Bus) {
	bus.OnPreparationUnitsCreated(recordConsumptions)
	bus.OnPreparationUnitsCancelled(voidConsumptions)
}

// recordConsumptions writes one Consumption Record per new unit while the
// inventory Store Feature is on (spec §4.1, §4.6).
func recordConsumptions(ctx context.Context, q *sqlc.Queries, ev hooks.PreparationUnitsCreated) error {
	on, err := features.Enabled(ctx, q, FeatureKey)
	if err != nil {
		return err
	}
	if !on {
		return nil
	}
	book, err := LoadBook(ctx, q)
	if err != nil {
		return err
	}
	for _, unit := range ev.Units {
		if err := insertConsumption(ctx, q, book, unit, ev.At); err != nil {
			return err
		}
	}
	return nil
}

func insertConsumption(ctx context.Context, q *sqlc.Queries, book *Book, unit hooks.CreatedUnit, at time.Time) error {
	comp := book.Compose(Selection{MenuItemID: unit.MenuItemID, SizeID: unit.SizeID, ModifierOptionIDs: unit.ModifierOptionIDs})
	options := unit.ModifierOptionIDs
	if options == nil {
		options = []uuid.UUID{}
	}
	id, err := q.InventoryInsertConsumption(ctx, sqlc.InventoryInsertConsumptionParams{
		PreparationUnitID: unit.PreparationUnitID, MenuItemID: unit.MenuItemID,
		SizeID: nullUUID(unit.SizeID), ModifierOptionIds: options,
		CostStatus: comp.Status, CostTotalVnd: comp.TotalVND, RecordedAt: at,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil // already recorded: ON CONFLICT DO NOTHING
	}
	if err != nil {
		return fmt.Errorf("insert consumption: %w", err)
	}
	for _, l := range comp.Lines {
		params := sqlc.InventoryInsertConsumptionLineParams{
			ConsumptionID: id, IngredientID: l.IngredientID, IngredientName: l.Name,
			IngredientKind: l.Kind, StockUnitCode: l.StockUnit, Quantity: l.StockQty,
		}
		if l.UnitCost != nil {
			params.CostPerStockUnit = sql.NullFloat64{Float64: *l.UnitCost, Valid: true}
			params.LineCostVnd = sql.NullFloat64{Float64: *l.LineCost, Valid: true}
		}
		if err := q.InventoryInsertConsumptionLine(ctx, params); err != nil {
			return fmt.Errorf("insert consumption line: %w", err)
		}
	}
	return nil
}

// voidConsumptions voids cancelled units' records. It runs whether or not the
// feature is on: it corrects records already written (spec §4.6).
func voidConsumptions(ctx context.Context, q *sqlc.Queries, ev hooks.PreparationUnitsCancelled) error {
	if err := q.InventoryVoidConsumptions(ctx, sqlc.InventoryVoidConsumptionsParams{
		VoidedAt: ev.At, PreparationUnitIds: ev.PreparationUnitIDs,
	}); err != nil {
		return fmt.Errorf("void consumptions: %w", err)
	}
	return nil
}
