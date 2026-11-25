# INV-1 Backend Implementation Plan: Ingredients, Recipes, Costing

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the backend of INV-1. That means a generic Core hook bus, a store-wide Store Feature switch, and the `internal/inventory` extension: ingredients, units, costs, recipes, per-Preparation-Unit Consumption Records, menu costing, and the gross-profit report. All of it sits behind the `/inventory` API, gated by the `inventory` Store Feature.

**Architecture:** Core modules (`sales`, `preparation`) publish facts on `internal/platform/hooks`, which runs subscribers inside the publishing transaction. `internal/platform/features` holds store-wide switches in `store_features`. `internal/inventory` subscribes to the bus and checks its switch, so nothing in the Core imports it. All commands go through `internal/platform/command`, as every slice does.

**Tech Stack:** Go 1.27, Echo v4, PostgreSQL via pgx stdlib, sqlc (`database/sql`), swag, and testify with integration tests under `-tags=integration` using `internal/testdb`.

**Spec:** `docs/superpowers/specs/2026-11-25-inv-1-ingredients-recipes-costing-design.md`

**Scope note:** This is Plan 1 of 2. The web work (spec §8: Settings section, feature gating, the Nguyên liệu tab, the Công thức & Giá vốn tab, the report UI) is Plan 2. It will be written after this plan lands, because it depends on the orval-generated names produced by Task 12's swagger regeneration.

## Global Constraints

