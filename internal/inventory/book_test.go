package inventory_test

import (
	"testing"

	"github.com/Mirai3103/open-pos/internal/inventory"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fixture struct {
	book                                      *inventory.Book
	coffeePowder, condensed, base, tea, sugar uuid.UUID
	pearls                                    uuid.UUID
	coffee, milkTea, sizeM, sizeL, pearlOpt   uuid.UUID
	lessSugarOpt                              uuid.UUID
}

func newFixture() fixture {
	f := fixture{book: inventory.NewBook()}
	add := func(name, kind, unit string) uuid.UUID {
		id := uuid.New()
		f.book.Ingredients[id] = inventory.Ingredient{ID: id, Name: name, Kind: kind, StockUnit: unit}
		return id
	}
	f.coffeePowder = add("Bột cà phê", inventory.KindRaw, "g")
	f.condensed = add("Sữa đặc", inventory.KindRaw, "g")
	f.base = add("Cốt cà phê", inventory.KindPrepared, "ml")
	f.tea = add("Trà", inventory.KindRaw, "ml")
	f.sugar = add("Đường", inventory.KindRaw, "g")
	f.pearls = add("Trân châu", inventory.KindRaw, "g")

	f.book.RawCosts[f.coffeePowder] = 250
	f.book.RawCosts[f.condensed] = 65.7895
	f.book.RawCosts[f.tea] = 20
	f.book.RawCosts[f.pearls] = 60
	// sugar deliberately has no cost.

	f.book.Batches[f.base] = inventory.Batch{
		Lines: []inventory.Line{{IngredientID: f.coffeePowder, StockQty: 250}}, Yield: 1000,
	}

	f.coffee, f.milkTea, f.sizeM, f.sizeL = uuid.New(), uuid.New(), uuid.New(), uuid.New()
	f.pearlOpt, f.lessSugarOpt = uuid.New(), uuid.New()
	f.book.Items[f.coffee] = []inventory.Line{{IngredientID: f.base, StockQty: 40}, {IngredientID: f.condensed, StockQty: 30}}
	f.book.Sizes[f.sizeM] = []inventory.Line{{IngredientID: f.tea, StockQty: 200}, {IngredientID: f.sugar, StockQty: 10}}
	f.book.Sizes[f.sizeL] = []inventory.Line{{IngredientID: f.tea, StockQty: 280}}
	f.book.Options[f.pearlOpt] = []inventory.Line{{IngredientID: f.pearls, StockQty: 50}}
	f.book.Options[f.lessSugarOpt] = []inventory.Line{{IngredientID: f.sugar, StockQty: -15}}
	return f
}

func TestPreparedUnitCost(t *testing.T) {
	f := newFixture()
	c, ok := f.book.UnitCost(f.base)
	require.True(t, ok)
	assert.Equal(t, 62.5, c)

	delete(f.book.RawCosts, f.coffeePowder)
	_, ok = f.book.UnitCost(f.base)
	assert.False(t, ok, "a prepared ingredient whose raw input has no cost has no cost")
}

func TestComposeItemWithoutSizes(t *testing.T) {
	f := newFixture()
	got := f.book.Compose(inventory.Selection{MenuItemID: f.coffee})
	assert.Equal(t, inventory.CostComplete, got.Status)
	require.Len(t, got.Lines, 2)
	assert.Equal(t, f.base, got.Lines[0].IngredientID)
	assert.Equal(t, 2500.0, *got.Lines[0].LineCost)
	assert.Equal(t, 1973.69, *got.Lines[1].LineCost)
	assert.Equal(t, 4473.69, got.TotalVND)
}

func TestComposeSizeWithOptionDelta(t *testing.T) {
	f := newFixture()
	got := f.book.Compose(inventory.Selection{MenuItemID: f.milkTea, SizeID: &f.sizeL, ModifierOptionIDs: []uuid.UUID{f.pearlOpt}})
	assert.Equal(t, inventory.CostComplete, got.Status)
	assert.Equal(t, 8600.0, got.TotalVND)
}

func TestComposeMissingCostIsPartial(t *testing.T) {
	f := newFixture()
	got := f.book.Compose(inventory.Selection{MenuItemID: f.milkTea, SizeID: &f.sizeM})
	assert.Equal(t, inventory.CostPartial, got.Status)
	assert.Equal(t, 4000.0, got.TotalVND, "only costed lines are summed")
	require.Len(t, got.Lines, 2)
	assert.Nil(t, got.Lines[1].LineCost)
}

func TestComposeClampsNegativeDeltaAtZero(t *testing.T) {
	f := newFixture()
	got := f.book.Compose(inventory.Selection{MenuItemID: f.milkTea, SizeID: &f.sizeM, ModifierOptionIDs: []uuid.UUID{f.lessSugarOpt}})
	require.Len(t, got.Lines, 1, "10 g sugar - 15 g clamps to zero and is dropped")
	assert.Equal(t, inventory.CostComplete, got.Status)
}

func TestComposeWithoutBaseRecipeIsNone(t *testing.T) {
	f := newFixture()
	got := f.book.Compose(inventory.Selection{MenuItemID: uuid.New(), ModifierOptionIDs: []uuid.UUID{f.pearlOpt}})
	assert.Equal(t, inventory.CostNone, got.Status)
	assert.Empty(t, got.Lines)
	assert.Zero(t, got.TotalVND)
}

func TestComposeIgnoresItemRecipeWhenSizeSelected(t *testing.T) {
	f := newFixture()
	f.book.Items[f.milkTea] = []inventory.Line{{IngredientID: f.tea, StockQty: 999}}
	unknownSize := uuid.New()
	got := f.book.Compose(inventory.Selection{MenuItemID: f.milkTea, SizeID: &unknownSize})
	assert.Equal(t, inventory.CostNone, got.Status)
}

func TestOptionDelta(t *testing.T) {
	f := newFixture()
	d := f.book.OptionDelta(f.pearlOpt)
	assert.True(t, d.HasRecipe)
	assert.True(t, d.Complete)
	assert.Equal(t, 3000.0, d.CostVND)

	none := f.book.OptionDelta(uuid.New())
	assert.False(t, none.HasRecipe)
}
