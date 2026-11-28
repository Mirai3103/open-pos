package inventory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/http"

	"github.com/Mirai3103/open-pos/internal/platform/database/sqlc"
	"github.com/google/uuid"
)

type resolvedLine struct {
	ingredientID uuid.UUID
	quantity     float64
	unit         UnitRef
}

// prevalidateLines checks the input without the database.
func prevalidateLines(ownerKind string, lines []RecipeLineInput) ([]RecipeLineInput, error) {
	if len(lines) > MaxRecipeLines {
		return nil, fmt.Errorf("%w: at most %d", ErrTooManyLines, MaxRecipeLines)
	}
	seen := map[uuid.UUID]bool{}
	out := make([]RecipeLineInput, len(lines))
	for i, l := range lines {
		if seen[l.IngredientID] {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateRecipeIngredient, l.IngredientID)
		}
		seen[l.IngredientID] = true
		if err := ValidateLineQuantity(ownerKind, l.Quantity); err != nil {
			return nil, err
		}
		if err := l.Unit.Validate(); err != nil {
			return nil, err
		}
		l.Quantity = Round3(l.Quantity)
		out[i] = l
	}
	return out, nil
}

// storedIngredientIDs returns the Ingredients already on the owner's stored
// Recipe, so a replace may keep a line whose Ingredient has since been retired.
func storedIngredientIDs(ctx context.Context, q *sqlc.Queries, ownerKind string, ownerID uuid.UUID) (map[uuid.UUID]bool, error) {
	out := map[uuid.UUID]bool{}
	recipe, err := q.InventoryGetRecipe(ctx, sqlc.InventoryGetRecipeParams{OwnerKind: ownerKind, OwnerID: ownerID})
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get recipe: %w", err)
	}
	rows, err := q.InventoryListRecipeLines(ctx, recipe.ID)
	if err != nil {
		return nil, fmt.Errorf("list recipe lines: %w", err)
	}
	for _, r := range rows {
		out[r.IngredientID] = true
	}
	return out, nil
}

// resolveLines checks each line's Ingredient and unit inside the transaction.
// rawOnly enforces the one-level Batch Recipe rule. A retired Ingredient is
// accepted only when it is in stored (spec §3.4: existing lines stay valid).
func resolveLines(ctx context.Context, q *sqlc.Queries, lines []RecipeLineInput, rawOnly bool,
	stored map[uuid.UUID]bool,
) ([]resolvedLine, error) {
	out := make([]resolvedLine, 0, len(lines))
	for _, l := range lines {
		ing, err := q.InventoryGetIngredient(ctx, l.IngredientID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", ErrIngredientNotFound, l.IngredientID)
		}
		if err != nil {
			return nil, fmt.Errorf("get ingredient: %w", err)
		}
		if ing.RetiredAt.Valid && !stored[ing.ID] {
			return nil, fmt.Errorf("%w: %s", ErrIngredientRetired, ing.Name)
		}
		if rawOnly && ing.Kind != KindRaw {
			return nil, fmt.Errorf("%w: %s", ErrBatchRecipeNotRaw, ing.Name)
		}
		factor, err := resolveFactor(ctx, q, ing, l.Unit)
		if err != nil {
			return nil, err
		}
		if math.Abs(l.Quantity)*factor > MaxStockQuantity {
			return nil, fmt.Errorf("%w: %s exceeds %v %s", ErrInvalidQuantity, ing.Name, MaxStockQuantity, ing.StockUnitCode)
		}
		out = append(out, resolvedLine{ingredientID: ing.ID, quantity: l.Quantity, unit: l.Unit})
	}
	return out, nil
}

