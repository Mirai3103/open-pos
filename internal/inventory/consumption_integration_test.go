//go:build integration

package inventory_test

import (
	"context"
	"testing"

	"github.com/Mirai3103/open-pos/internal/inventory"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFeatureOffRecordsNothing(t *testing.T) {
	env := newInvEnv(t)
	env.SetFeature(t, false)
	units := env.Submit(t, env.Menu.CoffeeID, nil, nil, 1)
	_, ok := Consumption(t, units[0].ID)
	assert.False(t, ok)
}

func TestSubmitRecordsCostedConsumption(t *testing.T) {
	env := newInvEnv(t)
	env.SetFeature(t, true)
	defer env.SetFeature(t, false)

	units := env.Submit(t, env.Menu.CoffeeID, nil, nil, 2)
	for _, u := range units {
		row, ok := Consumption(t, u.ID)
		require.True(t, ok)
		assert.Equal(t, "ACTIVE", row.Status)
		assert.Equal(t, inventory.CostComplete, row.CostStatus)
		assert.Equal(t, 4473.69, row.Total)
		assert.Equal(t, 2, row.Lines)
	}

	large := env.Submit(t, env.Menu.MilkTeaID, &env.Menu.SizeL, []uuid.UUID{env.Menu.PearlOptionID}, 1)
	row, ok := Consumption(t, large[0].ID)
	require.True(t, ok)
	assert.Equal(t, 8600.0, row.Total)
	assert.Equal(t, env.Menu.SizeL, row.SizeID.UUID)

	medium := env.Submit(t, env.Menu.MilkTeaID, &env.Menu.SizeM, []uuid.UUID{}, 1)
	row, _ = Consumption(t, medium[0].ID)
	assert.Equal(t, inventory.CostPartial, row.CostStatus)

	plain := env.Submit(t, env.Menu.PlainID, nil, nil, 1)
	row, _ = Consumption(t, plain[0].ID)
	assert.Equal(t, inventory.CostNone, row.CostStatus, "no recipe still sells")
	assert.Zero(t, row.Total)
}

func TestLaterCostChangeDoesNotRewriteHistory(t *testing.T) {
	env := newInvEnv(t)
	env.SetFeature(t, true)
	defer env.SetFeature(t, false)

	units := env.Submit(t, env.Menu.MilkTeaID, &env.Menu.SizeL, []uuid.UUID{}, 1)
	recordCost(t, env.Commands, env.Manager, env.Ing.Tea.ID, 999000, 1, code("l"))
	row, _ := Consumption(t, units[0].ID)
	assert.Equal(t, 5600.0, row.Total)
}

func TestCancelVoidsEvenWhenFeatureTurnedOff(t *testing.T) {
	env := newInvEnv(t)
	env.SetFeature(t, true)
	units := env.Submit(t, env.Menu.CoffeeID, nil, nil, 2)

	env.Cancel(t, units[0].ID)
	row, _ := Consumption(t, units[0].ID)
	assert.Equal(t, "VOIDED", row.Status)

	env.SetFeature(t, false)
	env.Cancel(t, units[1].ID)
	row, _ = Consumption(t, units[1].ID)
	assert.Equal(t, "VOIDED", row.Status, "voids still apply while the feature is off")
}

func TestWasteKeepsRecordAndRemakeAddsOne(t *testing.T) {
	env := newInvEnv(t)
	env.SetFeature(t, true)
	defer env.SetFeature(t, false)

	units := env.Submit(t, env.Menu.CoffeeID, nil, nil, 1)
	remakeID := env.WasteAndRemake(t, units[0].ID)

	wasted, _ := Consumption(t, units[0].ID)
	assert.Equal(t, "ACTIVE", wasted.Status)
	remade, ok := Consumption(t, remakeID)
	require.True(t, ok)
	assert.Equal(t, 4473.69, remade.Total)
}

func TestRecordingIsIdempotentPerUnit(t *testing.T) {
	env := newInvEnv(t)
	env.SetFeature(t, true)
	defer env.SetFeature(t, false)
	units := env.Submit(t, env.Menu.CoffeeID, nil, nil, 1)

	_, err := invTestDB.ExecContext(context.Background(), `INSERT INTO inventory_consumptions
		(preparation_unit_id, menu_item_id, cost_status, cost_total_vnd)
		VALUES ($1, $2, 'NONE', 0) ON CONFLICT (preparation_unit_id) DO NOTHING`, units[0].ID, env.Menu.CoffeeID)
	require.NoError(t, err)
	row, _ := Consumption(t, units[0].ID)
	assert.Equal(t, 4473.69, row.Total, "the original record is kept")
}
