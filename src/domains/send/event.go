package send

type EventRequest struct {
	BaseRequest
	Name               string `json:"name" form:"name"`
	Description        string `json:"description" form:"description"`
	StartTime          int64  `json:"start_time" form:"start_time"`
	EndTime            *int64 `json:"end_time" form:"end_time"`
	LocationName       string `json:"location_name" form:"location_name"`
	ExtraGuestsAllowed bool   `json:"extra_guests_allowed" form:"extra_guests_allowed"`
}
