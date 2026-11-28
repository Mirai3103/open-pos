//go:build integration

package inventory_test

import (
	"context"
	"testing"

	"github.com/Mirai3103/open-pos/internal/auth"
	"github.com/Mirai3103/open-pos/internal/inventory"
	"github.com/Mirai3103/open-pos/internal/platform/command"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateIngredientAndDuplicateName(t *testing.T) {
	c := newCommands()
	manager := seedActor(t, auth.RoleManager)
	name := uniqueName("Sữa đặc")

	res := createIngredient(t, c, manager, "  "+name+"  ", inventory.KindRaw, "g")
	assert.Equal(t, name, res.Name)
	assert.Equal(t, "g", res.StockUnit)

	_, _, err := c.CreateIngredient(context.Background(), manager, inventory.CreateIngredientCommand{
		RequestID: uuid.New(), Name: name, Kind: inventory.KindRaw, StockUnit: "g",
	})
	require.ErrorIs(t, err, inventory.ErrDuplicateIngredientName)
}

func TestCreateIngredientValidation(t *testing.T) {
	c := newCommands()
	manager := seedActor(t, auth.RoleManager)
	_, _, err := c.CreateIngredient(context.Background(), manager, inventory.CreateIngredientCommand{
		RequestID: uuid.New(), Name: uniqueName("X"), Kind: inventory.KindRaw, StockUnit: "kg",
	})
	require.ErrorIs(t, err, inventory.ErrUnitNotConvertible)
	_, _, err = c.CreateIngredient(context.Background(), manager, inventory.CreateIngredientCommand{
		RequestID: uuid.New(), Name: uniqueName("X"), Kind: "BOUGHT", StockUnit: "g",
	})
	require.ErrorIs(t, err, inventory.ErrInvalidKind)
}

func TestCashierCannotAdminister(t *testing.T) {
	_, _, err := newCommands().CreateIngredient(context.Background(), seedActor(t, auth.RoleCashier),
		inventory.CreateIngredientCommand{RequestID: uuid.New(), Name: uniqueName("X"), Kind: inventory.KindRaw, StockUnit: "g"})
	require.ErrorIs(t, err, command.ErrForbidden)
}

func TestIngredientUnitsAndCost(t *testing.T) {
	c := newCommands()
	ctx := context.Background()
	manager := seedActor(t, auth.RoleManager)
	milk := createIngredient(t, c, manager, uniqueName("Sữa đặc"), inventory.KindRaw, "g")

	_, withUnits, err := c.SetIngredientUnits(ctx, manager, inventory.SetIngredientUnitsCommand{
		RequestID: uuid.New(), IngredientID: milk.ID,
		Units: []inventory.IngredientUnitInput{{Name: "hộp", StockQuantity: 380}},
	})
	require.NoError(t, err)
	require.Len(t, withUnits.Units, 1)
	can := withUnits.Units[0].ID

	entry := recordCost(t, c, manager, milk.ID, 25000, 1, inventory.UnitRef{IngredientUnitID: &can})
	assert.Equal(t, 65.7895, entry.CostPerStockUnit)

	byKg := recordCost(t, c, manager, milk.ID, 120000, 1, code("kg"))
	assert.Equal(t, 120.0, byKg.CostPerStockUnit)

	_, _, err = c.RecordCost(ctx, manager, inventory.RecordCostCommand{
		RequestID: uuid.New(), IngredientID: milk.ID, AmountVND: 1000, Quantity: 1, Unit: code("ml"),
	})
	require.ErrorIs(t, err, inventory.ErrUnitNotConvertible)

	// The can is referenced by a cost entry: removing it is refused.
	_, _, err = c.SetIngredientUnits(ctx, manager, inventory.SetIngredientUnitsCommand{
		RequestID: uuid.New(), IngredientID: milk.ID, Units: []inventory.IngredientUnitInput{},
	})
	require.ErrorIs(t, err, inventory.ErrIngredientUnitInUse)
}

func TestCostOnPreparedIsRejected(t *testing.T) {
	c := newCommands()
	manager := seedActor(t, auth.RoleManager)
	base := createIngredient(t, c, manager, uniqueName("Cốt"), inventory.KindPrepared, "ml")
	_, _, err := c.RecordCost(context.Background(), manager, inventory.RecordCostCommand{
		RequestID: uuid.New(), IngredientID: base.ID, AmountVND: 1000, Quantity: 1, Unit: code("l"),
	})
	require.ErrorIs(t, err, inventory.ErrCostOnPrepared)
}

func TestRenameAndRetire(t *testing.T) {
	c := newCommands()
	ctx := context.Background()
	manager := seedActor(t, auth.RoleManager)
	ing := createIngredient(t, c, manager, uniqueName("Đường"), inventory.KindRaw, "g")

	newName := uniqueName("Đường cát")
	_, renamed, err := c.RenameIngredient(ctx, manager, inventory.RenameIngredientCommand{
		RequestID: uuid.New(), IngredientID: ing.ID, Name: newName,
	})
	require.NoError(t, err)
	assert.Equal(t, newName, renamed.Name)

	_, retired, err := c.RetireIngredient(ctx, manager, inventory.RetireIngredientCommand{RequestID: uuid.New(), IngredientID: ing.ID})
	require.NoError(t, err)
	assert.True(t, retired.Retired)

	_, _, err = c.RenameIngredient(ctx, manager, inventory.RenameIngredientCommand{
		RequestID: uuid.New(), IngredientID: ing.ID, Name: uniqueName("Y"),
	})
	require.ErrorIs(t, err, inventory.ErrIngredientRetired)
}