// writeRecipe replaces the owner's recipe rows; empty lines delete it.
func writeRecipe(ctx context.Context, q *sqlc.Queries, ownerKind string, ownerID uuid.UUID,
	yield *float64, lines []resolvedLine,
) error {
	recipeID, err := q.InventoryFindRecipe(ctx, sqlc.InventoryFindRecipeParams{OwnerKind: ownerKind, OwnerID: ownerID})
	exists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("find recipe: %w", err)
	}
	if len(lines) == 0 {
		if exists {
			if err := q.InventoryDeleteRecipe(ctx, recipeID); err != nil {
				return fmt.Errorf("delete recipe: %w", err)
			}
		}
		return nil
	}
	nullYield := sql.NullFloat64{}
	if yield != nil {
		nullYield = sql.NullFloat64{Float64: *yield, Valid: true}
	}
	if exists {
		if err := q.InventoryUpdateRecipeYield(ctx, sqlc.InventoryUpdateRecipeYieldParams{ID: recipeID, YieldQuantity: nullYield}); err != nil {
			return fmt.Errorf("update recipe: %w", err)
		}
		if err := q.InventoryDeleteRecipeLines(ctx, recipeID); err != nil {
			return fmt.Errorf("delete recipe lines: %w", err)
		}
	} else {
		params := sqlc.InventoryInsertRecipeParams{OwnerKind: ownerKind, YieldQuantity: nullYield}
		owner := uuid.NullUUID{UUID: ownerID, Valid: true}
		switch ownerKind {
		case OwnerMenuItem:
			params.MenuItemID = owner
		case OwnerSize:
			params.SizeID = owner
		case OwnerModifierOption:
			params.ModifierOptionID = owner
		case OwnerIngredient:
			params.IngredientID = owner
		}
		if recipeID, err = q.InventoryInsertRecipe(ctx, params); err != nil {
			return fmt.Errorf("insert recipe: %w", err)
		}
	}
	for i, l := range lines {
		if err := q.InventoryInsertRecipeLine(ctx, sqlc.InventoryInsertRecipeLineParams{
			RecipeID: recipeID, IngredientID: l.ingredientID, Quantity: l.quantity,
			UnitCode: nullString(l.unit.UnitCode), IngredientUnitID: nullUUID(l.unit.IngredientUnitID),
			Position: int32(i),
		}); err != nil {
			return fmt.Errorf("insert recipe line: %w", err)
		}
	}
	return nil
}

// verifyOwner checks the sale Recipe owner exists, is not retired, and (for a
// Menu Item with lines to store) has no Sizes. An empty replace deletes the
// Recipe, which must stay possible once the item gains Sizes.
func verifyOwner(ctx context.Context, q *sqlc.Queries, kind string, id uuid.UUID, hasLines bool) error {
	notFound := fmt.Errorf("%w: %s %s", ErrRecipeOwnerNotFound, kind, id)
	switch kind {
	case OwnerMenuItem:
		row, err := q.InventoryGetMenuItemForRecipe(ctx, id)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && row.RetiredAt.Valid) {
			return notFound
		}
		if err != nil {
			return fmt.Errorf("get menu item: %w", err)
		}
		if row.HasSizes && hasLines {
			return ErrItemHasSizes
		}
	case OwnerSize:
		row, err := q.InventoryGetSizeForRecipe(ctx, id)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && row.RetiredAt.Valid) {
			return notFound
		}
		if err != nil {
			return fmt.Errorf("get size: %w", err)
		}
	case OwnerModifierOption:
		row, err := q.InventoryGetOptionForRecipe(ctx, id)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && row.RetiredAt.Valid) {
			return notFound
		}
		if err != nil {
			return fmt.Errorf("get modifier option: %w", err)
		}
	default:
		return notFound
	}
	return nil
}

// SetRecipe replaces a Menu Item, Size or Modifier Option Recipe.
func (c *Commands) SetRecipe(ctx context.Context, actor Actor, cmd SetRecipeCommand) (int, RecipeResponse, error) {
	lines, err := prevalidateLines(cmd.OwnerKind, cmd.Lines)
	if err != nil {
		return 0, RecipeResponse{}, err
	}
	fp := map[string]any{"owner_kind": cmd.OwnerKind, "owner_id": cmd.OwnerID, "lines": lines}
	return executeMutation(ctx, c.runner, actor, cmd.RequestID, OpSetRecipe, fp,
		func(mc MutationContext) (int, RecipeResponse, AuditRecord, error) {
			q := mc.Queries
			fail := func(err error) (int, RecipeResponse, AuditRecord, error) {
				return 0, RecipeResponse{}, AuditRecord{}, err
			}
			if err := verifyOwner(ctx, q, cmd.OwnerKind, cmd.OwnerID, len(lines) > 0); err != nil {
				return fail(err)
			}
			stored, err := storedIngredientIDs(ctx, q, cmd.OwnerKind, cmd.OwnerID)
			if err != nil {
				return fail(err)
			}
			resolved, err := resolveLines(ctx, q, lines, false, stored)
			if err != nil {
				return fail(err)
			}
			if err := writeRecipe(ctx, q, cmd.OwnerKind, cmd.OwnerID, nil, resolved); err != nil {
				return fail(err)
			}
			out, err := loadRecipeResponse(ctx, q, cmd.OwnerKind, cmd.OwnerID)
			if err != nil {
				return fail(err)
			}
			return http.StatusOK, out, AuditRecord{EventType: EventRecipeSet, Details: map[string]any{
				"owner_kind": cmd.OwnerKind, "owner_id": cmd.OwnerID, "line_count": len(resolved)}}, nil
		})
}

