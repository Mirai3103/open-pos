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
