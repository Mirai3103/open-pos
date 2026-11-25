package inventory

import (
	"errors"

	"github.com/Mirai3103/open-pos/internal/platform/command"
)

// Domain sentinels. errors.go's HTTP mapping is added in Task 6.
var (
	ErrIngredientNotFound        = errors.New("ingredient not found")
	ErrIngredientUnitNotFound    = errors.New("ingredient unit not found")
	ErrRecipeOwnerNotFound       = errors.New("recipe owner not found")
	ErrIngredientRetired         = errors.New("ingredient is retired")
	ErrUnitNotConvertible        = errors.New("unit is not convertible to the ingredient's stock unit")
	ErrIngredientUnitInUse       = errors.New("ingredient unit is in use")
	ErrBatchRecipeNotRaw         = errors.New("batch recipe lines must be raw ingredients")
	ErrBatchRecipeOnRaw          = errors.New("only prepared ingredients have a batch recipe")
	ErrCostOnPrepared            = errors.New("prepared ingredient cost is derived from its batch recipe")
	ErrInvalidName               = errors.New("invalid name")
	ErrInvalidKind               = errors.New("invalid ingredient kind")
	ErrInvalidYield              = errors.New("yield must be positive")
	ErrInvalidQuantity           = errors.New("invalid quantity")
	ErrInvalidAmount             = errors.New("amount must be a positive whole VND")
	ErrInvalidPeriod             = errors.New("invalid report period")
	ErrTooManyLines              = errors.New("too many recipe lines")
	ErrDuplicateRecipeIngredient = errors.New("ingredient appears twice in one recipe")
	ErrItemHasSizes              = errors.New("menu item has sizes; set a recipe per size")
	ErrDuplicateIngredientName   = errors.New("an active ingredient already has this name")
	ErrDuplicateUnitName         = errors.New("the ingredient already has a unit with this name")

	ErrRequestConflict = command.ErrRequestConflict
	ErrForbidden       = command.ErrForbidden
	ErrUnauthorized    = command.ErrUnauthorized
)
