//go:build integration

package inventory_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSystemUnitsSeeded(t *testing.T) {
	var n int
	require.NoError(t, invTestDB.QueryRow(`SELECT count(*) FROM inventory_units`).Scan(&n))
	assert.Equal(t, 5, n)
}

func TestStockUnitMustBeBase(t *testing.T) {
	_, err := invTestDB.Exec(`INSERT INTO inventory_ingredients (name, kind, stock_unit_code)
		VALUES ('Bad unit', 'RAW', 'kg')`)
	require.Error(t, err)
}

func TestRecipeOwnerExactlyOne(t *testing.T) {
	_, err := invTestDB.Exec(`INSERT INTO inventory_recipes (owner_kind) VALUES ('MENU_ITEM')`)
	require.Error(t, err)
}
