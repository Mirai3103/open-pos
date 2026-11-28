//go:build integration

package inventory_test

import (
	"context"
	"testing"
	"time"

	"github.com/Mirai3103/open-pos/internal/inventory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGrossProfitReport(t *testing.T) {
	env := newInvEnv(t)
	from := time.Now().Add(-time.Minute)
	env.SetFeature(t, true)
	defer env.SetFeature(t, false)

	sold := env.Submit(t, env.Menu.CoffeeID, nil, nil, 3)
	env.WasteAndRemake(t, sold[1].ID) // wasted unit + remake
	env.Cancel(t, sold[2].ID)         // voided: excluded

	got, err := inventory.NewReads(inventory.NewRunner(invTestDB, queries())).GrossProfit(context.Background(), env.Manager,
		inventory.GrossProfitQuery{From: from, To: time.Now().Add(time.Minute), TZOffsetMinutes: 420})
	require.NoError(t, err)

	var coffee *inventory.GrossProfitItem
	for i := range got.Items {
		if got.Items[i].MenuItemID == env.Menu.CoffeeID {
			coffee = &got.Items[i]
		}
	}
	require.NotNil(t, coffee)
	assert.Equal(t, 3, coffee.Figures.Units, "two sold units + one remake; the cancelled unit is excluded")
	assert.Equal(t, int64(50000), coffee.Figures.RevenueVND, "no Comp recorded here; the remake earns 0")
	assert.Equal(t, 13421.07, coffee.Figures.CostVND)
	assert.Equal(t, 4473.69, coffee.Figures.WasteCostVND)
	assert.NotEmpty(t, got.DisabledIntervals, "the feature was off at the start of the period")
}

func TestGrossProfitRejectsBadPeriod(t *testing.T) {
	env := newInvEnv(t)
	now := time.Now()
	_, err := inventory.NewReads(inventory.NewRunner(invTestDB, queries())).GrossProfit(context.Background(), env.Manager,
		inventory.GrossProfitQuery{From: now, To: now.Add(-time.Hour)})
	require.ErrorIs(t, err, inventory.ErrInvalidPeriod)
}
