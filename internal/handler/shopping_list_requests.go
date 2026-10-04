package handler

import "github.com/gin-gonic/gin"

type itemQuantityRequest struct {
	ItemID   string   `json:"itemId"`
	UnitID   *string  `json:"unitId"`
	Quantity *float64 `json:"quantity"`
}

// parseItemQuantityRequest validates an {itemId, unitId?, quantity} body (manual add, new regular);
// writes the error response and returns ok=false on failure.
func parseItemQuantityRequest(c *gin.Context) (string, *string, float64, bool) {
	var req itemQuantityRequest
	if !bindJSON(c, &req) {
		return "", nil, 0, false
	}
	if !parseID(c, req.ItemID) {
		return "", nil, 0, false
	}

	unitID, ok := parseOptionalUnitID(c, req.UnitID)
	if !ok {
		return "", nil, 0, false
	}

	quantity, ok := validateQuantity(c, req.Quantity)
	if !ok {
		return "", nil, 0, false
	}

	return req.ItemID, unitID, quantity, true
}

type updateShoppingListItemRequest struct {
	Obtained *bool    `json:"obtained"`
	Quantity *float64 `json:"quantity"`
}

// parseUpdateShoppingListItemRequest validates the body into (obtained, quantity), at least one
// required; writes the error response and returns ok=false on failure.
func parseUpdateShoppingListItemRequest(c *gin.Context) (*bool, *float64, bool) {
	var req updateShoppingListItemRequest
	if !bindJSON(c, &req) {
		return nil, nil, false
	}
	if req.Obtained == nil && req.Quantity == nil {
		badRequest(c, "invalid_request")
		return nil, nil, false
	}
	if req.Quantity != nil {
		if _, ok := validateQuantity(c, req.Quantity); !ok {
			return nil, nil, false
		}
	}
	return req.Obtained, req.Quantity, true
}

type restockRequest struct {
	RegularIDs []string `json:"regularIds"`
}

func parseRestockRequest(c *gin.Context) ([]string, bool) {
	var req restockRequest
	if !bindJSON(c, &req) {
		return nil, false
	}
	if len(req.RegularIDs) == 0 || len(req.RegularIDs) > maxRegularsPerUser {
		badRequest(c, "invalid_request")
		return nil, false
	}
	for _, id := range req.RegularIDs {
		if !parseID(c, id) {
			return nil, false
		}
	}
	return req.RegularIDs, true
}
