//go:build integration

package inventory_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Mirai3103/open-pos/internal/auth"
	"github.com/Mirai3103/open-pos/internal/inventory"
	"github.com/Mirai3103/open-pos/internal/platform/command"
	"github.com/Mirai3103/open-pos/internal/platform/database/sqlc"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func queries() *sqlc.Queries { return sqlc.New(invTestDB) }

func seedActor(t *testing.T, roles ...string) command.Actor {
	t.Helper()
	ctx := context.Background()
	q := queries()
	code := "I" + strings.ReplaceAll(uuid.NewString(), "-", "")[:20]
	hash, err := auth.HashPin("1234")
	require.NoError(t, err)
	row, err := q.CreateStaffIdentity(ctx, sqlc.CreateStaffIdentityParams{
		DisplayName: "Inventory " + code, Btrim: code, PinHash: hash, Enabled: true,
	})
	require.NoError(t, err)
	for _, role := range roles {
		require.NoError(t, q.AddStaffRole(ctx, sqlc.AddStaffRoleParams{StaffIdentityID: row.ID, Role: role}))
	}
	session, err := q.CreateStaffSession(ctx, sqlc.CreateStaffSessionParams{
		TokenHash: "tok_" + uuid.NewString()[:16], StaffIdentityID: row.ID,
		State: auth.SessionStateActive, LastHumanActivityAt: time.Now(),
		ExpiresAt: time.Now().Add(8 * time.Hour),
	})
	require.NoError(t, err)
	return command.Actor{StaffID: row.ID, SessionID: session.ID}
}

func uniqueName(prefix string) string { return prefix + " " + uuid.NewString()[:8] }

func newCommands() *inventory.Commands {
	return inventory.NewCommands(inventory.NewRunner(invTestDB, queries()))
}

func createIngredient(t *testing.T, c *inventory.Commands, actor command.Actor, name, kind, unit string) inventory.IngredientResponse {
	t.Helper()
	_, res, err := c.CreateIngredient(context.Background(), actor, inventory.CreateIngredientCommand{
		RequestID: uuid.New(), Name: name, Kind: kind, StockUnit: unit,
	})
	require.NoError(t, err)
	return res
}

func recordCost(t *testing.T, c *inventory.Commands, actor command.Actor, id uuid.UUID, amount int64, qty float64, unit inventory.UnitRef) inventory.CostEntryResponse {
	t.Helper()
	_, res, err := c.RecordCost(context.Background(), actor, inventory.RecordCostCommand{
		RequestID: uuid.New(), IngredientID: id, AmountVND: amount, Quantity: qty, Unit: unit,
	})
	require.NoError(t, err)
	return res
}

func code(s string) inventory.UnitRef { return inventory.UnitRef{UnitCode: &s} }

type menuFixture struct {
	CategoryID, CoffeeID, MilkTeaID, SizeM, SizeL, PearlOptionID, PlainID uuid.UUID
}

// seedMenu creates a uniquely named category with: a 25,000 VND coffee, a
// sized milk tea (M 35,000 / L 45,000), a recipe-less 20,000 VND plain item,
// and a Topping group (pearls, +5,000) inherited by the category.
func seedMenu(t *testing.T) menuFixture {
	t.Helper()
	var m menuFixture
	db := invTestDB
	scan := func(dst *uuid.UUID, query string, args ...any) {
		require.NoError(t, db.QueryRow(query, args...).Scan(dst))
	}
	scan(&m.CategoryID, `INSERT INTO menu_categories (name, normalized_name) VALUES ($1, lower($1)) RETURNING id`, uniqueName("Đồ uống"))
	item := `INSERT INTO menu_items (category_id, name, normalized_name, price_vnd) VALUES ($1, $2, lower($2), $3) RETURNING id`
	scan(&m.CoffeeID, item, m.CategoryID, uniqueName("Cà phê sữa"), 25000)
	scan(&m.PlainID, item, m.CategoryID, uniqueName("Bánh"), 20000)
	scan(&m.MilkTeaID, `INSERT INTO menu_items (category_id, name, normalized_name) VALUES ($1, $2, lower($2)) RETURNING id`, m.CategoryID, uniqueName("Trà sữa"))
	size := `INSERT INTO menu_item_sizes (menu_item_id, name, normalized_name, price_vnd) VALUES ($1, $2, lower($2), $3) RETURNING id`
	scan(&m.SizeM, size, m.MilkTeaID, "Size M", 35000)
	scan(&m.SizeL, size, m.MilkTeaID, "Size L", 45000)
	var group uuid.UUID
	scan(&group, `INSERT INTO modifier_groups (name, normalized_name) VALUES ($1, lower($1)) RETURNING id`, uniqueName("Topping"))
	scan(&m.PearlOptionID, `INSERT INTO modifier_options (modifier_group_id, name, normalized_name, surcharge_vnd) VALUES ($1, $2, lower($2), 5000) RETURNING id`, group, "Trân châu")
	_, err := db.Exec(`INSERT INTO category_modifier_groups (menu_category_id, modifier_group_id) VALUES ($1, $2)`, m.CategoryID, group)
	require.NoError(t, err)
	return m
}

