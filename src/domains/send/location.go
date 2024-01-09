package send

type LocationRequest struct {
	Phone     string `json:"phone" form:"phone"`
	Latitude  string `json:"latitude" form:"latitude"`
	Longitude string `json:"longitude" form:"longitude"`
}
