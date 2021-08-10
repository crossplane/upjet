package log

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeTimestamp(t *testing.T) time.Time {
	ts, err := time.Parse(time.RFC3339, "2020-02-12T04:50:38-05:00")
	if err != nil {
		t.Fatal("Can't parse time: 2020-02-12T04:50:38-05:00")
	}
	return ts
}

func TestErrorReport(t *testing.T) {
	ts := makeTimestamp(t)
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

	fields := []zap.Field{
		zap.String(UserKey, "user12345"),
		zap.String("customFieldKey", "customFieldValue"),
		zap.Object(HTTPRequestKey, HTTPRequestToErrorContext(&httpRequest)),
	}

	entry := zapcore.Entry{
		Level:   zapcore.ErrorLevel,
		Time:    ts,
		Message: "my message",
	}

	expectedFields := []zap.Field{
		zap.Object(contextKey, &ErrorContext{
			User: "user12345",
			HTTPRequest: &HTTPRequestContext{
				Method:    "GET",
				URL:       "https://cloud.io/path/",
				UserAgent: "Mozilla/5.0",
				Referrer:  "https://cloud.io/login",
				RemoteIP:  "127.0.0.1",
			},
		}),
		zap.Time("eventTime", ts),
	}

	actualEntry, actualFields := (&core{}).createErrorReport(entry, fields)
	assert.ElementsMatch(t, expectedFields, actualFields)
	assert.True(t, strings.HasPrefix(actualEntry.Message, "my message; customFieldKey: customFieldValue;\ngoroutine "))
}

func TestIsError(t *testing.T) {
	assert.True(t, isError(zapcore.Entry{Level: zapcore.ErrorLevel}, nil))
	assert.False(t, isError(zapcore.Entry{Level: zapcore.InfoLevel}, nil))
	assert.True(t, isError(
		zapcore.Entry{Level: zapcore.InfoLevel},
		[]zapcore.Field{
			{
				Key:       "anyerror",
				Type:      zapcore.ErrorType,
				Interface: errors.New("Ooops"),
			},
		}))
}

func TestWrite(t *testing.T) {
	ts := makeTimestamp(t)
	testCore, logs := observer.New(zapcore.DebugLevel)
	core := &core{
		Core: testCore,
	}

	fields := []zap.Field{
		zap.String("customKey", "customValue"),
	}
	entry := zapcore.Entry{
		Message: "My Message",
		Level:   zapcore.WarnLevel,
		Time:    ts,
	}

	require.NoError(t, core.Write(entry, fields))

	fieldsWithError := append(fields, zap.Error(errors.New("Ooops")))
	require.NoError(t, core.Write(entry, fieldsWithError))

	entry.Level = zapcore.ErrorLevel
	require.NoError(t, core.Write(entry, fields))

	expected := []struct {
		message      string
		isPrefix     bool
		eventTimeSet bool
		eventTime    time.Time
	}{
		{
			message:      "My Message",
			isPrefix:     false,
			eventTimeSet: false,
		},
		{
			message:      "Ooops My Message; customKey: customValue;\ngoroutine ",
			isPrefix:     true,
			eventTimeSet: true,
			eventTime:    ts,
		},
		{
			message:      "My Message; customKey: customValue;\ngoroutine ",
			isPrefix:     true,
			eventTimeSet: true,
			eventTime:    ts,
		},
	}

	for i, v := range logs.All() {
		if expected[i].isPrefix {
			assert.True(t, strings.HasPrefix(v.Message, expected[i].message))
		} else {
			assert.Equal(t, expected[i].message, v.Message)
		}
		if expected[i].eventTimeSet {
			assert.Equal(t, expected[i].eventTime, v.ContextMap()["eventTime"])
		}
	}
}
