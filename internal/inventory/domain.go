package inventory

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// FeatureKey is this extension's Store Feature key.
const FeatureKey = "inventory"

// Capabilities.
const (
	CapAdminister = "inventory.administer"
	CapViewCosts  = "inventory.view_costs"
)

// Ingredient kinds.
const (
	KindRaw      = "RAW"
	KindPrepared = "PREPARED"
)

// Recipe owner kinds.
const (
	OwnerMenuItem       = "MENU_ITEM"
	OwnerSize           = "SIZE"
	OwnerModifierOption = "MODIFIER_OPTION"
	OwnerIngredient     = "INGREDIENT"
)

// Consumption cost statuses.
const (
	CostComplete = "COMPLETE"
	CostPartial  = "PARTIAL"
	CostNone     = "NONE"
)

// Idempotency operations (idempotency_keys.action is VARCHAR(50)).
const (
	OpCreateIngredient   = "inventory.create_ingredient"
	OpRenameIngredient   = "inventory.rename_ingredient"
	OpRetireIngredient   = "inventory.retire_ingredient"
	OpSetIngredientUnits = "inventory.set_ingredient_units"
	OpRecordCost         = "inventory.record_cost"
	OpSetBatchRecipe     = "inventory.set_batch_recipe"
	OpSetRecipe          = "inventory.set_recipe"
)

// Audit events.
const (
	EventIngredientCreated   = "INVENTORY_INGREDIENT_CREATED"
	EventIngredientRenamed   = "INVENTORY_INGREDIENT_RENAMED"
	EventIngredientRetired   = "INVENTORY_INGREDIENT_RETIRED"
	EventIngredientUnitsSet  = "INVENTORY_INGREDIENT_UNITS_SET"
	EventCostRecorded        = "INVENTORY_COST_RECORDED"
	EventBatchRecipeSet      = "INVENTORY_BATCH_RECIPE_SET"
	EventRecipeSet           = "INVENTORY_RECIPE_SET"
	EventAuthorizationDenied = "inventory.authorization_denied"
)

// Limits.
const (
	MaxIngredientNameLength = 120
	MaxUnitNameLength       = 40
	MaxRecipeLines          = 50
)

// OwnerKindFromPath maps a recipe route segment to an owner kind.
func OwnerKindFromPath(segment string) (string, bool) {
	switch segment {
	case "menu-item":
		return OwnerMenuItem, true
	case "size":
		return OwnerSize, true
	case "modifier-option":
		return OwnerModifierOption, true
	}
	return "", false
}

// NormalizeName trims and collapses internal whitespace.
func NormalizeName(s string) string { return strings.Join(strings.Fields(s), " ") }

// ValidateName checks a normalized name against [1, maxLen] code points.
func ValidateName(name string, maxLen int) error {
	n := utf8.RuneCountInString(name)
	if n < 1 || n > maxLen {
		return fmt.Errorf("%w: must be 1 to %d characters", ErrInvalidName, maxLen)
	}
	return nil
}

// ValidateLineQuantity checks a recipe line quantity's sign for its owner
// kind, after rounding to the stored 3 decimals.
func ValidateLineQuantity(ownerKind string, q float64) error {
	r := Round3(q)
	if ownerKind == OwnerModifierOption {
		if r == 0 {
			return fmt.Errorf("%w: an option line must not be zero", ErrInvalidQuantity)
		}
		return nil
	}
	if r <= 0 {
		return fmt.Errorf("%w: must be positive", ErrInvalidQuantity)
	}
	return nil
}
