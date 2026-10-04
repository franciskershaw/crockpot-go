package handler

import "github.com/gin-gonic/gin"

type updateRegularRequest struct {
	UnitID   *string  `json:"unitId"`
	Quantity *float64 `json:"quantity"`
}

// parseUpdateRegularRequest validates a full-replacement body; an absent, null or empty unitId clears the unit.
func parseUpdateRegularRequest(c *gin.Context) (*string, float64, bool) {
	var req updateRegularRequest
	if !bindJSON(c, &req) {
		return nil, 0, false
	}

	unitID, ok := parseOptionalUnitID(c, req.UnitID)
	if !ok {
		return nil, 0, false
	}

	quantity, ok := validateQuantity(c, req.Quantity)
	if !ok {
		return nil, 0, false
	}
	return unitID, quantity, true
}
