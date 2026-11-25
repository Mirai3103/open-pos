# Design Specification: Ingredients, Recipes and Costing (`internal/inventory`, INV-1)

- **Date:** 2026-11-25
- **Status:** Approved (brainstorming), pending spec review
- **Sub-project:** INV-1 of the Inventory Extension (INV-1 → INV-4, see §1.1)
- **Predecessors:** [catalog slice](2026-09-10-catalog-slice-design.md), [5D submission and closure](2026-09-15-sales-submission-closure-design.md), [6B preparation corrections](2026-09-17-preparation-corrections-design.md), [6C financial corrections](2026-09-18-preparation-financial-corrections-design.md)

---

## 1. Purpose

Open POS has no notion of what a drink is made of or what it costs to make.
The "Kho & Món Tạm Hết" screen is **Availability** only: an on/off switch, with
no quantities and no deduction. Managers cannot see the cost of a Menu Item,
its gross margin, or how much margin a period of sales actually produced.

INV-1 lets a Manager declare ingredients and their purchase cost, declare the
recipe of every Menu Item, Size, Modifier Option and prepared base, and see the
cost and gross margin of the menu today and of past sales. Every Preparation
Unit records what it consumed and what that cost at the moment it was created,
so past margins do not move when prices change later, and so INV-2 can deduct
stock from the same record.

### 1.1 The Inventory Extension

`CONTEXT.md` scopes the **Core POS** to menu, orders, checkout, payments and
shift reconciliation, and lists *Inventory* under Availability's `_Avoid_`.
Inventory is therefore an **extension outside the Core POS**, in the same way
the Loyalty Program is ([domain-rationale/03](../../domain-rationale/03-define-loyalty-extension-boundary.md)):
the Core POS must keep selling when the extension has no data, wrong data, or
is not wired at all.

Two properties make the extension loosely coupled:

- **The Core POS does not know the extension exists.** Core modules publish
  facts about their own records on a generic in-transaction hook bus (§4).
  Inventory subscribes to them. Removing `internal/inventory` and its one
  registration line in `cmd/api/main.go` leaves the Core POS building and
  behaving exactly as before.
- **The extension is off unless a Manager turns it on.** A store-wide
  **Store Feature** switch (§4.6), set in **Cài đặt**, gates recording, the
  `/inventory` API and the web tabs. Turning it off stops everything and keeps
  the data, and turning it back on resumes from there.

| # | Sub-project | Delivers | Depends on |
|---|---|---|---|
| **INV-1** | Ingredients, Recipes, Costing (this spec) | Ingredient catalog, units, costs, recipes, Consumption Records, menu costing, gross-profit report | — |
| INV-2 | Stock ledger | Stock receipts, automatic deduction from Consumption Records, batch production of prepared bases, current stock, cost from receipts | INV-1 |
| INV-3 | Stock counts | Count sheets, theoretical vs counted variance and its value | INV-2 |
| INV-4 | Low-stock alerts | Minimum levels, alerts, suggested or automatic Availability changes | INV-2 |

Each later sub-project gets its own spec. INV-1's decisions on units, recipe
shape and the Consumption Record are made with INV-2 to INV-4 in mind.

### Goals

1. Manage **Ingredients** (raw and prepared), their stock unit, their own
   units, and the purchase cost of raw ingredients.
2. Manage **Batch Recipes** for prepared ingredients and **Recipes** for Menu
   Items, Sizes and Modifier Options.
3. Record one **Consumption Record** per Preparation Unit, in the transaction
   that creates the unit, voided when the unit is cancelled.
4. Show the current cost and gross margin of every Menu Item, Size and
   Modifier Option.
5. Report gross profit for a period from Consumption Records.
6. Couple the extension to the Core POS only through a generic hook bus, and
   gate it with a store-wide Store Feature switch, off by default.

### Non-Goals

- Stock quantities, receipts, deduction, batch production (INV-2).
- Stock counts and variance (INV-3).
- Alerts and automatic Availability changes (INV-4).
- Updating ingredient costs from receipts (INV-2). INV-1 costs are entered.
- A recipe view for Baristas without costs. Possible later; no capability now.
- Consumption for Outage Recovery Entries. Phase 10 is not built. When it is,
  it publishes its own fact on the hook bus, and inventory decides then whether
  to subscribe.
