package inventory

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/Mirai3103/open-pos/internal/platform/command"
	"github.com/Mirai3103/open-pos/internal/response"
	"github.com/jackc/pgx/v5/pgconn"
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

// mapDBError turns driver errors into sentinels by constraint name.
func mapDBError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		switch pgErr.ConstraintName {
		case "inventory_ingredient_active_name_unique":
			return fmt.Errorf("%w: %s", ErrDuplicateIngredientName, pgErr.Detail)
		case "inventory_ingredient_unit_name_unique":
			return fmt.Errorf("%w: %s", ErrDuplicateUnitName, pgErr.Detail)
		}
	}
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrIngredientNotFound, err.Error())
	}
	return err
}

func badRequest(target error, code string) response.ErrorRule {
	return response.ErrorRule{Target: target, Spec: response.ErrorSpec{Status: http.StatusBadRequest, Code: code}}
}

var httpErrors = response.ErrorMapper{
	{Target: ErrIngredientNotFound, Spec: response.ErrorSpec{Status: http.StatusNotFound, Code: "INGREDIENT_NOT_FOUND", Message: ErrIngredientNotFound.Error()}},
	{Target: ErrIngredientUnitNotFound, Spec: response.ErrorSpec{Status: http.StatusNotFound, Code: "INGREDIENT_UNIT_NOT_FOUND"}},
	{Target: ErrRecipeOwnerNotFound, Spec: response.ErrorSpec{Status: http.StatusNotFound, Code: "RECIPE_OWNER_NOT_FOUND"}},
	{Target: ErrIngredientRetired, Spec: response.ErrorSpec{Status: http.StatusConflict, Code: "INGREDIENT_RETIRED"}},
	{Target: ErrIngredientUnitInUse, Spec: response.ErrorSpec{Status: http.StatusConflict, Code: "INGREDIENT_UNIT_IN_USE"}},
	{Target: ErrDuplicateIngredientName, Spec: response.ErrorSpec{Status: http.StatusConflict, Code: "DUPLICATE_INGREDIENT_NAME", Message: ErrDuplicateIngredientName.Error()}},
	{Target: ErrDuplicateUnitName, Spec: response.ErrorSpec{Status: http.StatusConflict, Code: "DUPLICATE_UNIT_NAME", Message: ErrDuplicateUnitName.Error()}},
	badRequest(ErrUnitNotConvertible, "UNIT_NOT_CONVERTIBLE"),
	badRequest(ErrBatchRecipeNotRaw, "BATCH_RECIPE_NOT_RAW"),
	badRequest(ErrBatchRecipeOnRaw, "BATCH_RECIPE_ON_RAW"),
	badRequest(ErrCostOnPrepared, "COST_ON_PREPARED"),
	badRequest(ErrInvalidName, "INVALID_NAME"),
	badRequest(ErrInvalidKind, "INVALID_KIND"),
	badRequest(ErrInvalidYield, "INVALID_YIELD"),
	badRequest(ErrInvalidQuantity, "INVALID_QUANTITY"),
	badRequest(ErrInvalidAmount, "INVALID_AMOUNT"),
	badRequest(ErrInvalidPeriod, "INVALID_PERIOD"),
	badRequest(ErrTooManyLines, "TOO_MANY_LINES"),
	badRequest(ErrDuplicateRecipeIngredient, "DUPLICATE_RECIPE_INGREDIENT"),
	badRequest(ErrItemHasSizes, "ITEM_HAS_SIZES"),
	{Target: ErrRequestConflict, Spec: response.ErrorSpec{Status: http.StatusConflict, Code: "REQUEST_CONFLICT"}},
	{Target: ErrForbidden, Spec: response.ErrorSpec{Status: http.StatusForbidden, Code: "FORBIDDEN"}},
	{Target: ErrUnauthorized, Spec: response.ErrorSpec{Status: http.StatusUnauthorized, Code: "UNAUTHORIZED"}},
	{Target: response.ErrInvalid, Spec: response.ErrorSpec{Status: http.StatusBadRequest, Code: "INVALID_INPUT"}},
}

// MapHTTPError maps inventory errors to *response.CodedError.
func MapHTTPError(err error) error { return httpErrors.Map(err) }
