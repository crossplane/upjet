package log

import (
	"net/http"

	"go.uber.org/zap/zapcore"
)

const (
	// HTTPRequestKey is a key for HttpRequest information in Stackdriver ErrorContext
	// (refer to https://cloud.google.com/error-reporting/reference/rest/v1beta1/ErrorContext)
	// as well as HttpRequest information in Stackdriver LogEntry
	// (refer to https://cloud.google.com/logging/docs/reference/v2/rest/v2/LogEntry#HttpRequest)
	HTTPRequestKey = "httpRequest"
)

// HTTPRequestContext see https://cloud.google.com/error-reporting/reference/rest/v1beta1/ErrorContext#HttpRequestContext
type HTTPRequestContext struct {
	Method             string `json:"method"`
	URL                string `json:"url"`
	UserAgent          string `json:"userAgent"`
	Referrer           string `json:"referrer"`
	ResponseStatusCode int32  `json:"responseStatusCode"`
	RemoteIP           string `json:"remoteIp"`
}

// MarshalLogObject implements zapcore.ObjectMarshaler
func (h *HTTPRequestContext) MarshalLogObject(e zapcore.ObjectEncoder) error {
	e.AddString("method", h.Method)
	e.AddString("url", h.URL)
	e.AddString("userAgent", h.UserAgent)
	e.AddString("referrer", h.Referrer)
	if h.ResponseStatusCode > 0 {
		e.AddInt32("responseStatusCode", h.ResponseStatusCode)
	}
	e.AddString("remoteIp", h.RemoteIP)
	return nil
}

// HTTPRequestToErrorContext converts http.Request to HTTPRequestContext implementing zapcore.ObjectMarshaler
func HTTPRequestToErrorContext(r *http.Request) *HTTPRequestContext {
	res := &HTTPRequestContext{
		Method:    r.Method,
		URL:       r.URL.String(),
		UserAgent: r.UserAgent(),
		Referrer:  r.Referer(),
		RemoteIP:  r.RemoteAddr,
	}

	if r.Response != nil {
		res.ResponseStatusCode = int32(r.Response.StatusCode)
	}

	return res
}

// HTTPRequest see https://cloud.google.com/logging/docs/reference/v2/rest/v2/LogEntry#HttpRequest
type HTTPRequest struct {
	RequestMethod                  string `json:"requestMethod"`
	RequestURL                     string `json:"requestUrl"`
	RequestSize                    int64  `json:"requestSize"`
	Status                         int64  `json:"status"`
	ResponseSize                   int64  `json:"responseSize"`
	UserAgent                      string `json:"userAgent"`
	RemoteIP                       string `json:"remoteIp"`
	ServerIP                       string `json:"serverIp"`
	Referer                        string `json:"referer"`
	Latency                        string `json:"latency"`
	CacheLookup                    bool   `json:"cacheLookup"`
	CacheHit                       bool   `json:"cacheHit"`
	CacheValidatedWithOriginServer bool   `json:"cacheValidatedWithOriginServer"`
	CacheFillBytes                 int64  `json:"cacheFillBytes"`
	Protocol                       string `json:"protocol"`
}

// MarshalLogObject implements zapcore.ObjectMarshaler
func (r *HTTPRequest) MarshalLogObject(e zapcore.ObjectEncoder) error {
	e.AddString("requestMethod", r.RequestMethod)
	e.AddString("requestUrl", r.RequestURL)
	e.AddInt64("requestSize", r.RequestSize)
	if r.Status > 0 {
		e.AddInt64("status", r.Status)
	}
	if r.ResponseSize > 0 {
		e.AddInt64("responseSize", r.ResponseSize)
	}
	e.AddString("userAgent", r.UserAgent)
	e.AddString("remoteIp", r.RemoteIP)
	e.AddString("serverIp", r.ServerIP)
	e.AddString("referer", r.Referer)
	e.AddString("protocol", r.Protocol)
	return nil
}

// HTTPRequestToLogEntry converts http.Request to HTTPRequest implementing zapcore.ObjectMarshaler
func HTTPRequestToLogEntry(r *http.Request) *HTTPRequest {
	res := &HTTPRequest{
		RequestMethod: r.Method,
		RequestURL:    r.URL.String(),
		RequestSize:   r.ContentLength,
		UserAgent:     r.UserAgent(),
		RemoteIP:      r.RemoteAddr,
		ServerIP:      r.Host,
		Referer:       r.Referer(),
		Protocol:      r.Proto,
	}
	if r.Response != nil {
		res.ResponseSize = r.Response.ContentLength
		res.Status = int64(r.Response.StatusCode)
	}

	return res
}