- Filtering the report by Sales Shift.
- Refunds in gross profit (§7.2).
- Recipe versioning. History lives in Consumption Records.
- Nested prepared ingredients (a prepared base made from another prepared base).

---

## 2. Terminology

These terms are added to `CONTEXT.md` under a new heading
**Inventory Extension**, which states that it lies outside the Core POS.

**Ingredient**:
Something the cafe buys or prepares and uses in drinks or food, tracked in one
Stock Unit. It is either *raw* (bought) or *prepared* (made in batches from raw
ingredients, such as phin coffee base or cooked tapioca pearls).
_Avoid_: Product, SKU, Menu Item, Modifier Option

**Stock Unit**:
The single unit, `g`, `ml` or `piece`, in which an Ingredient's quantities and
cost are stored.
_Avoid_: Purchase unit, display unit

**Ingredient Unit**:
A unit defined for one Ingredient (such as "tbsp", "can", "bag") with a fixed
number of Stock Units, because the same name weighs differently for different
ingredients.
_Avoid_: Global unit, conversion table

**Ingredient Cost**:
The cost in VND of one Stock Unit of a raw Ingredient, derived from an entered
purchase price ("25,000 VND per can"); a prepared Ingredient's cost is derived
from its Batch Recipe instead.
_Avoid_: Selling price, Menu Price

**Batch Recipe**:
The raw Ingredients that one batch of a prepared Ingredient consumes, and the
quantity of the prepared Ingredient that batch yields.
_Avoid_: Recipe, nested recipe

**Recipe**:
The Ingredients that one unit of a Menu Item without Sizes, or one Size,
consumes; for a Modifier Option, the quantities it adds to or removes from that
Recipe.
_Avoid_: Batch Recipe, Preparation Note

**Consumption Record**:
The immutable statement of which Ingredients one Preparation Unit consumed and
what they cost when the unit was created; it is voided, not deleted, when the
unit is cancelled.
_Avoid_: Stock movement, Order Item cost, recalculated cost

The following term is added to the Core POS **Language** section, because the
switch itself belongs to the Core POS even though what it gates does not:

**Store Feature**:
A store-wide, Manager-controlled switch that turns an extension outside the
Core POS on or off for every client at once, retaining who changed it and when;
it is distinct from per-device preferences on the same Settings screen.
_Avoid_: Device preference, build flag, per-staff setting

---

## 3. Domain Rules

### 3.1 Units

- System units are seeded and fixed: `g`, `kg` (MASS); `ml`, `l` (VOLUME);
  `piece` (COUNT). Factors to the dimension's base: `kg` = 1000 `g`,
  `l` = 1000 `ml`.
- An Ingredient's Stock Unit is one of the base units `g`, `ml`, `piece`.
- A quantity may be entered in:
  - a system unit of the **same dimension** as the Stock Unit, or
  - one of that Ingredient's **Ingredient Units**, whose factor is a positive
    number of Stock Units.
- Anything else is rejected (`ErrUnitNotConvertible`).
- An Ingredient's Stock Unit cannot change once any Recipe line, Batch Recipe
  line, cost entry or Consumption line references it. Staff retire it and
  create a new Ingredient.
- Ingredient Unit factors may be edited. Recipes are current state and are
  normalized at read time, so the edit applies to future Consumption Records
  only. The edit is audited.

### 3.2 Ingredient cost

- A raw Ingredient's cost is entered as an amount for a quantity, for example
  `amount_vnd = 25000`, `quantity = 1`, unit `can` (380 g). The cost per Stock
  Unit is computed at entry and stored: `25000 / 380 = 65.7895 VND/g`.
- The current cost is the latest entry. Entries are never edited.
- A raw Ingredient with no entry has **no cost** (not zero).
- A prepared Ingredient has no cost entries. Its cost per Stock Unit is
  `Σ(line stock quantity × raw cost) / yield stock quantity`, computed on read.
  If any line's raw Ingredient has no cost, the prepared Ingredient has no cost.

### 3.3 Batch Recipe

- Only prepared Ingredients have one; a prepared Ingredient with no Batch Recipe
  has no cost.
- Lines reference **raw** Ingredients only (one level), with positive quantities.
- Yield is a positive quantity of the prepared Ingredient.

### 3.4 Recipe

A Recipe belongs to exactly one owner:

