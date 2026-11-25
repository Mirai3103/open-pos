//go:build integration

package inventory_test

import (
	"context"
	"testing"

	"github.com/Mirai3103/open-pos/internal/auth"
	"github.com/Mirai3103/open-pos/internal/inventory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMenuCostingRead(t *testing.T) {
	c := newCommands()
	manager := seedActor(t, auth.RoleManager)
	m := seedMenu(t)
	f := seedIngredients(t, c, manager)
	seedRecipes(t, c, manager, m, f)

	got, err := inventory.NewReads(inventory.NewRunner(invTestDB, queries())).MenuCosting(context.Background(), manager)
	require.NoError(t, err)

	var coffee *inventory.ItemCosting
	for i := range got.Items {
		if got.Items[i].MenuItemID == m.CoffeeID {
			coffee = &got.Items[i]
		}
	}
	require.NotNil(t, coffee)
	assert.Equal(t, 4473.69, coffee.CostVND)
	assert.Equal(t, int64(25000), coffee.PriceVND)

	_, err = inventory.NewReads(inventory.NewRunner(invTestDB, queries())).MenuCosting(context.Background(), seedActor(t, auth.RoleCashier))
	require.Error(t, err)
}