// SetBatchRecipe replaces a prepared Ingredient's Batch Recipe and yield.
func (c *Commands) SetBatchRecipe(ctx context.Context, actor Actor, cmd SetBatchRecipeCommand) (int, RecipeResponse, error) {
	lines, err := prevalidateLines(OwnerIngredient, cmd.Lines)
	if err != nil {
		return 0, RecipeResponse{}, err
	}
	yield := Round3(cmd.YieldQuantity)
	if len(lines) > 0 && (yield <= 0 || yield > MaxStockQuantity) {
		return 0, RecipeResponse{}, ErrInvalidYield
	}
	fp := map[string]any{"ingredient_id": cmd.IngredientID, "yield": yield, "lines": lines}
	return executeMutation(ctx, c.runner, actor, cmd.RequestID, OpSetBatchRecipe, fp,
		func(mc MutationContext) (int, RecipeResponse, AuditRecord, error) {
			q := mc.Queries
			fail := func(err error) (int, RecipeResponse, AuditRecord, error) {
				return 0, RecipeResponse{}, AuditRecord{}, err
			}
			ing, err := lockIngredient(ctx, q, cmd.IngredientID)
			if err != nil {
				return fail(err)
			}
			if ing.RetiredAt.Valid {
				return fail(fmt.Errorf("%w: %s", ErrIngredientRetired, ing.ID))
			}
			if ing.Kind != KindPrepared {
				return fail(ErrBatchRecipeOnRaw)
			}
			stored, err := storedIngredientIDs(ctx, q, OwnerIngredient, ing.ID)
			if err != nil {
				return fail(err)
			}
			resolved, err := resolveLines(ctx, q, lines, true, stored)
			if err != nil {
				return fail(err)
			}
			if err := writeRecipe(ctx, q, OwnerIngredient, ing.ID, &yield, resolved); err != nil {
				return fail(err)
			}
			out, err := loadRecipeResponse(ctx, q, OwnerIngredient, ing.ID)
			if err != nil {
				return fail(err)
			}
			return http.StatusOK, out, AuditRecord{EventType: EventBatchRecipeSet, Details: map[string]any{
				"ingredient_id": ing.ID, "yield_quantity": yield, "line_count": len(resolved)}}, nil
		})
}

// loadRecipeResponse reads one Recipe without locking, for responses and reads.
func loadRecipeResponse(ctx context.Context, q *sqlc.Queries, ownerKind string, ownerID uuid.UUID) (RecipeResponse, error) {
	out := RecipeResponse{OwnerKind: ownerKind, OwnerID: ownerID, Lines: []RecipeLineResponse{}}
	recipe, err := q.InventoryGetRecipe(ctx, sqlc.InventoryGetRecipeParams{OwnerKind: ownerKind, OwnerID: ownerID})
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, fmt.Errorf("get recipe: %w", err)
	}
	if recipe.YieldQuantity.Valid {
		y := recipe.YieldQuantity.Float64
		out.YieldQuantity = &y
	}
	rows, err := q.InventoryListRecipeLines(ctx, recipe.ID)
	if err != nil {
		return out, fmt.Errorf("list recipe lines: %w", err)
	}
	for _, r := range rows {
		line := RecipeLineResponse{IngredientID: r.IngredientID, IngredientName: r.IngredientName,
			IngredientKind: r.IngredientKind, StockUnit: r.StockUnitCode, Quantity: r.Quantity}
		if r.UnitCode.Valid {
			s := r.UnitCode.String
			line.UnitCode = &s
		}
		if r.IngredientUnitID.Valid {
			id := r.IngredientUnitID.UUID
			line.IngredientUnitID = &id
		}
		if r.IngredientUnitName.Valid {
			s := r.IngredientUnitName.String
			line.IngredientUnitName = &s
		}
		out.Lines = append(out.Lines, line)
	}
	return out, nil
}
