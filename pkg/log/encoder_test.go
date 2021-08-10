package log

import (
	"net/http"
	"net/url"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/stretchr/testify/assert"
)

func TestJSONEncodeEntry(t *testing.T) {
	entry := zapcore.Entry{
		Level:   zapcore.InfoLevel,
		Time:    makeTimestamp(t),
		Message: "mymessage",
	}

	urlStr, err := url.Parse("https://cloud.io/path/")
	if err != nil {
		t.Fatal("Can't parse URL: https://cloud.io/path/")
	}

	httpRequest := http.Request{
		Method: "GET",
		URL:    urlStr,
		Proto:  "HTTP/1.1",
		Header: http.Header{
			"User-Agent": {"Mozilla/5.0"},
			"Referer":    {"https://cloud.io/login"},
		},
		RemoteAddr: "127.0.0.1",
	}

	tests := []struct {
		expected string
		entry    zapcore.Entry
		fields   []zapcore.Field
	}{
		{
			fields: []zapcore.Field{
				zap.Object(HTTPRequestKey, HTTPRequestToErrorContext(&httpRequest)),
				zap.String("myKey", "myValue"),
			},
			expected: `{"httpRequest":{"method":"GET", "referrer":"https://cloud.io/login", "remoteIp":"127.0.0.1", "url":"https://cloud.io/path/", "userAgent":"Mozilla/5.0"}, "message":"mymessage", "myKey":"myValue", "severity":"INFO", "timestamp":"2020-02-12T04:50:38-05:00"}`,
		},
		{
			fields: []zapcore.Field{
				zap.Object(HTTPRequestKey, HTTPRequestToLogEntry(&httpRequest)),
				zap.String("myKey", "myValue"),
			},
			expected: `{"httpRequest":{"protocol":"HTTP/1.1", "referer":"https://cloud.io/login", "remoteIp":"127.0.0.1", "requestMethod":"GET", "requestSize":0, "requestUrl":"https://cloud.io/path/", "serverIp":"", "userAgent":"Mozilla/5.0"}, "message":"mymessage", "myKey":"myValue", "severity":"INFO", "timestamp":"2020-02-12T04:50:38-05:00"}`,
		},
	}

	enc := zapcore.NewJSONEncoder(newEncoderConfig())
	for _, tt := range tests {
		buf, err := enc.EncodeEntry(entry, tt.fields)
		assert.NoError(t, err, "Unexpected JSON encoding error.")
		assert.JSONEq(t, tt.expected, buf.String())
		buf.Free()
	}
}