| Owner | Rule |
|---|---|
| Menu Item **without** Sizes | One Recipe for the item. |
| **Size** | One full Recipe per Size. A Menu Item with Sizes has no item-level Recipe. |
| **Modifier Option** | Delta lines: positive adds, negative removes. The same delta applies to every Size. |

- Lines reference raw or prepared Ingredients.
- Item and Size lines have positive quantities. Option lines are non-zero.
- One Ingredient appears at most once per Recipe.
- Retired Ingredients cannot be added. Existing lines that reference a
  retired Ingredient stay valid and keep costing.
- If a Menu Item gains its first Size while it has an item-level Recipe, the
  item-level Recipe stays stored but is ignored. The costing view flags the item
  so the Manager can copy it to the Sizes. The catalog is not changed by INV-1.

### 3.5 Composition of one Preparation Unit

Given `(menu_item_id, size_id?, modifier_option_ids[])`:

1. Take the base Recipe: the Size's if `size_id` is set, otherwise the item's.
2. Add every selected Option's delta lines, summed per Ingredient.
3. Clamp each Ingredient's total at zero and drop zero lines.
4. Cost each line at the Ingredient's current cost per Stock Unit.

`cost_status` is:

- `NONE`: there is no base Recipe.
- `PARTIAL`: there is a base Recipe, but at least one resulting line has no
  cost.
- `COMPLETE`: otherwise.

An Option with no Recipe contributes nothing. The costing view flags priced
Options with no Recipe (§7.1).

Composition is a pure function in `internal/inventory/compose.go`. Its inputs
are the loaded Recipes and costs, and its output is the lines, total and status.

---

## 4. Coupling: Hook Bus and Store Feature

### 4.1 Lifecycle

| Core POS fact | Published by | Inventory reaction |
|---|---|---|
| Preparation Units created by Submit | `sales.applySubmitOrder` → `insertOrderItems` | N `ACTIVE` records (when enabled) |
| Preparation Unit created by Remake | `preparation.applyRemake` → `insertRemakeUnit` | One `ACTIVE` record for the new unit (when enabled) |
| Preparation Units cancelled | `preparation.cancelUnitWrites.cancelUnit` | Existing records become `VOIDED`, with `voided_at`. This runs **even when disabled** (§4.6) |
| Waste | — (not published) | None. The record stays `ACTIVE`: ingredients were used |
| Preparation State Correction | — (not published) | None. Corrections only step back along `FULFILLED → READY → IN_PREPARATION → QUEUED` and never touch `CANCELLED` or `WASTED` (`preparation.RequiredPriorState`) |

### 4.2 The hook bus (`internal/platform/hooks`)

A synchronous, in-process bus whose subscribers run **inside the publishing
transaction**. It carries Core POS facts in Core POS language. Nothing in it
mentions inventory, consumption or cost.

```go
package hooks

type CreatedUnit struct {
    PreparationUnitID uuid.UUID
    MenuItemID        uuid.UUID
    SizeID            *uuid.UUID
    ModifierOptionIDs []uuid.UUID
}

type PreparationUnitsCreated struct {
    Origin string        // "SUBMIT" or "REMAKE"
    Units  []CreatedUnit
    At     time.Time
}

type PreparationUnitsCancelled struct {
    PreparationUnitIDs []uuid.UUID
    At                 time.Time
}

type Bus struct { /* subscriber slices; zero value has none */ }

func (b *Bus) OnPreparationUnitsCreated(fn func(context.Context, *sqlc.Queries, PreparationUnitsCreated) error)
func (b *Bus) OnPreparationUnitsCancelled(fn func(context.Context, *sqlc.Queries, PreparationUnitsCancelled) error)

func (b *Bus) PublishPreparationUnitsCreated(ctx context.Context, q *sqlc.Queries, ev PreparationUnitsCreated) error
func (b *Bus) PublishPreparationUnitsCancelled(ctx context.Context, q *sqlc.Queries, ev PreparationUnitsCancelled) error
```

- **Typed events, not `any`.** Each fact has its own `On…` and `Publish…`
  pair. Adding a fact later means adding a pair.
- **A nil or empty bus is a no-op.** The `sales` and `preparation` `Runner`s
  take a `*hooks.Bus` option. Tests and builds that pass none behave exactly
  as today.
- **Subscribers run in registration order** with the mutation's
  `*sqlc.Queries`. The first error is returned to the publisher and aborts the
  transaction.
- **The subscriber contract** (documented on the type): return only database
  errors, never business errors (§4.4).
