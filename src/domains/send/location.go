package send

type LocationRequest struct {
	BaseRequest
	Latitude  string `json:"latitude" form:"latitude"`
	Longitude string `json:"longitude" form:"longitude"`
}
