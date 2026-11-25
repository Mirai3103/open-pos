//go:build integration

package inventory_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Mirai3103/open-pos/internal/auth"
	"github.com/Mirai3103/open-pos/internal/inventory"
	"github.com/Mirai3103/open-pos/internal/platform/command"
	"github.com/Mirai3103/open-pos/internal/platform/database/sqlc"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func queries() *sqlc.Queries { return sqlc.New(invTestDB) }

func seedActor(t *testing.T, roles ...string) command.Actor {
	t.Helper()
	ctx := context.Background()
	q := queries()
	code := "I" + strings.ReplaceAll(uuid.NewString(), "-", "")[:20]
	hash, err := auth.HashPin("1234")
	require.NoError(t, err)
	row, err := q.CreateStaffIdentity(ctx, sqlc.CreateStaffIdentityParams{
		DisplayName: "Inventory " + code, Btrim: code, PinHash: hash, Enabled: true,
	})
	require.NoError(t, err)
	for _, role := range roles {
		require.NoError(t, q.AddStaffRole(ctx, sqlc.AddStaffRoleParams{StaffIdentityID: row.ID, Role: role}))
	}
	session, err := q.CreateStaffSession(ctx, sqlc.CreateStaffSessionParams{
		TokenHash: "tok_" + uuid.NewString()[:16], StaffIdentityID: row.ID,
		State: auth.SessionStateActive, LastHumanActivityAt: time.Now(),
		ExpiresAt: time.Now().Add(8 * time.Hour),
	})
	require.NoError(t, err)
	return command.Actor{StaffID: row.ID, SessionID: session.ID}
}

func uniqueName(prefix string) string { return prefix + " " + uuid.NewString()[:8] }

func newCommands() *inventory.Commands {
	return inventory.NewCommands(inventory.NewRunner(invTestDB, queries()))
}

func createIngredient(t *testing.T, c *inventory.Commands, actor command.Actor, name, kind, unit string) inventory.IngredientResponse {
	t.Helper()
	_, res, err := c.CreateIngredient(context.Background(), actor, inventory.CreateIngredientCommand{
		RequestID: uuid.New(), Name: name, Kind: kind, StockUnit: unit,
	})
	require.NoError(t, err)
	return res
}

func recordCost(t *testing.T, c *inventory.Commands, actor command.Actor, id uuid.UUID, amount int64, qty float64, unit inventory.UnitRef) inventory.CostEntryResponse {
	t.Helper()
	_, res, err := c.RecordCost(context.Background(), actor, inventory.RecordCostCommand{
		RequestID: uuid.New(), IngredientID: id, AmountVND: amount, Quantity: qty, Unit: unit,
	})
	require.NoError(t, err)
	return res
}

func code(s string) inventory.UnitRef { return inventory.UnitRef{UnitCode: &s} }
