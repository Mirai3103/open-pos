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
