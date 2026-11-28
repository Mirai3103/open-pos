-- Inventory Extension queries. Every name starts with Inventory.

-- name: InventoryCreateIngredient :one
INSERT INTO inventory_ingredients (name, kind, stock_unit_code)
VALUES ($1, $2, $3)
RETURNING *;

-- name: InventoryGetIngredient :one
SELECT * FROM inventory_ingredients WHERE id = $1;

-- name: InventoryLockIngredient :one
SELECT * FROM inventory_ingredients WHERE id = $1 FOR UPDATE;

-- name: InventoryRenameIngredient :one
UPDATE inventory_ingredients SET name = $2 WHERE id = $1 RETURNING *;

-- name: InventoryRetireIngredient :one
UPDATE inventory_ingredients SET retired_at = now() WHERE id = $1 RETURNING *;

-- name: InventoryListIngredients :many
SELECT * FROM inventory_ingredients
ORDER BY (retired_at IS NOT NULL), lower(name);

-- name: InventoryListIngredientUnits :many
SELECT * FROM inventory_ingredient_units
WHERE ingredient_id = ANY(sqlc.arg(ingredient_ids)::uuid[])
ORDER BY ingredient_id, lower(name);

-- name: InventoryGetIngredientUnit :one
SELECT * FROM inventory_ingredient_units WHERE id = $1 AND ingredient_id = $2;

-- name: InventoryInsertIngredientUnit :one
INSERT INTO inventory_ingredient_units (ingredient_id, name, stock_quantity)
VALUES ($1, $2, $3)
RETURNING *;

-- name: InventoryUpdateIngredientUnit :one
UPDATE inventory_ingredient_units SET name = $3, stock_quantity = $4
WHERE id = $1 AND ingredient_id = $2
RETURNING *;

-- name: InventoryDeleteIngredientUnit :exec
DELETE FROM inventory_ingredient_units WHERE id = $1 AND ingredient_id = $2;

-- name: InventoryIngredientUnitInUse :one
SELECT (EXISTS (SELECT 1 FROM inventory_recipe_lines WHERE ingredient_unit_id = sqlc.arg(id)::uuid)
     OR EXISTS (SELECT 1 FROM inventory_ingredient_costs WHERE ingredient_unit_id = sqlc.arg(id)::uuid))::boolean AS in_use;

-- name: InventoryInsertIngredientCost :one
INSERT INTO inventory_ingredient_costs (ingredient_id, amount_vnd, quantity, unit_code,
                                        ingredient_unit_id, cost_per_stock_unit,
                                        actor_staff_identity_id)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: InventoryListIngredientCosts :many
SELECT * FROM inventory_ingredient_costs WHERE ingredient_id = $1
ORDER BY recorded_at DESC, id DESC;

-- name: InventoryListCurrentCosts :many
SELECT DISTINCT ON (ingredient_id) ingredient_id, cost_per_stock_unit
FROM inventory_ingredient_costs
ORDER BY ingredient_id, recorded_at DESC, id DESC;

-- name: InventoryFindRecipe :one
SELECT id FROM inventory_recipes
WHERE owner_kind = sqlc.arg(owner_kind)::text
  AND (menu_item_id = sqlc.arg(owner_id)::uuid OR size_id = sqlc.arg(owner_id)::uuid
       OR modifier_option_id = sqlc.arg(owner_id)::uuid OR ingredient_id = sqlc.arg(owner_id)::uuid)
FOR UPDATE;

-- name: InventoryGetRecipe :one
SELECT id, yield_quantity FROM inventory_recipes
WHERE owner_kind = sqlc.arg(owner_kind)::text
  AND (menu_item_id = sqlc.arg(owner_id)::uuid OR size_id = sqlc.arg(owner_id)::uuid
       OR modifier_option_id = sqlc.arg(owner_id)::uuid OR ingredient_id = sqlc.arg(owner_id)::uuid);

-- name: InventoryInsertRecipe :one
INSERT INTO inventory_recipes (owner_kind, menu_item_id, size_id, modifier_option_id,
                               ingredient_id, yield_quantity)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id;

-- name: InventoryUpdateRecipeYield :exec
UPDATE inventory_recipes SET yield_quantity = $2, updated_at = now() WHERE id = $1;

-- name: InventoryDeleteRecipe :exec
DELETE FROM inventory_recipes WHERE id = $1;

-- name: InventoryDeleteRecipeLines :exec
DELETE FROM inventory_recipe_lines WHERE recipe_id = $1;

-- name: InventoryInsertRecipeLine :exec
INSERT INTO inventory_recipe_lines (recipe_id, ingredient_id, quantity, unit_code,
                                    ingredient_unit_id, position)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: InventoryListRecipeLines :many
SELECT l.ingredient_id, l.quantity, l.unit_code, l.ingredient_unit_id, l.position,
       i.name AS ingredient_name, i.kind AS ingredient_kind, i.stock_unit_code,
       iu.name AS ingredient_unit_name
