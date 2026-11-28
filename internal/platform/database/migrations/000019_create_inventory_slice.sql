-- 000019: Inventory Extension (INV-1). Outside the Core POS: these tables
-- reference Core tables; no Core table references them.

CREATE TABLE IF NOT EXISTS inventory_units (
    code TEXT PRIMARY KEY,                       -- g, kg, ml, l, piece
    dimension TEXT NOT NULL,                     -- MASS, VOLUME, COUNT
    base_code TEXT NOT NULL REFERENCES inventory_units(code),
    factor_to_base NUMERIC(14,3) NOT NULL,
    CONSTRAINT inventory_unit_dimension_valid CHECK (dimension IN ('MASS','VOLUME','COUNT')),
    CONSTRAINT inventory_unit_factor_positive CHECK (factor_to_base > 0)
);
-- seeded: (g,MASS,g,1) (kg,MASS,g,1000) (ml,VOLUME,ml,1) (l,VOLUME,ml,1000) (piece,COUNT,piece,1)

CREATE TABLE IF NOT EXISTS inventory_ingredients (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    kind TEXT NOT NULL,                          -- RAW, PREPARED
    stock_unit_code TEXT NOT NULL REFERENCES inventory_units(code),
    retired_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT inventory_ingredient_kind_valid CHECK (kind IN ('RAW','PREPARED')),
    CONSTRAINT inventory_ingredient_stock_unit_base CHECK (stock_unit_code IN ('g','ml','piece')),
    CONSTRAINT inventory_ingredient_name_valid CHECK (length(btrim(name)) BETWEEN 1 AND 120)
);
CREATE UNIQUE INDEX IF NOT EXISTS inventory_ingredient_active_name_unique
    ON inventory_ingredients (lower(btrim(name))) WHERE retired_at IS NULL;

CREATE TABLE IF NOT EXISTS inventory_ingredient_units (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ingredient_id UUID NOT NULL REFERENCES inventory_ingredients(id) ON DELETE RESTRICT,
    name TEXT NOT NULL,
    stock_quantity NUMERIC(14,3) NOT NULL,       -- Stock Units in one of this unit
    CONSTRAINT inventory_ingredient_unit_positive CHECK (stock_quantity > 0),
    CONSTRAINT inventory_ingredient_unit_name_valid CHECK (length(btrim(name)) BETWEEN 1 AND 40)
);
CREATE UNIQUE INDEX IF NOT EXISTS inventory_ingredient_unit_name_unique
    ON inventory_ingredient_units (ingredient_id, lower(btrim(name)));

CREATE TABLE IF NOT EXISTS inventory_ingredient_costs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ingredient_id UUID NOT NULL REFERENCES inventory_ingredients(id) ON DELETE RESTRICT,
    amount_vnd BIGINT NOT NULL,
    quantity NUMERIC(14,3) NOT NULL,             -- as entered
    unit_code TEXT REFERENCES inventory_units(code),
    ingredient_unit_id UUID REFERENCES inventory_ingredient_units(id) ON DELETE RESTRICT,
    cost_per_stock_unit NUMERIC(14,4) NOT NULL,  -- computed at entry
    actor_staff_identity_id UUID NOT NULL REFERENCES staff_identities(id) ON DELETE RESTRICT,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT inventory_cost_amount_positive CHECK (amount_vnd > 0),
    CONSTRAINT inventory_cost_quantity_positive CHECK (quantity > 0),
    CONSTRAINT inventory_cost_one_unit CHECK ((unit_code IS NULL) <> (ingredient_unit_id IS NULL))
);
CREATE INDEX IF NOT EXISTS inventory_cost_current_index
    ON inventory_ingredient_costs (ingredient_id, recorded_at DESC);

