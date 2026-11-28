package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEventBridgeOnly(t *testing.T) {
	ran := 0
	h := EventBridgeOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { ran++ }))
	scheduled := `{"source":"aws.events","detail-type":"Scheduled Event","detail":{}}`
	cases := []struct {
		name, body    string
		viaAPIGateway bool
		want          int
	}{
		{"scheduled event from EventBridge", scheduled, false, 1},
		{"same body through API Gateway", scheduled, true, 0},
		{"another event source", `{"source":"aws.s3","detail-type":"Scheduled Event"}`, false, 0},
		{"not JSON", `tick`, false, 0},
	}
	for _, c := range cases {
		ran = 0
		req := httptest.NewRequest(http.MethodPost, "/events", strings.NewReader(c.body))
		if c.viaAPIGateway {
			req.Header.Set("x-amzn-request-context", `{"requestId":"x"}`)
		}
		h.ServeHTTP(httptest.NewRecorder(), req)
		if ran != c.want {
			t.Errorf("%s: ran %d times, want %d", c.name, ran, c.want)
		}
	}
}
