package inventory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/Mirai3103/open-pos/internal/platform/database/sqlc"
	"github.com/google/uuid"
)

// Commands holds every Inventory mutation.
type Commands struct{ runner *Runner }

// NewCommands creates Commands on runner.
func NewCommands(runner *Runner) *Commands { return &Commands{runner: runner} }

func toIngredientResponse(row sqlc.InventoryIngredient, units []sqlc.InventoryIngredientUnit) IngredientResponse {
	out := IngredientResponse{
		ID: row.ID, Name: row.Name, Kind: row.Kind, StockUnit: row.StockUnitCode,
		Retired: row.RetiredAt.Valid, Units: make([]IngredientUnitResponse, 0, len(units)),
	}
	for _, u := range units {
		out.Units = append(out.Units, IngredientUnitResponse{ID: u.ID, Name: u.Name, StockQuantity: u.StockQuantity})
	}
	return out
}

func lockIngredient(ctx context.Context, q *sqlc.Queries, id uuid.UUID) (sqlc.InventoryIngredient, error) {
	row, err := q.InventoryLockIngredient(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return row, fmt.Errorf("%w: %s", ErrIngredientNotFound, id)
	}
	if err != nil {
		return row, fmt.Errorf("lock ingredient: %w", err)
	}
	return row, nil
}

func ingredientWithUnits(ctx context.Context, q *sqlc.Queries, row sqlc.InventoryIngredient) (IngredientResponse, error) {
	units, err := q.InventoryListIngredientUnits(ctx, []uuid.UUID{row.ID})
	if err != nil {
		return IngredientResponse{}, fmt.Errorf("list ingredient units: %w", err)
	}
	return toIngredientResponse(row, units), nil
}

// CreateIngredient creates a raw or prepared Ingredient.
func (c *Commands) CreateIngredient(ctx context.Context, actor Actor, cmd CreateIngredientCommand,
) (int, IngredientResponse, error) {
	name := NormalizeName(cmd.Name)
	if err := ValidateName(name, MaxIngredientNameLength); err != nil {
		return 0, IngredientResponse{}, err
	}
	if cmd.Kind != KindRaw && cmd.Kind != KindPrepared {
		return 0, IngredientResponse{}, fmt.Errorf("%w: %q", ErrInvalidKind, cmd.Kind)
	}
	if !IsStockUnit(cmd.StockUnit) {
		return 0, IngredientResponse{}, fmt.Errorf("%w: stock unit must be g, ml or piece", ErrUnitNotConvertible)
	}
	fp := map[string]string{"name": name, "kind": cmd.Kind, "stock_unit": cmd.StockUnit}
	return executeMutation(ctx, c.runner, actor, cmd.RequestID, OpCreateIngredient, fp,
		func(mc MutationContext) (int, IngredientResponse, AuditRecord, error) {
			row, err := mc.Queries.InventoryCreateIngredient(ctx, sqlc.InventoryCreateIngredientParams{
				Name: name, Kind: cmd.Kind, StockUnitCode: cmd.StockUnit,
			})
			if err != nil {
				return 0, IngredientResponse{}, AuditRecord{}, mapDBError(err)
			}
			return http.StatusCreated, toIngredientResponse(row, nil), AuditRecord{
				EventType: EventIngredientCreated,
				Details: map[string]any{"ingredient_id": row.ID, "name": name,
					"kind": row.Kind, "stock_unit": row.StockUnitCode},
			}, nil
		})
}

// RenameIngredient renames an active Ingredient. The same name is a no-op.
func (c *Commands) RenameIngredient(ctx context.Context, actor Actor, cmd RenameIngredientCommand,
) (int, IngredientResponse, error) {
	name := NormalizeName(cmd.Name)
	if err := ValidateName(name, MaxIngredientNameLength); err != nil {
		return 0, IngredientResponse{}, err
	}
	fp := map[string]any{"ingredient_id": cmd.IngredientID, "name": name}
	return executeMutation(ctx, c.runner, actor, cmd.RequestID, OpRenameIngredient, fp,
		func(mc MutationContext) (int, IngredientResponse, AuditRecord, error) {
			q := mc.Queries
			row, err := lockIngredient(ctx, q, cmd.IngredientID)
			if err != nil {
				return 0, IngredientResponse{}, AuditRecord{}, err
			}
			if row.RetiredAt.Valid {
				return 0, IngredientResponse{}, AuditRecord{}, fmt.Errorf("%w: %s", ErrIngredientRetired, row.ID)
			}
			audit := AuditRecord{}
			if row.Name != name {
				before := row.Name
				if row, err = q.InventoryRenameIngredient(ctx, sqlc.InventoryRenameIngredientParams{ID: row.ID, Name: name}); err != nil {
					return 0, IngredientResponse{}, AuditRecord{}, mapDBError(err)
				}
				audit = AuditRecord{EventType: EventIngredientRenamed, Details: map[string]any{
					"ingredient_id": row.ID, "before_name": before, "after_name": name}}
			}
			out, err := ingredientWithUnits(ctx, q, row)
			return http.StatusOK, out, audit, err
		})
}

