package inventory_test

import (
	"testing"

	"github.com/Mirai3103/open-pos/internal/inventory"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func strPtr(s string) *string { return &s }

func TestSystemFactor(t *testing.T) {
	f, err := inventory.SystemFactor("g", "kg")
	require.NoError(t, err)
	assert.Equal(t, 1000.0, f)

	f, err = inventory.SystemFactor("ml", "ml")
	require.NoError(t, err)
	assert.Equal(t, 1.0, f)

	_, err = inventory.SystemFactor("g", "ml")
	require.ErrorIs(t, err, inventory.ErrUnitNotConvertible)
	_, err = inventory.SystemFactor("g", "tbsp")
	require.ErrorIs(t, err, inventory.ErrUnitNotConvertible)
}

func TestUnitRefNeedsExactlyOne(t *testing.T) {
	id := uuid.New()
	require.Error(t, inventory.UnitRef{}.Validate())
	require.Error(t, inventory.UnitRef{UnitCode: strPtr("g"), IngredientUnitID: &id}.Validate())
	require.NoError(t, inventory.UnitRef{UnitCode: strPtr("g")}.Validate())
	require.NoError(t, inventory.UnitRef{IngredientUnitID: &id}.Validate())
}

func TestValidateLineQuantity(t *testing.T) {
	require.NoError(t, inventory.ValidateLineQuantity(inventory.OwnerSize, 1))
	require.ErrorIs(t, inventory.ValidateLineQuantity(inventory.OwnerSize, -1), inventory.ErrInvalidQuantity)
	require.ErrorIs(t, inventory.ValidateLineQuantity(inventory.OwnerIngredient, 0), inventory.ErrInvalidQuantity)
	require.NoError(t, inventory.ValidateLineQuantity(inventory.OwnerModifierOption, -10))
	require.ErrorIs(t, inventory.ValidateLineQuantity(inventory.OwnerModifierOption, 0.0001), inventory.ErrInvalidQuantity,
		"rounds to zero at 3 decimals")
}

func TestRounding(t *testing.T) {
	assert.Equal(t, 65.7895, inventory.Round4(25000.0/380))
	assert.Equal(t, 1973.69, inventory.Round2(30*65.7895))
}

func TestValidateLineQuantityBounds(t *testing.T) {
	require.NoError(t, inventory.ValidateLineQuantity(inventory.OwnerSize, inventory.MaxLineQuantity))
	require.ErrorIs(t, inventory.ValidateLineQuantity(inventory.OwnerSize, inventory.MaxLineQuantity+1), inventory.ErrInvalidQuantity)
	require.NoError(t, inventory.ValidateLineQuantity(inventory.OwnerModifierOption, -inventory.MaxLineQuantity))
	require.ErrorIs(t, inventory.ValidateLineQuantity(inventory.OwnerModifierOption, -inventory.MaxLineQuantity-1), inventory.ErrInvalidQuantity)
}
