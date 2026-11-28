package features

import (
	"database/sql"

	"github.com/Mirai3103/open-pos/internal/auth"
	"github.com/Mirai3103/open-pos/internal/platform/command"
	"github.com/Mirai3103/open-pos/internal/platform/database/sqlc"
	"github.com/labstack/echo/v4"
)

// Slices aggregates the Store Feature handlers.
type Slices struct {
	Runner *command.Runner
	List   *ListFeaturesHandler
	Set    *SetFeatureHandler
}

// NewSlices wires the handlers onto one Runner.
func NewSlices(db *sql.DB, queries *sqlc.Queries) *Slices {
	runner := NewRunner(db, queries)
	return &Slices{Runner: runner, List: NewListFeaturesHandler(runner), Set: NewSetFeatureHandler(runner)}
}

// RegisterRoutes mounts /features. These routes are never feature-gated.
func (s *Slices) RegisterRoutes(v1 *echo.Group, authn *auth.Middleware) {
	v1.GET("/features", s.handleList, authn.RequireAuth())
	v1.PUT("/features/:key", s.handleSet, authn.RequireAuth(), authn.RequireCapability(CapAdminister))
}
