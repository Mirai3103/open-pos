package hooks

import (
	"testing"

	"github.com/Mirai3103/open-pos/internal/platform/database/sqlc"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestGroupUnitSources(t *testing.T) {
	plain, sized := uuid.New(), uuid.New()
	itemA, itemB, size := uuid.New(), uuid.New(), uuid.New()
	opt1, opt2 := uuid.New(), uuid.New()

	got := groupUnitSources([]sqlc.ListPreparationUnitSourcesRow{
		{OrderItemID: plain, MenuItemID: itemA},
		{OrderItemID: sized, MenuItemID: itemB, SizeID: uuid.NullUUID{UUID: size, Valid: true},
			ModifierOptionID: uuid.NullUUID{UUID: opt1, Valid: true}},
		{OrderItemID: sized, MenuItemID: itemB, SizeID: uuid.NullUUID{UUID: size, Valid: true},
			ModifierOptionID: uuid.NullUUID{UUID: opt2, Valid: true}},
	})

	assert.Equal(t, itemA, got[plain].menuItemID)
	assert.Nil(t, got[plain].sizeID)
	assert.Empty(t, got[plain].optionIDs)
	assert.Equal(t, size, *got[sized].sizeID)
	assert.Equal(t, []uuid.UUID{opt1, opt2}, got[sized].optionIDs)
}