// RetireIngredient retires an Ingredient. Retiring a retired one is a no-op.
// Existing recipe lines keep it (spec §3.4).
func (c *Commands) RetireIngredient(ctx context.Context, actor Actor, cmd RetireIngredientCommand,
) (int, IngredientResponse, error) {
	fp := map[string]any{"ingredient_id": cmd.IngredientID}
	return executeMutation(ctx, c.runner, actor, cmd.RequestID, OpRetireIngredient, fp,
		func(mc MutationContext) (int, IngredientResponse, AuditRecord, error) {
			q := mc.Queries
			row, err := lockIngredient(ctx, q, cmd.IngredientID)
			if err != nil {
				return 0, IngredientResponse{}, AuditRecord{}, err
			}
			audit := AuditRecord{}
			if !row.RetiredAt.Valid {
				if row, err = q.InventoryRetireIngredient(ctx, row.ID); err != nil {
					return 0, IngredientResponse{}, AuditRecord{}, fmt.Errorf("retire ingredient: %w", err)
				}
				audit = AuditRecord{EventType: EventIngredientRetired, Details: map[string]any{"ingredient_id": row.ID}}
			}
			out, err := ingredientWithUnits(ctx, q, row)
			return http.StatusOK, out, audit, err
		})
}

// SetIngredientUnits replaces an Ingredient's own units: inputs with an id
// update that unit, inputs without one are added, and missing units are
// removed unless a recipe line or cost entry references them.
func (c *Commands) SetIngredientUnits(ctx context.Context, actor Actor, cmd SetIngredientUnitsCommand,
) (int, IngredientResponse, error) {
	inputs := make([]IngredientUnitInput, len(cmd.Units))
	for i, u := range cmd.Units {
		u.Name = NormalizeName(u.Name)
		if err := ValidateName(u.Name, MaxUnitNameLength); err != nil {
			return 0, IngredientResponse{}, err
		}
		u.StockQuantity = Round3(u.StockQuantity)
		if u.StockQuantity <= 0 {
			return 0, IngredientResponse{}, fmt.Errorf("%w: unit %q must hold a positive quantity", ErrInvalidQuantity, u.Name)
		}
		inputs[i] = u
	}
	fp := map[string]any{"ingredient_id": cmd.IngredientID, "units": inputs}
	return executeMutation(ctx, c.runner, actor, cmd.RequestID, OpSetIngredientUnits, fp,
		func(mc MutationContext) (int, IngredientResponse, AuditRecord, error) {
			q := mc.Queries
			fail := func(err error) (int, IngredientResponse, AuditRecord, error) {
				return 0, IngredientResponse{}, AuditRecord{}, err
			}
			row, err := lockIngredient(ctx, q, cmd.IngredientID)
			if err != nil {
				return fail(err)
			}
			if row.RetiredAt.Valid {
				return fail(fmt.Errorf("%w: %s", ErrIngredientRetired, row.ID))
			}
			existing, err := q.InventoryListIngredientUnits(ctx, []uuid.UUID{row.ID})
			if err != nil {
				return fail(fmt.Errorf("list ingredient units: %w", err))
			}
			kept := map[uuid.UUID]bool{}
			for _, in := range inputs {
				if in.ID != nil {
					kept[*in.ID] = true
				}
			}
			// Removals first, so a rename onto a removed unit's name does not
			// collide with it.
			for _, u := range existing {
				if kept[u.ID] {
					continue
				}
				inUse, err := q.InventoryIngredientUnitInUse(ctx, u.ID)
				if err != nil {
					return fail(fmt.Errorf("check ingredient unit use: %w", err))
				}
				if inUse {
					return fail(fmt.Errorf("%w: %s", ErrIngredientUnitInUse, u.Name))
				}
				if err := q.InventoryDeleteIngredientUnit(ctx, sqlc.InventoryDeleteIngredientUnitParams{ID: u.ID, IngredientID: row.ID}); err != nil {
					return fail(fmt.Errorf("delete ingredient unit: %w", err))
				}
			}
			for _, in := range inputs {
				if in.ID != nil {
					_, err = q.InventoryUpdateIngredientUnit(ctx, sqlc.InventoryUpdateIngredientUnitParams{
						ID: *in.ID, IngredientID: row.ID, Name: in.Name, StockQuantity: in.StockQuantity,
					})
					if errors.Is(err, sql.ErrNoRows) {
						return fail(fmt.Errorf("%w: %s", ErrIngredientUnitNotFound, *in.ID))
					}
				} else {
					_, err = q.InventoryInsertIngredientUnit(ctx, sqlc.InventoryInsertIngredientUnitParams{
						IngredientID: row.ID, Name: in.Name, StockQuantity: in.StockQuantity,
					})
				}
				if err != nil {
					return fail(mapDBError(err))
				}
			}
			out, err := ingredientWithUnits(ctx, q, row)
			if err != nil {
				return fail(err)
			}
			return http.StatusOK, out, AuditRecord{EventType: EventIngredientUnitsSet, Details: map[string]any{
				"ingredient_id": row.ID, "unit_count": len(inputs)}}, nil
		})
}

