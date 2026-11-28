package inventory

import (
	"database/sql"
	"testing"

	"github.com/Mirai3103/open-pos/internal/platform/database/sqlc"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildMenuCosting(t *testing.T) {
	b := NewBook()
	tea := uuid.New()
	b.Ingredients[tea] = Ingredient{ID: tea, Name: "Trà", Kind: KindRaw, StockUnit: "ml"}
	b.RawCosts[tea] = 20
	coffee, milkTea, sizeL, opt := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	b.Sizes[sizeL] = []Line{{IngredientID: tea, StockQty: 280}}
	b.Items[milkTea] = []Line{{IngredientID: tea, StockQty: 1}} // ignored: item has sizes

	cat := uuid.New()
	got := buildMenuCosting(b, []sqlc.InventoryListMenuForCostingRow{
		{MenuItemID: coffee, ItemName: "Cà phê", CategoryID: cat, CategoryName: "Đồ uống",
			PriceVnd: sql.NullInt64{Int64: 25000, Valid: true}},
		{MenuItemID: milkTea, ItemName: "Trà sữa", CategoryID: cat, CategoryName: "Đồ uống",
			SizeID: uuid.NullUUID{UUID: sizeL, Valid: true}, SizeName: sql.NullString{String: "Size L", Valid: true},
			SizePriceVnd: sql.NullInt64{Int64: 45000, Valid: true}},
	}, []sqlc.InventoryListOptionsForCostingRow{
		{ModifierOptionID: opt, OptionName: "Trân châu", GroupID: uuid.New(), GroupName: "Topping", SurchargeVnd: 5000},
	})

	require.Len(t, got.Items, 2)
	assert.Equal(t, CostNone, got.Items[0].CostStatus)
	assert.Equal(t, int64(45000), got.Items[1].PriceVND)
	assert.Equal(t, 5600.0, got.Items[1].CostVND)
	assert.Equal(t, 39400.0, got.Items[1].MarginVND)
	assert.Equal(t, 87.56, *got.Items[1].MarginPct)
	assert.True(t, got.Items[1].ItemRecipeIgnored)
	require.Len(t, got.Options, 1)
	assert.True(t, got.Options[0].PricedWithoutRecipe)
}
