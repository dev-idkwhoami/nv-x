package light

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nv-x/internal/config"
)

func TestControllerSetsLightOnWithConfiguredValues(t *testing.T) {
	var got lightsResponse
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(lightsResponse{
				NumberOfLights: 1,
				Lights: []light{{
					On:          0,
					Brightness:  10,
					Temperature: 200,
				}},
			})
		case http.MethodPut:
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	defer server.Close()

	controller := NewController(config.LightConfig{
		Enabled:     true,
		Address:     server.URL,
		Brightness:  42,
		Temperature: 230,
		TimeoutMS:   500,
	}, nil)
	controller.SetDesired(context.Background(), true)

	if len(got.Lights) != 1 || got.Lights[0].On != 1 || got.Lights[0].Brightness != 42 || got.Lights[0].Temperature != 230 {
		t.Fatalf("unexpected light payload: %+v", got)
	}
}

func TestControllerSetsLightOffWithoutChangingConfiguredValues(t *testing.T) {
	var got lightsResponse
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(lightsResponse{
				NumberOfLights: 1,
				Lights: []light{{
					On:          1,
					Brightness:  50,
					Temperature: 220,
				}},
			})
		case http.MethodPut:
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	defer server.Close()

	controller := NewController(config.LightConfig{
		Enabled:     true,
		Address:     server.URL,
		Brightness:  42,
		Temperature: 230,
		TimeoutMS:   500,
	}, nil)
	controller.SetDesired(context.Background(), false)

	if len(got.Lights) != 1 || got.Lights[0].On != 0 || got.Lights[0].Brightness != 50 || got.Lights[0].Temperature != 220 {
		t.Fatalf("unexpected light payload: %+v", got)
	}
}

func TestNormalizeAddressAddsDefaultPort(t *testing.T) {
	if got := normalizeAddress("192.0.2.10"); got != "192.0.2.10:9123" {
		t.Fatalf("expected default port, got %q", got)
	}
	if got := normalizeAddress("http://192.0.2.10:9999"); got != "192.0.2.10:9999" {
		t.Fatalf("expected URL host, got %q", got)
	}
}

func TestControllerMissingAddressIsNonFatalToCaller(t *testing.T) {
	var logs []string
	controller := NewController(config.LightConfig{Enabled: true, TimeoutMS: 500}, func(format string, args ...any) {
		logs = append(logs, format)
	})
	controller.SetDesired(context.Background(), true)
	if len(logs) != 1 || !strings.Contains(logs[0], "light auto-control skipped") {
		t.Fatalf("expected skipped log, got %#v", logs)
	}
}
