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
