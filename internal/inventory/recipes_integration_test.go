//go:build integration

package inventory_test

import (
	"context"
	"testing"

	"github.com/Mirai3103/open-pos/internal/auth"
	"github.com/Mirai3103/open-pos/internal/inventory"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecipesComposeThroughLoadedBook(t *testing.T) {
	c := newCommands()
	manager := seedActor(t, auth.RoleManager)
	m := seedMenu(t)
	f := seedIngredients(t, c, manager)
	seedRecipes(t, c, manager, m, f)

	book, err := inventory.LoadBook(context.Background(), queries())
	require.NoError(t, err)

	base, ok := book.UnitCost(f.Base.ID)
	require.True(t, ok)
	assert.Equal(t, 62.5, base)

	coffee := book.Compose(inventory.Selection{MenuItemID: m.CoffeeID})
	assert.Equal(t, inventory.CostComplete, coffee.Status)
	assert.Equal(t, 4473.69, coffee.TotalVND)

	large := book.Compose(inventory.Selection{MenuItemID: m.MilkTeaID, SizeID: &m.SizeL, ModifierOptionIDs: []uuid.UUID{m.PearlOptionID}})
	assert.Equal(t, 8600.0, large.TotalVND)

	medium := book.Compose(inventory.Selection{MenuItemID: m.MilkTeaID, SizeID: &m.SizeM})
	assert.Equal(t, inventory.CostPartial, medium.Status)
}

func TestRecipeRules(t *testing.T) {
	c := newCommands()
	ctx := context.Background()
	manager := seedActor(t, auth.RoleManager)
	m := seedMenu(t)
	f := seedIngredients(t, c, manager)

	set := func(kind string, owner uuid.UUID, lines ...inventory.RecipeLineInput) error {
		_, _, err := c.SetRecipe(ctx, manager, inventory.SetRecipeCommand{
			RequestID: uuid.New(), OwnerKind: kind, OwnerID: owner, Lines: lines,
		})
		return err
	}

	require.ErrorIs(t, set(inventory.OwnerMenuItem, m.MilkTeaID, line(f.Tea.ID, 100, "ml")), inventory.ErrItemHasSizes)
	require.ErrorIs(t, set(inventory.OwnerSize, m.SizeM, line(f.Tea.ID, -1, "ml")), inventory.ErrInvalidQuantity)
	require.ErrorIs(t, set(inventory.OwnerSize, m.SizeM, line(f.Tea.ID, 1, "g")), inventory.ErrUnitNotConvertible)
	require.ErrorIs(t, set(inventory.OwnerSize, m.SizeM, line(f.Tea.ID, 1, "ml"), line(f.Tea.ID, 2, "ml")), inventory.ErrDuplicateRecipeIngredient)
	require.ErrorIs(t, set(inventory.OwnerSize, uuid.New(), line(f.Tea.ID, 1, "ml")), inventory.ErrRecipeOwnerNotFound)
	require.NoError(t, set(inventory.OwnerModifierOption, m.PearlOptionID, line(f.Sugar.ID, -5, "g")), "option deltas may be negative")

	_, _, err := c.SetBatchRecipe(ctx, manager, inventory.SetBatchRecipeCommand{
		RequestID: uuid.New(), IngredientID: f.Tea.ID, YieldQuantity: 1,
		Lines: []inventory.RecipeLineInput{line(f.Powder.ID, 1, "g")},
	})
	require.ErrorIs(t, err, inventory.ErrBatchRecipeOnRaw)

	other := createIngredient(t, c, manager, uniqueName("Syrup"), inventory.KindPrepared, "ml")
	_, _, err = c.SetBatchRecipe(ctx, manager, inventory.SetBatchRecipeCommand{
		RequestID: uuid.New(), IngredientID: other.ID, YieldQuantity: 1,
		Lines: []inventory.RecipeLineInput{line(f.Base.ID, 1, "ml")},
	})
	require.ErrorIs(t, err, inventory.ErrBatchRecipeNotRaw)
}

func TestEmptyLinesDeleteRecipe(t *testing.T) {
	c := newCommands()
	manager := seedActor(t, auth.RoleManager)
	m := seedMenu(t)
	f := seedIngredients(t, c, manager)
	setRecipe(t, c, manager, inventory.OwnerMenuItem, m.CoffeeID, line(f.Base.ID, 40, "ml"))

	_, res, err := c.SetRecipe(context.Background(), manager, inventory.SetRecipeCommand{
		RequestID: uuid.New(), OwnerKind: inventory.OwnerMenuItem, OwnerID: m.CoffeeID, Lines: nil,
	})
	require.NoError(t, err)
	assert.Empty(t, res.Lines)

	book, err := inventory.LoadBook(context.Background(), queries())
	require.NoError(t, err)
	assert.Equal(t, inventory.CostNone, book.Compose(inventory.Selection{MenuItemID: m.CoffeeID}).Status)
}
