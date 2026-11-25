package inventory

import (
	"context"
	"fmt"
	"sort"

	"github.com/Mirai3103/open-pos/internal/platform/database/sqlc"
	"github.com/google/uuid"
)

// ItemCosting is one Menu Item, or one of its Sizes, at current cost.
type ItemCosting struct {
	MenuItemID        uuid.UUID  `json:"menu_item_id"`
	ItemName          string     `json:"item_name"`
	CategoryID        uuid.UUID  `json:"category_id"`
	CategoryName      string     `json:"category_name"`
	SizeID            *uuid.UUID `json:"size_id"`
	SizeName          *string    `json:"size_name"`
	PriceVND          int64      `json:"price_vnd"`
	CostVND           float64    `json:"cost_vnd"`
	MarginVND         float64    `json:"margin_vnd"`
	MarginPct         *float64   `json:"margin_pct"`
	CostStatus        string     `json:"cost_status"`
	ItemRecipeIgnored bool       `json:"item_recipe_ignored"`
}

// OptionCosting is one Modifier Option's surcharge against its cost delta.
type OptionCosting struct {
	ModifierOptionID    uuid.UUID `json:"modifier_option_id"`
	OptionName          string    `json:"option_name"`
	GroupID             uuid.UUID `json:"group_id"`
	GroupName           string    `json:"group_name"`
	SurchargeVND        int64     `json:"surcharge_vnd"`
	HasRecipe           bool      `json:"has_recipe"`
	CostDeltaVND        float64   `json:"cost_delta_vnd"`
	Complete            bool      `json:"complete"`
	PricedWithoutRecipe bool      `json:"priced_without_recipe"`
}

// PreparedCosting is one prepared Ingredient's derived unit cost.
type PreparedCosting struct {
	IngredientID     uuid.UUID `json:"ingredient_id"`
	Name             string    `json:"name"`
	StockUnit        string    `json:"stock_unit"`
	CostPerStockUnit *float64  `json:"cost_per_stock_unit"`
}

// MenuCostingResponse is spec §7.1.
type MenuCostingResponse struct {
	Items               []ItemCosting     `json:"items"`
	Options             []OptionCosting   `json:"options"`
	PreparedIngredients []PreparedCosting `json:"prepared_ingredients"`
}

func marginPct(price int64, cost float64) *float64 {
	if price <= 0 {
		return nil
	}
	pct := Round2((float64(price) - cost) / float64(price) * 100)
	return &pct
}

func buildMenuCosting(book *Book, items []sqlc.InventoryListMenuForCostingRow,
	options []sqlc.InventoryListOptionsForCostingRow,
) MenuCostingResponse {
	out := MenuCostingResponse{Items: []ItemCosting{}, Options: []OptionCosting{}, PreparedIngredients: []PreparedCosting{}}
	for _, r := range items {
		row := ItemCosting{MenuItemID: r.MenuItemID, ItemName: r.ItemName, CategoryID: r.CategoryID, CategoryName: r.CategoryName}
		sel := Selection{MenuItemID: r.MenuItemID}
		if r.SizeID.Valid {
			id, name := r.SizeID.UUID, r.SizeName.String
			row.SizeID, row.SizeName = &id, &name
			row.PriceVND = r.SizePriceVnd.Int64
			sel.SizeID = &id
			row.ItemRecipeIgnored = len(book.Items[r.MenuItemID]) > 0
		} else {
			row.PriceVND = r.PriceVnd.Int64
		}
		comp := book.Compose(sel)
		row.CostVND, row.CostStatus = comp.TotalVND, comp.Status
		row.MarginVND = Round2(float64(row.PriceVND) - comp.TotalVND)
		row.MarginPct = marginPct(row.PriceVND, comp.TotalVND)
		out.Items = append(out.Items, row)
	}
	for _, o := range options {
		d := book.OptionDelta(o.ModifierOptionID)
		out.Options = append(out.Options, OptionCosting{
			ModifierOptionID: o.ModifierOptionID, OptionName: o.OptionName, GroupID: o.GroupID,
			GroupName: o.GroupName, SurchargeVND: o.SurchargeVnd, HasRecipe: d.HasRecipe,
			CostDeltaVND: d.CostVND, Complete: d.Complete,
			PricedWithoutRecipe: o.SurchargeVnd > 0 && !d.HasRecipe,
		})
	}
	for id, ing := range book.Ingredients {
		if ing.Kind != KindPrepared || ing.Retired {
			continue
		}
		pc := PreparedCosting{IngredientID: id, Name: ing.Name, StockUnit: ing.StockUnit}
		if c, ok := book.UnitCost(id); ok {
			pc.CostPerStockUnit = &c
		}
		out.PreparedIngredients = append(out.PreparedIngredients, pc)
	}
	sort.Slice(out.PreparedIngredients, func(i, j int) bool {
		return out.PreparedIngredients[i].Name < out.PreparedIngredients[j].Name
	})
	return out
}

// MenuCosting returns spec §7.1 from current Recipes and costs.
func (r *Reads) MenuCosting(ctx context.Context, actor Actor) (MenuCostingResponse, error) {
	return executeRead(ctx, r.runner, actor, CapViewCosts, func(rc ReadContext) (MenuCostingResponse, error) {
		q := rc.Queries
		book, err := LoadBook(ctx, q)
		if err != nil {
			return MenuCostingResponse{}, err
		}
		items, err := q.InventoryListMenuForCosting(ctx)
		if err != nil {
			return MenuCostingResponse{}, fmt.Errorf("list menu for costing: %w", err)
		}
		options, err := q.InventoryListOptionsForCosting(ctx)
		if err != nil {
			return MenuCostingResponse{}, fmt.Errorf("list options for costing: %w", err)
		}
		return buildMenuCosting(book, items, options), nil
	})
}
