package utils

type ResponseData struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Results any    `json:"results"`
}