- `cmd/api/main.go` creates one bus, passes it to the `sales` and `preparation`
  runners, and calls `inventory.Subscribe(bus, featureGate)`. That one line is
  the extension's only wiring.

### 4.3 Building the payload

The publisher fills the payload from its own tables. Subscribers never read
Core POS tables on the write path.

- `InsertPreparationUnit` becomes `:one ... RETURNING id`, so Submit and Remake
  know each new unit's id.
- `committed_items` deliberately has no `size_id`. The publisher resolves it
  through one new query in `sql/queries/sales.sql`:

```sql
-- name: ListPreparationUnitSources :many
SELECT oi.id AS order_item_id, ci.menu_item_id, odi.size_id,
       coalesce(array_agg(cimo.modifier_option_id)
                FILTER (WHERE cimo.modifier_option_id IS NOT NULL),
                '{}')::uuid[] AS modifier_option_ids
FROM order_items oi
JOIN committed_items ci ON ci.id = oi.committed_item_id
JOIN order_draft_items odi ON odi.id = ci.source_draft_item_id
LEFT JOIN committed_item_modifier_options cimo ON cimo.committed_item_id = ci.id
WHERE oi.id = ANY(sqlc.arg(order_item_ids)::uuid[])
GROUP BY oi.id, ci.menu_item_id, odi.size_id;
```

Submit calls it for the new order items. Remake calls it for the source unit's
order item.

### 4.4 Never block a sale

- Inventory's subscribers **return no business errors**. A missing Recipe,
  missing cost, retired Ingredient, retired Menu Item or unknown Size produces a
  record with the matching `cost_status`.
- Only database errors are returned. They abort the transaction exactly as any
  other database error in Submit, Remake or Cancel does today.
- Voiding a unit that has no record is a no-op. Such units come from before
  INV-1 or from a period when the feature was off.
- Recording is idempotent per unit through the unique `preparation_unit_id`
  (`ON CONFLICT DO NOTHING`). The command pipeline's request idempotency already
  prevents replays. This is the database backstop.

### 4.5 What is recorded

- A prepared Ingredient is recorded **as itself** ("40 ml coffee base") at its
  derived cost, not exploded into raw Ingredients. In INV-2, consumption deducts
  the prepared stock, and batch production deducts the raw stock.
- Each line snapshots the Ingredient name, kind and Stock Unit, so later renames
  do not rewrite history.

### 4.6 The Store Feature switch (`internal/platform/features`)

A generic, store-wide switch kept on the server. The `inventory` key is the
first one, and a future Loyalty Program can reuse it.

- `features.Gate` has `Enabled(ctx, q *sqlc.Queries, key string) (bool, error)`.
  It reads `store_features` inside the caller's transaction, so a request sees
  the switch exactly as of its own transaction. An unknown key is off.
- **Default: off.** The migration seeds `('inventory', false)`.
- **When off:**
  - The `PreparationUnitsCreated` subscriber returns immediately. No record is
    written, and the Preparation Units in that period have no Consumption
    Record.
  - The `PreparationUnitsCancelled` subscriber **still voids existing
    records**. A unit recorded while on and cancelled after a Manager turned the
    feature off would otherwise keep a cost it never incurred. This corrects
    existing data and records nothing new.
  - Every `/inventory/*` route returns `403` with code `FEATURE_DISABLED`,
    through a middleware in `inventory.RegisterRoutes`.
  - The web hides the inventory tabs (§8).
  - Ingredients, Recipes, costs and Consumption Records are kept unchanged.
- **When turned back on:** recording resumes from the next Preparation Unit.
  Nothing is backfilled.
- **Changing it:** `PUT /features/:key` with `{ "enabled": bool, "request_id" }`.
  It requires the new Manager-only capability `features.administer`. It is a
  normal audited mutation (`STORE_FEATURE_ENABLED` / `STORE_FEATURE_DISABLED`)
  and needs no fresh Manager PIN. Each change appends a row to
  `store_feature_changes`, which the report reads (§7.2).
- **Reading it:** `GET /features` is available to every authenticated staff
  member and returns `{ key: enabled }`. Clients use it to show or hide
  extension UI.
- `internal/platform/features` is part of the Core POS and knows no extension.
  Keys are plain strings owned by each extension (`inventory.FeatureKey`).

---

## 5. Database

Two migrations, so the platform switch and the extension's tables stay
separate.

