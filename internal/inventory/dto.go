package inventory

import (
	"time"

	"github.com/google/uuid"
)

// CreateIngredientCommand creates an Ingredient.
type CreateIngredientCommand struct {
	RequestID uuid.UUID `json:"request_id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	StockUnit string    `json:"stock_unit"`
}

// RenameIngredientCommand renames an Ingredient. IngredientID comes from the route.
type RenameIngredientCommand struct {
	RequestID    uuid.UUID `json:"request_id"`
	IngredientID uuid.UUID `json:"-"`
	Name         string    `json:"name"`
}

// RetireIngredientCommand retires an Ingredient. IngredientID comes from the route.
type RetireIngredientCommand struct {
	RequestID    uuid.UUID `json:"request_id"`
	IngredientID uuid.UUID `json:"-"`
}

// IngredientUnitInput is one Ingredient Unit in a replace-all set. ID is nil
// for a new unit.
type IngredientUnitInput struct {
	ID            *uuid.UUID `json:"id"`
	Name          string     `json:"name"`
	StockQuantity float64    `json:"stock_quantity"`
}

// SetIngredientUnitsCommand replaces an Ingredient's own units.
type SetIngredientUnitsCommand struct {
	RequestID    uuid.UUID             `json:"request_id"`
	IngredientID uuid.UUID             `json:"-"`
	Units        []IngredientUnitInput `json:"units"`
}

// RecordCostCommand appends a purchase-price entry to a raw Ingredient:
// AmountVND paid for Quantity of Unit.
type RecordCostCommand struct {
	RequestID    uuid.UUID `json:"request_id"`
	IngredientID uuid.UUID `json:"-"`
	AmountVND    int64     `json:"amount_vnd"`
	Quantity     float64   `json:"quantity"`
	Unit         UnitRef   `json:"unit"`
}

// IngredientUnitResponse is one Ingredient Unit.
type IngredientUnitResponse struct {
	ID            uuid.UUID `json:"id"`
	Name          string    `json:"name"`
	StockQuantity float64   `json:"stock_quantity"`
}

// IngredientResponse is one Ingredient. CostPerStockUnit is null when the
// caller lacks inventory.view_costs or the Ingredient has no cost.
type IngredientResponse struct {
	ID               uuid.UUID                `json:"id"`
	Name             string                   `json:"name"`
	Kind             string                   `json:"kind"`
	StockUnit        string                   `json:"stock_unit"`
	Retired          bool                     `json:"retired"`
	Units            []IngredientUnitResponse `json:"units"`
	CostPerStockUnit *float64                 `json:"cost_per_stock_unit"`
}

// CostEntryResponse is one recorded cost entry.
type CostEntryResponse struct {
	ID               uuid.UUID  `json:"id"`
	IngredientID     uuid.UUID  `json:"ingredient_id"`
	AmountVND        int64      `json:"amount_vnd"`
	Quantity         float64    `json:"quantity"`
	UnitCode         *string    `json:"unit_code"`
	IngredientUnitID *uuid.UUID `json:"ingredient_unit_id"`
	CostPerStockUnit float64    `json:"cost_per_stock_unit"`
	RecordedAt       time.Time  `json:"recorded_at"`
}
