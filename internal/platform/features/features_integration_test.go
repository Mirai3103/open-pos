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
		State: auth.SessionStateActive, LastAuthenticatedAt: time.Now(),
		LastHumanActivityAt: time.Now(), ExpiresAt: time.Now().Add(8 * time.Hour),
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