### 5.1 `000018_create_store_features.sql` (Core POS platform)

```sql
CREATE TABLE IF NOT EXISTS store_features (
    key TEXT PRIMARY KEY,
    enabled BOOLEAN NOT NULL DEFAULT false,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by_staff_identity_id UUID REFERENCES staff_identities(id) ON DELETE RESTRICT,
    CONSTRAINT store_feature_key_valid CHECK (key ~ '^[a-z][a-z0-9_]{1,39}$')
);

-- Append-only history of every switch change; the gross-profit report reads it.
CREATE TABLE IF NOT EXISTS store_feature_changes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key TEXT NOT NULL REFERENCES store_features(key) ON DELETE RESTRICT,
    enabled BOOLEAN NOT NULL,
    actor_staff_identity_id UUID NOT NULL REFERENCES staff_identities(id) ON DELETE RESTRICT,
    changed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS store_feature_change_key_time_index
    ON store_feature_changes (key, changed_at);

INSERT INTO store_features (key, enabled) VALUES ('inventory', false)
    ON CONFLICT (key) DO NOTHING;
```

### 5.2 `000019_create_inventory_slice.sql` (extension)

Quantities are `NUMERIC(14,3)` in Stock Units. Costs per Stock Unit are
`NUMERIC(14,4)`. Line and record costs are `NUMERIC(14,2)`. Display rounds to
whole VND.

```sql
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
```

The extension references Core POS tables through foreign keys. No Core POS
table references the extension.

Queries live in new `sql/queries/inventory.sql` and `sql/queries/features.sql`
files. `ListPreparationUnitSources` goes in `sql/queries/sales.sql`, because
sales and preparation own the tables it reads.

---

## 6. Commands and Reads

All mutations go through `internal/platform/command` (`ExecuteMutation`,
request-id idempotency, audit) as `catalog` does. Inventory routes are under
`/inventory`, and every one of them is behind the `FEATURE_DISABLED` middleware
(§4.6). The `/features` routes (§4.6) are registered by
`internal/platform/features` and are never gated.

### 6.1 Capabilities

Added to `RoleCapabilities[RoleManager]` in `internal/auth/domain.go` only:

- `features.administer`: `PUT /features/:key`.
- `inventory.administer`: every inventory mutation below.
- `inventory.view_costs`: costing and report reads, and cost fields on
  ingredient reads.

Entering an Ingredient Cost needs no fresh Manager PIN, unlike
`catalog.change_price`: cost is not customer-facing. It is audited.

### 6.2 Mutations (`inventory.administer`)

| Route | Effect | Audit event |
|---|---|---|
| `POST /inventory/ingredients` | Create (name, kind, stock unit) | `INVENTORY_INGREDIENT_CREATED` |
| `PATCH /inventory/ingredients/:id/name` | Rename | `INVENTORY_INGREDIENT_RENAMED` |
| `POST /inventory/ingredients/:id/retirement` | Retire; existing Recipe lines keep it (§3.4) | `INVENTORY_INGREDIENT_RETIRED` |
| `PUT /inventory/ingredients/:id/units` | Replace the Ingredient Units set | `INVENTORY_INGREDIENT_UNITS_SET` |
| `POST /inventory/ingredients/:id/costs` | Append a cost entry (RAW only) | `INVENTORY_COST_RECORDED` |
| `PUT /inventory/ingredients/:id/batch-recipe` | Replace the Batch Recipe and yield (PREPARED only) | `INVENTORY_BATCH_RECIPE_SET` |
| `PUT /inventory/recipes/:owner_kind/:owner_id` | Replace a Recipe's lines (empty list deletes it) | `INVENTORY_RECIPE_SET` |

- `PUT .../units` keeps the ids of units matched by id, adds new ones, and
  rejects removing a unit that any recipe line or cost entry references.
- `PUT` recipe replaces every line in one statement set. The editor always sends
  the whole Recipe.
- The `:owner_kind` path segment is `menu-item`, `size` or `modifier-option`.

### 6.3 Reads

| Route | Capability | Returns |
|---|---|---|
| `GET /inventory/units` | `inventory.administer` | System units |
| `GET /inventory/ingredients` | `inventory.administer` | Ingredients, units; current cost only with `view_costs` |
| `GET /inventory/ingredients/:id` | `inventory.administer` | One Ingredient, its units, cost history, Batch Recipe |
| `GET /inventory/recipes/:owner_kind/:owner_id` | `inventory.administer` | One Recipe with lines in entered units |
| `GET /inventory/costing/menu` | `inventory.view_costs` | §7.1 |
| `GET /inventory/reports/gross-profit?from=&to=&tz_offset_minutes=` | `inventory.view_costs` | §7.2 |

