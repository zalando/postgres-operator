package httpclient

//go:generate go tool mockgen -package mocks -destination=../../../mocks/$GOFILE -source=$GOFILE

import "net/http"

// HTTPClient interface
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
	Get(url string) (resp *http.Response, err error)
}
