package utils

type ResponseData struct {
	Status  int
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
	Results any    `json:"results"`
}
