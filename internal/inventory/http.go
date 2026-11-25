package inventory

import (
	"fmt"
	"strconv"
	"time"

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
