package utils

type ResponseData struct {
	Status  int    `json:"-"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
	Results any    `json:"results"`
}
