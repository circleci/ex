package httprecorder

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/url"
	"sync"
)

type Request struct {
	Method string
	URL    url.URL
	Header http.Header
	Body   []byte
}

func (r *Request) StringBody() string {
	return string(r.Body)
}

// Decode decodes the JSON from the request into the supplied pointer
func (r *Request) Decode(x interface{}) error {
	return json.Unmarshal(r.Body, x)
}

type RequestRecorder struct {
	mu       sync.RWMutex
	requests []Request
}

func New() *RequestRecorder {
	return &RequestRecorder{}
}

// Record stores a copy of the incoming request ensuring the body can still
// be consumed by the caller
func (r *RequestRecorder) Record(request *http.Request) (err error) {
	req := Request{
		Method: request.Method,
		URL:    *request.URL,
	}

	req.Header = make(http.Header)
	for k, v := range request.Header {
		req.Header[k] = v
	}

	req.Body, err = ioutil.ReadAll(request.Body)
	if err != nil {
		return err
	}
	request.Body = ioutil.NopCloser(bytes.NewReader(req.Body))

	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, req)

	return nil
}

func (r *RequestRecorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = nil
}

func (r *RequestRecorder) AllRequests() []Request {
	r.mu.RLock()
	defer r.mu.RUnlock()
	requests := make([]Request, len(r.requests))
	copy(requests, r.requests)
	return requests
}

func (r *RequestRecorder) LastRequest() *Request {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.requests) == 0 {
		return nil
	}
	req := r.requests[len(r.requests)-1]
	return &req
}

func (r *RequestRecorder) FindRequests(method string, u url.URL) []Request {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var requests []Request
	for _, req := range r.requests {
		if req.Method == method && req.URL == u {
			requests = append(requests, req)
		}
	}
	return requests
}
