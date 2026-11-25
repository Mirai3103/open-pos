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