The client sends `from` and `to` as RFC 3339 instants, `[from, to)`. The client
computes local day boundaries, and the server has no time-zone setting.
`tz_offset_minutes` (for example `420` for UTC+7) is used only to bucket the
per-day rows.

---

## 7. Costing and Report

### 7.1 Menu costing (current)

Computed on read from current Recipes and costs with the §3.5 function, using
the menu's non-retired entities:

- For each Menu Item, or each Size if it has Sizes: selling price, cost,
  `margin_vnd = price − cost`, `margin_pct`, and `cost_status`.
- For each Modifier Option: surcharge, cost delta (may be negative), and a flag
  `priced_without_recipe` when the surcharge > 0 and it has no Recipe.
- For each prepared Ingredient: cost per Stock Unit, or no cost.
- An item flag `item_recipe_ignored` (§3.4).

The base cost excludes Option deltas. The UI shows Options separately.

Recipes are owned by `inventory`, not `catalog`: `catalog` is unchanged and
unaware of them. This read joins catalog tables (`menu_items`,
`menu_item_sizes`, `modifier_options`, categories, prices) **read-only**, under
the same read-model exception as §7.2. The recipe foreign keys use
`ON DELETE RESTRICT`. This never blocks a catalog command, because catalog only
retires menu entities and never deletes them (no `DELETE FROM` on those tables
in `sql/queries`).

### 7.2 Gross-profit report (period)

A read-only projection over `ACTIVE` Consumption Records with `recorded_at` in
`[from, to)`:

```text
per unit:
  revenue_vnd = 0                                    if preparation_units.priority = 'REMAKE'
              = committed_items.unit_price_vnd
                − Σ charge_adjustments.amount_vnd    (that unit, kind = 'COMP')
                                                     otherwise
  cost_vnd    = inventory_consumptions.cost_total_vnd
  wasted      = preparation_units.state = 'WASTED'
```

Totals and per-Menu-Item rows (with a per-Size breakdown keyed by the snapshot
`size_name`) and per-day rows report:

- `revenue_vnd`, `cost_vnd`, `gross_profit_vnd`, `gross_margin_pct`
- `waste_cost_vnd`: the cost of wasted units
- `units`, and `units_cost_none` / `units_cost_partial` for the completeness
  warning
- `disabled_intervals`: the parts of `[from, to)` during which the `inventory`
  Store Feature was off, derived from `store_feature_changes`. Units created
  then have no record and are absent from every figure. The UI shows
  "Kho tắt từ … đến …, số liệu không đầy đủ".

Rules:

- **Cancellation** adjustments only exist on cancelled units, whose records are
  `VOIDED`, so they never reach the report.
- **Refunds** are excluded. Gross profit is a management figure, not a
  financial ledger. Shift reconciliation and Payments remain the record of money.
  The report states this in its UI.
- The report joins Core POS tables read-only. The §4 rule that inventory never
  reads Core POS tables applies to the write path only.
- Per-day rows bucket by `recorded_at` shifted by `tz_offset_minutes` (§6.3).
- `committed_items.unit_price_vnd` already includes Modifier Option surcharges,
  so revenue needs no further join to options.

---

## 8. Web

### 8.1 Store Feature switch

- A `useStoreFeatures()` query (`GET /features`, shared cache) feeds a
  `useFeatureEnabled("inventory")` helper. Extension UI renders only when it
  returns true. A `FEATURE_DISABLED` response invalidates the query, so a
  client that missed a switch-off hides the tabs on its next call.
- **Cài đặt** (`features/settings/components/settings-view.tsx`) gains a section
  **"Tính năng cửa hàng"**, rendered only with `features.administer`, with a
  `Switch` row: **"Quản lý kho & giá vốn"**, hint *"Áp dụng cho toàn cửa hàng.
  Khi tắt: ngừng ghi tiêu hao, ẩn các màn hình kho; dữ liệu vẫn được giữ."*
  Turning it off asks for confirmation first.
