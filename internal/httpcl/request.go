package httpcl

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
)

// Request is an individual HTTP request to be executed by Client.Call.
// Use NewRequest to create one.
type Request struct {
	method      string
	route       string
	body        any
	rawBody     []byte
	contentType string
	decoder     func(io.Reader) error
	headers     http.Header
	respHeader  *http.Header
	query       url.Values
	routeParams []any
}

// NewRequest creates a request with functional options.
func NewRequest(method, route string, opts ...func(*Request)) Request {
	r := Request{
		method:  method,
		route:   route,
		headers: http.Header{},
		query:   url.Values{},
	}
	for _, opt := range opts {
		opt(&r)
	}
	return r
}

// Body sets a value that will be JSON-encoded as the request body.
func Body(v any) func(*Request) {
	return func(r *Request) { r.body = v }
}

// RawBody sets a pre-encoded request body sent verbatim with the given
// Content-Type, bypassing JSON marshaling (e.g. multipart/form-data uploads).
// Takes precedence over Body.
func RawBody(body []byte, contentType string) func(*Request) {
	return func(r *Request) {
		r.rawBody = body
		r.contentType = contentType
	}
}

// JSONDecoder decodes a 2xx response body as JSON into v.
func JSONDecoder(v any) func(*Request) {
	return func(r *Request) {
		r.decoder = func(rd io.Reader) error {
			return json.NewDecoder(rd).Decode(v)
		}
	}
}

// JSONLDecoder decodes a 2xx response body as newline-delimited JSON (JSONL),
// invoking fn once per decoded record as it is read off the wire so callers can
// consume records as a stream rather than buffering them. Blank lines and
// surrounding whitespace between records are tolerated. Decoding runs to EOF; a
// malformed record returns the decode error.
func JSONLDecoder[T any](fn func(T)) func(*Request) {
	return func(r *Request) {
		r.decoder = func(rd io.Reader) error {
			dec := json.NewDecoder(rd)
			for {
				var v T
				if err := dec.Decode(&v); err != nil {
					if errors.Is(err, io.EOF) {
						return nil
					}
					return err
				}
				fn(v)
			}
		}
	}
}

// StringDecoder decodes a 2xx response body as JSON into v.
func StringDecoder(s *string) func(*Request) {
	return func(r *Request) {
		r.decoder = func(rd io.Reader) error {
			b, err := io.ReadAll(rd)
			if err != nil {
				return err
			}
			*s = string(b)
			return nil
		}
	}
}

func CopyDecoder(w io.Writer) func(*Request) {
	return func(r *Request) {
		r.decoder = func(rd io.Reader) error {
			_, err := io.Copy(w, rd)
			return err
		}
	}
}

// BytesDecoder decodes a 2xx response body as JSON into v.
func BytesDecoder(resp *[]byte) func(*Request) {
	return func(r *Request) {
		r.decoder = func(rd io.Reader) error {
			bs, err := io.ReadAll(rd)
			if err != nil {
				return err
			}
			*resp = bs
			return nil
		}
	}
}

// Header sets a single request header.
func Header(key, val string) func(*Request) {
	return func(r *Request) { r.headers.Add(key, val) }
}

// CaptureHeader stores the response headers into h on a 2xx response, so callers
// can read values the typed decoders don't expose (e.g. a streaming endpoint's
// X-Terminal marker).
func CaptureHeader(h *http.Header) func(*Request) {
	return func(r *Request) { r.respHeader = h }
}

// QueryParam sets a single query parameter.
func QueryParam(key, val string) func(*Request) {
	return func(r *Request) {
		r.query.Add(key, val)
	}
}

func OptionalQueryParam(key, val string) func(*Request) {
	if val == "" {
		return noop
	}
	return QueryParam(key, val)
}

func noop(_ *Request) {}

func RouteParams(v ...any) func(*Request) {
	return func(r *Request) { r.routeParams = v }
}
