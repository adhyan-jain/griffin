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
		name, body     string
		requestContext string
		want           int
	}{
		{"scheduled event from EventBridge", scheduled, "", 1},
		{"direct invoke: the adapter sends null", scheduled, "null", 1},
		{"same body through API Gateway", scheduled, `{"requestId":"x"}`, 0},
		{"another event source", `{"source":"aws.s3","detail-type":"Scheduled Event"}`, "", 0},
		{"not JSON", `tick`, "", 0},
	}
	for _, c := range cases {
		ran = 0
		req := httptest.NewRequest(http.MethodPost, "/events", strings.NewReader(c.body))
		if c.requestContext != "" {
			req.Header.Set("x-amzn-request-context", c.requestContext)
		}
		h.ServeHTTP(httptest.NewRecorder(), req)
		if ran != c.want {
			t.Errorf("%s: ran %d times, want %d", c.name, ran, c.want)
		}
	}
}
