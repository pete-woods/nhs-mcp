package httpcl

import (
	"errors"
	"fmt"
	"net/http"
)

// HTTPError represents a non-2xx HTTP response.
type HTTPError struct {
	Method     string
	Route      string
	StatusCode int
	Body       []byte // raw response body
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("%s %s: %d %s", e.Method, e.Route, e.StatusCode, http.StatusText(e.StatusCode))
}

// HasStatusCode checks if err is an *HTTPError with any of the given status codes.
func HasStatusCode(err error, codes ...int) bool {
	he, ok := errors.AsType[*HTTPError](err)
	if !ok {
		return false
	}
	for _, c := range codes {
		if he.StatusCode == c {
			return true
		}
	}
	return false
}
