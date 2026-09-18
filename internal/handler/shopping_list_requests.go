package handler

import (
	"strings"

	"github.com/gin-gonic/gin"
)

type addManualShoppingListItemRequest struct {
	ItemID   string   `json:"itemId"`
	UnitID   *string  `json:"unitId"`
	Quantity *float64 `json:"quantity"`
}

// parseAddManualShoppingListItemRequest validates the body into (itemID, unitID, quantity);
// writes the error response and returns ok=false on failure.
func parseAddManualShoppingListItemRequest(c *gin.Context) (string, *string, float64, bool) {
	var req addManualShoppingListItemRequest
	if !bindJSON(c, &req) {
		return "", nil, 0, false
	}
	if !parseID(c, req.ItemID) {
		return "", nil, 0, false
	}

	var unitID *string
	if req.UnitID != nil {
		if trimmed := strings.TrimSpace(*req.UnitID); trimmed != "" {
			if !parseID(c, trimmed) {
				return "", nil, 0, false
			}
			unitID = &trimmed
		}
	}

	if req.Quantity == nil || *req.Quantity <= 0 {
		badRequest(c, "invalid_quantity")
		return "", nil, 0, false
	}

	return req.ItemID, unitID, *req.Quantity, true
}