- The page subtitle changes from "Các tùy chọn chỉ áp dụng cho máy này." to
  name both scopes. The existing sections stay per-device (`usePreferencesStore`)
  and are labelled "Máy này". The new section is labelled "Toàn cửa hàng".

### 8.2 Inventory tabs

Two tabs in **Quản lý** (`web/src/routes/_app/manage/`), shown only when the
feature is on and the capabilities are held, in a new
`web/src/features/inventory/`. When the feature is off, the routes redirect to
`/manage`.

1. **Nguyên liệu** (`manage/ingredients.tsx`)
   - List: name, kind (Thô / Bán thành phẩm), Stock Unit, current cost per
     Stock Unit and per its most common Ingredient Unit, a "no cost" badge.
   - Detail sheet: rename, retire, Ingredient Units editor, cost entry
     ("Giá nhập: [amount] cho [qty] [unit]") with history, Batch Recipe editor
     with yield for prepared Ingredients.
2. **Công thức & Giá vốn** (`manage/recipes.tsx`)
   - Menu grouped by Category. Each item or Size row shows price, cost, margin,
     % and a status badge ("Chưa có công thức", "Thiếu giá NL").
   - Recipe editor: ingredient picker (search by Vietnamese-normalized name, as
     `lib/search` does), quantity, unit picker limited to convertible units,
     live cost per line, and a "Sao chép từ size …" action.
   - A "Topping / Tuỳ chọn" section with delta editors.
   - A **Báo cáo lãi gộp** sub-view: date range (Hôm nay / 7 ngày / Tháng này /
     custom), KPI row, per-item table, per-day chart, completeness warning.

Costing math shown live in the editor reuses the §3.5 rules in a small
`features/inventory/lib/compose.ts`. The server stays authoritative.

---

## 9. Errors

Admin-path errors (`internal/inventory/errors.go`), mapped to 4xx with the
existing response envelope:

| Error | When |
|---|---|
| `ErrIngredientNotFound` / `ErrRecipeOwnerNotFound` | Unknown id |
| `ErrIngredientRetired` | Adding a retired Ingredient to a Recipe or Batch Recipe, or entering its cost |
| `ErrUnitNotConvertible` | A unit that is neither the same dimension nor the Ingredient's own |
| `ErrStockUnitLocked` | Changing the Stock Unit of a referenced Ingredient |
| `ErrIngredientUnitInUse` | Removing an Ingredient Unit that is referenced |
| `ErrBatchRecipeNotRaw` | A Batch Recipe line that is a prepared Ingredient |
| `ErrBatchRecipeOnRaw` / `ErrCostOnPrepared` | Wrong kind for the operation |
| `ErrInvalidYield` / `ErrInvalidQuantity` | Non-positive yield; zero or wrongly signed line quantity |
| `ErrDuplicateRecipeIngredient` | The same Ingredient twice in one Recipe |
| `ErrItemHasSizes` | An item-level Recipe for a Menu Item that has Sizes |
| `ErrDuplicateIngredientName` | Active name clash |
| `ErrFeatureDisabled` (`403`, code `FEATURE_DISABLED`) | Any `/inventory/*` call while the `inventory` Store Feature is off |

`internal/platform/features` adds `ErrUnknownFeature` (`404`) for
`PUT /features/:key` with a key that has no `store_features` row. Keys are
created by migrations, not by the API.

The consumption path has no business errors (§4.4).

---

## 10. Testing

- **Unit (`internal/inventory`)**
  - Unit conversion: same dimension, Ingredient Unit, rejection.
  - Cost per Stock Unit at entry.
  - Prepared cost: complete, and a missing raw cost.
  - Composition: Size vs item base, Option deltas summed, clamp at zero,
    `cost_status` for each case.
  - Admin validation rules in §3 and §9.
- **Integration (postgres template, `-tags=integration`)**
  - Submit with N units writes N `ACTIVE` records, with costs matching
    composition.
  - Cancel sets `VOIDED` and `voided_at`.
  - Waste leaves the record `ACTIVE`.
  - Remake adds a record for the new unit.
  - A Menu Item with no Recipe still submits and records `NONE`.
  - With no bus, the existing sales and preparation suites pass unchanged.
  - A cost change after Submit does not change the stored record.
  - Gross-profit report on a fixed scenario with one Comp + Remake and one
    Cancellation matches hand-computed totals.
  - Capability checks: Cashier and Barista are denied.
