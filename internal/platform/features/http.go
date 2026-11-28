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
