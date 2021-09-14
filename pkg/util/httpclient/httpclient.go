package httpclient

//go:generate mockgen -package mocks -destination=../../../mocks/$GOFILE -source=$GOFILE -build_flags=-mod=vendor

import "net/http"

// HTTPClient interface
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
	Get(url string) (resp *http.Response, err error)
}