FROM inventory_recipe_lines l
JOIN inventory_ingredients i ON i.id = l.ingredient_id
LEFT JOIN inventory_ingredient_units iu ON iu.id = l.ingredient_unit_id
WHERE l.recipe_id = $1
ORDER BY l.position;

-- name: InventoryListBookLines :many
-- Every recipe line with what it needs to be normalized to Stock Units in Go:
-- a system unit's factor or an Ingredient Unit's stock quantity.
SELECT r.owner_kind, r.menu_item_id, r.size_id, r.modifier_option_id,
       r.ingredient_id AS owner_ingredient_id, r.yield_quantity,
       l.ingredient_id, l.quantity, u.factor_to_base, iu.stock_quantity
FROM inventory_recipes r
JOIN inventory_recipe_lines l ON l.recipe_id = r.id
LEFT JOIN inventory_units u ON u.code = l.unit_code
LEFT JOIN inventory_ingredient_units iu ON iu.id = l.ingredient_unit_id
ORDER BY r.id, l.position;

-- name: InventoryGetMenuItemForRecipe :one
SELECT mi.id, mi.retired_at,
       EXISTS (SELECT 1 FROM menu_item_sizes s
               WHERE s.menu_item_id = mi.id AND s.retired_at IS NULL)::boolean AS has_sizes
FROM menu_items mi WHERE mi.id = $1;

-- name: InventoryGetSizeForRecipe :one
SELECT id, menu_item_id, retired_at FROM menu_item_sizes WHERE id = $1;

-- name: InventoryGetOptionForRecipe :one
SELECT id, retired_at FROM modifier_options WHERE id = $1;

-- name: InventoryInsertConsumption :one
INSERT INTO inventory_consumptions (preparation_unit_id, menu_item_id, size_id,
                                    modifier_option_ids, cost_status, cost_total_vnd,
                                    recorded_at)
VALUES (sqlc.arg(preparation_unit_id), sqlc.arg(menu_item_id), sqlc.narg(size_id),
        sqlc.arg(modifier_option_ids)::uuid[], sqlc.arg(cost_status),
        sqlc.arg(cost_total_vnd), sqlc.arg(recorded_at))
ON CONFLICT (preparation_unit_id) DO NOTHING
RETURNING id;

-- name: InventoryInsertConsumptionLine :exec
INSERT INTO inventory_consumption_lines (consumption_id, ingredient_id, ingredient_name,
                                         ingredient_kind, stock_unit_code, quantity,
                                         cost_per_stock_unit, line_cost_vnd)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: InventoryVoidConsumptions :exec
UPDATE inventory_consumptions
SET status = 'VOIDED', voided_at = sqlc.arg(voided_at)::timestamptz
WHERE preparation_unit_id = ANY(sqlc.arg(preparation_unit_ids)::uuid[])
  AND status = 'ACTIVE';

-- name: InventoryListMenuForCosting :many
-- Read-only join into catalog tables (spec §7.1 read-model exception).
SELECT mi.id AS menu_item_id, mi.name AS item_name, mc.id AS category_id,
       mc.name AS category_name, mi.price_vnd,
       s.id AS size_id, s.name AS size_name, s.price_vnd AS size_price_vnd
FROM menu_items mi
JOIN menu_categories mc ON mc.id = mi.category_id
LEFT JOIN menu_item_sizes s ON s.menu_item_id = mi.id AND s.retired_at IS NULL
WHERE mi.retired_at IS NULL AND mc.retired_at IS NULL
ORDER BY mc.name, mi.name, s.price_vnd;

-- name: InventoryListOptionsForCosting :many
SELECT o.id AS modifier_option_id, o.name AS option_name, g.id AS group_id,
       g.name AS group_name, o.surcharge_vnd
FROM modifier_options o
JOIN modifier_groups g ON g.id = o.modifier_group_id
WHERE o.retired_at IS NULL AND g.retired_at IS NULL
ORDER BY g.name, o.name;

-- name: InventoryListGrossProfitUnits :many
-- Read-only join into Core tables (spec §7.2 read-model exception).
SELECT c.preparation_unit_id, c.menu_item_id, c.recorded_at, c.cost_status,
       c.cost_total_vnd, pu.item_name, pu.size_name, pu.priority, pu.state,
       ci.unit_price_vnd
FROM inventory_consumptions c
JOIN preparation_units pu ON pu.id = c.preparation_unit_id
JOIN order_items oi ON oi.id = pu.order_item_id
JOIN committed_items ci ON ci.id = oi.committed_item_id
WHERE c.status = 'ACTIVE'
  AND c.recorded_at >= sqlc.arg(from_at)::timestamptz
  AND c.recorded_at < sqlc.arg(to_at)::timestamptz
ORDER BY c.recorded_at;

-- name: InventoryListUnitComps :many
SELECT preparation_unit_id, amount_vnd FROM charge_adjustments
WHERE kind = 'COMP' AND preparation_unit_id = ANY(sqlc.arg(preparation_unit_ids)::uuid[]);