// resolveFactor returns how many Stock Units one unit of ref is for ing.
func resolveFactor(ctx context.Context, q *sqlc.Queries, ing sqlc.InventoryIngredient, ref UnitRef) (float64, error) {
	if err := ref.Validate(); err != nil {
		return 0, err
	}
	if ref.UnitCode != nil {
		return SystemFactor(ing.StockUnitCode, *ref.UnitCode)
	}
	u, err := q.InventoryGetIngredientUnit(ctx, sqlc.InventoryGetIngredientUnitParams{ID: *ref.IngredientUnitID, IngredientID: ing.ID})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("%w: %s", ErrIngredientUnitNotFound, *ref.IngredientUnitID)
	}
	if err != nil {
		return 0, fmt.Errorf("get ingredient unit: %w", err)
	}
	return u.StockQuantity, nil
}

func nullString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

func nullUUID(id *uuid.UUID) uuid.NullUUID {
	if id == nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: *id, Valid: true}
}

func toCostEntry(row sqlc.InventoryIngredientCost) CostEntryResponse {
	out := CostEntryResponse{ID: row.ID, IngredientID: row.IngredientID, AmountVND: row.AmountVnd,
		Quantity: row.Quantity, CostPerStockUnit: row.CostPerStockUnit, RecordedAt: row.RecordedAt}
	if row.UnitCode.Valid {
		code := row.UnitCode.String
		out.UnitCode = &code
	}
	if row.IngredientUnitID.Valid {
		id := row.IngredientUnitID.UUID
		out.IngredientUnitID = &id
	}
	return out
}

// RecordCost appends a purchase-price entry to a raw Ingredient and derives
// its cost per Stock Unit.
func (c *Commands) RecordCost(ctx context.Context, actor Actor, cmd RecordCostCommand,
) (int, CostEntryResponse, error) {
	if cmd.AmountVND <= 0 {
		return 0, CostEntryResponse{}, ErrInvalidAmount
	}
	qty := Round3(cmd.Quantity)
	if qty <= 0 {
		return 0, CostEntryResponse{}, fmt.Errorf("%w: must be positive", ErrInvalidQuantity)
	}
	if err := cmd.Unit.Validate(); err != nil {
		return 0, CostEntryResponse{}, err
	}
	fp := map[string]any{"ingredient_id": cmd.IngredientID, "amount_vnd": cmd.AmountVND,
		"quantity": qty, "unit": cmd.Unit}
	return executeMutation(ctx, c.runner, actor, cmd.RequestID, OpRecordCost, fp,
		func(mc MutationContext) (int, CostEntryResponse, AuditRecord, error) {
			q := mc.Queries
			fail := func(err error) (int, CostEntryResponse, AuditRecord, error) {
				return 0, CostEntryResponse{}, AuditRecord{}, err
			}
			ing, err := lockIngredient(ctx, q, cmd.IngredientID)
			if err != nil {
				return fail(err)
			}
			if ing.RetiredAt.Valid {
				return fail(fmt.Errorf("%w: %s", ErrIngredientRetired, ing.ID))
			}
			if ing.Kind != KindRaw {
				return fail(ErrCostOnPrepared)
			}
			factor, err := resolveFactor(ctx, q, ing, cmd.Unit)
			if err != nil {
				return fail(err)
			}
			perStock := Round4(float64(cmd.AmountVND) / Round3(qty*factor))
			row, err := q.InventoryInsertIngredientCost(ctx, sqlc.InventoryInsertIngredientCostParams{
				IngredientID: ing.ID, AmountVnd: cmd.AmountVND, Quantity: qty,
				UnitCode: nullString(cmd.Unit.UnitCode), IngredientUnitID: nullUUID(cmd.Unit.IngredientUnitID),
				CostPerStockUnit: perStock, ActorStaffIdentityID: mc.Actor.StaffID,
			})
			if err != nil {
				return fail(fmt.Errorf("insert ingredient cost: %w", err))
			}
			return http.StatusCreated, toCostEntry(row), AuditRecord{EventType: EventCostRecorded, Details: map[string]any{
				"ingredient_id": ing.ID, "amount_vnd": cmd.AmountVND, "quantity": qty,
				"cost_per_stock_unit": perStock}}, nil
		})
}
