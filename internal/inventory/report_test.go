package inventory

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAggregateGrossProfit(t *testing.T) {
	coffee := uuid.New()
	day := time.Date(2026, 11, 20, 3, 0, 0, 0, time.UTC) // 10:00 at UTC+7
	units := []reportUnit{
		{MenuItemID: coffee, ItemName: "Cà phê", RecordedAt: day, CostStatus: CostComplete, CostVND: 4473.69, PriceVND: 25000},
		// Wasted and comped: revenue 0, cost counted, waste cost counted.
		{MenuItemID: coffee, ItemName: "Cà phê", RecordedAt: day, CostStatus: CostComplete, CostVND: 4473.69, PriceVND: 25000, CompVND: 25000, Wasted: true},
		// Its remake: revenue 0, cost counted.
		{MenuItemID: coffee, ItemName: "Cà phê", RecordedAt: day.Add(time.Hour), CostStatus: CostComplete, CostVND: 4473.69, PriceVND: 25000, Remake: true},
		// Recipe-less item on the next local day.
		{MenuItemID: uuid.New(), ItemName: "Bánh", RecordedAt: day.Add(20 * time.Hour), CostStatus: CostNone, PriceVND: 20000},
	}
	totals, items, days := aggregateGrossProfit(units, time.FixedZone("UTC+7", 7*3600))

	assert.Equal(t, int64(45000), totals.RevenueVND)
	assert.Equal(t, 13421.07, totals.CostVND)
	assert.Equal(t, 31578.93, totals.GrossProfitVND)
	assert.Equal(t, 4473.69, totals.WasteCostVND)
	assert.Equal(t, 4, totals.Units)
	assert.Equal(t, 1, totals.UnitsCostNone)
	require.NotNil(t, totals.GrossMarginPct)
	assert.Equal(t, 70.18, *totals.GrossMarginPct)

	require.Len(t, items, 2)
	assert.Equal(t, "Bánh", items[0].ItemName, "items sort by name")
	require.Len(t, days, 2)
	assert.Equal(t, "2026-11-20", days[0].Date)
	assert.Equal(t, "2026-11-21", days[1].Date)
}
