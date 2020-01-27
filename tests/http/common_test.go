package timeline_http_test

import (
	"net/http"
	"time"

	"github.com/uol/gotest"
	"github.com/uol/timeline"
)

/**
* The timeline library tests.
* @author rnojiri
**/

// createTimeseriesBackend - creates a new test server simulating a timeseries backend
func createTimeseriesBackend() *gotest.HTTPServer {

	headers := http.Header{}
	headers.Add("Content-type", "application/json")

	responses := gotest.ResponseData{
		RequestData: gotest.RequestData{
			URI:     "/api/put",
			Method:  "PUT",
			Headers: headers,
		},
		Status: 201,
	}

	return gotest.CreateNewTestHTTPServer([]gotest.ResponseData{responses})
}

const (
	numberPoint = "numberJSON"
	textPoint   = "textJSON"
)

// createHTTPTransport - creates the http transport
func createHTTPTransport() *timeline.HTTPTransport {

	transportConf := timeline.HTTPTransportConfig{
		DefaultTransportConfiguration: timeline.DefaultTransportConfiguration{
			RequestTimeout:       time.Second,
			BatchSendInterval:    time.Second,
			TransportBufferSize:  1024,
			SerializerBufferSize: 5,
		},
		ServiceEndpoint:        "/api/put",
		Method:                 "PUT",
		ExpectedResponseStatus: 201,
		TimestampProperty:      "timestamp",
		ValueProperty:          "value",
	}

	transport, err := timeline.NewHTTPTransport(&transportConf)
	if err != nil {
		panic(err)
	}

	transport.AddJSONMapping(
		numberPoint,
		timeline.NumberPoint{},
		"metric",
		"value",
		"timestamp",
		"tags",
	)

	transport.AddJSONMapping(
		textPoint,
		timeline.TextPoint{},
		"metric",
		"text",
		"timestamp",
		"tags",
	)

	return transport
}

// newNumberPoint - creates a new number point
func newNumberPoint(value float64) *timeline.NumberPoint {

	return &timeline.NumberPoint{
		Point: timeline.Point{
			Metric:    "number-metric",
			Timestamp: time.Now().Unix(),
			Tags: map[string]string{
				"type":      "number",
				"customTag": "number-test",
			},
		},
		Value: value,
	}
}

// newTextPoint - creates a new text point
func newTextPoint(text string) *timeline.TextPoint {

	return &timeline.TextPoint{
		Point: timeline.Point{
			Metric:    "text-metric",
			Timestamp: time.Now().Unix(),
			Tags: map[string]string{
				"type":      "text",
				"customTag": "text-test",
			},
		},
		Text: text,
	}
}