// ingredientsFixture is the costed set used by the recipe and consumption tests.
type ingredientsFixture struct {
	Powder, Condensed, Base, Tea, Pearls, Sugar inventory.IngredientResponse
	Can                                         uuid.UUID
}

// seedIngredients creates and costs: coffee powder 250 VND/g, condensed milk
// 65.7895 VND/g (25,000 per 380 g can), coffee base 62.5 VND/ml (250 g powder
// → 1000 ml), tea 20 VND/ml, pearls 60 VND/g, and uncosted sugar.
func seedIngredients(t *testing.T, c *inventory.Commands, manager command.Actor) ingredientsFixture {
	t.Helper()
	ctx := context.Background()
	var f ingredientsFixture
	f.Powder = createIngredient(t, c, manager, uniqueName("Bột cà phê"), inventory.KindRaw, "g")
	f.Condensed = createIngredient(t, c, manager, uniqueName("Sữa đặc"), inventory.KindRaw, "g")
	f.Base = createIngredient(t, c, manager, uniqueName("Cốt cà phê"), inventory.KindPrepared, "ml")
	f.Tea = createIngredient(t, c, manager, uniqueName("Trà"), inventory.KindRaw, "ml")
	f.Pearls = createIngredient(t, c, manager, uniqueName("Trân châu"), inventory.KindRaw, "g")
	f.Sugar = createIngredient(t, c, manager, uniqueName("Đường"), inventory.KindRaw, "g")

	_, withCan, err := c.SetIngredientUnits(ctx, manager, inventory.SetIngredientUnitsCommand{
		RequestID: uuid.New(), IngredientID: f.Condensed.ID,
		Units: []inventory.IngredientUnitInput{{Name: "hộp", StockQuantity: 380}},
	})
	require.NoError(t, err)
	f.Can = withCan.Units[0].ID

	recordCost(t, c, manager, f.Powder.ID, 250000, 1, code("kg"))
	recordCost(t, c, manager, f.Condensed.ID, 25000, 1, inventory.UnitRef{IngredientUnitID: &f.Can})
	recordCost(t, c, manager, f.Tea.ID, 20000, 1, code("l"))
	recordCost(t, c, manager, f.Pearls.ID, 60000, 1, code("kg"))

	_, _, err = c.SetBatchRecipe(ctx, manager, inventory.SetBatchRecipeCommand{
		RequestID: uuid.New(), IngredientID: f.Base.ID, YieldQuantity: 1000,
		Lines: []inventory.RecipeLineInput{{IngredientID: f.Powder.ID, Quantity: 250, Unit: code("g")}},
	})
	require.NoError(t, err)
	return f
}

func setRecipe(t *testing.T, c *inventory.Commands, manager command.Actor, kind string, owner uuid.UUID, lines ...inventory.RecipeLineInput) {
	t.Helper()
	_, _, err := c.SetRecipe(context.Background(), manager, inventory.SetRecipeCommand{
		RequestID: uuid.New(), OwnerKind: kind, OwnerID: owner, Lines: lines,
	})
	require.NoError(t, err)
}

func line(id uuid.UUID, qty float64, unit string) inventory.RecipeLineInput {
	return inventory.RecipeLineInput{IngredientID: id, Quantity: qty, Unit: code(unit)}
}

// seedRecipes gives the menu fixture its recipes: coffee 40 ml base + 30 g
// condensed (4,473.69), M 200 ml tea + 10 g sugar (PARTIAL 4,000), L 280 ml
// tea (5,600), pearls +50 g (3,000). The plain item has no recipe.
func seedRecipes(t *testing.T, c *inventory.Commands, manager command.Actor, m menuFixture, f ingredientsFixture) {
	t.Helper()
	setRecipe(t, c, manager, inventory.OwnerMenuItem, m.CoffeeID, line(f.Base.ID, 40, "ml"), line(f.Condensed.ID, 30, "g"))
	setRecipe(t, c, manager, inventory.OwnerSize, m.SizeM, line(f.Tea.ID, 200, "ml"), line(f.Sugar.ID, 10, "g"))
	setRecipe(t, c, manager, inventory.OwnerSize, m.SizeL, line(f.Tea.ID, 280, "ml"))
	setRecipe(t, c, manager, inventory.OwnerModifierOption, m.PearlOptionID, line(f.Pearls.ID, 50, "g"))
}
