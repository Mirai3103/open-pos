package inventory

import (
	"context"
	"fmt"

	"github.com/Mirai3103/open-pos/internal/platform/database/sqlc"
)

// LoadBook reads every Ingredient, current raw cost and Recipe into a Book.
func LoadBook(ctx context.Context, q *sqlc.Queries) (*Book, error) {
	b := NewBook()
	ings, err := q.InventoryListIngredients(ctx)
	if err != nil {
		return nil, fmt.Errorf("list ingredients: %w", err)
	}
	for _, i := range ings {
		b.Ingredients[i.ID] = Ingredient{ID: i.ID, Name: i.Name, Kind: i.Kind,
			StockUnit: i.StockUnitCode, Retired: i.RetiredAt.Valid}
	}
	costs, err := q.InventoryListCurrentCosts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list current costs: %w", err)
	}
	for _, c := range costs {
		b.RawCosts[c.IngredientID] = c.CostPerStockUnit
	}
	rows, err := q.InventoryListBookLines(ctx)
	if err != nil {
		return nil, fmt.Errorf("list recipe lines: %w", err)
	}
	for _, r := range rows {
		var factor float64
		switch {
		case r.FactorToBase.Valid:
			factor = r.FactorToBase.Float64
		case r.StockQuantity.Valid:
			factor = r.StockQuantity.Float64
		default:
			continue
		}
		line := Line{IngredientID: r.IngredientID, StockQty: Round3(r.Quantity * factor)}
		switch r.OwnerKind {
		case OwnerMenuItem:
			b.Items[r.MenuItemID.UUID] = append(b.Items[r.MenuItemID.UUID], line)
		case OwnerSize:
			b.Sizes[r.SizeID.UUID] = append(b.Sizes[r.SizeID.UUID], line)
		case OwnerModifierOption:
			b.Options[r.ModifierOptionID.UUID] = append(b.Options[r.ModifierOptionID.UUID], line)
		case OwnerIngredient:
			batch := b.Batches[r.OwnerIngredientID.UUID]
			batch.Yield = r.YieldQuantity.Float64
			batch.Lines = append(batch.Lines, line)
			b.Batches[r.OwnerIngredientID.UUID] = batch
		}
	}
	return b, nil
}
