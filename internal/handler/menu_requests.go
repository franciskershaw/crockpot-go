package handler

import "github.com/gin-gonic/gin"

type upsertMenuEntryRequest struct {
	RecipeID string `json:"recipeId"`
	Serves   int    `json:"serves"`
}

type updateMenuEntryServesRequest struct {
	Serves int `json:"serves"`
}

// parseUpsertMenuEntryRequest validates the body into (recipeID, serves); writes the error response and returns ok=false on failure.
func parseUpsertMenuEntryRequest(c *gin.Context) (string, int, bool) {
	var req upsertMenuEntryRequest
	if !bindJSON(c, &req) {
		return "", 0, false
	}
	if !parseID(c, req.RecipeID) {
		return "", 0, false
	}
	serves, ok := validateServes(c, req.Serves)
	if !ok {
		return "", 0, false
	}
	return req.RecipeID, serves, true
}

// parseMenuEntryServesRequest validates the body into serves; writes the error response and returns ok=false on failure.
func parseMenuEntryServesRequest(c *gin.Context) (int, bool) {
	var req updateMenuEntryServesRequest
	if !bindJSON(c, &req) {
		return 0, false
	}
	return validateServes(c, req.Serves)
}