CREATE TABLE IF NOT EXISTS inventory_recipes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_kind TEXT NOT NULL,                    -- MENU_ITEM, SIZE, MODIFIER_OPTION, INGREDIENT
    menu_item_id UUID REFERENCES menu_items(id) ON DELETE RESTRICT,
    size_id UUID REFERENCES menu_item_sizes(id) ON DELETE RESTRICT,
    modifier_option_id UUID REFERENCES modifier_options(id) ON DELETE RESTRICT,
    ingredient_id UUID REFERENCES inventory_ingredients(id) ON DELETE RESTRICT,
    yield_quantity NUMERIC(14,3),                -- INGREDIENT only, in the prepared Ingredient's Stock Units
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT inventory_recipe_owner_valid CHECK (
        (owner_kind = 'MENU_ITEM'       AND menu_item_id IS NOT NULL AND size_id IS NULL AND modifier_option_id IS NULL AND ingredient_id IS NULL) OR
        (owner_kind = 'SIZE'            AND size_id IS NOT NULL AND menu_item_id IS NULL AND modifier_option_id IS NULL AND ingredient_id IS NULL) OR
        (owner_kind = 'MODIFIER_OPTION' AND modifier_option_id IS NOT NULL AND menu_item_id IS NULL AND size_id IS NULL AND ingredient_id IS NULL) OR
        (owner_kind = 'INGREDIENT'      AND ingredient_id IS NOT NULL AND menu_item_id IS NULL AND size_id IS NULL AND modifier_option_id IS NULL)
    ),
    CONSTRAINT inventory_recipe_yield_valid CHECK (
        (owner_kind = 'INGREDIENT' AND yield_quantity > 0) OR
        (owner_kind <> 'INGREDIENT' AND yield_quantity IS NULL)
    )
);
CREATE UNIQUE INDEX IF NOT EXISTS inventory_recipe_item_unique   ON inventory_recipes (menu_item_id)       WHERE menu_item_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS inventory_recipe_size_unique   ON inventory_recipes (size_id)            WHERE size_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS inventory_recipe_option_unique ON inventory_recipes (modifier_option_id) WHERE modifier_option_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS inventory_recipe_batch_unique  ON inventory_recipes (ingredient_id)      WHERE ingredient_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS inventory_recipe_lines (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    recipe_id UUID NOT NULL REFERENCES inventory_recipes(id) ON DELETE CASCADE,
    ingredient_id UUID NOT NULL REFERENCES inventory_ingredients(id) ON DELETE RESTRICT,
    quantity NUMERIC(14,3) NOT NULL,             -- as entered; sign checked in Go by owner kind
    unit_code TEXT REFERENCES inventory_units(code),
    ingredient_unit_id UUID REFERENCES inventory_ingredient_units(id) ON DELETE RESTRICT,
    position INTEGER NOT NULL,
    CONSTRAINT inventory_recipe_line_nonzero CHECK (quantity <> 0),
    CONSTRAINT inventory_recipe_line_one_unit CHECK ((unit_code IS NULL) <> (ingredient_unit_id IS NULL)),
    CONSTRAINT inventory_recipe_line_ingredient_unique UNIQUE (recipe_id, ingredient_id)
);

CREATE TABLE IF NOT EXISTS inventory_consumptions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    preparation_unit_id UUID NOT NULL REFERENCES preparation_units(id) ON DELETE RESTRICT,
    menu_item_id UUID NOT NULL REFERENCES menu_items(id) ON DELETE RESTRICT,
    size_id UUID REFERENCES menu_item_sizes(id) ON DELETE RESTRICT,
    modifier_option_ids UUID[] NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'ACTIVE',
    cost_status TEXT NOT NULL,
    cost_total_vnd NUMERIC(14,2) NOT NULL,       -- sum of costed lines; 0 when NONE
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    voided_at TIMESTAMPTZ,
    CONSTRAINT inventory_consumption_unit_unique UNIQUE (preparation_unit_id),
    CONSTRAINT inventory_consumption_status_valid CHECK (
        (status = 'ACTIVE' AND voided_at IS NULL) OR (status = 'VOIDED' AND voided_at IS NOT NULL)),
    CONSTRAINT inventory_consumption_cost_status_valid CHECK (cost_status IN ('COMPLETE','PARTIAL','NONE'))
);
CREATE INDEX IF NOT EXISTS inventory_consumption_recorded_index
    ON inventory_consumptions (recorded_at) WHERE status = 'ACTIVE';

CREATE TABLE IF NOT EXISTS inventory_consumption_lines (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    consumption_id UUID NOT NULL REFERENCES inventory_consumptions(id) ON DELETE RESTRICT,
    ingredient_id UUID NOT NULL REFERENCES inventory_ingredients(id) ON DELETE RESTRICT,
    ingredient_name TEXT NOT NULL,
    ingredient_kind TEXT NOT NULL,
    stock_unit_code TEXT NOT NULL,
    quantity NUMERIC(14,3) NOT NULL,
    cost_per_stock_unit NUMERIC(14,4),           -- NULL when the Ingredient had no cost
    line_cost_vnd NUMERIC(14,2),
    CONSTRAINT inventory_consumption_line_quantity_positive CHECK (quantity > 0),
    CONSTRAINT inventory_consumption_line_cost_pair CHECK (
        (cost_per_stock_unit IS NULL) = (line_cost_vnd IS NULL))
);
CREATE INDEX IF NOT EXISTS inventory_consumption_line_consumption_index
    ON inventory_consumption_lines (consumption_id);

INSERT INTO inventory_units (code, dimension, base_code, factor_to_base) VALUES
    ('g', 'MASS', 'g', 1), ('ml', 'VOLUME', 'ml', 1), ('piece', 'COUNT', 'piece', 1)
ON CONFLICT (code) DO NOTHING;
INSERT INTO inventory_units (code, dimension, base_code, factor_to_base) VALUES
    ('kg', 'MASS', 'g', 1000), ('l', 'VOLUME', 'ml', 1000)
ON CONFLICT (code) DO NOTHING;