- **Hook bus (`internal/platform/hooks`)**
  - Subscribers run in order with the publisher's queries.
  - A subscriber error aborts the publishing transaction.
  - A nil bus publishes nothing.
- **Store Feature (integration)**
  - With `inventory` off (the seeded default), Submit writes no record and
    `/inventory/*` returns `FEATURE_DISABLED`.
  - Turn on → Submit records. Turn off → Cancel of that unit still voids its
    record.
  - Each change writes `store_feature_changes` and an Audit Event.
  - `PUT /features/:key` is denied without `features.administer`.
  - The report returns `disabled_intervals` for a period spanning an off
    interval.
- **Decoupling guard**: a test in `internal/platform/hooks` (or a `go vet`-style
  check in CI) asserts that no package under `internal/sales`,
  `internal/preparation`, `internal/catalog` or `internal/platform` imports
  `internal/inventory`.
- **Web (vitest)**: `compose.ts`, the costing table, the recipe editor's unit
  picker, and the report's completeness warning, following the
  `availability-*.test.tsx` style. Also: the Settings section renders only with
  `features.administer`, and the inventory tabs are hidden when the feature is
  off.

---

## 11. Decisions

1. **Inventory is an extension outside the Core POS.** `CONTEXT.md` scopes it
   out. The extension may depend on the Core POS, never the reverse, and it can
   never stop a sale.
2. **One level of prepared Ingredients.** This covers coffee base, cooked pearls
   and house syrups without recursive costing or cycle checks.
3. **Stock Unit plus Ingredient Units** instead of a global conversion table.
   "1 tbsp" differs per ingredient, so a per-Ingredient factor is the only
   correct one. System units cover kg/g and l/ml.
4. **A full Recipe per Size, fixed deltas per Option.** Per-Size deltas were
   rejected as too much data entry. A Size ratio was rejected because it is
   wrong for cups and straws.
5. **Consumption is anchored on the Preparation Unit, recorded synchronously at
   creation.** The unit is the physical drink. Cancellation (nothing made),
   Waste (made, lost) and Remake (made again) map onto it directly, and INV-2
   deducts stock from the same record. A read-side join was rejected because it
   cannot drive INV-2. A background projector was rejected because it adds
   infrastructure the single-server deployment does not need.
6. **No recipe versioning.** The Consumption Record is the history.
7. **Gross profit excludes Refunds** and is labelled a management figure.
8. **A generic hook bus instead of a `ConsumptionRecorder` interface in the
   Core.** An interface named for consumption would put the extension's concept
   inside `sales` and `preparation`. The bus carries only Core facts, and any
   extension can subscribe. It is typed per fact rather than `any`-based, to
   keep compile-time checks.
9. **A server-side Store Feature, not a device preference.** Recording happens
   on the server, so a per-device switch would let one terminal record and
   another not. The switch is generic (`store_features`) so later extensions
   reuse it.
10. **Off means stopped, data kept, and voids still applied.** Turning the
    feature off stops recording, hides the UI and blocks the API, but never
    deletes data. Voiding existing records on Cancellation continues while off,
    because skipping it would leave permanently wrong costs. Nothing is
    backfilled when the feature is turned back on, and the report names the gap
    instead.
11. **No fresh Manager PIN to toggle.** The switch has no financial effect on
    the Core POS. The Manager role plus an Audit Event is enough.

---

## 12. Delivery

One branch, in this order. Each step is independently green.

1. Platform: `internal/platform/hooks` and `internal/platform/features`
   (migration `000018`, `/features` routes, `features.administer`). Publish
   `PreparationUnitsCreated` / `PreparationUnitsCancelled` from Submit, Remake
   and Cancel, with the `InsertPreparationUnit` change and
   `ListPreparationUnitSources`. No subscribers yet, so behaviour is unchanged.
2. Inventory: migration `000019`, sqlc queries, units seed, domain and
   composition, with unit tests.
3. Inventory admin commands and reads, capabilities, `FEATURE_DISABLED`
   middleware, swagger.
4. Inventory subscribers and their `main.go` registration, with integration
   tests, including the Store Feature cases.
5. Menu costing and gross-profit report reads, including `disabled_intervals`.
6. `CONTEXT.md`: **Store Feature** in Language, plus the Inventory Extension
   section.
7. Web: orval regeneration, the Settings "Tính năng cửa hàng" section, feature
   gating, the Nguyên liệu tab, the Công thức & Giá vốn tab, the report.
