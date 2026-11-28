package inventory

import (
	"fmt"
	"math"

	"github.com/google/uuid"
)

// SystemUnit is one fixed unit of the system unit table.
type SystemUnit struct {
	Code      string  `json:"code"`
	Dimension string  `json:"dimension"`
	BaseCode  string  `json:"base_code"`
	Factor    float64 `json:"factor_to_base"`
}

var systemUnits = []SystemUnit{
	{Code: "g", Dimension: "MASS", BaseCode: "g", Factor: 1},
	{Code: "kg", Dimension: "MASS", BaseCode: "g", Factor: 1000},
	{Code: "ml", Dimension: "VOLUME", BaseCode: "ml", Factor: 1},
	{Code: "l", Dimension: "VOLUME", BaseCode: "ml", Factor: 1000},
	{Code: "piece", Dimension: "COUNT", BaseCode: "piece", Factor: 1},
}

// SystemUnits returns a copy of the system unit table.
func SystemUnits() []SystemUnit { return append([]SystemUnit(nil), systemUnits...) }

// IsStockUnit reports whether code may be an Ingredient's Stock Unit.
func IsStockUnit(code string) bool { return code == "g" || code == "ml" || code == "piece" }

// SystemFactor returns how many stockUnit one code is.
func SystemFactor(stockUnit, code string) (float64, error) {
	for _, u := range systemUnits {
		if u.Code == code {
			if u.BaseCode != stockUnit {
				return 0, fmt.Errorf("%w: %s is not convertible to %s", ErrUnitNotConvertible, code, stockUnit)
			}
			return u.Factor, nil
		}
	}
	return 0, fmt.Errorf("%w: unknown unit %q", ErrUnitNotConvertible, code)
}

// UnitRef names the unit a quantity was entered in: a system unit code or one
// of the Ingredient's own units, exactly one.
type UnitRef struct {
	UnitCode         *string    `json:"unit_code"`
	IngredientUnitID *uuid.UUID `json:"ingredient_unit_id"`
}

// Validate requires exactly one of the two.
func (u UnitRef) Validate() error {
	if (u.UnitCode == nil) == (u.IngredientUnitID == nil) {
		return fmt.Errorf("%w: exactly one of unit_code and ingredient_unit_id is required",
			ErrUnitNotConvertible)
	}
	return nil
}

func roundTo(v float64, places int) float64 {
	p := math.Pow10(places)
	return math.Round(v*p) / p
}

// Round2 rounds to 2 decimals (line and total costs).
func Round2(v float64) float64 { return roundTo(v, 2) }

// Round3 rounds to 3 decimals (quantities).
func Round3(v float64) float64 { return roundTo(v, 3) }

// Round4 rounds to 4 decimals (cost per Stock Unit).
func Round4(v float64) float64 { return roundTo(v, 4) }