- Inventory is outside the Core POS. No package under `internal/sales`, `internal/preparation`, `internal/catalog`, `internal/tables`, `internal/shift`, `internal/auth` or `internal/platform` may import `internal/inventory` (spec §1.1, §11.8).
- Inventory subscribers return **only database errors**, never business errors. A missing recipe or cost produces `cost_status` `NONE` or `PARTIAL` (spec §4.4).
- The `inventory` Store Feature is seeded **off**. While off: nothing is recorded, every `/inventory/*` route answers `403` with code `FEATURE_DISABLED`, and cancellations **still void** existing records (spec §4.6).
- Units: system units are `g`, `kg`, `ml`, `l` and `piece`. Stock Units are `g`, `ml` and `piece` only (spec §3.1).
- Prepared ingredients are one level deep. Batch Recipe lines are RAW only (spec §3.3).
- A Menu Item with Sizes has no item-level Recipe. Option lines are deltas: non-zero, summed, and the total clamped at 0 (spec §3.4, §3.5).
- Precision: quantities are `NUMERIC(14,3)`, cost per Stock Unit `NUMERIC(14,4)`, line and total costs `NUMERIC(14,2)`. Go uses `float64` (sqlc override, Task 4) and rounds with `Round3`, `Round4` and `Round2` before every write.
- Capabilities `features.administer`, `inventory.administer` and `inventory.view_costs` go to `RoleManager` only.
- Migrations: `000018_create_store_features.sql` (platform) and `000019_create_inventory_slice.sql` (extension).
- Every inventory sqlc query name starts with `Inventory`.
- A sqlc query uses either positional `$n` parameters or `sqlc.arg`/`sqlc.narg`, never both (sqlc rejects mixed queries).
- Run integration tests with `TEST_DATABASE_URL` set as in the Makefile (`make docker-up` first). Commands below use `make`-equivalent invocations.
- Commit after every task, with messages ending in `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## File Map

| File | Responsibility | Task |
|---|---|---|
| `internal/platform/hooks/hooks.go` | Bus, fact types, publish | 1 |
| `internal/platform/hooks/sources.go` | Resolve Order Items to catalog identities and publish created units | 2 |
| `internal/platform/command/command.go`, `mutation.go` | `Runner.SetHooks/Hooks`, `MutationContext.Hooks` | 1 |
| `sql/queries/sales.sql` | `InsertPreparationUnit :one`, `ListPreparationUnitSources` | 2 |
| `internal/sales/submit.go` | Publish on Submit | 2 |
| `internal/preparation/remake.go`, `cancel.go` | Publish on Remake and Cancel | 2 |
| `internal/platform/database/migrations/000018_create_store_features.sql` | Store Feature tables | 3 |
| `sql/queries/features.sql` | Feature queries | 3 |
| `internal/platform/features/*.go` | Switch, `Enabled`, `OffIntervals`, middleware, routes | 3 |
| `internal/auth/domain.go` | New capabilities | 3, 6 |
| `internal/testdb/config.go` | Allow `features` and `inventory` | 3, 4 |
| `cmd/api/main.go` | Wiring | 3, 8, 9 |
| `internal/platform/database/migrations/000019_create_inventory_slice.sql` | Inventory tables | 4 |
| `sqlc.yaml` | `numeric` → `float64` | 4 |
| `sql/queries/inventory.sql` | All inventory queries | 4 |
| `internal/inventory/domain.go`, `units.go`, `book.go`, `errors.go` | Pure domain | 5 |
| `internal/inventory/executor.go`, `dto.go`, `ingredients.go` | Ingredient commands | 6 |
| `internal/inventory/recipes.go`, `loadbook.go` | Recipe commands, book loading | 7 |
| `internal/inventory/reads.go`, `http.go`, `routes.go` | Reads and HTTP | 8 |
| `internal/inventory/consumption.go` | Hook subscribers | 9 |
| `internal/inventory/costing.go` | Menu costing | 10 |
| `internal/inventory/report.go` | Gross-profit report | 11 |
| `internal/inventory/decoupling_test.go`, `CONTEXT.md`, `api/openapi/*` | Guard, glossary, swagger | 12 |

---

### Task 1: Hook bus and command wiring

**Files:**
- Create: `internal/platform/hooks/hooks.go`
- Create: `internal/platform/hooks/hooks_test.go`
- Create: `internal/platform/command/hooks_test.go`
- Modify: `internal/platform/command/command.go` (the `Runner` struct and `NewRunner`, around lines 52-70)
- Modify: `internal/platform/command/mutation.go` (`MutationContext`, around line 68; step 9 call, around line 196)

**Interfaces:**
- Produces:
  - `hooks.New() *hooks.Bus`
  - `(*Bus).OnPreparationUnitsCreated(CreatedHandler)`
  - `(*Bus).OnPreparationUnitsCancelled(CancelledHandler)`
  - `(*Bus).PublishPreparationUnitsCreated(ctx, *sqlc.Queries, PreparationUnitsCreated) error`
  - `(*Bus).PublishPreparationUnitsCancelled(ctx, *sqlc.Queries, PreparationUnitsCancelled) error`
  - Types `CreatedUnit`, `PreparationUnitsCreated`, `PreparationUnitsCancelled`; constants `OriginSubmit`, `OriginRemake`
  - `(*command.Runner).SetHooks(*hooks.Bus)`, `(*command.Runner).Hooks() *hooks.Bus`
  - `command.MutationContext.Hooks *hooks.Bus`

- [ ] **Step 1: Write the failing bus tests**

`internal/platform/hooks/hooks_test.go`:

```go
package hooks_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Mirai3103/open-pos/internal/platform/database/sqlc"
	"github.com/Mirai3103/open-pos/internal/platform/hooks"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createdEvent() hooks.PreparationUnitsCreated {
	return hooks.PreparationUnitsCreated{
		Origin: hooks.OriginSubmit,
		Units:  []hooks.CreatedUnit{{PreparationUnitID: uuid.New(), MenuItemID: uuid.New()}},
		At:     time.Now(),
	}
}

func TestNilBusPublishesNothing(t *testing.T) {
	var bus *hooks.Bus
	require.NoError(t, bus.PublishPreparationUnitsCreated(context.Background(), nil, createdEvent()))
	require.NoError(t, bus.PublishPreparationUnitsCancelled(context.Background(), nil,
		hooks.PreparationUnitsCancelled{PreparationUnitIDs: []uuid.UUID{uuid.New()}}))
}

func TestSubscribersRunInRegistrationOrder(t *testing.T) {
	bus := hooks.New()
	var calls []string
	bus.OnPreparationUnitsCreated(func(context.Context, *sqlc.Queries, hooks.PreparationUnitsCreated) error {
		calls = append(calls, "first")
		return nil
	})
	bus.OnPreparationUnitsCreated(func(context.Context, *sqlc.Queries, hooks.PreparationUnitsCreated) error {
		calls = append(calls, "second")
		return nil
	})
	require.NoError(t, bus.PublishPreparationUnitsCreated(context.Background(), nil, createdEvent()))
	assert.Equal(t, []string{"first", "second"}, calls)
}

func TestFirstSubscriberErrorStopsAndIsWrapped(t *testing.T) {
	bus := hooks.New()
	boom := errors.New("boom")
	secondRan := false
	bus.OnPreparationUnitsCancelled(func(context.Context, *sqlc.Queries, hooks.PreparationUnitsCancelled) error {
		return boom
	})
	bus.OnPreparationUnitsCancelled(func(context.Context, *sqlc.Queries, hooks.PreparationUnitsCancelled) error {
		secondRan = true
		return nil
	})
	err := bus.PublishPreparationUnitsCancelled(context.Background(), nil,
		hooks.PreparationUnitsCancelled{PreparationUnitIDs: []uuid.UUID{uuid.New()}})
	require.ErrorIs(t, err, boom)
	assert.False(t, secondRan)
}

func TestEmptyEventsAreNotDelivered(t *testing.T) {
	bus := hooks.New()
	called := false
	bus.OnPreparationUnitsCreated(func(context.Context, *sqlc.Queries, hooks.PreparationUnitsCreated) error {
		called = true
		return nil
	})
	require.NoError(t, bus.PublishPreparationUnitsCreated(context.Background(), nil,
		hooks.PreparationUnitsCreated{Origin: hooks.OriginSubmit}))
	assert.False(t, called)
}
```

`internal/platform/command/hooks_test.go`:

```go
package command_test

import (
	"testing"

	"github.com/Mirai3103/open-pos/internal/platform/command"
	"github.com/Mirai3103/open-pos/internal/platform/hooks"
	"github.com/stretchr/testify/assert"
)

func TestRunnerCarriesHooks(t *testing.T) {
	r := command.NewRunner(nil, nil, command.Policy{DenialEventType: "probe.authorization_denied"})
	assert.Nil(t, r.Hooks())

	bus := hooks.New()
	r.SetHooks(bus)
	assert.Same(t, bus, r.Hooks())
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/platform/hooks/ ./internal/platform/command/ -run 'Bus|Subscriber|Empty|RunnerCarriesHooks'`
Expected: FAIL, with a compile error because package `hooks` does not exist.

- [ ] **Step 3: Implement the bus**

`internal/platform/hooks/hooks.go`:

```go
// Package hooks is the Core POS's in-transaction extension point.
//
// Core modules publish facts about their own records; extensions outside the
// Core POS subscribe to them. Subscribers run synchronously inside the
// publishing transaction with its queries, so whatever they write commits or
// rolls back with the Core change. Facts are stated in Core POS language
// only: this package knows no extension.
//
// Subscriber contract: return only database errors. A business problem inside
// an extension (missing data, unknown ids) must never fail a Core command.
package hooks

import (
	"context"
	"fmt"
	"time"

	"github.com/Mirai3103/open-pos/internal/platform/database/sqlc"
	"github.com/google/uuid"
)

// Origins of PreparationUnitsCreated.
const (
	OriginSubmit = "SUBMIT"
	OriginRemake = "REMAKE"
)

// CreatedUnit is one new Preparation Unit and the catalog identities its
// Order Item was sold as.
type CreatedUnit struct {
	PreparationUnitID uuid.UUID
	MenuItemID        uuid.UUID
	SizeID            *uuid.UUID
	ModifierOptionIDs []uuid.UUID
}

// PreparationUnitsCreated states that Submit or Remake created units.
type PreparationUnitsCreated struct {
	Origin string
	Units  []CreatedUnit
	At     time.Time
}

// PreparationUnitsCancelled states that units were cancelled before
// preparation began.
type PreparationUnitsCancelled struct {
	PreparationUnitIDs []uuid.UUID
	At                 time.Time
}

// CreatedHandler reacts to PreparationUnitsCreated.
type CreatedHandler func(context.Context, *sqlc.Queries, PreparationUnitsCreated) error

// CancelledHandler reacts to PreparationUnitsCancelled.
type CancelledHandler func(context.Context, *sqlc.Queries, PreparationUnitsCancelled) error

// Bus holds subscribers. Register every subscriber during wiring, before the
// server serves: registration is not synchronized with publishing. A nil *Bus
// is valid and publishes nothing.
type Bus struct {
	created   []CreatedHandler
	cancelled []CancelledHandler
}

// New returns an empty Bus.
func New() *Bus { return &Bus{} }

// OnPreparationUnitsCreated registers fn.
func (b *Bus) OnPreparationUnitsCreated(fn CreatedHandler) { b.created = append(b.created, fn) }

// OnPreparationUnitsCancelled registers fn.
func (b *Bus) OnPreparationUnitsCancelled(fn CancelledHandler) {
	b.cancelled = append(b.cancelled, fn)
}

// PublishPreparationUnitsCreated runs every created subscriber in
// registration order and returns the first error.
func (b *Bus) PublishPreparationUnitsCreated(ctx context.Context, q *sqlc.Queries,
	ev PreparationUnitsCreated,
) error {
	if b == nil || len(ev.Units) == 0 {
		return nil
	}
	for _, fn := range b.created {
		if err := fn(ctx, q, ev); err != nil {
			return fmt.Errorf("preparation units created subscriber: %w", err)
		}
	}
	return nil
}

// PublishPreparationUnitsCancelled runs every cancelled subscriber in
// registration order and returns the first error.
func (b *Bus) PublishPreparationUnitsCancelled(ctx context.Context, q *sqlc.Queries,
	ev PreparationUnitsCancelled,
) error {
	if b == nil || len(ev.PreparationUnitIDs) == 0 {
		return nil
	}
	for _, fn := range b.cancelled {
		if err := fn(ctx, q, ev); err != nil {
			return fmt.Errorf("preparation units cancelled subscriber: %w", err)
		}
	}
	return nil
}

func (b *Bus) hasCreatedSubscribers() bool { return b != nil && len(b.created) > 0 }
```

- [ ] **Step 4: Wire hooks into the command pipeline**

In `internal/platform/command/command.go`, add the import `"github.com/Mirai3103/open-pos/internal/platform/hooks"`. Then add the field and two methods:

```go
// Runner holds the database connection, the generated queries, the owning
// slice's Policy, and the Core hook bus mutations publish on.
type Runner struct {
	db      *sql.DB
	queries *sqlc.Queries
	policy  Policy
	hooks   *hooks.Bus
}
```

```go
// SetHooks attaches the Core hook bus. Call it during wiring, before serving.
func (r *Runner) SetHooks(b *hooks.Bus) { r.hooks = b }

// Hooks returns the attached hook bus, or nil when none is attached.
func (r *Runner) Hooks() *hooks.Bus { return r.hooks }
```

In `internal/platform/command/mutation.go`, add the same import. Extend `MutationContext`:

```go
	// Hooks is the Runner's Core hook bus, nil when none is attached. Bodies
	// publish Core facts on it with the transaction's Queries.
	Hooks *hooks.Bus
```

Change step 9 to pass it:

```go
	resultCode, result, audit, err := fn(MutationContext{
		Queries: q, Tx: tx, Actor: actor, Approver: approver, Hooks: r.hooks,
	})
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/platform/hooks/ ./internal/platform/command/`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/platform/hooks internal/platform/command
git commit -m "feat(platform): add in-transaction Core hook bus

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Publish Core facts from Submit, Remake and Cancel

**Files:**
- Modify: `sql/queries/sales.sql:792-796` (`InsertPreparationUnit`), and append `ListPreparationUnitSources`
- Regenerate: `internal/platform/database/sqlc/*` (`make sqlc`)
- Create: `internal/platform/hooks/sources.go`
- Create: `internal/platform/hooks/sources_internal_test.go`
- Modify: `internal/sales/submit.go` (`Handle` body around line 47, `applySubmitOrder`, `insertOrderItems`)
- Modify: `internal/preparation/remake.go` (`Handle` around line 131, `applyRemake`)
- Modify: `internal/preparation/cancel.go` (`Handle` around line 79, `applyCancelUnits`)
- Test: `internal/preparation/hooks_integration_test.go`

**Interfaces:**
- Consumes: Task 1's `hooks.Bus`, `MutationContext.Hooks`
- Produces:
  - `hooks.UnitRef{PreparationUnitID, OrderItemID uuid.UUID}`
  - `(*hooks.Bus).PublishCreatedUnits(ctx, q *sqlc.Queries, origin string, refs []hooks.UnitRef, at time.Time) error`
  - `sqlc.ListPreparationUnitSourcesRow{OrderItemID uuid.UUID; MenuItemID uuid.UUID; SizeID uuid.NullUUID; ModifierOptionID uuid.NullUUID}`

- [ ] **Step 1: Change and add the queries**

In `sql/queries/sales.sql`, replace the `InsertPreparationUnit` block:

```sql
-- name: InsertPreparationUnit :one
INSERT INTO preparation_units (order_item_id, unit_number, service_number,
                               category_name, item_name, size_name,
                               modifiers, preparation_note, queued_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING id;
```

Append:

```sql
-- name: ListPreparationUnitSources :many
-- The catalog identities each Order Item was sold as, one row per selected
-- Modifier Option (one row with a NULL option when none), for Core hook
-- payloads. committed_items deliberately has no size_id; the source draft
-- item still holds it.
SELECT oi.id AS order_item_id, ci.menu_item_id, odi.size_id,
       cimo.modifier_option_id
FROM order_items oi
JOIN committed_items ci ON ci.id = oi.committed_item_id
JOIN order_draft_items odi ON odi.id = ci.source_draft_item_id
LEFT JOIN committed_item_modifier_options cimo ON cimo.committed_item_id = ci.id
WHERE oi.id = ANY(sqlc.arg(order_item_ids)::uuid[])
ORDER BY oi.id, cimo.modifier_option_id;
```

Run: `make sqlc`
Expected: these signatures appear in `internal/platform/database/sqlc/sales.sql.go`:
- `InsertPreparationUnit(ctx, arg) (uuid.UUID, error)`
- `ListPreparationUnitSources(ctx, orderItemIds []uuid.UUID) ([]ListPreparationUnitSourcesRow, error)`

`SizeID` and `ModifierOptionID` should be `uuid.NullUUID`. `go build ./...` now fails in `internal/sales/submit.go`; Step 4 fixes that.

- [ ] **Step 2: Write the failing source-grouping test**

`internal/platform/hooks/sources_internal_test.go`:

```go
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
```

Run: `go test ./internal/platform/hooks/ -run TestGroupUnitSources`
Expected: FAIL, because `groupUnitSources` is undefined.

- [ ] **Step 3: Implement `sources.go`**

`internal/platform/hooks/sources.go`:

```go
package hooks

import (
	"context"
	"fmt"
	"time"

	"github.com/Mirai3103/open-pos/internal/platform/database/sqlc"
	"github.com/google/uuid"
)

// UnitRef names a freshly inserted Preparation Unit and its Order Item.
type UnitRef struct {
	PreparationUnitID uuid.UUID
	OrderItemID       uuid.UUID
}

// PublishCreatedUnits resolves refs to the catalog identities their Order
// Items were sold as and publishes PreparationUnitsCreated. It reads nothing
// when no subscriber listens, so a build without extensions pays nothing.
func (b *Bus) PublishCreatedUnits(ctx context.Context, q *sqlc.Queries, origin string,
	refs []UnitRef, at time.Time,
) error {
	if !b.hasCreatedSubscribers() || len(refs) == 0 {
		return nil
	}
	seen := make(map[uuid.UUID]struct{}, len(refs))
	ids := make([]uuid.UUID, 0, len(refs))
	for _, ref := range refs {
		if _, ok := seen[ref.OrderItemID]; !ok {
			seen[ref.OrderItemID] = struct{}{}
			ids = append(ids, ref.OrderItemID)
		}
	}
	rows, err := q.ListPreparationUnitSources(ctx, ids)
	if err != nil {
		return fmt.Errorf("list preparation unit sources: %w", err)
	}
	sources := groupUnitSources(rows)

	units := make([]CreatedUnit, 0, len(refs))
	for _, ref := range refs {
		src, ok := sources[ref.OrderItemID]
		if !ok {
			return fmt.Errorf("preparation unit source for order item %s not found", ref.OrderItemID)
		}
		units = append(units, CreatedUnit{
			PreparationUnitID: ref.PreparationUnitID,
			MenuItemID:        src.menuItemID,
			SizeID:            src.sizeID,
			ModifierOptionIDs: append([]uuid.UUID{}, src.optionIDs...),
		})
	}
	return b.PublishPreparationUnitsCreated(ctx, q, PreparationUnitsCreated{
		Origin: origin, Units: units, At: at,
	})
}

type unitSource struct {
	menuItemID uuid.UUID
	sizeID     *uuid.UUID
	optionIDs  []uuid.UUID
}

func groupUnitSources(rows []sqlc.ListPreparationUnitSourcesRow) map[uuid.UUID]unitSource {
	out := make(map[uuid.UUID]unitSource, len(rows))
	for _, row := range rows {
		src, ok := out[row.OrderItemID]
		if !ok {
			src = unitSource{menuItemID: row.MenuItemID, optionIDs: []uuid.UUID{}}
			if row.SizeID.Valid {
				id := row.SizeID.UUID
				src.sizeID = &id
			}
		}
		if row.ModifierOptionID.Valid {
			src.optionIDs = append(src.optionIDs, row.ModifierOptionID.UUID)
		}
		out[row.OrderItemID] = src
	}
	return out
}
```

Run: `go test ./internal/platform/hooks/`
Expected: PASS

- [ ] **Step 4: Publish from Submit**

In `internal/sales/submit.go`, add the import `"github.com/Mirai3103/open-pos/internal/platform/hooks"`.

In `Handle`, pass the bus:

```go
			result, audit, err := applySubmitOrder(ctx, mc.Queries, mc.Hooks, actor, cmd)
```

Change the `applySubmitOrder` signature and the unit insertion:

```go
func applySubmitOrder(ctx context.Context, q *sqlc.Queries, bus *hooks.Bus, actor Actor,
	cmd SubmitOrderCommand,
) (ServiceSessionResponse, AuditRecord, error) {
```

```go
	created, err := insertOrderItems(ctx, q, orderID, source.ServiceNumber, items, occurredAt)
	if err != nil {
		return ServiceSessionResponse{}, AuditRecord{}, err
	}
	if err := bus.PublishCreatedUnits(ctx, q, hooks.OriginSubmit, created, occurredAt); err != nil {
		return ServiceSessionResponse{}, AuditRecord{}, err
	}
	unitCount := len(created)
```

Make `insertOrderItems` return the refs instead of a count:

```go
// insertOrderItems appends one Order Item per Committed Item and one
// Preparation Unit per unit of its quantity, and returns every new unit.
func insertOrderItems(ctx context.Context, q *sqlc.Queries, orderID uuid.UUID, serviceNumber string,
	items []sqlc.ListCommittedItemsForSubmissionRow, queuedAt time.Time,
) ([]hooks.UnitRef, error) {
```

Inside it, replace every `return 0, ...` with `return nil, ...`, declare `created := make([]hooks.UnitRef, 0, len(items))` instead of `unitCount := 0`, and replace the unit insert loop body:

```go
		for n := int32(1); n <= item.Quantity; n++ {
			unitID, err := q.InsertPreparationUnit(ctx, sqlc.InsertPreparationUnitParams{
				OrderItemID:     orderItemID,
				UnitNumber:      n,
				ServiceNumber:   serviceNumber,
				CategoryName:    item.CategoryName,
				ItemName:        item.ItemName,
				SizeName:        item.SizeName,
				Modifiers:       encoded,
				PreparationNote: item.PreparationNote,
				QueuedAt:        queuedAt,
			})
			if err != nil {
				return nil, fmt.Errorf("insert preparation unit: %w", err)
			}
			created = append(created, hooks.UnitRef{PreparationUnitID: unitID, OrderItemID: orderItemID})
		}
```

End the function with `return created, nil`.

- [ ] **Step 5: Publish from Remake and Cancel**

In `internal/preparation/remake.go`, add the import `"github.com/Mirai3103/open-pos/internal/platform/hooks"`. In `Handle`:

```go
			response, err := applyRemake(ctx, mc.Queries, mc.Hooks, actor, source, cmd.Reason, note)
```

Change the signature to `func applyRemake(ctx context.Context, q *sqlc.Queries, bus *hooks.Bus, actor Actor, source remakeSource, reason string, note *string) (RemakeResponse, error)`. Directly after the `insertRemakeUnit` error check (step 5–6), insert:

```go
	if err := bus.PublishCreatedUnits(ctx, q, hooks.OriginRemake, []hooks.UnitRef{{
		PreparationUnitID: replacement.ID, OrderItemID: waste.OrderItemID,
	}}, occurredAt); err != nil {
		return RemakeResponse{}, err
	}
```

In `internal/preparation/cancel.go`, add the same import. In `Handle`:

```go
			result, err := applyCancelUnits(ctx, mc.Queries, mc.Hooks, actor, cmd, note)
```

Change the signature to `func applyCancelUnits(ctx context.Context, q *sqlc.Queries, bus *hooks.Bus, actor Actor, cmd CancelUnitsCommand, note *string) (CancelUnitsResponse, error)`. Directly after `writes.apply` succeeds, before `writePreparationAudits`, insert:

```go
	if err := bus.PublishPreparationUnitsCancelled(ctx, q, hooks.PreparationUnitsCancelled{
		PreparationUnitIDs: ids, At: occurredAt,
	}); err != nil {
		return CancelUnitsResponse{}, err
	}
```

Run: `go build ./... && go vet ./internal/sales/ ./internal/preparation/`
Expected: no output.

- [ ] **Step 6: Write the integration tests**

`internal/preparation/hooks_integration_test.go`:

```go
//go:build integration

package preparation_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Mirai3103/open-pos/internal/platform/database/sqlc"
	"github.com/Mirai3103/open-pos/internal/platform/hooks"
	"github.com/Mirai3103/open-pos/internal/preparation"
	"github.com/Mirai3103/open-pos/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordedFacts struct {
	created   []hooks.PreparationUnitsCreated
	cancelled []hooks.PreparationUnitsCancelled
}

// attachRecorder puts a recording bus on both runners the env drives.
func attachRecorder(env *prepEnv) *recordedFacts {
	rec := &recordedFacts{}
	bus := hooks.New()
	bus.OnPreparationUnitsCreated(func(_ context.Context, _ *sqlc.Queries, ev hooks.PreparationUnitsCreated) error {
		rec.created = append(rec.created, ev)
		return nil
	})
	bus.OnPreparationUnitsCancelled(func(_ context.Context, _ *sqlc.Queries, ev hooks.PreparationUnitsCancelled) error {
		rec.cancelled = append(rec.cancelled, ev)
		return nil
	})
	env.SalesRunner.SetHooks(bus)
	env.PreparationRunner.SetHooks(bus)
	return rec
}

func TestSubmitPublishesPreparationUnitsCreated(t *testing.T) {
	env := newPrepEnv(t)
	rec := attachRecorder(env)

	units := env.SubmittedUnits(t, 2)

	require.Len(t, rec.created, 1)
	ev := rec.created[0]
	assert.Equal(t, hooks.OriginSubmit, ev.Origin)
	require.Len(t, ev.Units, 2)
	for i, u := range ev.Units {
		assert.Equal(t, units[i].ID, u.PreparationUnitID)
		assert.Equal(t, env.CoffeeID, u.MenuItemID)
		assert.Nil(t, u.SizeID)
		assert.Empty(t, u.ModifierOptionIDs)
	}
}

func TestRemakePublishesPreparationUnitsCreated(t *testing.T) {
	env := newPrepEnv(t)
	units := env.SubmittedUnits(t, 1)
	_, _, err := env.Advance(t, units[0].ID, preparation.StateInPreparation)
	require.NoError(t, err)
	waste, _, err := env.Waste(t, units[0].ID, preparation.ReasonPreparationError, nil)
	require.NoError(t, err)

	rec := attachRecorder(env)
	remade, _, err := env.Remake(t, waste.ID, preparation.ReasonPreparationError, nil)
	require.NoError(t, err)

	require.Len(t, rec.created, 1)
	assert.Equal(t, hooks.OriginRemake, rec.created[0].Origin)
	require.Len(t, rec.created[0].Units, 1)
	assert.Equal(t, remade.Unit.ID, rec.created[0].Units[0].PreparationUnitID)
	assert.Equal(t, env.CoffeeID, rec.created[0].Units[0].MenuItemID)
}

func TestCancelPublishesPreparationUnitsCancelled(t *testing.T) {
	env := newPrepEnv(t)
	units := env.SubmittedUnits(t, 1)
	rec := attachRecorder(env)

	_, _, err := preparation.NewCancelUnitsHandler(env.PreparationRunner).Handle(context.Background(),
		env.ManagerActor(), preparation.CancelUnitsCommand{
			RequestID:          uuid.New(),
			PreparationUnitIDs: []uuid.UUID{units[0].ID},
			Kind:               preparation.CancelKindCancellation,
			Reason:             preparation.ReasonOrderEntryError,
		})
	require.NoError(t, err)

	require.Len(t, rec.cancelled, 1)
	assert.Equal(t, []uuid.UUID{units[0].ID}, rec.cancelled[0].PreparationUnitIDs)
}

func TestSubscriberErrorRollsBackSubmit(t *testing.T) {
	env := newPrepEnv(t)
	bus := hooks.New()
	bus.OnPreparationUnitsCreated(func(context.Context, *sqlc.Queries, hooks.PreparationUnitsCreated) error {
		return errors.New("subscriber database failure")
	})
	env.SalesRunner.SetHooks(bus)

	ctx := context.Background()
	actor := env.salesActor(env.ManagerActor())
	_, session, err := sales.NewStartDineInSessionHandler(env.SalesRunner).Handle(ctx, actor,
		sales.StartDineInSessionCommand{RequestID: uuid.New(), TableIDs: []uuid.UUID{env.TableID}})
	require.NoError(t, err)
	_, _, err = sales.NewAddDraftItemHandler(env.SalesRunner).Handle(ctx, actor, sales.AddDraftItemCommand{
		RequestID: uuid.New(), ServiceSessionID: session.ID, MenuItemID: env.CoffeeID,
	})
	require.NoError(t, err)
	_, _, err = sales.NewCommitOrderDraftHandler(env.SalesRunner).Handle(ctx, actor,
		sales.CommitOrderDraftCommand{RequestID: uuid.New(), ServiceSessionID: session.ID})
	require.NoError(t, err)

	_, _, err = sales.NewSubmitOrderHandler(env.SalesRunner).Handle(ctx, actor,
		sales.SubmitOrderCommand{RequestID: uuid.New(), ServiceSessionID: session.ID})
	require.Error(t, err)

	var units int
	require.NoError(t, env.DB.QueryRow(`
		SELECT count(*) FROM preparation_units pu
		JOIN order_items oi ON oi.id = pu.order_item_id
		JOIN orders o ON o.id = oi.order_id
		WHERE o.service_session_id = $1`, session.ID).Scan(&units))
	assert.Zero(t, units, "a failing subscriber must roll the whole Submit back")
}
```

- [ ] **Step 7: Run the integration suites**

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -count=1 -tags=integration ./internal/preparation/ ./internal/sales/`
Expected: PASS, including the four new tests. The existing suites must not change behaviour: they attach no bus.

- [ ] **Step 8: Commit**

```bash
git add sql/queries/sales.sql internal/platform/database/sqlc internal/platform/hooks internal/sales/submit.go internal/preparation/remake.go internal/preparation/cancel.go internal/preparation/hooks_integration_test.go
git commit -m "feat: publish Preparation Unit facts on the Core hook bus

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Store Feature switch

**Files:**
- Create: `internal/platform/database/migrations/000018_create_store_features.sql`
- Create: `sql/queries/features.sql`
- Create: `internal/platform/features/features.go`, `intervals.go`, `commands.go`, `http.go`, `routes.go`
- Create: `internal/platform/features/intervals_test.go`
- Create: `internal/platform/features/testmain_integration_test.go`, `features_integration_test.go`
- Modify: `internal/auth/domain.go:48-59` (Manager capabilities)
- Modify: `internal/testdb/config.go:13-16` (allowlist)
- Modify: `cmd/api/main.go` (route registration, after the preparation block)

**Interfaces:**
- Produces:
  - `features.Enabled(ctx, q *sqlc.Queries, key string) (bool, error)`
  - `features.OffIntervals(ctx, q *sqlc.Queries, key string, from, to time.Time) ([]features.Interval, error)`
  - `features.Interval{From, To time.Time}` with JSON `from`, `to`
  - `features.RequireEnabled(q *sqlc.Queries, key string) echo.MiddlewareFunc`
  - `features.NewSlices(db, q) *features.Slices`, with `.Set *SetFeatureHandler` and `.RegisterRoutes(v1, authn)`
  - `features.SetFeatureCommand{RequestID uuid.UUID; Key string; Enabled *bool}`
  - `features.CapAdminister = "features.administer"`

- [ ] **Step 1: Migration and queries**

`internal/platform/database/migrations/000018_create_store_features.sql`:

```sql
-- 000018: Store Features. A store-wide, Manager-controlled switch that turns
-- an extension outside the Core POS on or off for every client at once.

CREATE TABLE IF NOT EXISTS store_features (
    key TEXT PRIMARY KEY,
    enabled BOOLEAN NOT NULL DEFAULT false,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by_staff_identity_id UUID REFERENCES staff_identities(id) ON DELETE RESTRICT,
    CONSTRAINT store_feature_key_valid CHECK (key ~ '^[a-z][a-z0-9_]{1,39}$')
);

-- Append-only history of every change; the gross-profit report reads it.
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

`sql/queries/features.sql`:

```sql
-- name: ListStoreFeatures :many
SELECT key, enabled FROM store_features ORDER BY key;

-- name: GetStoreFeatureEnabled :one
SELECT enabled FROM store_features WHERE key = $1;

-- name: LockStoreFeature :one
SELECT key, enabled FROM store_features WHERE key = $1 FOR UPDATE;

-- name: SetStoreFeatureEnabled :exec
UPDATE store_features
SET enabled = sqlc.arg(enabled), updated_at = now(),
    updated_by_staff_identity_id = sqlc.arg(actor_id)::uuid
WHERE key = sqlc.arg(key);

-- name: InsertStoreFeatureChange :exec
INSERT INTO store_feature_changes (key, enabled, actor_staff_identity_id)
VALUES ($1, $2, $3);

-- name: ListStoreFeatureChanges :many
SELECT enabled, changed_at FROM store_feature_changes
WHERE key = sqlc.arg(key) AND changed_at < sqlc.arg(until)::timestamptz
ORDER BY changed_at, id;
```

Run: `make sqlc && go build ./...`
Expected: success. `internal/platform/database/sqlc/features.sql.go` exists.

- [ ] **Step 2: Write the failing interval test**

`internal/platform/features/intervals_test.go`:

```go
package features

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestOffIntervals(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	h := func(n int) time.Time { return t0.Add(time.Duration(n) * time.Hour) }

	cases := []struct {
		name    string
		changes []change
		want    []Interval
	}{
		{"never enabled is off throughout", nil, []Interval{{h(0), h(10)}}},
		{"enabled before the period is on throughout",
			[]change{{true, h(-5)}}, []Interval{}},
		{"enabled inside the period",
			[]change{{true, h(3)}}, []Interval{{h(0), h(3)}}},
		{"on, off, on inside the period",
			[]change{{true, h(-1)}, {false, h(2)}, {true, h(4)}}, []Interval{{h(2), h(4)}}},
		{"turned off and left off",
			[]change{{true, h(-1)}, {false, h(7)}}, []Interval{{h(7), h(10)}}},
		{"enabled exactly at from",
			[]change{{true, h(0)}}, []Interval{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, offIntervals(tc.changes, h(0), h(10)))
		})
	}
}
```

Run: `go test ./internal/platform/features/`
Expected: FAIL, because the package does not exist.

- [ ] **Step 3: Implement the package**

`internal/platform/features/features.go`:

```go
// Package features is the Core POS's Store Feature switch: a store-wide,
// Manager-controlled on/off for an extension outside the Core POS. It knows no
// extension; keys are plain strings each extension owns and a migration seeds.
package features

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Mirai3103/open-pos/internal/platform/command"
	"github.com/Mirai3103/open-pos/internal/platform/database/sqlc"
)

// Capability, operation and audit names.
const (
	CapAdminister            = "features.administer"
	OpSetFeature             = "features.set_feature"
	EventFeatureEnabled      = "STORE_FEATURE_ENABLED"
	EventFeatureDisabled     = "STORE_FEATURE_DISABLED"
	EventAuthorizationDenied = "features.authorization_denied"
)

// Sentinel errors.
var (
	ErrUnknownFeature  = errors.New("unknown store feature")
	ErrFeatureDisabled = errors.New("store feature disabled")
)

var policy = command.Policy{
	DenialEventType:      EventAuthorizationDenied,
	LogScope:             "features",
	Attribution:          command.AttributeActorIfConfirmed,
	OnDenialAuditFailure: command.ReturnAuditError,
}

// NewRunner creates a Runner bound to the features policy.
func NewRunner(db *sql.DB, queries *sqlc.Queries) *command.Runner {
	return command.NewRunner(db, queries, policy)
}

// Enabled reports whether key is on, reading inside the caller's transaction
// when q is transactional. An unknown key is off.
func Enabled(ctx context.Context, q *sqlc.Queries, key string) (bool, error) {
	on, err := q.GetStoreFeatureEnabled(ctx, key)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read store feature %q: %w", key, err)
	}
	return on, nil
}
```

`internal/platform/features/intervals.go`:

```go
package features

import (
	"context"
	"fmt"
	"time"

	"github.com/Mirai3103/open-pos/internal/platform/database/sqlc"
)

// Interval is a half-open time range [From, To).
type Interval struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

type change struct {
	enabled bool
	at      time.Time
}

// OffIntervals returns the parts of [from, to) during which key was off. A key
// with no change before from was off then, because every feature is seeded
// off.
func OffIntervals(ctx context.Context, q *sqlc.Queries, key string, from, to time.Time) ([]Interval, error) {
	rows, err := q.ListStoreFeatureChanges(ctx, sqlc.ListStoreFeatureChangesParams{Key: key, Until: to})
	if err != nil {
		return nil, fmt.Errorf("list store feature changes: %w", err)
	}
	changes := make([]change, 0, len(rows))
	for _, r := range rows {
		changes = append(changes, change{enabled: r.Enabled, at: r.ChangedAt})
	}
	return offIntervals(changes, from, to), nil
}

func offIntervals(changes []change, from, to time.Time) []Interval {
	enabled := false
	i := 0
	for ; i < len(changes) && !changes[i].at.After(from); i++ {
		enabled = changes[i].enabled
	}
	out := []Interval{}
	var offStart *time.Time
	if !enabled {
		start := from
		offStart = &start
	}
	for ; i < len(changes) && changes[i].at.Before(to); i++ {
		c := changes[i]
		switch {
		case c.enabled && offStart != nil:
			if c.at.After(*offStart) {
				out = append(out, Interval{From: *offStart, To: c.at})
			}
			offStart = nil
		case !c.enabled && offStart == nil:
			start := c.at
			offStart = &start
		}
	}
	if offStart != nil && to.After(*offStart) {
		out = append(out, Interval{From: *offStart, To: to})
	}
	return out
}
```

`internal/platform/features/commands.go`:

```go
package features

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/Mirai3103/open-pos/internal/platform/command"
	"github.com/Mirai3103/open-pos/internal/platform/database/sqlc"
	"github.com/Mirai3103/open-pos/internal/response"
	"github.com/google/uuid"
)

// SetFeatureCommand turns one Store Feature on or off. Key comes from the route.
type SetFeatureCommand struct {
	RequestID uuid.UUID `json:"request_id"`
	Key       string    `json:"-"`
	Enabled   *bool     `json:"enabled"`
}

// FeatureResponse is one Store Feature's state.
type FeatureResponse struct {
	Key     string `json:"key"`
	Enabled bool   `json:"enabled"`
}

// SetFeatureHandler changes a Store Feature.
type SetFeatureHandler struct{ runner *command.Runner }

// NewSetFeatureHandler creates a SetFeatureHandler.
func NewSetFeatureHandler(runner *command.Runner) *SetFeatureHandler {
	return &SetFeatureHandler{runner: runner}
}

// Handle sets the feature. Setting the current value again is a no-op that
// writes no change row and no audit event.
func (h *SetFeatureHandler) Handle(ctx context.Context, actor command.Actor, cmd SetFeatureCommand,
) (int, FeatureResponse, error) {
	if cmd.Enabled == nil {
		return 0, FeatureResponse{}, fmt.Errorf("%w: enabled is required", response.ErrInvalid)
	}
	enabled := *cmd.Enabled
	spec := command.MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpSetFeature,
		Fingerprint: map[string]any{"key": cmd.Key, "enabled": enabled},
		Required:    []string{CapAdminister},
	}
	return command.ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc command.MutationContext) (int, FeatureResponse, command.AuditRecord, error) {
			var zero FeatureResponse
			row, err := mc.Queries.LockStoreFeature(ctx, cmd.Key)
			if errors.Is(err, sql.ErrNoRows) {
				return 0, zero, command.AuditRecord{}, fmt.Errorf("%w: %s", ErrUnknownFeature, cmd.Key)
			}
			if err != nil {
				return 0, zero, command.AuditRecord{}, fmt.Errorf("lock store feature: %w", err)
			}
			out := FeatureResponse{Key: row.Key, Enabled: enabled}
			if row.Enabled == enabled {
				return http.StatusOK, out, command.AuditRecord{}, nil
			}
			if err := mc.Queries.SetStoreFeatureEnabled(ctx, sqlc.SetStoreFeatureEnabledParams{
				Enabled: enabled, ActorID: mc.Actor.StaffID, Key: cmd.Key,
			}); err != nil {
				return 0, zero, command.AuditRecord{}, fmt.Errorf("set store feature: %w", err)
			}
			if err := mc.Queries.InsertStoreFeatureChange(ctx, sqlc.InsertStoreFeatureChangeParams{
				Key: cmd.Key, Enabled: enabled, ActorStaffIdentityID: mc.Actor.StaffID,
			}); err != nil {
				return 0, zero, command.AuditRecord{}, fmt.Errorf("insert store feature change: %w", err)
			}
			event := EventFeatureDisabled
			if enabled {
				event = EventFeatureEnabled
			}
			return http.StatusOK, out, command.AuditRecord{
				EventType: event,
				Details:   map[string]any{"key": cmd.Key, "enabled": enabled},
			}, nil
		})
}

// ListFeaturesHandler reads every Store Feature. Any authenticated staff
// member may read them, so clients can show or hide extension UI.
type ListFeaturesHandler struct{ runner *command.Runner }

// NewListFeaturesHandler creates a ListFeaturesHandler.
func NewListFeaturesHandler(runner *command.Runner) *ListFeaturesHandler {
	return &ListFeaturesHandler{runner: runner}
}

// Handle returns key → enabled.
func (h *ListFeaturesHandler) Handle(ctx context.Context, actor command.Actor) (map[string]bool, error) {
	return command.ExecuteRead(ctx, h.runner, actor, command.ReadSpec{},
		func(rc command.ReadContext) (map[string]bool, error) {
			rows, err := rc.Queries.ListStoreFeatures(ctx)
			if err != nil {
				return nil, fmt.Errorf("list store features: %w", err)
			}
			out := make(map[string]bool, len(rows))
			for _, r := range rows {
				out[r.Key] = r.Enabled
			}
			return out, nil
		})
}
```

`internal/platform/features/http.go`:

```go
package features

import (
	"fmt"
	"net/http"

	"github.com/Mirai3103/open-pos/internal/platform/command"
	"github.com/Mirai3103/open-pos/internal/platform/database/sqlc"
	"github.com/Mirai3103/open-pos/internal/platform/httpx"
	"github.com/Mirai3103/open-pos/internal/response"
	"github.com/labstack/echo/v4"
)

var httpErrors = response.ErrorMapper{
	{Target: ErrUnknownFeature, Spec: response.ErrorSpec{
		Status: http.StatusNotFound, Code: "FEATURE_NOT_FOUND", Message: ErrUnknownFeature.Error(),
	}},
	{Target: command.ErrRequestConflict, Spec: response.ErrorSpec{Status: http.StatusConflict, Code: "REQUEST_CONFLICT"}},
	{Target: command.ErrForbidden, Spec: response.ErrorSpec{Status: http.StatusForbidden, Code: "FORBIDDEN"}},
	{Target: command.ErrUnauthorized, Spec: response.ErrorSpec{Status: http.StatusUnauthorized, Code: "UNAUTHORIZED"}},
	{Target: response.ErrInvalid, Spec: response.ErrorSpec{Status: http.StatusBadRequest, Code: "INVALID_INPUT"}},
}

func sendError(c echo.Context, err error) error { return httpx.SendError(c, err, httpErrors.Map) }

// RequireEnabled answers 403 FEATURE_DISABLED unless key is on. It reads the
// switch on every request, so turning a feature off takes effect at once.
func RequireEnabled(q *sqlc.Queries, key string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			on, err := Enabled(c.Request().Context(), q, key)
			if err != nil {
				return response.Error(c, err)
			}
			if !on {
				return response.Error(c, response.NewCodedError(http.StatusForbidden,
					"FEATURE_DISABLED", "Tính năng chưa được bật",
					fmt.Errorf("%w: %s", ErrFeatureDisabled, key)))
			}
			return next(c)
		}
	}
}

// handleList returns every Store Feature.
//
//	@Summary		List Store Features
//	@Description	Returns each store-wide feature switch as key → enabled
//	@Tags			features
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	response.APIResponse{data=map[string]bool}
//	@Failure		401	{object}	response.APIResponse
//	@Router			/features [get]
func (s *Slices) handleList(c echo.Context) error {
	actor, err := httpx.Actor(c)
	if err != nil {
		return sendError(c, err)
	}
	out, err := s.List.Handle(c.Request().Context(), actor)
	if err != nil {
		return sendError(c, err)
	}
	return response.OK(c, out)
}

// handleSet turns a Store Feature on or off.
//
//	@Summary		Set a Store Feature
//	@Description	Turns a store-wide feature on or off for every client. Setting the current value again is a no-op.
//	@Tags			features
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			key		path		string				true	"Feature key"
//	@Param			request	body		SetFeatureCommand	true	"New state"
//	@Success		200		{object}	response.APIResponse{data=FeatureResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Router			/features/{key} [put]
func (s *Slices) handleSet(c echo.Context) error {
	actor, err := httpx.Actor(c)
	if err != nil {
		return sendError(c, err)
	}
	cmd, err := httpx.BindBody[SetFeatureCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := httpx.RequireRequestID(cmd.RequestID); err != nil {
		return sendError(c, err)
	}
	cmd.Key = c.Param("key")
	status, out, err := s.Set.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return httpx.SendResult(c, status, out)
}
```

`internal/platform/features/routes.go`:

```go
package features

import (
	"database/sql"

	"github.com/Mirai3103/open-pos/internal/auth"
	"github.com/Mirai3103/open-pos/internal/platform/command"
	"github.com/Mirai3103/open-pos/internal/platform/database/sqlc"
	"github.com/labstack/echo/v4"
)

// Slices aggregates the Store Feature handlers.
type Slices struct {
	Runner *command.Runner
	List   *ListFeaturesHandler
	Set    *SetFeatureHandler
}

// NewSlices wires the handlers onto one Runner.
func NewSlices(db *sql.DB, queries *sqlc.Queries) *Slices {
	runner := NewRunner(db, queries)
	return &Slices{Runner: runner, List: NewListFeaturesHandler(runner), Set: NewSetFeatureHandler(runner)}
}

// RegisterRoutes mounts /features. These routes are never feature-gated.
func (s *Slices) RegisterRoutes(v1 *echo.Group, authn *auth.Middleware) {
	v1.GET("/features", s.handleList, authn.RequireAuth())
	v1.PUT("/features/:key", s.handleSet, authn.RequireAuth(), authn.RequireCapability(CapAdminister))
}
```

`internal/platform/features` imports `internal/auth`, as `internal/platform/httpx` already does. Check for an import cycle with `go build ./...`. If `auth` ever imports `features`, move `routes.go` into `cmd/api`. Today it does not.

In `internal/auth/domain.go`, add `"features.administer",` to `RoleManager`'s list, after `"tables.administer",`.

In `internal/testdb/config.go`, add `"features": {},` to `allowedPackages`.

In `cmd/api/main.go`, add the import `"github.com/Mirai3103/open-pos/internal/platform/features"` and, after `preparationSlices.RegisterRoutes(...)`:

```go
	featuresSlices := features.NewSlices(db, queries)
	featuresSlices.RegisterRoutes(v1, authSlices.Middleware)
```

Run: `go build ./... && go test ./internal/platform/features/ ./internal/testdb/`
Expected: PASS

- [ ] **Step 4: Write the integration tests**

`internal/platform/features/testmain_integration_test.go`:

```go
//go:build integration

package features_test

import (
	"database/sql"
	"os"
	"testing"

	"github.com/Mirai3103/open-pos/internal/testdb"
)

var featuresTestDB *sql.DB

func TestMain(m *testing.M) {
	os.Exit(testdb.Run(m, "features", func(db *sql.DB) { featuresTestDB = db }))
}
```

`internal/platform/features/features_integration_test.go`:

```go
//go:build integration

package features_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Mirai3103/open-pos/internal/auth"
	"github.com/Mirai3103/open-pos/internal/platform/command"
	"github.com/Mirai3103/open-pos/internal/platform/database/sqlc"
	"github.com/Mirai3103/open-pos/internal/platform/features"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedActor(t *testing.T, q *sqlc.Queries, roles ...string) command.Actor {
	t.Helper()
	ctx := context.Background()
	code := "F" + strings.ReplaceAll(uuid.NewString(), "-", "")[:20]
	row, err := q.CreateStaffIdentity(ctx, sqlc.CreateStaffIdentityParams{
		DisplayName: "Features " + code, Btrim: code, PinHash: "", Enabled: true,
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

func boolPtr(b bool) *bool { return &b }

func setInventory(t *testing.T, s *features.Slices, actor command.Actor, on bool) error {
	t.Helper()
	_, _, err := s.Set.Handle(context.Background(), actor, features.SetFeatureCommand{
		RequestID: uuid.New(), Key: "inventory", Enabled: boolPtr(on),
	})
	return err
}

func TestInventoryFeatureIsSeededOff(t *testing.T) {
	q := sqlc.New(featuresTestDB)
	on, err := features.Enabled(context.Background(), q, "inventory")
	require.NoError(t, err)
	assert.False(t, on)

	unknown, err := features.Enabled(context.Background(), q, "no_such_feature")
	require.NoError(t, err)
	assert.False(t, unknown)
}

func TestSetFeatureRecordsChangeAndAudit(t *testing.T) {
	q := sqlc.New(featuresTestDB)
	s := features.NewSlices(featuresTestDB, q)
	manager := seedActor(t, q, auth.RoleManager)

	require.NoError(t, setInventory(t, s, manager, true))
	on, err := features.Enabled(context.Background(), q, "inventory")
	require.NoError(t, err)
	assert.True(t, on)

	var changes, audits int
	require.NoError(t, featuresTestDB.QueryRow(`SELECT count(*) FROM store_feature_changes
		WHERE key = 'inventory' AND actor_staff_identity_id = $1`, manager.StaffID).Scan(&changes))
	require.NoError(t, featuresTestDB.QueryRow(`SELECT count(*) FROM audit_events
		WHERE event_type = $1 AND actor_id = $2`, features.EventFeatureEnabled, manager.StaffID).Scan(&audits))
	assert.Equal(t, 1, changes)
	assert.Equal(t, 1, audits)

	// The same value again is a no-op.
	require.NoError(t, setInventory(t, s, manager, true))
	require.NoError(t, featuresTestDB.QueryRow(`SELECT count(*) FROM store_feature_changes
		WHERE key = 'inventory' AND actor_staff_identity_id = $1`, manager.StaffID).Scan(&changes))
	assert.Equal(t, 1, changes)

	require.NoError(t, setInventory(t, s, manager, false))
}

func TestSetFeatureDeniesCashierAndUnknownKey(t *testing.T) {
	q := sqlc.New(featuresTestDB)
	s := features.NewSlices(featuresTestDB, q)

	err := setInventory(t, s, seedActor(t, q, auth.RoleCashier), true)
	require.ErrorIs(t, err, command.ErrForbidden)

	_, _, err = s.Set.Handle(context.Background(), seedActor(t, q, auth.RoleManager), features.SetFeatureCommand{
		RequestID: uuid.New(), Key: "no_such_feature", Enabled: boolPtr(true),
	})
	require.ErrorIs(t, err, features.ErrUnknownFeature)
}

func TestRequireEnabledMiddleware(t *testing.T) {
	q := sqlc.New(featuresTestDB)
	s := features.NewSlices(featuresTestDB, q)
	manager := seedActor(t, q, auth.RoleManager)

	e := echo.New()
	e.GET("/probe", func(c echo.Context) error { return c.String(http.StatusOK, "ok") },
		features.RequireEnabled(q, "inventory"))

	call := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/probe", nil))
		return rec
	}

	require.NoError(t, setInventory(t, s, manager, false))
	off := call()
	assert.Equal(t, http.StatusForbidden, off.Code)
	assert.Contains(t, off.Body.String(), "FEATURE_DISABLED")

	require.NoError(t, setInventory(t, s, manager, true))
	assert.Equal(t, http.StatusOK, call().Code)
	require.NoError(t, setInventory(t, s, manager, false))
}

func TestOffIntervalsReadsChanges(t *testing.T) {
	q := sqlc.New(featuresTestDB)
	s := features.NewSlices(featuresTestDB, q)
	manager := seedActor(t, q, auth.RoleManager)
	from := time.Now().Add(-time.Minute)

	require.NoError(t, setInventory(t, s, manager, false))
	require.NoError(t, setInventory(t, s, manager, true))
	got, err := features.OffIntervals(context.Background(), q, "inventory", from, time.Now().Add(time.Minute))
	require.NoError(t, err)
	require.NotEmpty(t, got)
	assert.True(t, got[len(got)-1].To.Before(time.Now().Add(time.Second)),
		"the last off interval ends when the feature was turned on")
	require.NoError(t, setInventory(t, s, manager, false))
}
```

- [ ] **Step 5: Run the tests**

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -count=1 -tags=integration ./internal/platform/features/`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/platform/database/migrations/000018_create_store_features.sql sql/queries/features.sql internal/platform/database/sqlc internal/platform/features internal/auth/domain.go internal/testdb/config.go cmd/api/main.go
git commit -m "feat(platform): add store-wide Store Feature switch

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: Inventory schema and queries

**Files:**
- Create: `internal/platform/database/migrations/000019_create_inventory_slice.sql`
- Modify: `sqlc.yaml` (add overrides)
- Create: `sql/queries/inventory.sql`
- Modify: `internal/testdb/config.go` (allow `inventory`)
- Modify: `internal/testdb/config_test.go:26` (the "unknown package" case uses `inventory`; change it to `nonexistent`)
- Create: `internal/inventory/doc.go`, `internal/inventory/testmain_integration_test.go`, `internal/inventory/schema_integration_test.go`

**Interfaces:**
- Produces sqlc methods, all on `*sqlc.Queries` (models `InventoryIngredient`, `InventoryIngredientUnit`, `InventoryIngredientCost`):
  - `InventoryCreateIngredient`, `InventoryGetIngredient`, `InventoryLockIngredient`, `InventoryRenameIngredient`, `InventoryRetireIngredient`, `InventoryListIngredients`
  - `InventoryListIngredientUnits(ingredientIds)`, `InventoryGetIngredientUnit`, `InventoryInsertIngredientUnit`, `InventoryUpdateIngredientUnit`, `InventoryDeleteIngredientUnit`, `InventoryIngredientUnitInUse`
  - `InventoryInsertIngredientCost`, `InventoryListIngredientCosts`, `InventoryListCurrentCosts`
  - `InventoryFindRecipe` (locking), `InventoryGetRecipe`, `InventoryInsertRecipe`, `InventoryUpdateRecipeYield`, `InventoryDeleteRecipe`, `InventoryDeleteRecipeLines`, `InventoryInsertRecipeLine`, `InventoryListRecipeLines`, `InventoryListBookLines`
  - `InventoryGetMenuItemForRecipe`, `InventoryGetSizeForRecipe`, `InventoryGetOptionForRecipe`
  - `InventoryInsertConsumption`, `InventoryInsertConsumptionLine`, `InventoryVoidConsumptions`
  - `InventoryListMenuForCosting`, `InventoryListOptionsForCosting`
  - `InventoryListGrossProfitUnits`, `InventoryListUnitComps`

- [ ] **Step 1: Migration**

Create `internal/platform/database/migrations/000019_create_inventory_slice.sql`. Copy the DDL **verbatim from spec §5.2** (`inventory_units` through `inventory_consumption_lines`, with every index), under this header:

```sql
-- 000019: Inventory Extension (INV-1). Outside the Core POS: these tables
-- reference Core tables; no Core table references them.
```

Append the units seed:

```sql
INSERT INTO inventory_units (code, dimension, base_code, factor_to_base) VALUES
    ('g', 'MASS', 'g', 1), ('ml', 'VOLUME', 'ml', 1), ('piece', 'COUNT', 'piece', 1)
ON CONFLICT (code) DO NOTHING;
INSERT INTO inventory_units (code, dimension, base_code, factor_to_base) VALUES
    ('kg', 'MASS', 'g', 1000), ('l', 'VOLUME', 'ml', 1000)
ON CONFLICT (code) DO NOTHING;
```

The base units go first, because `base_code` references `inventory_units(code)`.

- [ ] **Step 2: sqlc numeric override**

In `sqlc.yaml`, under `gen.go`, add:

```yaml
        overrides:
          - db_type: "pg_catalog.numeric"
            go_type: "float64"
          - db_type: "pg_catalog.numeric"
            nullable: true
            go_type:
              import: "database/sql"
              type: "NullFloat64"
```

No existing table uses `NUMERIC` (`grep -ri numeric internal/platform/database/migrations` before this task finds none), so the override changes no generated code.

- [ ] **Step 3: Queries**

`sql/queries/inventory.sql`:

```sql
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
```

Run: `make sqlc && go build ./...`
Expected: success. Spot-check `internal/platform/database/sqlc/inventory.sql.go`:
- `InventoryIngredientCost.CostPerStockUnit` is `float64`.
- `InventoryListBookLinesRow.FactorToBase` and `.StockQuantity` are `sql.NullFloat64`.
- `InventoryInsertConsumptionParams` has `ModifierOptionIds []uuid.UUID` and `CostTotalVnd float64`.
- `InventoryInsertConsumptionLineParams.CostPerStockUnit` is `sql.NullFloat64`.

If sqlc names a parameter differently from these, use the generated name everywhere below. The shapes stay the same.

- [ ] **Step 4: Test harness and schema test**

`internal/inventory/doc.go`:

```go
// Package inventory is the Inventory Extension: ingredients, units, costs,
// recipes, and what each Preparation Unit consumed. It lies outside the Core
// POS: it depends on Core tables, the Core hook bus and the Store Feature
// switch, and nothing in the Core depends on it.
package inventory
```

In `internal/testdb/config.go`, add `"inventory": {},`. In `internal/testdb/config_test.go`, change the `"unknown package"` case's package argument from `"inventory"` to `"nonexistent"`.

`internal/inventory/testmain_integration_test.go`:

```go
//go:build integration

package inventory_test

import (
	"database/sql"
	"os"
	"testing"

	"github.com/Mirai3103/open-pos/internal/testdb"
)

var invTestDB *sql.DB

func TestMain(m *testing.M) {
	os.Exit(testdb.Run(m, "inventory", func(db *sql.DB) { invTestDB = db }))
}
```

`internal/inventory/schema_integration_test.go`:

```go
//go:build integration

package inventory_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSystemUnitsSeeded(t *testing.T) {
	var n int
	require.NoError(t, invTestDB.QueryRow(`SELECT count(*) FROM inventory_units`).Scan(&n))
	assert.Equal(t, 5, n)
}

func TestStockUnitMustBeBase(t *testing.T) {
	_, err := invTestDB.Exec(`INSERT INTO inventory_ingredients (name, kind, stock_unit_code)
		VALUES ('Bad unit', 'RAW', 'kg')`)
	require.Error(t, err)
}

func TestRecipeOwnerExactlyOne(t *testing.T) {
	_, err := invTestDB.Exec(`INSERT INTO inventory_recipes (owner_kind) VALUES ('MENU_ITEM')`)
	require.Error(t, err)
}
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/testdb/ && TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -count=1 -tags=integration ./internal/inventory/`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/platform/database/migrations/000019_create_inventory_slice.sql sqlc.yaml sql/queries/inventory.sql internal/platform/database/sqlc internal/testdb internal/inventory
git commit -m "feat(inventory): add inventory schema and queries

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: Inventory domain (units, rounding, composition)

**Files:**
- Create: `internal/inventory/domain.go`, `units.go`, `book.go`, `errors.go`
- Test: `internal/inventory/units_test.go`, `internal/inventory/book_test.go`

**Interfaces:**
- Produces:
  - Constants `FeatureKey="inventory"`, `CapAdminister`, `CapViewCosts`, `KindRaw`, `KindPrepared`, `OwnerMenuItem`, `OwnerSize`, `OwnerModifierOption`, `OwnerIngredient`, `CostComplete`, `CostPartial`, `CostNone`, the `Op*` and `Event*` names, `MaxIngredientNameLength=120`, `MaxUnitNameLength=40`, `MaxRecipeLines=50`
  - `OwnerKindFromPath(string) (string, bool)`, `NormalizeName(string) string`, `ValidateLineQuantity(ownerKind string, q float64) error`
  - `SystemUnit{Code, Dimension, BaseCode string; Factor float64}`, `SystemUnits() []SystemUnit`, `IsStockUnit(string) bool`, `SystemFactor(stockUnit, code string) (float64, error)`
  - `UnitRef{UnitCode *string; IngredientUnitID *uuid.UUID}` with `.Validate() error`
  - `Round2`, `Round3`, `Round4 func(float64) float64`
  - `Book`, `NewBook()`, `Line`, `Batch`, `Ingredient`, `Selection`, `ComposedLine`, `Composition`, `Delta`
  - `(*Book).UnitCost(uuid.UUID) (float64, bool)`, `(*Book).Compose(Selection) Composition`, `(*Book).OptionDelta(uuid.UUID) Delta`
  - Every sentinel error listed in `errors.go` below

- [ ] **Step 1: Write the failing tests**

`internal/inventory/units_test.go`:

```go
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
```

`internal/inventory/book_test.go`:

```go
package inventory_test

import (
	"testing"

	"github.com/Mirai3103/open-pos/internal/inventory"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fixture struct {
	book                                    *inventory.Book
	coffeePowder, condensed, base, tea, sugar uuid.UUID
	pearls                                  uuid.UUID
	coffee, milkTea, sizeM, sizeL, pearlOpt uuid.UUID
	lessSugarOpt                            uuid.UUID
}

func newFixture() fixture {
	f := fixture{book: inventory.NewBook()}
	add := func(name, kind, unit string) uuid.UUID {
		id := uuid.New()
		f.book.Ingredients[id] = inventory.Ingredient{ID: id, Name: name, Kind: kind, StockUnit: unit}
		return id
	}
	f.coffeePowder = add("Bột cà phê", inventory.KindRaw, "g")
	f.condensed = add("Sữa đặc", inventory.KindRaw, "g")
	f.base = add("Cốt cà phê", inventory.KindPrepared, "ml")
	f.tea = add("Trà", inventory.KindRaw, "ml")
	f.sugar = add("Đường", inventory.KindRaw, "g")
	f.pearls = add("Trân châu", inventory.KindRaw, "g")

	f.book.RawCosts[f.coffeePowder] = 250
	f.book.RawCosts[f.condensed] = 65.7895
	f.book.RawCosts[f.tea] = 20
	f.book.RawCosts[f.pearls] = 60
	// sugar deliberately has no cost.

	f.book.Batches[f.base] = inventory.Batch{
		Lines: []inventory.Line{{IngredientID: f.coffeePowder, StockQty: 250}}, Yield: 1000,
	}

	f.coffee, f.milkTea, f.sizeM, f.sizeL = uuid.New(), uuid.New(), uuid.New(), uuid.New()
	f.pearlOpt, f.lessSugarOpt = uuid.New(), uuid.New()
	f.book.Items[f.coffee] = []inventory.Line{{IngredientID: f.base, StockQty: 40}, {IngredientID: f.condensed, StockQty: 30}}
	f.book.Sizes[f.sizeM] = []inventory.Line{{IngredientID: f.tea, StockQty: 200}, {IngredientID: f.sugar, StockQty: 10}}
	f.book.Sizes[f.sizeL] = []inventory.Line{{IngredientID: f.tea, StockQty: 280}}
	f.book.Options[f.pearlOpt] = []inventory.Line{{IngredientID: f.pearls, StockQty: 50}}
	f.book.Options[f.lessSugarOpt] = []inventory.Line{{IngredientID: f.sugar, StockQty: -15}}
	return f
}

func TestPreparedUnitCost(t *testing.T) {
	f := newFixture()
	c, ok := f.book.UnitCost(f.base)
	require.True(t, ok)
	assert.Equal(t, 62.5, c)

	delete(f.book.RawCosts, f.coffeePowder)
	_, ok = f.book.UnitCost(f.base)
	assert.False(t, ok, "a prepared ingredient whose raw input has no cost has no cost")
}

func TestComposeItemWithoutSizes(t *testing.T) {
	f := newFixture()
	got := f.book.Compose(inventory.Selection{MenuItemID: f.coffee})
	assert.Equal(t, inventory.CostComplete, got.Status)
	require.Len(t, got.Lines, 2)
	assert.Equal(t, f.base, got.Lines[0].IngredientID)
	assert.Equal(t, 2500.0, *got.Lines[0].LineCost)
	assert.Equal(t, 1973.69, *got.Lines[1].LineCost)
	assert.Equal(t, 4473.69, got.TotalVND)
}

func TestComposeSizeWithOptionDelta(t *testing.T) {
	f := newFixture()
	got := f.book.Compose(inventory.Selection{MenuItemID: f.milkTea, SizeID: &f.sizeL, ModifierOptionIDs: []uuid.UUID{f.pearlOpt}})
	assert.Equal(t, inventory.CostComplete, got.Status)
	assert.Equal(t, 8600.0, got.TotalVND)
}

func TestComposeMissingCostIsPartial(t *testing.T) {
	f := newFixture()
	got := f.book.Compose(inventory.Selection{MenuItemID: f.milkTea, SizeID: &f.sizeM})
	assert.Equal(t, inventory.CostPartial, got.Status)
	assert.Equal(t, 4000.0, got.TotalVND, "only costed lines are summed")
	require.Len(t, got.Lines, 2)
	assert.Nil(t, got.Lines[1].LineCost)
}

func TestComposeClampsNegativeDeltaAtZero(t *testing.T) {
	f := newFixture()
	got := f.book.Compose(inventory.Selection{MenuItemID: f.milkTea, SizeID: &f.sizeM, ModifierOptionIDs: []uuid.UUID{f.lessSugarOpt}})
	require.Len(t, got.Lines, 1, "10 g sugar - 15 g clamps to zero and is dropped")
	assert.Equal(t, inventory.CostComplete, got.Status)
}

func TestComposeWithoutBaseRecipeIsNone(t *testing.T) {
	f := newFixture()
	got := f.book.Compose(inventory.Selection{MenuItemID: uuid.New(), ModifierOptionIDs: []uuid.UUID{f.pearlOpt}})
	assert.Equal(t, inventory.CostNone, got.Status)
	assert.Empty(t, got.Lines)
	assert.Zero(t, got.TotalVND)
}

func TestComposeIgnoresItemRecipeWhenSizeSelected(t *testing.T) {
	f := newFixture()
	f.book.Items[f.milkTea] = []inventory.Line{{IngredientID: f.tea, StockQty: 999}}
	unknownSize := uuid.New()
	got := f.book.Compose(inventory.Selection{MenuItemID: f.milkTea, SizeID: &unknownSize})
	assert.Equal(t, inventory.CostNone, got.Status)
}

func TestOptionDelta(t *testing.T) {
	f := newFixture()
	d := f.book.OptionDelta(f.pearlOpt)
	assert.True(t, d.HasRecipe)
	assert.True(t, d.Complete)
	assert.Equal(t, 3000.0, d.CostVND)

	none := f.book.OptionDelta(uuid.New())
	assert.False(t, none.HasRecipe)
}
```

Run: `go test ./internal/inventory/`
Expected: FAIL, with undefined identifiers.

- [ ] **Step 2: Implement `errors.go` (sentinels only, for now)**

```go
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
```

- [ ] **Step 3: Implement `domain.go`**

```go
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

// ValidateName checks a normalized name against [1, max] code points.
func ValidateName(name string, max int) error {
	n := utf8.RuneCountInString(name)
	if n < 1 || n > max {
		return fmt.Errorf("%w: must be 1 to %d characters", ErrInvalidName, max)
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
```

- [ ] **Step 4: Implement `units.go`**

```go
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
```

- [ ] **Step 5: Implement `book.go`**

```go
package inventory

import "github.com/google/uuid"

// Line is one recipe line normalized to the Ingredient's Stock Unit.
type Line struct {
	IngredientID uuid.UUID
	StockQty     float64
}

// Batch is a prepared Ingredient's Batch Recipe; Yield is in its Stock Units.
type Batch struct {
	Lines []Line
	Yield float64
}

// Ingredient is what composition needs to know about an Ingredient.
type Ingredient struct {
	ID        uuid.UUID
	Name      string
	Kind      string
	StockUnit string
	Retired   bool
}

// Book is every Ingredient, current raw cost and Recipe, in memory. It is
// loaded per use (LoadBook); a single cafe's book is a few hundred rows.
type Book struct {
	Ingredients map[uuid.UUID]Ingredient
	RawCosts    map[uuid.UUID]float64
	Items       map[uuid.UUID][]Line
	Sizes       map[uuid.UUID][]Line
	Options     map[uuid.UUID][]Line
	Batches     map[uuid.UUID]Batch
}

// NewBook returns an empty Book.
func NewBook() *Book {
	return &Book{
		Ingredients: map[uuid.UUID]Ingredient{},
		RawCosts:    map[uuid.UUID]float64{},
		Items:       map[uuid.UUID][]Line{},
		Sizes:       map[uuid.UUID][]Line{},
		Options:     map[uuid.UUID][]Line{},
		Batches:     map[uuid.UUID]Batch{},
	}
}

// UnitCost returns the cost of one Stock Unit, and false when it has none: a
// raw Ingredient with no cost entry, or a prepared one without a Batch Recipe
// or with an uncosted raw input.
func (b *Book) UnitCost(id uuid.UUID) (float64, bool) {
	ing, ok := b.Ingredients[id]
	if !ok {
		return 0, false
	}
	if ing.Kind == KindRaw {
		c, ok := b.RawCosts[id]
		return c, ok
	}
	batch, ok := b.Batches[id]
	if !ok || batch.Yield <= 0 || len(batch.Lines) == 0 {
		return 0, false
	}
	total := 0.0
	for _, l := range batch.Lines {
		c, ok := b.RawCosts[l.IngredientID]
		if !ok {
			return 0, false
		}
		total += l.StockQty * c
	}
	return Round4(total / batch.Yield), true
}

// Selection is what one Preparation Unit was sold as.
type Selection struct {
	MenuItemID        uuid.UUID
	SizeID            *uuid.UUID
	ModifierOptionIDs []uuid.UUID
}

// ComposedLine is one consumed Ingredient. UnitCost and LineCost are nil when
// the Ingredient had no cost.
type ComposedLine struct {
	IngredientID uuid.UUID
	Name         string
	Kind         string
	StockUnit    string
	StockQty     float64
	UnitCost     *float64
	LineCost     *float64
}

// Composition is what one Preparation Unit consumed and cost (spec §3.5).
type Composition struct {
	Lines    []ComposedLine
	TotalVND float64
	Status   string
}

func (b *Book) baseLines(sel Selection) ([]Line, bool) {
	if sel.SizeID != nil {
		l, ok := b.Sizes[*sel.SizeID]
		return l, ok && len(l) > 0
	}
	l, ok := b.Items[sel.MenuItemID]
	return l, ok && len(l) > 0
}

// Compose applies spec §3.5: base Recipe (Size, else item), plus Option
// deltas summed per Ingredient, clamped at zero, each line costed.
func (b *Book) Compose(sel Selection) Composition {
	base, ok := b.baseLines(sel)
	if !ok {
		return Composition{Lines: []ComposedLine{}, Status: CostNone}
	}
	totals := map[uuid.UUID]float64{}
	var order []uuid.UUID
	add := func(lines []Line) {
		for _, l := range lines {
			if _, seen := totals[l.IngredientID]; !seen {
				order = append(order, l.IngredientID)
			}
			totals[l.IngredientID] += l.StockQty
		}
	}
	add(base)
	for _, id := range sel.ModifierOptionIDs {
		add(b.Options[id])
	}

	out := Composition{Lines: make([]ComposedLine, 0, len(order)), Status: CostComplete}
	total := 0.0
	for _, id := range order {
		qty := Round3(totals[id])
		if qty <= 0 {
			continue
		}
		ing, ok := b.Ingredients[id]
		if !ok {
			continue
		}
		line := ComposedLine{IngredientID: id, Name: ing.Name, Kind: ing.Kind, StockUnit: ing.StockUnit, StockQty: qty}
		if c, ok := b.UnitCost(id); ok {
			unit := c
			lineCost := Round2(qty * c)
			line.UnitCost, line.LineCost = &unit, &lineCost
			total += lineCost
		} else {
			out.Status = CostPartial
		}
		out.Lines = append(out.Lines, line)
	}
	out.TotalVND = Round2(total)
	return out
}

// Delta is the cost one Modifier Option's Recipe adds or removes on its own.
type Delta struct {
	HasRecipe bool
	CostVND   float64
	Complete  bool
}

// OptionDelta costs an Option's delta lines without a base Recipe.
func (b *Book) OptionDelta(id uuid.UUID) Delta {
	lines, ok := b.Options[id]
	if !ok || len(lines) == 0 {
		return Delta{Complete: true}
	}
	d := Delta{HasRecipe: true, Complete: true}
	for _, l := range lines {
		c, ok := b.UnitCost(l.IngredientID)
		if !ok {
			d.Complete = false
			continue
		}
		d.CostVND += l.StockQty * c
	}
	d.CostVND = Round2(d.CostVND)
	return d
}
```

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/inventory/`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/inventory
git commit -m "feat(inventory): add units, rounding and recipe composition

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: Runner, HTTP error mapping, ingredient commands

**Files:**
- Create: `internal/inventory/executor.go`, `dto.go`, `ingredients.go`
- Modify: `internal/inventory/errors.go` (append `mapDBError`, `httpErrors`, `MapHTTPError`)
- Modify: `internal/auth/domain.go` (Manager: `"inventory.administer"`, `"inventory.view_costs"`)
- Test: `internal/inventory/env_integration_test.go` (actor helpers only for now), `internal/inventory/ingredients_integration_test.go`

**Interfaces:**
- Consumes: Task 5 domain, Task 4 sqlc
- Produces:
  - `inventory.Runner` (= `command.Runner`), `inventory.Actor`, `NewRunner(db, q)`
  - `Commands` with `NewCommands(*Runner) *Commands` and these methods:
    - `CreateIngredient(ctx, Actor, CreateIngredientCommand) (int, IngredientResponse, error)`
    - `RenameIngredient(ctx, Actor, RenameIngredientCommand) (int, IngredientResponse, error)`
    - `RetireIngredient(ctx, Actor, RetireIngredientCommand) (int, IngredientResponse, error)`
    - `SetIngredientUnits(ctx, Actor, SetIngredientUnitsCommand) (int, IngredientResponse, error)`
    - `RecordCost(ctx, Actor, RecordCostCommand) (int, CostEntryResponse, error)`
  - DTOs listed in `dto.go` below
  - `MapHTTPError(error) error`

- [ ] **Step 1: Add capabilities**

In `internal/auth/domain.go`, add `"inventory.administer",` and `"inventory.view_costs",` to `RoleManager`.

- [ ] **Step 2: Executor**

`internal/inventory/executor.go`:

```go
package inventory

import (
	"context"
	"database/sql"

	"github.com/Mirai3103/open-pos/internal/platform/command"
	"github.com/Mirai3103/open-pos/internal/platform/database/sqlc"
	"github.com/google/uuid"
)

// Actor identifies the authenticated staff member.
type Actor = command.Actor

// Runner executes Inventory commands and reads.
type Runner = command.Runner

// Shared pipeline types.
type (
	MutationContext = command.MutationContext
	AuditRecord     = command.AuditRecord
	ReadContext     = command.ReadContext
)

var policy = command.Policy{
	DenialEventType:      EventAuthorizationDenied,
	LogScope:             "inventory",
	Attribution:          command.AttributeActorIfConfirmed,
	OnDenialAuditFailure: command.ReturnAuditError,
}

// NewRunner creates a Runner bound to the Inventory policy.
func NewRunner(db *sql.DB, queries *sqlc.Queries) *Runner {
	return command.NewRunner(db, queries, policy)
}

// executeMutation runs an Inventory mutation; every one needs
// inventory.administer.
func executeMutation[T any](ctx context.Context, r *Runner, actor Actor, requestID uuid.UUID,
	op string, fingerprint any, fn func(MutationContext) (int, T, AuditRecord, error),
) (int, T, error) {
	return command.ExecuteMutation(ctx, r, actor, command.MutationSpec{
		RequestID: requestID, Operation: op, Fingerprint: fingerprint,
		Required: []string{CapAdminister},
	}, fn)
}

// executeRead runs an Inventory read requiring one capability.
func executeRead[T any](ctx context.Context, r *Runner, actor Actor, required string,
	fn func(ReadContext) (T, error),
) (T, error) {
	return command.ExecuteRead(ctx, r, actor, command.ReadSpec{Required: []string{required}}, fn)
}
```

- [ ] **Step 3: DTOs**

`internal/inventory/dto.go`:

```go
package inventory

import (
	"time"

	"github.com/google/uuid"
)

// CreateIngredientCommand creates an Ingredient.
type CreateIngredientCommand struct {
	RequestID uuid.UUID `json:"request_id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	StockUnit string    `json:"stock_unit"`
}

// RenameIngredientCommand renames an Ingredient. IngredientID comes from the route.
type RenameIngredientCommand struct {
	RequestID    uuid.UUID `json:"request_id"`
	IngredientID uuid.UUID `json:"-"`
	Name         string    `json:"name"`
}

// RetireIngredientCommand retires an Ingredient. IngredientID comes from the route.
type RetireIngredientCommand struct {
	RequestID    uuid.UUID `json:"request_id"`
	IngredientID uuid.UUID `json:"-"`
}

// IngredientUnitInput is one Ingredient Unit in a replace-all set. ID is nil
// for a new unit.
type IngredientUnitInput struct {
	ID            *uuid.UUID `json:"id"`
	Name          string     `json:"name"`
	StockQuantity float64    `json:"stock_quantity"`
}

// SetIngredientUnitsCommand replaces an Ingredient's own units.
type SetIngredientUnitsCommand struct {
	RequestID    uuid.UUID             `json:"request_id"`
	IngredientID uuid.UUID             `json:"-"`
	Units        []IngredientUnitInput `json:"units"`
}

// RecordCostCommand appends a purchase-price entry to a raw Ingredient:
// AmountVND paid for Quantity of Unit.
type RecordCostCommand struct {
	RequestID    uuid.UUID `json:"request_id"`
	IngredientID uuid.UUID `json:"-"`
	AmountVND    int64     `json:"amount_vnd"`
	Quantity     float64   `json:"quantity"`
	Unit         UnitRef   `json:"unit"`
}

// IngredientUnitResponse is one Ingredient Unit.
type IngredientUnitResponse struct {
	ID            uuid.UUID `json:"id"`
	Name          string    `json:"name"`
	StockQuantity float64   `json:"stock_quantity"`
}

// IngredientResponse is one Ingredient. CostPerStockUnit is null when the
// caller lacks inventory.view_costs or the Ingredient has no cost.
type IngredientResponse struct {
	ID               uuid.UUID                `json:"id"`
	Name             string                   `json:"name"`
	Kind             string                   `json:"kind"`
	StockUnit        string                   `json:"stock_unit"`
	Retired          bool                     `json:"retired"`
	Units            []IngredientUnitResponse `json:"units"`
	CostPerStockUnit *float64                 `json:"cost_per_stock_unit"`
}

// CostEntryResponse is one recorded cost entry.
type CostEntryResponse struct {
	ID               uuid.UUID  `json:"id"`
	IngredientID     uuid.UUID  `json:"ingredient_id"`
	AmountVND        int64      `json:"amount_vnd"`
	Quantity         float64    `json:"quantity"`
	UnitCode         *string    `json:"unit_code"`
	IngredientUnitID *uuid.UUID `json:"ingredient_unit_id"`
	CostPerStockUnit float64    `json:"cost_per_stock_unit"`
	RecordedAt       time.Time  `json:"recorded_at"`
}
```

- [ ] **Step 4: Error mapping**

Append to `internal/inventory/errors.go` (add the imports `database/sql`, `fmt`, `net/http`, `github.com/jackc/pgx/v5/pgconn`, `github.com/Mirai3103/open-pos/internal/response`):

```go
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
```

- [ ] **Step 5: Ingredient commands**

`internal/inventory/ingredients.go`:

```go
package inventory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/Mirai3103/open-pos/internal/platform/database/sqlc"
	"github.com/google/uuid"
)

// Commands holds every Inventory mutation.
type Commands struct{ runner *Runner }

// NewCommands creates Commands on runner.
func NewCommands(runner *Runner) *Commands { return &Commands{runner: runner} }

func toIngredientResponse(row sqlc.InventoryIngredient, units []sqlc.InventoryIngredientUnit) IngredientResponse {
	out := IngredientResponse{
		ID: row.ID, Name: row.Name, Kind: row.Kind, StockUnit: row.StockUnitCode,
		Retired: row.RetiredAt.Valid, Units: make([]IngredientUnitResponse, 0, len(units)),
	}
	for _, u := range units {
		out.Units = append(out.Units, IngredientUnitResponse{ID: u.ID, Name: u.Name, StockQuantity: u.StockQuantity})
	}
	return out
}

func lockIngredient(ctx context.Context, q *sqlc.Queries, id uuid.UUID) (sqlc.InventoryIngredient, error) {
	row, err := q.InventoryLockIngredient(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return row, fmt.Errorf("%w: %s", ErrIngredientNotFound, id)
	}
	if err != nil {
		return row, fmt.Errorf("lock ingredient: %w", err)
	}
	return row, nil
}

func ingredientWithUnits(ctx context.Context, q *sqlc.Queries, row sqlc.InventoryIngredient) (IngredientResponse, error) {
	units, err := q.InventoryListIngredientUnits(ctx, []uuid.UUID{row.ID})
	if err != nil {
		return IngredientResponse{}, fmt.Errorf("list ingredient units: %w", err)
	}
	return toIngredientResponse(row, units), nil
}

// CreateIngredient creates a raw or prepared Ingredient.
func (c *Commands) CreateIngredient(ctx context.Context, actor Actor, cmd CreateIngredientCommand,
) (int, IngredientResponse, error) {
	name := NormalizeName(cmd.Name)
	if err := ValidateName(name, MaxIngredientNameLength); err != nil {
		return 0, IngredientResponse{}, err
	}
	if cmd.Kind != KindRaw && cmd.Kind != KindPrepared {
		return 0, IngredientResponse{}, fmt.Errorf("%w: %q", ErrInvalidKind, cmd.Kind)
	}
	if !IsStockUnit(cmd.StockUnit) {
		return 0, IngredientResponse{}, fmt.Errorf("%w: stock unit must be g, ml or piece", ErrUnitNotConvertible)
	}
	fp := map[string]string{"name": name, "kind": cmd.Kind, "stock_unit": cmd.StockUnit}
	return executeMutation(ctx, c.runner, actor, cmd.RequestID, OpCreateIngredient, fp,
		func(mc MutationContext) (int, IngredientResponse, AuditRecord, error) {
			row, err := mc.Queries.InventoryCreateIngredient(ctx, sqlc.InventoryCreateIngredientParams{
				Name: name, Kind: cmd.Kind, StockUnitCode: cmd.StockUnit,
			})
			if err != nil {
				return 0, IngredientResponse{}, AuditRecord{}, mapDBError(err)
			}
			return http.StatusCreated, toIngredientResponse(row, nil), AuditRecord{
				EventType: EventIngredientCreated,
				Details: map[string]any{"ingredient_id": row.ID, "name": name,
					"kind": row.Kind, "stock_unit": row.StockUnitCode},
			}, nil
		})
}

// RenameIngredient renames an active Ingredient. The same name is a no-op.
func (c *Commands) RenameIngredient(ctx context.Context, actor Actor, cmd RenameIngredientCommand,
) (int, IngredientResponse, error) {
	name := NormalizeName(cmd.Name)
	if err := ValidateName(name, MaxIngredientNameLength); err != nil {
		return 0, IngredientResponse{}, err
	}
	fp := map[string]any{"ingredient_id": cmd.IngredientID, "name": name}
	return executeMutation(ctx, c.runner, actor, cmd.RequestID, OpRenameIngredient, fp,
		func(mc MutationContext) (int, IngredientResponse, AuditRecord, error) {
			q := mc.Queries
			row, err := lockIngredient(ctx, q, cmd.IngredientID)
			if err != nil {
				return 0, IngredientResponse{}, AuditRecord{}, err
			}
			if row.RetiredAt.Valid {
				return 0, IngredientResponse{}, AuditRecord{}, fmt.Errorf("%w: %s", ErrIngredientRetired, row.ID)
			}
			audit := AuditRecord{}
			if row.Name != name {
				before := row.Name
				if row, err = q.InventoryRenameIngredient(ctx, sqlc.InventoryRenameIngredientParams{ID: row.ID, Name: name}); err != nil {
					return 0, IngredientResponse{}, AuditRecord{}, mapDBError(err)
				}
				audit = AuditRecord{EventType: EventIngredientRenamed, Details: map[string]any{
					"ingredient_id": row.ID, "before_name": before, "after_name": name}}
			}
			out, err := ingredientWithUnits(ctx, q, row)
			return http.StatusOK, out, audit, err
		})
}

// RetireIngredient retires an Ingredient. Retiring a retired one is a no-op.
// Existing recipe lines keep it (spec §3.4).
func (c *Commands) RetireIngredient(ctx context.Context, actor Actor, cmd RetireIngredientCommand,
) (int, IngredientResponse, error) {
	fp := map[string]any{"ingredient_id": cmd.IngredientID}
	return executeMutation(ctx, c.runner, actor, cmd.RequestID, OpRetireIngredient, fp,
		func(mc MutationContext) (int, IngredientResponse, AuditRecord, error) {
			q := mc.Queries
			row, err := lockIngredient(ctx, q, cmd.IngredientID)
			if err != nil {
				return 0, IngredientResponse{}, AuditRecord{}, err
			}
			audit := AuditRecord{}
			if !row.RetiredAt.Valid {
				if row, err = q.InventoryRetireIngredient(ctx, row.ID); err != nil {
					return 0, IngredientResponse{}, AuditRecord{}, fmt.Errorf("retire ingredient: %w", err)
				}
				audit = AuditRecord{EventType: EventIngredientRetired, Details: map[string]any{"ingredient_id": row.ID}}
			}
			out, err := ingredientWithUnits(ctx, q, row)
			return http.StatusOK, out, audit, err
		})
}

// SetIngredientUnits replaces an Ingredient's own units: inputs with an id
// update that unit, inputs without one are added, and missing units are
// removed unless a recipe line or cost entry references them.
func (c *Commands) SetIngredientUnits(ctx context.Context, actor Actor, cmd SetIngredientUnitsCommand,
) (int, IngredientResponse, error) {
	inputs := make([]IngredientUnitInput, len(cmd.Units))
	for i, u := range cmd.Units {
		u.Name = NormalizeName(u.Name)
		if err := ValidateName(u.Name, MaxUnitNameLength); err != nil {
			return 0, IngredientResponse{}, err
		}
		u.StockQuantity = Round3(u.StockQuantity)
		if u.StockQuantity <= 0 {
			return 0, IngredientResponse{}, fmt.Errorf("%w: unit %q must hold a positive quantity", ErrInvalidQuantity, u.Name)
		}
		inputs[i] = u
	}
	fp := map[string]any{"ingredient_id": cmd.IngredientID, "units": inputs}
	return executeMutation(ctx, c.runner, actor, cmd.RequestID, OpSetIngredientUnits, fp,
		func(mc MutationContext) (int, IngredientResponse, AuditRecord, error) {
			q := mc.Queries
			fail := func(err error) (int, IngredientResponse, AuditRecord, error) {
				return 0, IngredientResponse{}, AuditRecord{}, err
			}
			row, err := lockIngredient(ctx, q, cmd.IngredientID)
			if err != nil {
				return fail(err)
			}
			if row.RetiredAt.Valid {
				return fail(fmt.Errorf("%w: %s", ErrIngredientRetired, row.ID))
			}
			existing, err := q.InventoryListIngredientUnits(ctx, []uuid.UUID{row.ID})
			if err != nil {
				return fail(fmt.Errorf("list ingredient units: %w", err))
			}
			kept := map[uuid.UUID]bool{}
			for _, in := range inputs {
				if in.ID != nil {
					kept[*in.ID] = true
				}
			}
			// Removals first, so a rename onto a removed unit's name does not
			// collide with it.
			for _, u := range existing {
				if kept[u.ID] {
					continue
				}
				inUse, err := q.InventoryIngredientUnitInUse(ctx, u.ID)
				if err != nil {
					return fail(fmt.Errorf("check ingredient unit use: %w", err))
				}
				if inUse {
					return fail(fmt.Errorf("%w: %s", ErrIngredientUnitInUse, u.Name))
				}
				if err := q.InventoryDeleteIngredientUnit(ctx, sqlc.InventoryDeleteIngredientUnitParams{ID: u.ID, IngredientID: row.ID}); err != nil {
					return fail(fmt.Errorf("delete ingredient unit: %w", err))
				}
			}
			for _, in := range inputs {
				if in.ID != nil {
					_, err = q.InventoryUpdateIngredientUnit(ctx, sqlc.InventoryUpdateIngredientUnitParams{
						ID: *in.ID, IngredientID: row.ID, Name: in.Name, StockQuantity: in.StockQuantity,
					})
					if errors.Is(err, sql.ErrNoRows) {
						return fail(fmt.Errorf("%w: %s", ErrIngredientUnitNotFound, *in.ID))
					}
				} else {
					_, err = q.InventoryInsertIngredientUnit(ctx, sqlc.InventoryInsertIngredientUnitParams{
						IngredientID: row.ID, Name: in.Name, StockQuantity: in.StockQuantity,
					})
				}
				if err != nil {
					return fail(mapDBError(err))
				}
			}
			out, err := ingredientWithUnits(ctx, q, row)
			if err != nil {
				return fail(err)
			}
			return http.StatusOK, out, AuditRecord{EventType: EventIngredientUnitsSet, Details: map[string]any{
				"ingredient_id": row.ID, "unit_count": len(inputs)}}, nil
		})
}

// resolveFactor returns how many Stock Units one unit of ref is for ing.
func resolveFactor(ctx context.Context, q *sqlc.Queries, ing sqlc.InventoryIngredient, ref UnitRef) (float64, error) {
	if err := ref.Validate(); err != nil {
		return 0, err
	}
	if ref.UnitCode != nil {
		return SystemFactor(ing.StockUnitCode, *ref.UnitCode)
	}
	u, err := q.InventoryGetIngredientUnit(ctx, sqlc.InventoryGetIngredientUnitParams{ID: *ref.IngredientUnitID, IngredientID: ing.ID})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("%w: %s", ErrIngredientUnitNotFound, *ref.IngredientUnitID)
	}
	if err != nil {
		return 0, fmt.Errorf("get ingredient unit: %w", err)
	}
	return u.StockQuantity, nil
}

func nullString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

func nullUUID(id *uuid.UUID) uuid.NullUUID {
	if id == nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: *id, Valid: true}
}

func toCostEntry(row sqlc.InventoryIngredientCost) CostEntryResponse {
	out := CostEntryResponse{ID: row.ID, IngredientID: row.IngredientID, AmountVND: row.AmountVnd,
		Quantity: row.Quantity, CostPerStockUnit: row.CostPerStockUnit, RecordedAt: row.RecordedAt}
	if row.UnitCode.Valid {
		code := row.UnitCode.String
		out.UnitCode = &code
	}
	if row.IngredientUnitID.Valid {
		id := row.IngredientUnitID.UUID
		out.IngredientUnitID = &id
	}
	return out
}

// RecordCost appends a purchase-price entry to a raw Ingredient and derives
// its cost per Stock Unit.
func (c *Commands) RecordCost(ctx context.Context, actor Actor, cmd RecordCostCommand,
) (int, CostEntryResponse, error) {
	if cmd.AmountVND <= 0 {
		return 0, CostEntryResponse{}, ErrInvalidAmount
	}
	qty := Round3(cmd.Quantity)
	if qty <= 0 {
		return 0, CostEntryResponse{}, fmt.Errorf("%w: must be positive", ErrInvalidQuantity)
	}
	if err := cmd.Unit.Validate(); err != nil {
		return 0, CostEntryResponse{}, err
	}
	fp := map[string]any{"ingredient_id": cmd.IngredientID, "amount_vnd": cmd.AmountVND,
		"quantity": qty, "unit": cmd.Unit}
	return executeMutation(ctx, c.runner, actor, cmd.RequestID, OpRecordCost, fp,
		func(mc MutationContext) (int, CostEntryResponse, AuditRecord, error) {
			q := mc.Queries
			fail := func(err error) (int, CostEntryResponse, AuditRecord, error) {
				return 0, CostEntryResponse{}, AuditRecord{}, err
			}
			ing, err := lockIngredient(ctx, q, cmd.IngredientID)
			if err != nil {
				return fail(err)
			}
			if ing.RetiredAt.Valid {
				return fail(fmt.Errorf("%w: %s", ErrIngredientRetired, ing.ID))
			}
			if ing.Kind != KindRaw {
				return fail(ErrCostOnPrepared)
			}
			factor, err := resolveFactor(ctx, q, ing, cmd.Unit)
			if err != nil {
				return fail(err)
			}
			perStock := Round4(float64(cmd.AmountVND) / Round3(qty*factor))
			row, err := q.InventoryInsertIngredientCost(ctx, sqlc.InventoryInsertIngredientCostParams{
				IngredientID: ing.ID, AmountVnd: cmd.AmountVND, Quantity: qty,
				UnitCode: nullString(cmd.Unit.UnitCode), IngredientUnitID: nullUUID(cmd.Unit.IngredientUnitID),
				CostPerStockUnit: perStock, ActorStaffIdentityID: mc.Actor.StaffID,
			})
			if err != nil {
				return fail(fmt.Errorf("insert ingredient cost: %w", err))
			}
			return http.StatusCreated, toCostEntry(row), AuditRecord{EventType: EventCostRecorded, Details: map[string]any{
				"ingredient_id": ing.ID, "amount_vnd": cmd.AmountVND, "quantity": qty,
				"cost_per_stock_unit": perStock}}, nil
		})
}
```

- [ ] **Step 6: Test env helpers and ingredient integration tests**

`internal/inventory/env_integration_test.go` (Task 9 extends this file):

```go
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
```

`internal/inventory/ingredients_integration_test.go`:

```go
//go:build integration

package inventory_test

import (
	"context"
	"testing"

	"github.com/Mirai3103/open-pos/internal/auth"
	"github.com/Mirai3103/open-pos/internal/inventory"
	"github.com/Mirai3103/open-pos/internal/platform/command"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateIngredientAndDuplicateName(t *testing.T) {
	c := newCommands()
	manager := seedActor(t, auth.RoleManager)
	name := uniqueName("Sữa đặc")

	res := createIngredient(t, c, manager, "  "+name+"  ", inventory.KindRaw, "g")
	assert.Equal(t, name, res.Name)
	assert.Equal(t, "g", res.StockUnit)

	_, _, err := c.CreateIngredient(context.Background(), manager, inventory.CreateIngredientCommand{
		RequestID: uuid.New(), Name: name, Kind: inventory.KindRaw, StockUnit: "g",
	})
	require.ErrorIs(t, err, inventory.ErrDuplicateIngredientName)
}

func TestCreateIngredientValidation(t *testing.T) {
	c := newCommands()
	manager := seedActor(t, auth.RoleManager)
	_, _, err := c.CreateIngredient(context.Background(), manager, inventory.CreateIngredientCommand{
		RequestID: uuid.New(), Name: uniqueName("X"), Kind: inventory.KindRaw, StockUnit: "kg",
	})
	require.ErrorIs(t, err, inventory.ErrUnitNotConvertible)
	_, _, err = c.CreateIngredient(context.Background(), manager, inventory.CreateIngredientCommand{
		RequestID: uuid.New(), Name: uniqueName("X"), Kind: "BOUGHT", StockUnit: "g",
	})
	require.ErrorIs(t, err, inventory.ErrInvalidKind)
}

func TestCashierCannotAdminister(t *testing.T) {
	_, _, err := newCommands().CreateIngredient(context.Background(), seedActor(t, auth.RoleCashier),
		inventory.CreateIngredientCommand{RequestID: uuid.New(), Name: uniqueName("X"), Kind: inventory.KindRaw, StockUnit: "g"})
	require.ErrorIs(t, err, command.ErrForbidden)
}

func TestIngredientUnitsAndCost(t *testing.T) {
	c := newCommands()
	ctx := context.Background()
	manager := seedActor(t, auth.RoleManager)
	milk := createIngredient(t, c, manager, uniqueName("Sữa đặc"), inventory.KindRaw, "g")

	_, withUnits, err := c.SetIngredientUnits(ctx, manager, inventory.SetIngredientUnitsCommand{
		RequestID: uuid.New(), IngredientID: milk.ID,
		Units: []inventory.IngredientUnitInput{{Name: "hộp", StockQuantity: 380}},
	})
	require.NoError(t, err)
	require.Len(t, withUnits.Units, 1)
	can := withUnits.Units[0].ID

	entry := recordCost(t, c, manager, milk.ID, 25000, 1, inventory.UnitRef{IngredientUnitID: &can})
	assert.Equal(t, 65.7895, entry.CostPerStockUnit)

	byKg := recordCost(t, c, manager, milk.ID, 120000, 1, code("kg"))
	assert.Equal(t, 120.0, byKg.CostPerStockUnit)

	_, _, err = c.RecordCost(ctx, manager, inventory.RecordCostCommand{
		RequestID: uuid.New(), IngredientID: milk.ID, AmountVND: 1000, Quantity: 1, Unit: code("ml"),
	})
	require.ErrorIs(t, err, inventory.ErrUnitNotConvertible)

	// The can is referenced by a cost entry: removing it is refused.
	_, _, err = c.SetIngredientUnits(ctx, manager, inventory.SetIngredientUnitsCommand{
		RequestID: uuid.New(), IngredientID: milk.ID, Units: []inventory.IngredientUnitInput{},
	})
	require.ErrorIs(t, err, inventory.ErrIngredientUnitInUse)
}

func TestCostOnPreparedIsRejected(t *testing.T) {
	c := newCommands()
	manager := seedActor(t, auth.RoleManager)
	base := createIngredient(t, c, manager, uniqueName("Cốt"), inventory.KindPrepared, "ml")
	_, _, err := c.RecordCost(context.Background(), manager, inventory.RecordCostCommand{
		RequestID: uuid.New(), IngredientID: base.ID, AmountVND: 1000, Quantity: 1, Unit: code("l"),
	})
	require.ErrorIs(t, err, inventory.ErrCostOnPrepared)
}

func TestRenameAndRetire(t *testing.T) {
	c := newCommands()
	ctx := context.Background()
	manager := seedActor(t, auth.RoleManager)
	ing := createIngredient(t, c, manager, uniqueName("Đường"), inventory.KindRaw, "g")

	newName := uniqueName("Đường cát")
	_, renamed, err := c.RenameIngredient(ctx, manager, inventory.RenameIngredientCommand{
		RequestID: uuid.New(), IngredientID: ing.ID, Name: newName,
	})
	require.NoError(t, err)
	assert.Equal(t, newName, renamed.Name)

	_, retired, err := c.RetireIngredient(ctx, manager, inventory.RetireIngredientCommand{RequestID: uuid.New(), IngredientID: ing.ID})
	require.NoError(t, err)
	assert.True(t, retired.Retired)

	_, _, err = c.RenameIngredient(ctx, manager, inventory.RenameIngredientCommand{
		RequestID: uuid.New(), IngredientID: ing.ID, Name: uniqueName("Y"),
	})
	require.ErrorIs(t, err, inventory.ErrIngredientRetired)
}
```

- [ ] **Step 7: Run the tests**

Run: `go build ./... && TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -count=1 -tags=integration ./internal/inventory/`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/inventory internal/auth/domain.go
git commit -m "feat(inventory): add ingredient, unit and cost commands

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: Recipe and Batch Recipe commands, book loading

**Files:**
- Create: `internal/inventory/recipes.go`, `loadbook.go`
- Modify: `internal/inventory/dto.go` (append the recipe DTOs)
- Test: `internal/inventory/recipes_integration_test.go`
- Modify: `internal/inventory/env_integration_test.go` (append the catalog seed helpers)

**Interfaces:**
- Consumes: Task 6 `Commands`, `resolveFactor`, `lockIngredient`, `nullUUID`, `nullString`
- Produces:
  - `(*Commands).SetRecipe(ctx, Actor, SetRecipeCommand) (int, RecipeResponse, error)`
  - `(*Commands).SetBatchRecipe(ctx, Actor, SetBatchRecipeCommand) (int, RecipeResponse, error)`
  - `LoadBook(ctx, q *sqlc.Queries) (*Book, error)`
  - `loadRecipeResponse(ctx, q, ownerKind string, ownerID uuid.UUID) (RecipeResponse, error)`
  - DTOs `RecipeLineInput`, `SetRecipeCommand`, `SetBatchRecipeCommand`, `RecipeLineResponse`, `RecipeResponse`

- [ ] **Step 1: DTOs**

Append to `internal/inventory/dto.go`:

```go
// RecipeLineInput is one Recipe line as entered.
type RecipeLineInput struct {
	IngredientID uuid.UUID `json:"ingredient_id"`
	Quantity     float64   `json:"quantity"`
	Unit         UnitRef   `json:"unit"`
}

// SetRecipeCommand replaces a sale Recipe's lines; an empty list deletes it.
// OwnerKind and OwnerID come from the route.
type SetRecipeCommand struct {
	RequestID uuid.UUID         `json:"request_id"`
	OwnerKind string            `json:"-"`
	OwnerID   uuid.UUID         `json:"-"`
	Lines     []RecipeLineInput `json:"lines"`
}

// SetBatchRecipeCommand replaces a prepared Ingredient's Batch Recipe.
// YieldQuantity is in the prepared Ingredient's Stock Units. An empty Lines
// deletes the Batch Recipe.
type SetBatchRecipeCommand struct {
	RequestID     uuid.UUID         `json:"request_id"`
	IngredientID  uuid.UUID         `json:"-"`
	YieldQuantity float64           `json:"yield_quantity"`
	Lines         []RecipeLineInput `json:"lines"`
}

// RecipeLineResponse is one stored line, in its entered unit.
type RecipeLineResponse struct {
	IngredientID       uuid.UUID  `json:"ingredient_id"`
	IngredientName     string     `json:"ingredient_name"`
	IngredientKind     string     `json:"ingredient_kind"`
	StockUnit          string     `json:"stock_unit"`
	Quantity           float64    `json:"quantity"`
	UnitCode           *string    `json:"unit_code"`
	IngredientUnitID   *uuid.UUID `json:"ingredient_unit_id"`
	IngredientUnitName *string    `json:"ingredient_unit_name"`
}

// RecipeResponse is one Recipe or Batch Recipe. Lines is empty when none exists.
type RecipeResponse struct {
	OwnerKind     string               `json:"owner_kind"`
	OwnerID       uuid.UUID            `json:"owner_id"`
	YieldQuantity *float64             `json:"yield_quantity"`
	Lines         []RecipeLineResponse `json:"lines"`
}
```

- [ ] **Step 2: Implement `recipes.go`**

```go
package inventory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/Mirai3103/open-pos/internal/platform/database/sqlc"
	"github.com/google/uuid"
)

type resolvedLine struct {
	ingredientID uuid.UUID
	quantity     float64
	unit         UnitRef
}

// prevalidateLines checks the input without the database.
func prevalidateLines(ownerKind string, lines []RecipeLineInput) ([]RecipeLineInput, error) {
	if len(lines) > MaxRecipeLines {
		return nil, fmt.Errorf("%w: at most %d", ErrTooManyLines, MaxRecipeLines)
	}
	seen := map[uuid.UUID]bool{}
	out := make([]RecipeLineInput, len(lines))
	for i, l := range lines {
		if seen[l.IngredientID] {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateRecipeIngredient, l.IngredientID)
		}
		seen[l.IngredientID] = true
		if err := ValidateLineQuantity(ownerKind, l.Quantity); err != nil {
			return nil, err
		}
		if err := l.Unit.Validate(); err != nil {
			return nil, err
		}
		l.Quantity = Round3(l.Quantity)
		out[i] = l
	}
	return out, nil
}

// resolveLines checks each line's Ingredient and unit inside the transaction.
// rawOnly enforces the one-level Batch Recipe rule.
func resolveLines(ctx context.Context, q *sqlc.Queries, lines []RecipeLineInput, rawOnly bool) ([]resolvedLine, error) {
	out := make([]resolvedLine, 0, len(lines))
	for _, l := range lines {
		ing, err := q.InventoryGetIngredient(ctx, l.IngredientID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", ErrIngredientNotFound, l.IngredientID)
		}
		if err != nil {
			return nil, fmt.Errorf("get ingredient: %w", err)
		}
		if ing.RetiredAt.Valid {
			return nil, fmt.Errorf("%w: %s", ErrIngredientRetired, ing.Name)
		}
		if rawOnly && ing.Kind != KindRaw {
			return nil, fmt.Errorf("%w: %s", ErrBatchRecipeNotRaw, ing.Name)
		}
		if _, err := resolveFactor(ctx, q, ing, l.Unit); err != nil {
			return nil, err
		}
		out = append(out, resolvedLine{ingredientID: ing.ID, quantity: l.Quantity, unit: l.Unit})
	}
	return out, nil
}

// writeRecipe replaces the owner's recipe rows; empty lines delete it.
func writeRecipe(ctx context.Context, q *sqlc.Queries, ownerKind string, ownerID uuid.UUID,
	yield *float64, lines []resolvedLine,
) error {
	recipeID, err := q.InventoryFindRecipe(ctx, sqlc.InventoryFindRecipeParams{OwnerKind: ownerKind, OwnerID: ownerID})
	exists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("find recipe: %w", err)
	}
	if len(lines) == 0 {
		if exists {
			if err := q.InventoryDeleteRecipe(ctx, recipeID); err != nil {
				return fmt.Errorf("delete recipe: %w", err)
			}
		}
		return nil
	}
	nullYield := sql.NullFloat64{}
	if yield != nil {
		nullYield = sql.NullFloat64{Float64: *yield, Valid: true}
	}
	if exists {
		if err := q.InventoryUpdateRecipeYield(ctx, sqlc.InventoryUpdateRecipeYieldParams{ID: recipeID, YieldQuantity: nullYield}); err != nil {
			return fmt.Errorf("update recipe: %w", err)
		}
		if err := q.InventoryDeleteRecipeLines(ctx, recipeID); err != nil {
			return fmt.Errorf("delete recipe lines: %w", err)
		}
	} else {
		params := sqlc.InventoryInsertRecipeParams{OwnerKind: ownerKind, YieldQuantity: nullYield}
		owner := uuid.NullUUID{UUID: ownerID, Valid: true}
		switch ownerKind {
		case OwnerMenuItem:
			params.MenuItemID = owner
		case OwnerSize:
			params.SizeID = owner
		case OwnerModifierOption:
			params.ModifierOptionID = owner
		case OwnerIngredient:
			params.IngredientID = owner
		}
		if recipeID, err = q.InventoryInsertRecipe(ctx, params); err != nil {
			return fmt.Errorf("insert recipe: %w", err)
		}
	}
	for i, l := range lines {
		if err := q.InventoryInsertRecipeLine(ctx, sqlc.InventoryInsertRecipeLineParams{
			RecipeID: recipeID, IngredientID: l.ingredientID, Quantity: l.quantity,
			UnitCode: nullString(l.unit.UnitCode), IngredientUnitID: nullUUID(l.unit.IngredientUnitID),
			Position: int32(i),
		}); err != nil {
			return fmt.Errorf("insert recipe line: %w", err)
		}
	}
	return nil
}

// verifyOwner checks the sale Recipe owner exists, is not retired, and (for
// a Menu Item) has no Sizes.
func verifyOwner(ctx context.Context, q *sqlc.Queries, kind string, id uuid.UUID) error {
	notFound := fmt.Errorf("%w: %s %s", ErrRecipeOwnerNotFound, kind, id)
	switch kind {
	case OwnerMenuItem:
		row, err := q.InventoryGetMenuItemForRecipe(ctx, id)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && row.RetiredAt.Valid) {
			return notFound
		}
		if err != nil {
			return fmt.Errorf("get menu item: %w", err)
		}
		if row.HasSizes {
			return ErrItemHasSizes
		}
	case OwnerSize:
		row, err := q.InventoryGetSizeForRecipe(ctx, id)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && row.RetiredAt.Valid) {
			return notFound
		}
		if err != nil {
			return fmt.Errorf("get size: %w", err)
		}
	case OwnerModifierOption:
		row, err := q.InventoryGetOptionForRecipe(ctx, id)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && row.RetiredAt.Valid) {
			return notFound
		}
		if err != nil {
			return fmt.Errorf("get modifier option: %w", err)
		}
	default:
		return notFound
	}
	return nil
}

// SetRecipe replaces a Menu Item, Size or Modifier Option Recipe.
func (c *Commands) SetRecipe(ctx context.Context, actor Actor, cmd SetRecipeCommand) (int, RecipeResponse, error) {
	lines, err := prevalidateLines(cmd.OwnerKind, cmd.Lines)
	if err != nil {
		return 0, RecipeResponse{}, err
	}
	fp := map[string]any{"owner_kind": cmd.OwnerKind, "owner_id": cmd.OwnerID, "lines": lines}
	return executeMutation(ctx, c.runner, actor, cmd.RequestID, OpSetRecipe, fp,
		func(mc MutationContext) (int, RecipeResponse, AuditRecord, error) {
			q := mc.Queries
			fail := func(err error) (int, RecipeResponse, AuditRecord, error) {
				return 0, RecipeResponse{}, AuditRecord{}, err
			}
			if err := verifyOwner(ctx, q, cmd.OwnerKind, cmd.OwnerID); err != nil {
				return fail(err)
			}
			resolved, err := resolveLines(ctx, q, lines, false)
			if err != nil {
				return fail(err)
			}
			if err := writeRecipe(ctx, q, cmd.OwnerKind, cmd.OwnerID, nil, resolved); err != nil {
				return fail(err)
			}
			out, err := loadRecipeResponse(ctx, q, cmd.OwnerKind, cmd.OwnerID)
			if err != nil {
				return fail(err)
			}
			return http.StatusOK, out, AuditRecord{EventType: EventRecipeSet, Details: map[string]any{
				"owner_kind": cmd.OwnerKind, "owner_id": cmd.OwnerID, "line_count": len(resolved)}}, nil
		})
}

// SetBatchRecipe replaces a prepared Ingredient's Batch Recipe and yield.
func (c *Commands) SetBatchRecipe(ctx context.Context, actor Actor, cmd SetBatchRecipeCommand) (int, RecipeResponse, error) {
	lines, err := prevalidateLines(OwnerIngredient, cmd.Lines)
	if err != nil {
		return 0, RecipeResponse{}, err
	}
	yield := Round3(cmd.YieldQuantity)
	if len(lines) > 0 && yield <= 0 {
		return 0, RecipeResponse{}, ErrInvalidYield
	}
	fp := map[string]any{"ingredient_id": cmd.IngredientID, "yield": yield, "lines": lines}
	return executeMutation(ctx, c.runner, actor, cmd.RequestID, OpSetBatchRecipe, fp,
		func(mc MutationContext) (int, RecipeResponse, AuditRecord, error) {
			q := mc.Queries
			fail := func(err error) (int, RecipeResponse, AuditRecord, error) {
				return 0, RecipeResponse{}, AuditRecord{}, err
			}
			ing, err := lockIngredient(ctx, q, cmd.IngredientID)
			if err != nil {
				return fail(err)
			}
			if ing.RetiredAt.Valid {
				return fail(fmt.Errorf("%w: %s", ErrIngredientRetired, ing.ID))
			}
			if ing.Kind != KindPrepared {
				return fail(ErrBatchRecipeOnRaw)
			}
			resolved, err := resolveLines(ctx, q, lines, true)
			if err != nil {
				return fail(err)
			}
			if err := writeRecipe(ctx, q, OwnerIngredient, ing.ID, &yield, resolved); err != nil {
				return fail(err)
			}
			out, err := loadRecipeResponse(ctx, q, OwnerIngredient, ing.ID)
			if err != nil {
				return fail(err)
			}
			return http.StatusOK, out, AuditRecord{EventType: EventBatchRecipeSet, Details: map[string]any{
				"ingredient_id": ing.ID, "yield_quantity": yield, "line_count": len(resolved)}}, nil
		})
}

// loadRecipeResponse reads one Recipe without locking, for responses and reads.
func loadRecipeResponse(ctx context.Context, q *sqlc.Queries, ownerKind string, ownerID uuid.UUID) (RecipeResponse, error) {
	out := RecipeResponse{OwnerKind: ownerKind, OwnerID: ownerID, Lines: []RecipeLineResponse{}}
	recipe, err := q.InventoryGetRecipe(ctx, sqlc.InventoryGetRecipeParams{OwnerKind: ownerKind, OwnerID: ownerID})
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, fmt.Errorf("get recipe: %w", err)
	}
	if recipe.YieldQuantity.Valid {
		y := recipe.YieldQuantity.Float64
		out.YieldQuantity = &y
	}
	rows, err := q.InventoryListRecipeLines(ctx, recipe.ID)
	if err != nil {
		return out, fmt.Errorf("list recipe lines: %w", err)
	}
	for _, r := range rows {
		line := RecipeLineResponse{IngredientID: r.IngredientID, IngredientName: r.IngredientName,
			IngredientKind: r.IngredientKind, StockUnit: r.StockUnitCode, Quantity: r.Quantity}
		if r.UnitCode.Valid {
			s := r.UnitCode.String
			line.UnitCode = &s
		}
		if r.IngredientUnitID.Valid {
			id := r.IngredientUnitID.UUID
			line.IngredientUnitID = &id
		}
		if r.IngredientUnitName.Valid {
			s := r.IngredientUnitName.String
			line.IngredientUnitName = &s
		}
		out.Lines = append(out.Lines, line)
	}
	return out, nil
}
```

`internal/inventory/loadbook.go`:

```go
package inventory

import (
	"context"
	"fmt"

	"github.com/Mirai3103/open-pos/internal/platform/database/sqlc"
)

// LoadBook reads every Ingredient, current raw cost and Recipe into a Book.
func LoadBook(ctx context.Context, q *sqlc.Queries) (*Book, error) {
	b := NewBook()
	ings, err := q.InventoryListIngredients(ctx)
	if err != nil {
		return nil, fmt.Errorf("list ingredients: %w", err)
	}
	for _, i := range ings {
		b.Ingredients[i.ID] = Ingredient{ID: i.ID, Name: i.Name, Kind: i.Kind,
			StockUnit: i.StockUnitCode, Retired: i.RetiredAt.Valid}
	}
	costs, err := q.InventoryListCurrentCosts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list current costs: %w", err)
	}
	for _, c := range costs {
		b.RawCosts[c.IngredientID] = c.CostPerStockUnit
	}
	rows, err := q.InventoryListBookLines(ctx)
	if err != nil {
		return nil, fmt.Errorf("list recipe lines: %w", err)
	}
	for _, r := range rows {
		var factor float64
		switch {
		case r.FactorToBase.Valid:
			factor = r.FactorToBase.Float64
		case r.StockQuantity.Valid:
			factor = r.StockQuantity.Float64
		default:
			continue
		}
		line := Line{IngredientID: r.IngredientID, StockQty: Round3(r.Quantity * factor)}
		switch r.OwnerKind {
		case OwnerMenuItem:
			b.Items[r.MenuItemID.UUID] = append(b.Items[r.MenuItemID.UUID], line)
		case OwnerSize:
			b.Sizes[r.SizeID.UUID] = append(b.Sizes[r.SizeID.UUID], line)
		case OwnerModifierOption:
			b.Options[r.ModifierOptionID.UUID] = append(b.Options[r.ModifierOptionID.UUID], line)
		case OwnerIngredient:
			batch := b.Batches[r.OwnerIngredientID.UUID]
			batch.Yield = r.YieldQuantity.Float64
			batch.Lines = append(batch.Lines, line)
			b.Batches[r.OwnerIngredientID.UUID] = batch
		}
	}
	return b, nil
}
```

- [ ] **Step 3: Catalog seed helpers**

Append to `internal/inventory/env_integration_test.go`:

```go
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
```

- [ ] **Step 4: Write the recipe integration tests**

`internal/inventory/recipes_integration_test.go`:

```go
//go:build integration

package inventory_test

import (
	"context"
	"testing"

	"github.com/Mirai3103/open-pos/internal/auth"
	"github.com/Mirai3103/open-pos/internal/inventory"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecipesComposeThroughLoadedBook(t *testing.T) {
	c := newCommands()
	manager := seedActor(t, auth.RoleManager)
	m := seedMenu(t)
	f := seedIngredients(t, c, manager)
	seedRecipes(t, c, manager, m, f)

	book, err := inventory.LoadBook(context.Background(), queries())
	require.NoError(t, err)

	base, ok := book.UnitCost(f.Base.ID)
	require.True(t, ok)
	assert.Equal(t, 62.5, base)

	coffee := book.Compose(inventory.Selection{MenuItemID: m.CoffeeID})
	assert.Equal(t, inventory.CostComplete, coffee.Status)
	assert.Equal(t, 4473.69, coffee.TotalVND)

	large := book.Compose(inventory.Selection{MenuItemID: m.MilkTeaID, SizeID: &m.SizeL, ModifierOptionIDs: []uuid.UUID{m.PearlOptionID}})
	assert.Equal(t, 8600.0, large.TotalVND)

	medium := book.Compose(inventory.Selection{MenuItemID: m.MilkTeaID, SizeID: &m.SizeM})
	assert.Equal(t, inventory.CostPartial, medium.Status)
}

func TestRecipeRules(t *testing.T) {
	c := newCommands()
	ctx := context.Background()
	manager := seedActor(t, auth.RoleManager)
	m := seedMenu(t)
	f := seedIngredients(t, c, manager)

	set := func(kind string, owner uuid.UUID, lines ...inventory.RecipeLineInput) error {
		_, _, err := c.SetRecipe(ctx, manager, inventory.SetRecipeCommand{
			RequestID: uuid.New(), OwnerKind: kind, OwnerID: owner, Lines: lines,
		})
		return err
	}

	require.ErrorIs(t, set(inventory.OwnerMenuItem, m.MilkTeaID, line(f.Tea.ID, 100, "ml")), inventory.ErrItemHasSizes)
	require.ErrorIs(t, set(inventory.OwnerSize, m.SizeM, line(f.Tea.ID, -1, "ml")), inventory.ErrInvalidQuantity)
	require.ErrorIs(t, set(inventory.OwnerSize, m.SizeM, line(f.Tea.ID, 1, "g")), inventory.ErrUnitNotConvertible)
	require.ErrorIs(t, set(inventory.OwnerSize, m.SizeM, line(f.Tea.ID, 1, "ml"), line(f.Tea.ID, 2, "ml")), inventory.ErrDuplicateRecipeIngredient)
	require.ErrorIs(t, set(inventory.OwnerSize, uuid.New(), line(f.Tea.ID, 1, "ml")), inventory.ErrRecipeOwnerNotFound)
	require.NoError(t, set(inventory.OwnerModifierOption, m.PearlOptionID, line(f.Sugar.ID, -5, "g")), "option deltas may be negative")

	_, _, err := c.SetBatchRecipe(ctx, manager, inventory.SetBatchRecipeCommand{
		RequestID: uuid.New(), IngredientID: f.Tea.ID, YieldQuantity: 1,
		Lines: []inventory.RecipeLineInput{line(f.Powder.ID, 1, "g")},
	})
	require.ErrorIs(t, err, inventory.ErrBatchRecipeOnRaw)

	other := createIngredient(t, c, manager, uniqueName("Syrup"), inventory.KindPrepared, "ml")
	_, _, err = c.SetBatchRecipe(ctx, manager, inventory.SetBatchRecipeCommand{
		RequestID: uuid.New(), IngredientID: other.ID, YieldQuantity: 1,
		Lines: []inventory.RecipeLineInput{line(f.Base.ID, 1, "ml")},
	})
	require.ErrorIs(t, err, inventory.ErrBatchRecipeNotRaw)
}

func TestEmptyLinesDeleteRecipe(t *testing.T) {
	c := newCommands()
	manager := seedActor(t, auth.RoleManager)
	m := seedMenu(t)
	f := seedIngredients(t, c, manager)
	setRecipe(t, c, manager, inventory.OwnerMenuItem, m.CoffeeID, line(f.Base.ID, 40, "ml"))

	_, res, err := c.SetRecipe(context.Background(), manager, inventory.SetRecipeCommand{
		RequestID: uuid.New(), OwnerKind: inventory.OwnerMenuItem, OwnerID: m.CoffeeID, Lines: nil,
	})
	require.NoError(t, err)
	assert.Empty(t, res.Lines)

	book, err := inventory.LoadBook(context.Background(), queries())
	require.NoError(t, err)
	assert.Equal(t, inventory.CostNone, book.Compose(inventory.Selection{MenuItemID: m.CoffeeID}).Status)
}
```

- [ ] **Step 5: Run the tests**

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -count=1 -tags=integration ./internal/inventory/`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/inventory
git commit -m "feat(inventory): add recipe and batch recipe commands

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 8: Reads, HTTP routes behind the feature gate, wiring

**Files:**
- Create: `internal/inventory/reads.go`, `http.go`, `routes.go`
- Modify: `internal/inventory/dto.go` (append `IngredientDetailResponse`)
- Modify: `cmd/api/main.go`
- Test: `internal/inventory/http_integration_test.go`

**Interfaces:**
- Consumes: Tasks 6–7, `features.RequireEnabled`
- Produces:
  - `Reads` with `NewReads(*Runner)` and methods `ListUnits`, `ListIngredients`, `GetIngredient`, `GetRecipe`
  - `Slices{Runner, Commands, Reads}`, `NewSlices(db, q) *Slices`, `(*Slices).RegisterRoutes(v1, authn)`
  - Tasks 10 and 11 add `MenuCosting` and `GrossProfit` to `Reads` and register their routes in `RegisterRoutes`.

- [ ] **Step 1: Detail DTO and reads**

Append to `dto.go`:

```go
// IngredientDetailResponse is one Ingredient with its cost history (only with
// inventory.view_costs) and, for a prepared Ingredient, its Batch Recipe.
type IngredientDetailResponse struct {
	IngredientResponse
	CostHistory []CostEntryResponse `json:"cost_history"`
	BatchRecipe *RecipeResponse     `json:"batch_recipe"`
}
```

`internal/inventory/reads.go`:

```go
package inventory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
)

// Reads holds every Inventory read.
type Reads struct{ runner *Runner }

// NewReads creates Reads on runner.
func NewReads(runner *Runner) *Reads { return &Reads{runner: runner} }

func canViewCosts(caps []string) bool { return slices.Contains(caps, CapViewCosts) }

// ListUnits returns the system units.
func (r *Reads) ListUnits(ctx context.Context, actor Actor) ([]SystemUnit, error) {
	return executeRead(ctx, r.runner, actor, CapAdminister, func(ReadContext) ([]SystemUnit, error) {
		return SystemUnits(), nil
	})
}

// ListIngredients returns every Ingredient, active first.
func (r *Reads) ListIngredients(ctx context.Context, actor Actor) ([]IngredientResponse, error) {
	return executeRead(ctx, r.runner, actor, CapAdminister, func(rc ReadContext) ([]IngredientResponse, error) {
		q := rc.Queries
		rows, err := q.InventoryListIngredients(ctx)
		if err != nil {
			return nil, fmt.Errorf("list ingredients: %w", err)
		}
		ids := make([]uuid.UUID, len(rows))
		for i, row := range rows {
			ids[i] = row.ID
		}
		units, err := q.InventoryListIngredientUnits(ctx, ids)
		if err != nil {
			return nil, fmt.Errorf("list ingredient units: %w", err)
		}
		byIngredient := map[uuid.UUID][]sqlcUnit{}
		for _, u := range units {
			byIngredient[u.IngredientID] = append(byIngredient[u.IngredientID], u)
		}
		var book *Book
		if canViewCosts(rc.Capabilities) {
			if book, err = LoadBook(ctx, q); err != nil {
				return nil, err
			}
		}
		out := make([]IngredientResponse, 0, len(rows))
		for _, row := range rows {
			res := toIngredientResponse(row, byIngredient[row.ID])
			if book != nil {
				if c, ok := book.UnitCost(row.ID); ok {
					res.CostPerStockUnit = &c
				}
			}
			out = append(out, res)
		}
		return out, nil
	})
}

// GetIngredient returns one Ingredient with its detail.
func (r *Reads) GetIngredient(ctx context.Context, actor Actor, id uuid.UUID) (IngredientDetailResponse, error) {
	return executeRead(ctx, r.runner, actor, CapAdminister, func(rc ReadContext) (IngredientDetailResponse, error) {
		q := rc.Queries
		row, err := q.InventoryGetIngredient(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return IngredientDetailResponse{}, fmt.Errorf("%w: %s", ErrIngredientNotFound, id)
		}
		if err != nil {
			return IngredientDetailResponse{}, fmt.Errorf("get ingredient: %w", err)
		}
		base, err := ingredientWithUnits(ctx, q, row)
		if err != nil {
			return IngredientDetailResponse{}, err
		}
		out := IngredientDetailResponse{IngredientResponse: base, CostHistory: []CostEntryResponse{}}
		if canViewCosts(rc.Capabilities) {
			book, err := LoadBook(ctx, q)
			if err != nil {
				return out, err
			}
			if c, ok := book.UnitCost(id); ok {
				out.CostPerStockUnit = &c
			}
			history, err := q.InventoryListIngredientCosts(ctx, id)
			if err != nil {
				return out, fmt.Errorf("list ingredient costs: %w", err)
			}
			for _, h := range history {
				out.CostHistory = append(out.CostHistory, toCostEntry(h))
			}
		}
		if row.Kind == KindPrepared {
			batch, err := loadRecipeResponse(ctx, q, OwnerIngredient, id)
			if err != nil {
				return out, err
			}
			out.BatchRecipe = &batch
		}
		return out, nil
	})
}

// GetRecipe returns one sale Recipe (empty lines when none).
func (r *Reads) GetRecipe(ctx context.Context, actor Actor, ownerKind string, ownerID uuid.UUID) (RecipeResponse, error) {
	return executeRead(ctx, r.runner, actor, CapAdminister, func(rc ReadContext) (RecipeResponse, error) {
		return loadRecipeResponse(ctx, rc.Queries, ownerKind, ownerID)
	})
}
```

Add `type sqlcUnit = sqlc.InventoryIngredientUnit` at the bottom of `reads.go`, and import `github.com/Mirai3103/open-pos/internal/platform/database/sqlc`.

- [ ] **Step 2: HTTP handlers**

`internal/inventory/http.go`. Every handler follows the `tables` shape: resolve the actor, bind, require `request_id` on mutations, call, then `SendResult` or `OK`.

```go
package inventory

import (
	"fmt"

	"github.com/Mirai3103/open-pos/internal/platform/httpx"
	"github.com/Mirai3103/open-pos/internal/response"
	"github.com/labstack/echo/v4"
)

func sendError(c echo.Context, err error) error { return httpx.SendError(c, err, MapHTTPError) }

func ownerFromPath(c echo.Context) (string, error) {
	kind, ok := OwnerKindFromPath(c.Param("owner_kind"))
	if !ok {
		return "", fmt.Errorf("%w: owner_kind must be menu-item, size or modifier-option", response.ErrInvalid)
	}
	return kind, nil
}

// handleListUnits lists system units.
//
//	@Summary	List system units
//	@Tags		inventory
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	response.APIResponse{data=[]SystemUnit}
//	@Failure	403	{object}	response.APIResponse
//	@Router		/inventory/units [get]
func (s *Slices) handleListUnits(c echo.Context) error {
	actor, err := httpx.Actor(c)
	if err != nil {
		return sendError(c, err)
	}
	out, err := s.Reads.ListUnits(c.Request().Context(), actor)
	if err != nil {
		return sendError(c, err)
	}
	return response.OK(c, out)
}

// handleListIngredients lists Ingredients.
//
//	@Summary	List Ingredients
//	@Tags		inventory
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	response.APIResponse{data=[]IngredientResponse}
//	@Failure	403	{object}	response.APIResponse
//	@Router		/inventory/ingredients [get]
func (s *Slices) handleListIngredients(c echo.Context) error {
	actor, err := httpx.Actor(c)
	if err != nil {
		return sendError(c, err)
	}
	out, err := s.Reads.ListIngredients(c.Request().Context(), actor)
	if err != nil {
		return sendError(c, err)
	}
	return response.OK(c, out)
}

// handleGetIngredient returns one Ingredient.
//
//	@Summary	Get an Ingredient
//	@Tags		inventory
//	@Produce	json
//	@Security	BearerAuth
//	@Param		ingredient_id	path		string	true	"Ingredient ID"
//	@Success	200				{object}	response.APIResponse{data=IngredientDetailResponse}
//	@Failure	404				{object}	response.APIResponse
//	@Router		/inventory/ingredients/{ingredient_id} [get]
func (s *Slices) handleGetIngredient(c echo.Context) error {
	actor, err := httpx.Actor(c)
	if err != nil {
		return sendError(c, err)
	}
	id, err := httpx.UUIDParam(c, "ingredient_id")
	if err != nil {
		return sendError(c, err)
	}
	out, err := s.Reads.GetIngredient(c.Request().Context(), actor, id)
	if err != nil {
		return sendError(c, err)
	}
	return response.OK(c, out)
}

// handleCreateIngredient creates an Ingredient.
//
//	@Summary	Create an Ingredient
//	@Tags		inventory
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		request	body		CreateIngredientCommand	true	"Ingredient"
//	@Success	201		{object}	response.APIResponse{data=IngredientResponse}
//	@Failure	400		{object}	response.APIResponse
//	@Failure	409		{object}	response.APIResponse
//	@Router		/inventory/ingredients [post]
func (s *Slices) handleCreateIngredient(c echo.Context) error {
	actor, err := httpx.Actor(c)
	if err != nil {
		return sendError(c, err)
	}
	cmd, err := httpx.BindBody[CreateIngredientCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := httpx.RequireRequestID(cmd.RequestID); err != nil {
		return sendError(c, err)
	}
	status, out, err := s.Commands.CreateIngredient(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return httpx.SendResult(c, status, out)
}

// handleRenameIngredient renames an Ingredient.
//
//	@Summary	Rename an Ingredient
//	@Tags		inventory
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		ingredient_id	path		string					true	"Ingredient ID"
//	@Param		request			body		RenameIngredientCommand	true	"New name"
//	@Success	200				{object}	response.APIResponse{data=IngredientResponse}
//	@Router		/inventory/ingredients/{ingredient_id}/name [patch]
func (s *Slices) handleRenameIngredient(c echo.Context) error {
	actor, err := httpx.Actor(c)
	if err != nil {
		return sendError(c, err)
	}
	id, err := httpx.UUIDParam(c, "ingredient_id")
	if err != nil {
		return sendError(c, err)
	}
	cmd, err := httpx.BindBody[RenameIngredientCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := httpx.RequireRequestID(cmd.RequestID); err != nil {
		return sendError(c, err)
	}
	cmd.IngredientID = id
	status, out, err := s.Commands.RenameIngredient(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return httpx.SendResult(c, status, out)
}

// handleRetireIngredient retires an Ingredient.
//
//	@Summary	Retire an Ingredient
//	@Tags		inventory
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		ingredient_id	path		string					true	"Ingredient ID"
//	@Param		request			body		RetireIngredientCommand	true	"Request"
//	@Success	200				{object}	response.APIResponse{data=IngredientResponse}
//	@Router		/inventory/ingredients/{ingredient_id}/retirement [post]
func (s *Slices) handleRetireIngredient(c echo.Context) error {
	actor, err := httpx.Actor(c)
	if err != nil {
		return sendError(c, err)
	}
	id, err := httpx.UUIDParam(c, "ingredient_id")
	if err != nil {
		return sendError(c, err)
	}
	cmd, err := httpx.BindBody[RetireIngredientCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := httpx.RequireRequestID(cmd.RequestID); err != nil {
		return sendError(c, err)
	}
	cmd.IngredientID = id
	status, out, err := s.Commands.RetireIngredient(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return httpx.SendResult(c, status, out)
}

// handleSetIngredientUnits replaces an Ingredient's units.
//
//	@Summary	Replace an Ingredient's units
//	@Tags		inventory
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		ingredient_id	path		string						true	"Ingredient ID"
//	@Param		request			body		SetIngredientUnitsCommand	true	"Units"
//	@Success	200				{object}	response.APIResponse{data=IngredientResponse}
//	@Router		/inventory/ingredients/{ingredient_id}/units [put]
func (s *Slices) handleSetIngredientUnits(c echo.Context) error {
	actor, err := httpx.Actor(c)
	if err != nil {
		return sendError(c, err)
	}
	id, err := httpx.UUIDParam(c, "ingredient_id")
	if err != nil {
		return sendError(c, err)
	}
	cmd, err := httpx.BindBody[SetIngredientUnitsCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := httpx.RequireRequestID(cmd.RequestID); err != nil {
		return sendError(c, err)
	}
	cmd.IngredientID = id
	status, out, err := s.Commands.SetIngredientUnits(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return httpx.SendResult(c, status, out)
}

// handleRecordCost appends a cost entry.
//
//	@Summary	Record an Ingredient cost
//	@Tags		inventory
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		ingredient_id	path		string				true	"Ingredient ID"
//	@Param		request			body		RecordCostCommand	true	"Cost"
//	@Success	201				{object}	response.APIResponse{data=CostEntryResponse}
//	@Router		/inventory/ingredients/{ingredient_id}/costs [post]
func (s *Slices) handleRecordCost(c echo.Context) error {
	actor, err := httpx.Actor(c)
	if err != nil {
		return sendError(c, err)
	}
	id, err := httpx.UUIDParam(c, "ingredient_id")
	if err != nil {
		return sendError(c, err)
	}
	cmd, err := httpx.BindBody[RecordCostCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := httpx.RequireRequestID(cmd.RequestID); err != nil {
		return sendError(c, err)
	}
	cmd.IngredientID = id
	status, out, err := s.Commands.RecordCost(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return httpx.SendResult(c, status, out)
}

// handleSetBatchRecipe replaces a Batch Recipe.
//
//	@Summary	Replace a Batch Recipe
//	@Tags		inventory
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		ingredient_id	path		string					true	"Prepared Ingredient ID"
//	@Param		request			body		SetBatchRecipeCommand	true	"Batch Recipe"
//	@Success	200				{object}	response.APIResponse{data=RecipeResponse}
//	@Router		/inventory/ingredients/{ingredient_id}/batch-recipe [put]
func (s *Slices) handleSetBatchRecipe(c echo.Context) error {
	actor, err := httpx.Actor(c)
	if err != nil {
		return sendError(c, err)
	}
	id, err := httpx.UUIDParam(c, "ingredient_id")
	if err != nil {
		return sendError(c, err)
	}
	cmd, err := httpx.BindBody[SetBatchRecipeCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := httpx.RequireRequestID(cmd.RequestID); err != nil {
		return sendError(c, err)
	}
	cmd.IngredientID = id
	status, out, err := s.Commands.SetBatchRecipe(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return httpx.SendResult(c, status, out)
}

// handleGetRecipe returns a sale Recipe.
//
//	@Summary	Get a Recipe
//	@Tags		inventory
//	@Produce	json
//	@Security	BearerAuth
//	@Param		owner_kind	path		string	true	"menu-item, size or modifier-option"
//	@Param		owner_id	path		string	true	"Owner ID"
//	@Success	200			{object}	response.APIResponse{data=RecipeResponse}
//	@Router		/inventory/recipes/{owner_kind}/{owner_id} [get]
func (s *Slices) handleGetRecipe(c echo.Context) error {
	actor, err := httpx.Actor(c)
	if err != nil {
		return sendError(c, err)
	}
	kind, err := ownerFromPath(c)
	if err != nil {
		return sendError(c, err)
	}
	id, err := httpx.UUIDParam(c, "owner_id")
	if err != nil {
		return sendError(c, err)
	}
	out, err := s.Reads.GetRecipe(c.Request().Context(), actor, kind, id)
	if err != nil {
		return sendError(c, err)
	}
	return response.OK(c, out)
}

// handleSetRecipe replaces a sale Recipe.
//
//	@Summary	Replace a Recipe
//	@Tags		inventory
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		owner_kind	path		string				true	"menu-item, size or modifier-option"
//	@Param		owner_id	path		string				true	"Owner ID"
//	@Param		request		body		SetRecipeCommand	true	"Recipe"
//	@Success	200			{object}	response.APIResponse{data=RecipeResponse}
//	@Router		/inventory/recipes/{owner_kind}/{owner_id} [put]
func (s *Slices) handleSetRecipe(c echo.Context) error {
	actor, err := httpx.Actor(c)
	if err != nil {
		return sendError(c, err)
	}
	kind, err := ownerFromPath(c)
	if err != nil {
		return sendError(c, err)
	}
	id, err := httpx.UUIDParam(c, "owner_id")
	if err != nil {
		return sendError(c, err)
	}
	cmd, err := httpx.BindBody[SetRecipeCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := httpx.RequireRequestID(cmd.RequestID); err != nil {
		return sendError(c, err)
	}
	cmd.OwnerKind, cmd.OwnerID = kind, id
	status, out, err := s.Commands.SetRecipe(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return httpx.SendResult(c, status, out)
}
```

`internal/inventory/routes.go`:

```go
package inventory

import (
	"database/sql"

	"github.com/Mirai3103/open-pos/internal/auth"
	"github.com/Mirai3103/open-pos/internal/platform/database/sqlc"
	"github.com/Mirai3103/open-pos/internal/platform/features"
	"github.com/labstack/echo/v4"
)

// Slices aggregates the Inventory handlers.
type Slices struct {
	Runner   *Runner
	Commands *Commands
	Reads    *Reads
}

// NewSlices wires Inventory onto one Runner.
func NewSlices(db *sql.DB, queries *sqlc.Queries) *Slices {
	runner := NewRunner(db, queries)
	return &Slices{Runner: runner, Commands: NewCommands(runner), Reads: NewReads(runner)}
}

// RegisterRoutes mounts /inventory. Every route is behind the inventory Store
// Feature: authentication, then the feature gate, then the capability.
func (s *Slices) RegisterRoutes(v1 *echo.Group, authn *auth.Middleware) {
	gate := features.RequireEnabled(s.Runner.Queries(), FeatureKey)
	admin := []echo.MiddlewareFunc{authn.RequireAuth(), gate, authn.RequireCapability(CapAdminister)}

	v1.GET("/inventory/units", s.handleListUnits, admin...)
	v1.GET("/inventory/ingredients", s.handleListIngredients, admin...)
	v1.POST("/inventory/ingredients", s.handleCreateIngredient, admin...)
	v1.GET("/inventory/ingredients/:ingredient_id", s.handleGetIngredient, admin...)
	v1.PATCH("/inventory/ingredients/:ingredient_id/name", s.handleRenameIngredient, admin...)
	v1.POST("/inventory/ingredients/:ingredient_id/retirement", s.handleRetireIngredient, admin...)
	v1.PUT("/inventory/ingredients/:ingredient_id/units", s.handleSetIngredientUnits, admin...)
	v1.POST("/inventory/ingredients/:ingredient_id/costs", s.handleRecordCost, admin...)
	v1.PUT("/inventory/ingredients/:ingredient_id/batch-recipe", s.handleSetBatchRecipe, admin...)
	v1.GET("/inventory/recipes/:owner_kind/:owner_id", s.handleGetRecipe, admin...)
	v1.PUT("/inventory/recipes/:owner_kind/:owner_id", s.handleSetRecipe, admin...)
}
```

In `cmd/api/main.go`, add the import `"github.com/Mirai3103/open-pos/internal/inventory"` and, after the features block:

```go
	// Inventory Extension (outside the Core POS; gated by its Store Feature).
	inventorySlices := inventory.NewSlices(db, queries)
	inventorySlices.RegisterRoutes(v1, authSlices.Middleware)
```

- [ ] **Step 3: HTTP integration test**

`internal/inventory/http_integration_test.go`:

```go
//go:build integration

package inventory_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Mirai3103/open-pos/internal/auth"
	"github.com/Mirai3103/open-pos/internal/httpvalidator"
	"github.com/Mirai3103/open-pos/internal/inventory"
	"github.com/Mirai3103/open-pos/internal/platform/database/sqlc"
	"github.com/Mirai3103/open-pos/internal/platform/features"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newServer(t *testing.T) *echo.Echo {
	t.Helper()
	q := queries()
	e := echo.New()
	e.Validator = httpvalidator.New()
	v1 := e.Group("/api/v1")
	authSlices := auth.NewSlices(invTestDB, q)
	authSlices.RegisterRoutes(v1)
	features.NewSlices(invTestDB, q).RegisterRoutes(v1, authSlices.Middleware)
	inventory.NewSlices(invTestDB, q).RegisterRoutes(v1, authSlices.Middleware)
	return e
}

func signIn(t *testing.T, e *echo.Echo, role string) string {
	t.Helper()
	ctx := context.Background()
	q := queries()
	login := "H" + strings.ReplaceAll(uuid.NewString(), "-", "")[:20]
	hash, err := auth.HashPin("2468")
	require.NoError(t, err)
	row, err := q.CreateStaffIdentity(ctx, sqlc.CreateStaffIdentityParams{DisplayName: "HTTP " + login, Btrim: login, PinHash: hash, Enabled: true})
	require.NoError(t, err)
	require.NoError(t, q.AddStaffRole(ctx, sqlc.AddStaffRoleParams{StaffIdentityID: row.ID, Role: role}))
	rec := do(t, e, http.MethodPost, "/api/v1/auth/sign-in", "", map[string]string{"login_code": login, "pin": "2468"})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var env struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	return env.Data.Token
}

func do(t *testing.T, e *echo.Echo, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	if token != "" {
		req.Header.Set(echo.HeaderAuthorization, "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func setFeature(t *testing.T, e *echo.Echo, token string, on bool) {
	t.Helper()
	rec := do(t, e, http.MethodPut, "/api/v1/features/inventory", token,
		map[string]any{"request_id": uuid.New(), "enabled": on})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

func TestInventoryRoutesAreFeatureGated(t *testing.T) {
	e := newServer(t)
	manager := signIn(t, e, auth.RoleManager)
	cashier := signIn(t, e, auth.RoleCashier)

	setFeature(t, e, manager, false)
	off := do(t, e, http.MethodGet, "/api/v1/inventory/ingredients", manager, nil)
	assert.Equal(t, http.StatusForbidden, off.Code)
	assert.Contains(t, off.Body.String(), "FEATURE_DISABLED")

	setFeature(t, e, manager, true)
	defer setFeature(t, e, manager, false)

	created := do(t, e, http.MethodPost, "/api/v1/inventory/ingredients", manager, map[string]any{
		"request_id": uuid.New(), "name": uniqueName("Bột"), "kind": "RAW", "stock_unit": "g",
	})
	assert.Equal(t, http.StatusCreated, created.Code, created.Body.String())

	list := do(t, e, http.MethodGet, "/api/v1/inventory/ingredients", manager, nil)
	assert.Equal(t, http.StatusOK, list.Code)

	denied := do(t, e, http.MethodGet, "/api/v1/inventory/ingredients", cashier, nil)
	assert.Equal(t, http.StatusForbidden, denied.Code)
	assert.NotContains(t, denied.Body.String(), "FEATURE_DISABLED")
}
```

- [ ] **Step 4: Run the tests**

Run: `go build ./... && TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -count=1 -tags=integration ./internal/inventory/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/inventory cmd/api/main.go
git commit -m "feat(inventory): expose ingredient and recipe API behind the Store Feature

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 9: Consumption Records via hook subscribers

**Files:**
- Create: `internal/inventory/consumption.go`
- Modify: `cmd/api/main.go` (bus wiring)
- Modify: `internal/inventory/env_integration_test.go` (append the sales and preparation drivers)
- Test: `internal/inventory/consumption_integration_test.go`

**Interfaces:**
- Consumes: `hooks.Bus`, `features.Enabled`, `LoadBook`, `Book.Compose`
- Produces: `inventory.Subscribe(bus *hooks.Bus)`

- [ ] **Step 1: Implement the subscribers**

`internal/inventory/consumption.go`:

```go
package inventory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Mirai3103/open-pos/internal/platform/database/sqlc"
	"github.com/Mirai3103/open-pos/internal/platform/features"
	"github.com/Mirai3103/open-pos/internal/platform/hooks"
	"github.com/google/uuid"
)

// Subscribe registers Inventory's reactions to Core facts. Both subscribers
// honour the hooks contract: they return only database errors.
func Subscribe(bus *hooks.Bus) {
	bus.OnPreparationUnitsCreated(recordConsumptions)
	bus.OnPreparationUnitsCancelled(voidConsumptions)
}

// recordConsumptions writes one Consumption Record per new unit while the
// inventory Store Feature is on (spec §4.1, §4.6).
func recordConsumptions(ctx context.Context, q *sqlc.Queries, ev hooks.PreparationUnitsCreated) error {
	on, err := features.Enabled(ctx, q, FeatureKey)
	if err != nil {
		return err
	}
	if !on {
		return nil
	}
	book, err := LoadBook(ctx, q)
	if err != nil {
		return err
	}
	for _, unit := range ev.Units {
		if err := insertConsumption(ctx, q, book, unit, ev.At); err != nil {
			return err
		}
	}
	return nil
}

func insertConsumption(ctx context.Context, q *sqlc.Queries, book *Book, unit hooks.CreatedUnit, at time.Time) error {
	comp := book.Compose(Selection{MenuItemID: unit.MenuItemID, SizeID: unit.SizeID, ModifierOptionIDs: unit.ModifierOptionIDs})
	options := unit.ModifierOptionIDs
	if options == nil {
		options = []uuid.UUID{}
	}
	id, err := q.InventoryInsertConsumption(ctx, sqlc.InventoryInsertConsumptionParams{
		PreparationUnitID: unit.PreparationUnitID, MenuItemID: unit.MenuItemID,
		SizeID: nullUUID(unit.SizeID), ModifierOptionIds: options,
		CostStatus: comp.Status, CostTotalVnd: comp.TotalVND, RecordedAt: at,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil // already recorded: ON CONFLICT DO NOTHING
	}
	if err != nil {
		return fmt.Errorf("insert consumption: %w", err)
	}
	for _, l := range comp.Lines {
		params := sqlc.InventoryInsertConsumptionLineParams{
			ConsumptionID: id, IngredientID: l.IngredientID, IngredientName: l.Name,
			IngredientKind: l.Kind, StockUnitCode: l.StockUnit, Quantity: l.StockQty,
		}
		if l.UnitCost != nil {
			params.CostPerStockUnit = sql.NullFloat64{Float64: *l.UnitCost, Valid: true}
			params.LineCostVnd = sql.NullFloat64{Float64: *l.LineCost, Valid: true}
		}
		if err := q.InventoryInsertConsumptionLine(ctx, params); err != nil {
			return fmt.Errorf("insert consumption line: %w", err)
		}
	}
	return nil
}

// voidConsumptions voids cancelled units' records. It runs whether or not the
// feature is on: it corrects records already written (spec §4.6).
func voidConsumptions(ctx context.Context, q *sqlc.Queries, ev hooks.PreparationUnitsCancelled) error {
	if err := q.InventoryVoidConsumptions(ctx, sqlc.InventoryVoidConsumptionsParams{
		VoidedAt: ev.At, PreparationUnitIds: ev.PreparationUnitIDs,
	}); err != nil {
		return fmt.Errorf("void consumptions: %w", err)
	}
	return nil
}
```

In `cmd/api/main.go`, add the import `"github.com/Mirai3103/open-pos/internal/platform/hooks"`. **Before** `salesSlices` and `preparationSlices` serve, create the bus. Put this block right after `preparationSlices.RegisterRoutes(...)`:

```go
	// Core hook bus: Core modules publish, extensions subscribe.
	bus := hooks.New()
	salesSlices.Runner.SetHooks(bus)
	preparationSlices.Runner.SetHooks(bus)
```

Then, inside the inventory block, after `inventorySlices.RegisterRoutes(...)`:

```go
	inventory.Subscribe(bus)
```

- [ ] **Step 2: Sales and preparation drivers for the tests**

Append to `internal/inventory/env_integration_test.go` (add the imports `sync`, `github.com/Mirai3103/open-pos/internal/platform/features`, `github.com/Mirai3103/open-pos/internal/platform/hooks`, `github.com/Mirai3103/open-pos/internal/preparation`, `github.com/Mirai3103/open-pos/internal/sales`):

```go
var shiftOnce sync.Once

// invEnv drives real Submit, Cancel, Waste and Remake with Inventory
// subscribed on a bus, exactly as cmd/api/main.go wires it.
type invEnv struct {
	Sales    *sales.Runner
	Prep     *preparation.Runner
	Commands *inventory.Commands
	Features *features.Slices
	Manager  command.Actor
	TableID  uuid.UUID
	Menu     menuFixture
	Ing      ingredientsFixture
}

func newInvEnv(t *testing.T) *invEnv {
	t.Helper()
	q := queries()
	env := &invEnv{
		Sales:    sales.NewRunner(invTestDB, q),
		Prep:     preparation.NewRunner(invTestDB, q),
		Commands: newCommands(),
		Features: features.NewSlices(invTestDB, q),
		Manager:  seedActor(t, auth.RoleManager),
	}
	bus := hooks.New()
	inventory.Subscribe(bus)
	env.Sales.SetHooks(bus)
	env.Prep.SetHooks(bus)

	shiftOnce.Do(func() {
		_, err := q.OpenSalesShift(context.Background(), sqlc.OpenSalesShiftParams{
			OpenedByStaffIdentityID: env.Manager.StaffID, OpeningFloatVnd: 100000,
		})
		require.NoError(t, err)
	})
	require.NoError(t, invTestDB.QueryRow(`INSERT INTO tables (name, normalized_name, available)
		VALUES ($1, lower($1), true) RETURNING id`, uniqueName("Bàn")).Scan(&env.TableID))

	env.Menu = seedMenu(t)
	env.Ing = seedIngredients(t, env.Commands, env.Manager)
	seedRecipes(t, env.Commands, env.Manager, env.Menu, env.Ing)
	return env
}

func (e *invEnv) SetFeature(t *testing.T, on bool) {
	t.Helper()
	_, _, err := e.Features.Set.Handle(context.Background(), e.Manager, features.SetFeatureCommand{
		RequestID: uuid.New(), Key: inventory.FeatureKey, Enabled: &on,
	})
	require.NoError(t, err)
}

// Submit sells qty of one configuration dine-in and returns its units.
func (e *invEnv) Submit(t *testing.T, item uuid.UUID, size *uuid.UUID, options []uuid.UUID, qty int32) []sales.PreparationUnitResponse {
	t.Helper()
	ctx := context.Background()
	_, session, err := sales.NewStartDineInSessionHandler(e.Sales).Handle(ctx, e.Manager,
		sales.StartDineInSessionCommand{RequestID: uuid.New(), TableIDs: []uuid.UUID{e.TableID}})
	require.NoError(t, err)
	add := sales.AddDraftItemCommand{RequestID: uuid.New(), ServiceSessionID: session.ID, MenuItemID: item, SizeID: size}
	if options != nil {
		add.ModifierOptionIDs = &options
	}
	_, draft, err := sales.NewAddDraftItemHandler(e.Sales).Handle(ctx, e.Manager, add)
	require.NoError(t, err)
	if qty > 1 {
		_, _, err = sales.NewSetDraftItemQuantityHandler(e.Sales).Handle(ctx, e.Manager, sales.SetDraftItemQuantityCommand{
			RequestID: uuid.New(), ServiceSessionID: session.ID, DraftItemID: draft.Draft.Items[0].ID, Quantity: &qty,
		})
		require.NoError(t, err)
	}
	_, _, err = sales.NewCommitOrderDraftHandler(e.Sales).Handle(ctx, e.Manager,
		sales.CommitOrderDraftCommand{RequestID: uuid.New(), ServiceSessionID: session.ID})
	require.NoError(t, err)
	_, submitted, err := sales.NewSubmitOrderHandler(e.Sales).Handle(ctx, e.Manager,
		sales.SubmitOrderCommand{RequestID: uuid.New(), ServiceSessionID: session.ID})
	require.NoError(t, err)
	return submitted.PreparationUnits
}

func (e *invEnv) Cancel(t *testing.T, unitID uuid.UUID) {
	t.Helper()
	_, _, err := preparation.NewCancelUnitsHandler(e.Prep).Handle(context.Background(), e.Manager, preparation.CancelUnitsCommand{
		RequestID: uuid.New(), PreparationUnitIDs: []uuid.UUID{unitID},
		Kind: preparation.CancelKindCancellation, Reason: preparation.ReasonOrderEntryError,
	})
	require.NoError(t, err)
}

// WasteAndRemake starts, wastes and remakes a unit, returning the remake unit id.
func (e *invEnv) WasteAndRemake(t *testing.T, unitID uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	_, _, err := preparation.NewAdvanceUnitHandler(e.Prep).Handle(ctx, e.Manager, preparation.AdvanceUnitCommand{
		RequestID: uuid.New(), UnitID: unitID, TargetState: preparation.StateInPreparation,
	})
	require.NoError(t, err)
	_, waste, err := preparation.NewWasteUnitHandler(e.Prep).Handle(ctx, e.Manager, preparation.WasteUnitCommand{
		RequestID: uuid.New(), UnitID: unitID, Reason: preparation.ReasonPreparationError,
	})
	require.NoError(t, err)
	_, remade, err := preparation.NewRemakeUnitHandler(e.Prep).Handle(ctx, e.Manager, preparation.RemakeUnitCommand{
		RequestID: uuid.New(), WasteID: waste.ID, Reason: preparation.ReasonPreparationError,
	})
	require.NoError(t, err)
	return remade.Unit.ID
}

type consumptionRow struct {
	Status, CostStatus string
	Total              float64
	SizeID             uuid.NullUUID
	Lines              int
}

// Consumption reads a unit's record; ok is false when none exists.
func Consumption(t *testing.T, unitID uuid.UUID) (consumptionRow, bool) {
	t.Helper()
	var r consumptionRow
	err := invTestDB.QueryRow(`SELECT c.status, c.cost_status, c.cost_total_vnd::float8, c.size_id,
		(SELECT count(*) FROM inventory_consumption_lines l WHERE l.consumption_id = c.id)
		FROM inventory_consumptions c WHERE c.preparation_unit_id = $1`, unitID).
		Scan(&r.Status, &r.CostStatus, &r.Total, &r.SizeID, &r.Lines)
	if errors.Is(err, sql.ErrNoRows) {
		return r, false
	}
	require.NoError(t, err)
	return r, true
}
```

Also add the imports `database/sql` and `errors`.

- [ ] **Step 3: Write the consumption tests**

`internal/inventory/consumption_integration_test.go`:

```go
//go:build integration

package inventory_test

import (
	"context"
	"testing"

	"github.com/Mirai3103/open-pos/internal/inventory"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFeatureOffRecordsNothing(t *testing.T) {
	env := newInvEnv(t)
	env.SetFeature(t, false)
	units := env.Submit(t, env.Menu.CoffeeID, nil, nil, 1)
	_, ok := Consumption(t, units[0].ID)
	assert.False(t, ok)
}

func TestSubmitRecordsCostedConsumption(t *testing.T) {
	env := newInvEnv(t)
	env.SetFeature(t, true)
	defer env.SetFeature(t, false)

	units := env.Submit(t, env.Menu.CoffeeID, nil, nil, 2)
	for _, u := range units {
		row, ok := Consumption(t, u.ID)
		require.True(t, ok)
		assert.Equal(t, "ACTIVE", row.Status)
		assert.Equal(t, inventory.CostComplete, row.CostStatus)
		assert.Equal(t, 4473.69, row.Total)
		assert.Equal(t, 2, row.Lines)
	}

	large := env.Submit(t, env.Menu.MilkTeaID, &env.Menu.SizeL, []uuid.UUID{env.Menu.PearlOptionID}, 1)
	row, ok := Consumption(t, large[0].ID)
	require.True(t, ok)
	assert.Equal(t, 8600.0, row.Total)
	assert.Equal(t, env.Menu.SizeL, row.SizeID.UUID)

	medium := env.Submit(t, env.Menu.MilkTeaID, &env.Menu.SizeM, []uuid.UUID{}, 1)
	row, _ = Consumption(t, medium[0].ID)
	assert.Equal(t, inventory.CostPartial, row.CostStatus)

	plain := env.Submit(t, env.Menu.PlainID, nil, nil, 1)
	row, _ = Consumption(t, plain[0].ID)
	assert.Equal(t, inventory.CostNone, row.CostStatus, "no recipe still sells")
	assert.Zero(t, row.Total)
}

func TestLaterCostChangeDoesNotRewriteHistory(t *testing.T) {
	env := newInvEnv(t)
	env.SetFeature(t, true)
	defer env.SetFeature(t, false)

	units := env.Submit(t, env.Menu.MilkTeaID, &env.Menu.SizeL, []uuid.UUID{}, 1)
	recordCost(t, env.Commands, env.Manager, env.Ing.Tea.ID, 999000, 1, code("l"))
	row, _ := Consumption(t, units[0].ID)
	assert.Equal(t, 5600.0, row.Total)
}

func TestCancelVoidsEvenWhenFeatureTurnedOff(t *testing.T) {
	env := newInvEnv(t)
	env.SetFeature(t, true)
	units := env.Submit(t, env.Menu.CoffeeID, nil, nil, 2)

	env.Cancel(t, units[0].ID)
	row, _ := Consumption(t, units[0].ID)
	assert.Equal(t, "VOIDED", row.Status)

	env.SetFeature(t, false)
	env.Cancel(t, units[1].ID)
	row, _ = Consumption(t, units[1].ID)
	assert.Equal(t, "VOIDED", row.Status, "voids still apply while the feature is off")
}

func TestWasteKeepsRecordAndRemakeAddsOne(t *testing.T) {
	env := newInvEnv(t)
	env.SetFeature(t, true)
	defer env.SetFeature(t, false)

	units := env.Submit(t, env.Menu.CoffeeID, nil, nil, 1)
	remakeID := env.WasteAndRemake(t, units[0].ID)

	wasted, _ := Consumption(t, units[0].ID)
	assert.Equal(t, "ACTIVE", wasted.Status)
	remade, ok := Consumption(t, remakeID)
	require.True(t, ok)
	assert.Equal(t, 4473.69, remade.Total)
}

func TestRecordingIsIdempotentPerUnit(t *testing.T) {
	env := newInvEnv(t)
	env.SetFeature(t, true)
	defer env.SetFeature(t, false)
	units := env.Submit(t, env.Menu.CoffeeID, nil, nil, 1)

	_, err := invTestDB.ExecContext(context.Background(), `INSERT INTO inventory_consumptions
		(preparation_unit_id, menu_item_id, cost_status, cost_total_vnd)
		VALUES ($1, $2, 'NONE', 0) ON CONFLICT (preparation_unit_id) DO NOTHING`, units[0].ID, env.Menu.CoffeeID)
	require.NoError(t, err)
	row, _ := Consumption(t, units[0].ID)
	assert.Equal(t, 4473.69, row.Total, "the original record is kept")
}
```

- [ ] **Step 4: Run all affected suites**

Run: `go build ./... && TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -count=1 -tags=integration ./internal/inventory/ ./internal/sales/ ./internal/preparation/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/inventory cmd/api/main.go
git commit -m "feat(inventory): record Consumption Records through the Core hook bus

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 10: Menu costing read

**Files:**
- Create: `internal/inventory/costing.go`, `internal/inventory/costing_test.go`
- Modify: `internal/inventory/routes.go`, `internal/inventory/http.go`
- Test: `internal/inventory/costing_integration_test.go`

**Interfaces:**
- Produces:
  - `(*Reads).MenuCosting(ctx, Actor) (MenuCostingResponse, error)`
  - `buildMenuCosting(book *Book, items []sqlc.InventoryListMenuForCostingRow, options []sqlc.InventoryListOptionsForCostingRow) MenuCostingResponse`
  - Route `GET /inventory/costing/menu` (`inventory.view_costs`)

- [ ] **Step 1: Write the failing pure test**

`internal/inventory/costing_test.go`:

```go
package inventory

import (
	"database/sql"
	"testing"

	"github.com/Mirai3103/open-pos/internal/platform/database/sqlc"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildMenuCosting(t *testing.T) {
	b := NewBook()
	tea := uuid.New()
	b.Ingredients[tea] = Ingredient{ID: tea, Name: "Trà", Kind: KindRaw, StockUnit: "ml"}
	b.RawCosts[tea] = 20
	coffee, milkTea, sizeL, opt := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	b.Sizes[sizeL] = []Line{{IngredientID: tea, StockQty: 280}}
	b.Items[milkTea] = []Line{{IngredientID: tea, StockQty: 1}} // ignored: item has sizes

	cat := uuid.New()
	got := buildMenuCosting(b, []sqlc.InventoryListMenuForCostingRow{
		{MenuItemID: coffee, ItemName: "Cà phê", CategoryID: cat, CategoryName: "Đồ uống",
			PriceVnd: sql.NullInt64{Int64: 25000, Valid: true}},
		{MenuItemID: milkTea, ItemName: "Trà sữa", CategoryID: cat, CategoryName: "Đồ uống",
			SizeID: uuid.NullUUID{UUID: sizeL, Valid: true}, SizeName: sql.NullString{String: "Size L", Valid: true},
			SizePriceVnd: sql.NullInt64{Int64: 45000, Valid: true}},
	}, []sqlc.InventoryListOptionsForCostingRow{
		{ModifierOptionID: opt, OptionName: "Trân châu", GroupID: uuid.New(), GroupName: "Topping", SurchargeVnd: 5000},
	})

	require.Len(t, got.Items, 2)
	assert.Equal(t, CostNone, got.Items[0].CostStatus)
	assert.Equal(t, int64(45000), got.Items[1].PriceVND)
	assert.Equal(t, 5600.0, got.Items[1].CostVND)
	assert.Equal(t, 39400.0, got.Items[1].MarginVND)
	assert.Equal(t, 87.56, *got.Items[1].MarginPct)
	assert.True(t, got.Items[1].ItemRecipeIgnored)
	require.Len(t, got.Options, 1)
	assert.True(t, got.Options[0].PricedWithoutRecipe)
}
```

Run: `go test ./internal/inventory/ -run TestBuildMenuCosting`
Expected: FAIL, because `buildMenuCosting` is undefined.

- [ ] **Step 2: Implement `costing.go`**

```go
package inventory

import (
	"context"
	"fmt"

	"github.com/Mirai3103/open-pos/internal/platform/database/sqlc"
	"github.com/google/uuid"
)

// ItemCosting is one Menu Item, or one of its Sizes, at current cost.
type ItemCosting struct {
	MenuItemID        uuid.UUID  `json:"menu_item_id"`
	ItemName          string     `json:"item_name"`
	CategoryID        uuid.UUID  `json:"category_id"`
	CategoryName      string     `json:"category_name"`
	SizeID            *uuid.UUID `json:"size_id"`
	SizeName          *string    `json:"size_name"`
	PriceVND          int64      `json:"price_vnd"`
	CostVND           float64    `json:"cost_vnd"`
	MarginVND         float64    `json:"margin_vnd"`
	MarginPct         *float64   `json:"margin_pct"`
	CostStatus        string     `json:"cost_status"`
	ItemRecipeIgnored bool       `json:"item_recipe_ignored"`
}

// OptionCosting is one Modifier Option's surcharge against its cost delta.
type OptionCosting struct {
	ModifierOptionID    uuid.UUID `json:"modifier_option_id"`
	OptionName          string    `json:"option_name"`
	GroupID             uuid.UUID `json:"group_id"`
	GroupName           string    `json:"group_name"`
	SurchargeVND        int64     `json:"surcharge_vnd"`
	HasRecipe           bool      `json:"has_recipe"`
	CostDeltaVND        float64   `json:"cost_delta_vnd"`
	Complete            bool      `json:"complete"`
	PricedWithoutRecipe bool      `json:"priced_without_recipe"`
}

// PreparedCosting is one prepared Ingredient's derived unit cost.
type PreparedCosting struct {
	IngredientID     uuid.UUID `json:"ingredient_id"`
	Name             string    `json:"name"`
	StockUnit        string    `json:"stock_unit"`
	CostPerStockUnit *float64  `json:"cost_per_stock_unit"`
}

// MenuCostingResponse is spec §7.1.
type MenuCostingResponse struct {
	Items               []ItemCosting     `json:"items"`
	Options             []OptionCosting   `json:"options"`
	PreparedIngredients []PreparedCosting `json:"prepared_ingredients"`
}

func marginPct(price int64, cost float64) *float64 {
	if price <= 0 {
		return nil
	}
	pct := Round2((float64(price) - cost) / float64(price) * 100)
	return &pct
}

func buildMenuCosting(book *Book, items []sqlc.InventoryListMenuForCostingRow,
	options []sqlc.InventoryListOptionsForCostingRow,
) MenuCostingResponse {
	out := MenuCostingResponse{Items: []ItemCosting{}, Options: []OptionCosting{}, PreparedIngredients: []PreparedCosting{}}
	for _, r := range items {
		row := ItemCosting{MenuItemID: r.MenuItemID, ItemName: r.ItemName, CategoryID: r.CategoryID, CategoryName: r.CategoryName}
		sel := Selection{MenuItemID: r.MenuItemID}
		if r.SizeID.Valid {
			id, name := r.SizeID.UUID, r.SizeName.String
			row.SizeID, row.SizeName = &id, &name
			row.PriceVND = r.SizePriceVnd.Int64
			sel.SizeID = &id
			row.ItemRecipeIgnored = len(book.Items[r.MenuItemID]) > 0
		} else {
			row.PriceVND = r.PriceVnd.Int64
		}
		comp := book.Compose(sel)
		row.CostVND, row.CostStatus = comp.TotalVND, comp.Status
		row.MarginVND = Round2(float64(row.PriceVND) - comp.TotalVND)
		row.MarginPct = marginPct(row.PriceVND, comp.TotalVND)
		out.Items = append(out.Items, row)
	}
	for _, o := range options {
		d := book.OptionDelta(o.ModifierOptionID)
		out.Options = append(out.Options, OptionCosting{
			ModifierOptionID: o.ModifierOptionID, OptionName: o.OptionName, GroupID: o.GroupID,
			GroupName: o.GroupName, SurchargeVND: o.SurchargeVnd, HasRecipe: d.HasRecipe,
			CostDeltaVND: d.CostVND, Complete: d.Complete,
			PricedWithoutRecipe: o.SurchargeVnd > 0 && !d.HasRecipe,
		})
	}
	for id, ing := range book.Ingredients {
		if ing.Kind != KindPrepared || ing.Retired {
			continue
		}
		pc := PreparedCosting{IngredientID: id, Name: ing.Name, StockUnit: ing.StockUnit}
		if c, ok := book.UnitCost(id); ok {
			pc.CostPerStockUnit = &c
		}
		out.PreparedIngredients = append(out.PreparedIngredients, pc)
	}
	return out
}

// MenuCosting returns spec §7.1 from current Recipes and costs.
func (r *Reads) MenuCosting(ctx context.Context, actor Actor) (MenuCostingResponse, error) {
	return executeRead(ctx, r.runner, actor, CapViewCosts, func(rc ReadContext) (MenuCostingResponse, error) {
		q := rc.Queries
		book, err := LoadBook(ctx, q)
		if err != nil {
			return MenuCostingResponse{}, err
		}
		items, err := q.InventoryListMenuForCosting(ctx)
		if err != nil {
			return MenuCostingResponse{}, fmt.Errorf("list menu for costing: %w", err)
		}
		options, err := q.InventoryListOptionsForCosting(ctx)
		if err != nil {
			return MenuCostingResponse{}, fmt.Errorf("list options for costing: %w", err)
		}
		return buildMenuCosting(book, items, options), nil
	})
}
```

Sort `PreparedIngredients` by name so the output is deterministic. Add `sort.Slice(out.PreparedIngredients, func(i, j int) bool { return out.PreparedIngredients[i].Name < out.PreparedIngredients[j].Name })` before `return out`, and import `sort`.

- [ ] **Step 3: Route and handler**

In `routes.go`, add:

```go
	costs := []echo.MiddlewareFunc{authn.RequireAuth(), gate, authn.RequireCapability(CapViewCosts)}
	v1.GET("/inventory/costing/menu", s.handleMenuCosting, costs...)
```

In `http.go`, add:

```go
// handleMenuCosting returns current cost and margin per item, size and option.
//
//	@Summary	Menu costing
//	@Tags		inventory
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	response.APIResponse{data=MenuCostingResponse}
//	@Failure	403	{object}	response.APIResponse
//	@Router		/inventory/costing/menu [get]
func (s *Slices) handleMenuCosting(c echo.Context) error {
	actor, err := httpx.Actor(c)
	if err != nil {
		return sendError(c, err)
	}
	out, err := s.Reads.MenuCosting(c.Request().Context(), actor)
	if err != nil {
		return sendError(c, err)
	}
	return response.OK(c, out)
}
```

- [ ] **Step 4: Integration test**

`internal/inventory/costing_integration_test.go`:

```go
//go:build integration

package inventory_test

import (
	"context"
	"testing"

	"github.com/Mirai3103/open-pos/internal/auth"
	"github.com/Mirai3103/open-pos/internal/inventory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMenuCostingRead(t *testing.T) {
	c := newCommands()
	manager := seedActor(t, auth.RoleManager)
	m := seedMenu(t)
	f := seedIngredients(t, c, manager)
	seedRecipes(t, c, manager, m, f)

	got, err := inventory.NewReads(inventory.NewRunner(invTestDB, queries())).MenuCosting(context.Background(), manager)
	require.NoError(t, err)

	var coffee *inventory.ItemCosting
	for i := range got.Items {
		if got.Items[i].MenuItemID == m.CoffeeID {
			coffee = &got.Items[i]
		}
	}
	require.NotNil(t, coffee)
	assert.Equal(t, 4473.69, coffee.CostVND)
	assert.Equal(t, int64(25000), coffee.PriceVND)

	_, err = inventory.NewReads(inventory.NewRunner(invTestDB, queries())).MenuCosting(context.Background(), seedActor(t, auth.RoleCashier))
	require.Error(t, err)
}
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/inventory/ && TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -count=1 -tags=integration ./internal/inventory/`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/inventory
git commit -m "feat(inventory): add menu costing read

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 11: Gross-profit report

**Files:**
- Create: `internal/inventory/report.go`, `internal/inventory/report_test.go`
- Modify: `internal/inventory/routes.go`, `internal/inventory/http.go`
- Test: `internal/inventory/report_integration_test.go`

**Interfaces:**
- Consumes: `features.OffIntervals`, `features.Interval`
- Produces:
  - `(*Reads).GrossProfit(ctx, Actor, GrossProfitQuery) (GrossProfitResponse, error)`
  - `GrossProfitQuery{From, To time.Time; TZOffsetMinutes int}`
  - `aggregateGrossProfit(units []reportUnit, loc *time.Location) (GrossProfitFigures, []GrossProfitItem, []GrossProfitDay)`
  - Route `GET /inventory/reports/gross-profit?from=&to=&tz_offset_minutes=`

- [ ] **Step 1: Write the failing aggregation test**

`internal/inventory/report_test.go`:

```go
package inventory

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAggregateGrossProfit(t *testing.T) {
	coffee := uuid.New()
	day := time.Date(2026, 11, 20, 3, 0, 0, 0, time.UTC) // 10:00 at UTC+7
	units := []reportUnit{
		{MenuItemID: coffee, ItemName: "Cà phê", RecordedAt: day, CostStatus: CostComplete, CostVND: 4473.69, PriceVND: 25000},
		// Wasted and comped: revenue 0, cost counted, waste cost counted.
		{MenuItemID: coffee, ItemName: "Cà phê", RecordedAt: day, CostStatus: CostComplete, CostVND: 4473.69, PriceVND: 25000, CompVND: 25000, Wasted: true},
		// Its remake: revenue 0, cost counted.
		{MenuItemID: coffee, ItemName: "Cà phê", RecordedAt: day.Add(time.Hour), CostStatus: CostComplete, CostVND: 4473.69, PriceVND: 25000, Remake: true},
		// Recipe-less item on the next local day.
		{MenuItemID: uuid.New(), ItemName: "Bánh", RecordedAt: day.Add(20 * time.Hour), CostStatus: CostNone, PriceVND: 20000},
	}
	totals, items, days := aggregateGrossProfit(units, time.FixedZone("UTC+7", 7*3600))

	assert.Equal(t, int64(45000), totals.RevenueVND)
	assert.Equal(t, 13421.07, totals.CostVND)
	assert.Equal(t, 31578.93, totals.GrossProfitVND)
	assert.Equal(t, 4473.69, totals.WasteCostVND)
	assert.Equal(t, 4, totals.Units)
	assert.Equal(t, 1, totals.UnitsCostNone)
	require.NotNil(t, totals.GrossMarginPct)
	assert.Equal(t, 70.18, *totals.GrossMarginPct)

	require.Len(t, items, 2)
	assert.Equal(t, "Bánh", items[0].ItemName, "items sort by name")
	require.Len(t, days, 2)
	assert.Equal(t, "2026-11-20", days[0].Date)
	assert.Equal(t, "2026-11-21", days[1].Date)
}
```

Run: `go test ./internal/inventory/ -run TestAggregateGrossProfit`
Expected: FAIL, because `aggregateGrossProfit` is undefined.

- [ ] **Step 2: Implement `report.go`**

```go
package inventory

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/Mirai3103/open-pos/internal/platform/features"
	"github.com/google/uuid"
)

// MaxReportDays bounds one report's period.
const MaxReportDays = 366

// GrossProfitQuery is the report period [From, To) and the client's UTC
// offset, used only to bucket days.
type GrossProfitQuery struct {
	From            time.Time
	To              time.Time
	TZOffsetMinutes int
}

// GrossProfitFigures are the report's measures (spec §7.2).
type GrossProfitFigures struct {
	RevenueVND       int64    `json:"revenue_vnd"`
	CostVND          float64  `json:"cost_vnd"`
	GrossProfitVND   float64  `json:"gross_profit_vnd"`
	GrossMarginPct   *float64 `json:"gross_margin_pct"`
	WasteCostVND     float64  `json:"waste_cost_vnd"`
	Units            int      `json:"units"`
	UnitsCostNone    int      `json:"units_cost_none"`
	UnitsCostPartial int      `json:"units_cost_partial"`
}

// GrossProfitSize is one Size row (SizeName nil for unsized items).
type GrossProfitSize struct {
	SizeName *string            `json:"size_name"`
	Figures  GrossProfitFigures `json:"figures"`
}

// GrossProfitItem is one Menu Item row with its Size breakdown.
type GrossProfitItem struct {
	MenuItemID uuid.UUID          `json:"menu_item_id"`
	ItemName   string             `json:"item_name"`
	Figures    GrossProfitFigures `json:"figures"`
	Sizes      []GrossProfitSize  `json:"sizes"`
}

// GrossProfitDay is one local day.
type GrossProfitDay struct {
	Date    string             `json:"date"`
	Figures GrossProfitFigures `json:"figures"`
}

// GrossProfitResponse is spec §7.2.
type GrossProfitResponse struct {
	From              time.Time           `json:"from"`
	To                time.Time           `json:"to"`
	Totals            GrossProfitFigures  `json:"totals"`
	Items             []GrossProfitItem   `json:"items"`
	Days              []GrossProfitDay    `json:"days"`
	DisabledIntervals []features.Interval `json:"disabled_intervals"`
}

type reportUnit struct {
	MenuItemID uuid.UUID
	ItemName   string
	SizeName   *string
	RecordedAt time.Time
	CostStatus string
	CostVND    float64
	Remake     bool
	Wasted     bool
	PriceVND   int64
	CompVND    int64
}

// revenue is the unit price less its Comp; a Remake earns nothing.
func (u reportUnit) revenue() int64 {
	if u.Remake {
		return 0
	}
	if r := u.PriceVND - u.CompVND; r > 0 {
		return r
	}
	return 0
}

func (f *GrossProfitFigures) add(u reportUnit) {
	f.RevenueVND += u.revenue()
	f.CostVND += u.CostVND
	if u.Wasted {
		f.WasteCostVND += u.CostVND
	}
	f.Units++
	switch u.CostStatus {
	case CostNone:
		f.UnitsCostNone++
	case CostPartial:
		f.UnitsCostPartial++
	}
}

func (f *GrossProfitFigures) finish() {
	f.CostVND = Round2(f.CostVND)
	f.WasteCostVND = Round2(f.WasteCostVND)
	f.GrossProfitVND = Round2(float64(f.RevenueVND) - f.CostVND)
	f.GrossMarginPct = marginPct(f.RevenueVND, f.CostVND)
}

func sizeKey(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func aggregateGrossProfit(units []reportUnit, loc *time.Location) (GrossProfitFigures, []GrossProfitItem, []GrossProfitDay) {
	var totals GrossProfitFigures
	items := map[uuid.UUID]*GrossProfitItem{}
	sizes := map[uuid.UUID]map[string]*GrossProfitSize{}
	days := map[string]*GrossProfitDay{}
	for _, u := range units {
		totals.add(u)
		it, ok := items[u.MenuItemID]
		if !ok {
			it = &GrossProfitItem{MenuItemID: u.MenuItemID, ItemName: u.ItemName}
			items[u.MenuItemID] = it
			sizes[u.MenuItemID] = map[string]*GrossProfitSize{}
		}
		it.Figures.add(u)
		sz, ok := sizes[u.MenuItemID][sizeKey(u.SizeName)]
		if !ok {
			sz = &GrossProfitSize{SizeName: u.SizeName}
			sizes[u.MenuItemID][sizeKey(u.SizeName)] = sz
		}
		sz.Figures.add(u)
		date := u.RecordedAt.In(loc).Format("2006-01-02")
		d, ok := days[date]
		if !ok {
			d = &GrossProfitDay{Date: date}
			days[date] = d
		}
		d.Figures.add(u)
	}
	totals.finish()

	outItems := make([]GrossProfitItem, 0, len(items))
	for id, it := range items {
		it.Figures.finish()
		it.Sizes = []GrossProfitSize{}
		for _, sz := range sizes[id] {
			sz.Figures.finish()
			it.Sizes = append(it.Sizes, *sz)
		}
		sort.Slice(it.Sizes, func(i, j int) bool { return sizeKey(it.Sizes[i].SizeName) < sizeKey(it.Sizes[j].SizeName) })
		outItems = append(outItems, *it)
	}
	sort.Slice(outItems, func(i, j int) bool { return outItems[i].ItemName < outItems[j].ItemName })

	outDays := make([]GrossProfitDay, 0, len(days))
	for _, d := range days {
		d.Figures.finish()
		outDays = append(outDays, *d)
	}
	sort.Slice(outDays, func(i, j int) bool { return outDays[i].Date < outDays[j].Date })
	return totals, outItems, outDays
}

// ValidateGrossProfitQuery checks the period and offset.
func ValidateGrossProfitQuery(q GrossProfitQuery) error {
	if !q.From.Before(q.To) {
		return fmt.Errorf("%w: from must be before to", ErrInvalidPeriod)
	}
	if q.To.Sub(q.From) > MaxReportDays*24*time.Hour {
		return fmt.Errorf("%w: at most %d days", ErrInvalidPeriod, MaxReportDays)
	}
	if q.TZOffsetMinutes < -720 || q.TZOffsetMinutes > 840 {
		return fmt.Errorf("%w: tz_offset_minutes out of range", ErrInvalidPeriod)
	}
	return nil
}

// GrossProfit returns spec §7.2 for the period.
func (r *Reads) GrossProfit(ctx context.Context, actor Actor, query GrossProfitQuery) (GrossProfitResponse, error) {
	if err := ValidateGrossProfitQuery(query); err != nil {
		return GrossProfitResponse{}, err
	}
	return executeRead(ctx, r.runner, actor, CapViewCosts, func(rc ReadContext) (GrossProfitResponse, error) {
		q := rc.Queries
		rows, err := q.InventoryListGrossProfitUnits(ctx, sqlcGrossProfitParams(query))
		if err != nil {
			return GrossProfitResponse{}, fmt.Errorf("list gross profit units: %w", err)
		}
		ids := make([]uuid.UUID, len(rows))
		for i, row := range rows {
			ids[i] = row.PreparationUnitID
		}
		comps := map[uuid.UUID]int64{}
		if len(ids) > 0 {
			compRows, err := q.InventoryListUnitComps(ctx, ids)
			if err != nil {
				return GrossProfitResponse{}, fmt.Errorf("list unit comps: %w", err)
			}
			for _, c := range compRows {
				comps[c.PreparationUnitID] += c.AmountVnd
			}
		}
		units := make([]reportUnit, 0, len(rows))
		for _, row := range rows {
			u := reportUnit{
				MenuItemID: row.MenuItemID, ItemName: row.ItemName, RecordedAt: row.RecordedAt,
				CostStatus: row.CostStatus, CostVND: row.CostTotalVnd,
				Remake: row.Priority == "REMAKE", Wasted: row.State == "WASTED",
				PriceVND: row.UnitPriceVnd, CompVND: comps[row.PreparationUnitID],
			}
			if row.SizeName.Valid {
				s := row.SizeName.String
				u.SizeName = &s
			}
			units = append(units, u)
		}
		loc := time.FixedZone("client", query.TZOffsetMinutes*60)
		totals, items, days := aggregateGrossProfit(units, loc)
		off, err := features.OffIntervals(ctx, q, FeatureKey, query.From, query.To)
		if err != nil {
			return GrossProfitResponse{}, err
		}
		return GrossProfitResponse{From: query.From, To: query.To, Totals: totals,
			Items: items, Days: days, DisabledIntervals: off}, nil
	})
}
```

Add a helper in the same file. Import `github.com/Mirai3103/open-pos/internal/platform/database/sqlc`:

```go
func sqlcGrossProfitParams(q GrossProfitQuery) sqlc.InventoryListGrossProfitUnitsParams {
	return sqlc.InventoryListGrossProfitUnitsParams{FromAt: q.From, ToAt: q.To}
}
```

- [ ] **Step 3: Route and handler**

In `routes.go`, add `v1.GET("/inventory/reports/gross-profit", s.handleGrossProfit, costs...)`.

In `http.go`, add (and import `strconv` and `time`):

```go
// handleGrossProfit returns the gross-profit report.
//
//	@Summary		Gross-profit report
//	@Description	Revenue, cost and gross profit of Preparation Units recorded in [from, to). A management figure: Refunds are excluded.
//	@Tags			inventory
//	@Produce		json
//	@Security		BearerAuth
//	@Param			from				query		string	true	"RFC 3339 start (inclusive)"
//	@Param			to					query		string	true	"RFC 3339 end (exclusive)"
//	@Param			tz_offset_minutes	query		int		false	"Client UTC offset for day buckets"
//	@Success		200					{object}	response.APIResponse{data=GrossProfitResponse}
//	@Failure		400					{object}	response.APIResponse
//	@Router			/inventory/reports/gross-profit [get]
func (s *Slices) handleGrossProfit(c echo.Context) error {
	actor, err := httpx.Actor(c)
	if err != nil {
		return sendError(c, err)
	}
	from, errFrom := time.Parse(time.RFC3339, c.QueryParam("from"))
	to, errTo := time.Parse(time.RFC3339, c.QueryParam("to"))
	if errFrom != nil || errTo != nil {
		return sendError(c, fmt.Errorf("%w: from and to must be RFC 3339", ErrInvalidPeriod))
	}
	offset := 0
	if raw := c.QueryParam("tz_offset_minutes"); raw != "" {
		if offset, err = strconv.Atoi(raw); err != nil {
			return sendError(c, fmt.Errorf("%w: tz_offset_minutes must be an integer", ErrInvalidPeriod))
		}
	}
	out, err := s.Reads.GrossProfit(c.Request().Context(), actor, GrossProfitQuery{From: from, To: to, TZOffsetMinutes: offset})
	if err != nil {
		return sendError(c, err)
	}
	return response.OK(c, out)
}
```

- [ ] **Step 4: Integration test**

`internal/inventory/report_integration_test.go`:

```go
//go:build integration

package inventory_test

import (
	"context"
	"testing"
	"time"

	"github.com/Mirai3103/open-pos/internal/inventory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGrossProfitReport(t *testing.T) {
	env := newInvEnv(t)
	from := time.Now().Add(-time.Minute)
	env.SetFeature(t, true)
	defer env.SetFeature(t, false)

	sold := env.Submit(t, env.Menu.CoffeeID, nil, nil, 3)
	env.WasteAndRemake(t, sold[1].ID) // wasted unit + remake
	env.Cancel(t, sold[2].ID)         // voided: excluded

	got, err := inventory.NewReads(inventory.NewRunner(invTestDB, queries())).GrossProfit(context.Background(), env.Manager,
		inventory.GrossProfitQuery{From: from, To: time.Now().Add(time.Minute), TZOffsetMinutes: 420})
	require.NoError(t, err)

	var coffee *inventory.GrossProfitItem
	for i := range got.Items {
		if got.Items[i].MenuItemID == env.Menu.CoffeeID {
			coffee = &got.Items[i]
		}
	}
	require.NotNil(t, coffee)
	assert.Equal(t, 3, coffee.Figures.Units, "two sold units + one remake; the cancelled unit is excluded")
	assert.Equal(t, int64(50000), coffee.Figures.RevenueVND, "no Comp recorded here; the remake earns 0")
	assert.Equal(t, 13421.07, coffee.Figures.CostVND)
	assert.Equal(t, 4473.69, coffee.Figures.WasteCostVND)
	assert.NotEmpty(t, got.DisabledIntervals, "the feature was off at the start of the period")
}

func TestGrossProfitRejectsBadPeriod(t *testing.T) {
	env := newInvEnv(t)
	now := time.Now()
	_, err := inventory.NewReads(inventory.NewRunner(invTestDB, queries())).GrossProfit(context.Background(), env.Manager,
		inventory.GrossProfitQuery{From: now, To: now.Add(-time.Hour)})
	require.ErrorIs(t, err, inventory.ErrInvalidPeriod)
}
```

The spec's §10 Comp scenario is covered by `TestAggregateGrossProfit`. Recording a real Comp needs the Manager-approval flow of `sales.CompWaste`. The integration test covers the join and the void filter, and the unit test covers Comp arithmetic.

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/inventory/ && TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -count=1 -tags=integration ./internal/inventory/`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/inventory
git commit -m "feat(inventory): add gross-profit report

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 12: Decoupling guard, glossary, swagger, full verification

**Files:**
- Create: `internal/inventory/decoupling_test.go`
- Modify: `CONTEXT.md`
- Regenerate: `api/openapi/*` (`make swagger`)

- [ ] **Step 1: Write the decoupling guard**

`internal/inventory/decoupling_test.go`:

```go
package inventory_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCoreDoesNotImportInventory pins spec §1.1: removing internal/inventory
// must leave the Core POS building.
func TestCoreDoesNotImportInventory(t *testing.T) {
	const module = "github.com/Mirai3103/open-pos/"
	core := []string{
		"internal/sales", "internal/preparation", "internal/catalog", "internal/tables",
		"internal/shift", "internal/auth", "internal/platform/...",
	}
	args := []string{"list", "-deps"}
	for _, p := range core {
		args = append(args, module+p)
	}
	out, err := exec.Command("go", args...).CombinedOutput()
	require.NoError(t, err, string(out))
	require.NotContains(t, string(out), module+"internal/inventory",
		"a Core package imports internal/inventory:\n%s", strings.TrimSpace(string(out)))
}
```

Run: `go test ./internal/inventory/ -run TestCoreDoesNotImportInventory`
Expected: PASS

- [ ] **Step 2: Update `CONTEXT.md`**

In the **Language** section, after **Staff Workspace**, insert the **Store Feature** entry verbatim from spec §2.

At the end of the file, add the extension section:

```markdown
## Inventory Extension

An extension outside the Core POS. The Core POS never depends on it: it reacts to Core facts on the hook bus, and it is active only while its Store Feature is on.
```

Then copy the seven terms verbatim from spec §2, in this order: **Ingredient**, **Stock Unit**, **Ingredient Unit**, **Ingredient Cost**, **Batch Recipe**, **Recipe**, **Consumption Record**.

- [ ] **Step 3: Regenerate swagger**

Run: `make swagger`
Expected: `api/openapi/swagger.yaml` contains `/features`, `/features/{key}`, `/inventory/ingredients`, `/inventory/recipes/{owner_kind}/{owner_id}`, `/inventory/costing/menu` and `/inventory/reports/gross-profit`.

Check with: `grep -c "/inventory/" api/openapi/swagger.yaml` (expect at least 9).

- [ ] **Step 4: Full verification**

Run:
```bash
make fmt && make vet && make lint && make test
TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -count=1 -race -tags=integration ./...
```
Expected: everything passes, with no lint findings in the new packages.

Then a manual smoke run:
1. `make run`
2. Sign in as a Manager. `PUT /api/v1/features/inventory {"enabled":true,...}` → 200.
3. Create an ingredient, record a cost, set a recipe on a menu item.
4. Sell and submit that item from the POS. `GET /api/v1/inventory/reports/gross-profit?...` shows the unit with its cost.
5. `PUT .../features/inventory {"enabled":false}` → `/inventory/*` returns `FEATURE_DISABLED`, and sales keep working.

- [ ] **Step 5: Commit**

```bash
git add internal/inventory/decoupling_test.go CONTEXT.md api/openapi
git commit -m "docs: add Store Feature and Inventory Extension terms; regenerate API docs

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

## Spec Coverage

| Spec section | Task |
|---|---|
| §1.1 coupling and switch, §11.8–11 | 1, 2, 3, 9, 12 |
| §2 terminology | 12 |
| §3.1 units | 5, 6 |
| §3.2 ingredient cost | 6 (entry), 5 (prepared derivation) |
| §3.3 Batch Recipe | 7 |
| §3.4 Recipe | 7 |
| §3.5 composition | 5 |
| §4.1–4.5 lifecycle, bus, payload, never block, what is recorded | 1, 2, 9 |
| §4.6 Store Feature | 3, 8, 9 |
| §5 database | 3, 4 |
| §6 commands, reads, capabilities | 3, 6, 7, 8 |
| §7.1 menu costing | 10 |
| §7.2 report and disabled intervals | 11 |
| §8 web | **Plan 2** |
| §9 errors | 5, 6 (`ErrStockUnitLocked` is unneeded: no endpoint changes a Stock Unit, so the rule holds by construction) |
| §10 testing | every task. The Comp case is unit-tested in Task 11 |
