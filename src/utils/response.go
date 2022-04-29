package utils

type ResponseData struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Results interface{} `json:"results"`
}
