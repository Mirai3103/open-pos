package inventory

import (
	"database/sql"

	"github.com/Mirai3103/open-pos/internal/auth"
	"github.com/Mirai3103/open-pos/internal/platform/database/sqlc"
	"github.com/Mirai3103/open-pos/internal/platform/features"
	"github.com/labstack/echo/v4"
)

// Slices aggregates the Inventory handlers.
type Slices struct {
	Runner   *Runner
	Commands *Commands
	Reads    *Reads
}

// NewSlices wires Inventory onto one Runner.
func NewSlices(db *sql.DB, queries *sqlc.Queries) *Slices {
	runner := NewRunner(db, queries)
	return &Slices{Runner: runner, Commands: NewCommands(runner), Reads: NewReads(runner)}
}

// RegisterRoutes mounts /inventory. Every route is behind the inventory Store
// Feature: authentication, then the feature gate, then the capability.
func (s *Slices) RegisterRoutes(v1 *echo.Group, authn *auth.Middleware) {
	gate := features.RequireEnabled(s.Runner.Queries(), FeatureKey)
	admin := []echo.MiddlewareFunc{authn.RequireAuth(), gate, authn.RequireCapability(CapAdminister)}
	costs := []echo.MiddlewareFunc{authn.RequireAuth(), gate, authn.RequireCapability(CapViewCosts)}

	v1.GET("/inventory/units", s.handleListUnits, admin...)
	v1.GET("/inventory/ingredients", s.handleListIngredients, admin...)
	v1.POST("/inventory/ingredients", s.handleCreateIngredient, admin...)
	v1.GET("/inventory/ingredients/:ingredient_id", s.handleGetIngredient, admin...)
	v1.PATCH("/inventory/ingredients/:ingredient_id/name", s.handleRenameIngredient, admin...)
	v1.POST("/inventory/ingredients/:ingredient_id/retirement", s.handleRetireIngredient, admin...)
	v1.PUT("/inventory/ingredients/:ingredient_id/units", s.handleSetIngredientUnits, admin...)
	v1.POST("/inventory/ingredients/:ingredient_id/costs", s.handleRecordCost, admin...)
	v1.PUT("/inventory/ingredients/:ingredient_id/batch-recipe", s.handleSetBatchRecipe, admin...)
	v1.GET("/inventory/recipes/:owner_kind/:owner_id", s.handleGetRecipe, admin...)
	v1.PUT("/inventory/recipes/:owner_kind/:owner_id", s.handleSetRecipe, admin...)
	v1.GET("/inventory/costing/menu", s.handleMenuCosting, costs...)
}
