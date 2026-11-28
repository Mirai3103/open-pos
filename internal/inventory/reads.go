package inventory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"github.com/Mirai3103/open-pos/internal/platform/database/sqlc"
	"github.com/google/uuid"
)

// Reads holds every Inventory read.
type Reads struct{ runner *Runner }

// NewReads creates Reads on runner.
func NewReads(runner *Runner) *Reads { return &Reads{runner: runner} }

func canViewCosts(caps []string) bool { return slices.Contains(caps, CapViewCosts) }

// ListUnits returns the system units.
func (r *Reads) ListUnits(ctx context.Context, actor Actor) ([]SystemUnit, error) {
	return executeRead(ctx, r.runner, actor, CapAdminister, func(ReadContext) ([]SystemUnit, error) {
		return SystemUnits(), nil
	})
}

// ListIngredients returns every Ingredient, active first.
func (r *Reads) ListIngredients(ctx context.Context, actor Actor) ([]IngredientResponse, error) {
	return executeRead(ctx, r.runner, actor, CapAdminister, func(rc ReadContext) ([]IngredientResponse, error) {
		q := rc.Queries
		rows, err := q.InventoryListIngredients(ctx)
		if err != nil {
			return nil, fmt.Errorf("list ingredients: %w", err)
		}
		ids := make([]uuid.UUID, len(rows))
		for i, row := range rows {
			ids[i] = row.ID
		}
		units, err := q.InventoryListIngredientUnits(ctx, ids)
		if err != nil {
			return nil, fmt.Errorf("list ingredient units: %w", err)
		}
		byIngredient := map[uuid.UUID][]sqlcUnit{}
		for _, u := range units {
			byIngredient[u.IngredientID] = append(byIngredient[u.IngredientID], u)
		}
		var book *Book
		if canViewCosts(rc.Capabilities) {
			if book, err = LoadBook(ctx, q); err != nil {
				return nil, err
			}
		}
		out := make([]IngredientResponse, 0, len(rows))
		for _, row := range rows {
			res := toIngredientResponse(row, byIngredient[row.ID])
			if book != nil {
				if c, ok := book.UnitCost(row.ID); ok {
					res.CostPerStockUnit = &c
				}
			}
			out = append(out, res)
		}
		return out, nil
	})
}

// GetIngredient returns one Ingredient with its detail.
func (r *Reads) GetIngredient(ctx context.Context, actor Actor, id uuid.UUID) (IngredientDetailResponse, error) {
	return executeRead(ctx, r.runner, actor, CapAdminister, func(rc ReadContext) (IngredientDetailResponse, error) {
		q := rc.Queries
		row, err := q.InventoryGetIngredient(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return IngredientDetailResponse{}, fmt.Errorf("%w: %s", ErrIngredientNotFound, id)
		}
		if err != nil {
			return IngredientDetailResponse{}, fmt.Errorf("get ingredient: %w", err)
		}
		base, err := ingredientWithUnits(ctx, q, row)
		if err != nil {
			return IngredientDetailResponse{}, err
		}
		out := IngredientDetailResponse{IngredientResponse: base, CostHistory: []CostEntryResponse{}}
		if canViewCosts(rc.Capabilities) {
			book, err := LoadBook(ctx, q)
			if err != nil {
				return out, err
			}
			if c, ok := book.UnitCost(id); ok {
				out.CostPerStockUnit = &c
			}
			history, err := q.InventoryListIngredientCosts(ctx, id)
			if err != nil {
				return out, fmt.Errorf("list ingredient costs: %w", err)
			}
			for _, h := range history {
				out.CostHistory = append(out.CostHistory, toCostEntry(h))
			}
		}
		if row.Kind == KindPrepared {
			batch, err := loadRecipeResponse(ctx, q, OwnerIngredient, id)
			if err != nil {
				return out, err
			}
			out.BatchRecipe = &batch
		}
		return out, nil
	})
}

// GetRecipe returns one sale Recipe (empty lines when none).
func (r *Reads) GetRecipe(ctx context.Context, actor Actor, ownerKind string, ownerID uuid.UUID) (RecipeResponse, error) {
	return executeRead(ctx, r.runner, actor, CapAdminister, func(rc ReadContext) (RecipeResponse, error) {
		return loadRecipeResponse(ctx, rc.Queries, ownerKind, ownerID)
	})
}

type sqlcUnit = sqlc.InventoryIngredientUnit
