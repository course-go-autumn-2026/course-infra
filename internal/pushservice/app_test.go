package pushservice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func testConfig() Config {
	return Config{
		HTTPAddr: ":8090", GRPCAddr: ":9095", LogLevel: "info",
		RateLimitBurst: 1, ShutdownTimeout: time.Second,
	}
}

func validRequest() PushRequest {
	return PushRequest{
		RequestID: "request-1", RecipientID: "8860b315-ec86-42eb-a17c-7c163d721ff5",
		Kind: "REQUEST_POSITION", Data: map[string]string{"trip_id": "1f0a9c62-4a1c-4f2e-9d33-2a4bb0f0b111"},
	}
}

func TestAppGracefulShutdown(t *testing.T) {
	config := testConfig()
	config.HTTPAddr = "127.0.0.1:0"
	config.GRPCAddr = "127.0.0.1:0"
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- (App{Config: &config, Logger: slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))}).Run(ctx)
	}()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("App.Run did not stop after cancellation")
	}
}

func TestProcessOrderValidationBeforeGlobalRateLimit(t *testing.T) {
	config := testConfig()
	config.RateLimitRPS = 1
	service := NewService(config, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))
	service.random = func() float64 { return 1 }

	invalid := validRequest()
	invalid.RecipientID = "not-a-uuid"
	if _, failure := service.Process(t.Context(), "http", invalid); failure == nil || failure.Kind != FailureInvalid {
		t.Fatalf("invalid failure = %#v", failure)
	}
	if _, failure := service.Process(t.Context(), "grpc", validRequest()); failure != nil {
		t.Fatalf("first valid request consumed by invalid request: %v", failure)
	}
	if _, failure := service.Process(t.Context(), "http", validRequest()); failure == nil || failure.Kind != FailureRateLimited || failure.RetryAfter < time.Second {
		t.Fatalf("global limiter failure = %#v", failure)
	}
}

func TestLatencyCancellationAndInjectedFailure(t *testing.T) {
	config := testConfig()
	config.Latency = time.Minute
	service := NewService(config, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, failure := service.Process(ctx, "http", validRequest()); failure == nil || failure.Kind != FailureCanceled {
		t.Fatalf("canceled failure = %#v", failure)
	}

	config.Latency = 0
	config.FailRate = 1
	service = NewService(config, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))
	service.random = func() float64 { return 0 }
	if _, failure := service.Process(t.Context(), "grpc", validRequest()); failure == nil || failure.Kind != FailureInjected {
		t.Fatalf("injected failure = %#v", failure)
	}
}

func TestBehaviourPatchIsPartialAndConcurrent(t *testing.T) {
	config := testConfig()
	config.FailRate = 0.25
	config.Latency = 10 * time.Millisecond
	service := NewService(config, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))
	rateLimit := 7
	if err := service.PatchBehaviour(BehaviourPatch{RateLimitRPS: &rateLimit}); err != nil {
		t.Fatal(err)
	}
	got := service.Behaviour()
	if got.FailRate != 0.25 || got.Latency != 10*time.Millisecond || got.RateLimitRPS != 7 {
		t.Fatalf("partial patch = %+v", got)
	}

	var group sync.WaitGroup
	for index := 0; index < 20; index++ {
		group.Add(1)
		go func(value int) {
			defer group.Done()
			rate := value % 3
			_ = service.PatchBehaviour(BehaviourPatch{RateLimitRPS: &rate})
			_ = service.Behaviour()
		}(index)
	}
	group.Wait()
}

func TestStructuredLogsCoverAllAttemptResults(t *testing.T) {
	var output bytes.Buffer
	config := testConfig()
	service := NewService(config, slog.New(slog.NewJSONHandler(&output, nil)))
	service.random = func() float64 { return 0 }
	invalid := validRequest()
	invalid.RequestID = ""
	_, _ = service.Process(t.Context(), "http", invalid)
	_, _ = service.Process(t.Context(), "http", validRequest())
	failRate := 1.0
	_ = service.PatchBehaviour(BehaviourPatch{FailRate: &failRate})
	_, _ = service.Process(t.Context(), "grpc", validRequest())
	failRate = 0
	latency := int64(1000)
	_ = service.PatchBehaviour(BehaviourPatch{FailRate: &failRate, LatencyMS: &latency})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, _ = service.Process(ctx, "http", validRequest())

	rateConfig := testConfig()
	rateConfig.RateLimitRPS = 1
	rateService := NewService(rateConfig, slog.New(slog.NewJSONHandler(&output, nil)))
	_, _ = rateService.Process(t.Context(), "http", validRequest())
	_, _ = rateService.Process(t.Context(), "grpc", validRequest())
	for _, result := range []string{"invalid", "accepted", "injected_failure", "canceled", "rate_limited"} {
		if !strings.Contains(output.String(), `"result":"`+result+`"`) {
			t.Errorf("logs do not contain result %q:\n%s", result, output.String())
		}
	}
}

func TestHTTPStrictJSONEmptyErrorsAndAdminPatch(t *testing.T) {
	config := testConfig()
	service := NewService(config, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))
	server := httptest.NewServer(HTTPHandler(service))
	defer server.Close()

	response := postJSON(t, server.URL+"/api/v1/push", `{"request_id":"x","recipient_id":"8860b315-ec86-42eb-a17c-7c163d721ff5","kind":"TRIP_CREATED","unknown":true}`)
	assertEmptyStatus(t, response, http.StatusBadRequest)

	response = postJSON(t, server.URL+"/admin/behaviour", `{"fail_rate":1}`)
	assertEmptyStatus(t, response, http.StatusOK)
	response = postJSON(t, server.URL+"/api/v1/push", `{"request_id":"x","recipient_id":"8860b315-ec86-42eb-a17c-7c163d721ff5","kind":"TRIP_CREATED"}`)
	assertEmptyStatus(t, response, http.StatusServiceUnavailable)

	response = postJSON(t, server.URL+"/admin/behaviour", `{"latency_ms":-1}`)
	assertEmptyStatus(t, response, http.StatusBadRequest)
	response = postJSON(t, server.URL+"/admin/behaviour", `{"unknown":1}`)
	assertEmptyStatus(t, response, http.StatusBadRequest)
}

func TestHTTPRateLimitRetryAfter(t *testing.T) {
	config := testConfig()
	config.RateLimitRPS = 1
	service := NewService(config, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))
	server := httptest.NewServer(HTTPHandler(service))
	defer server.Close()
	body := `{"request_id":"x","recipient_id":"8860b315-ec86-42eb-a17c-7c163d721ff5","kind":"TRIP_CREATED"}`
	first := postJSON(t, server.URL+"/api/v1/push", body)
	defer func() { _ = first.Body.Close() }()
	if first.StatusCode != http.StatusAccepted {
		t.Fatalf("first status = %d", first.StatusCode)
	}
	second := postJSON(t, server.URL+"/api/v1/push", body)
	if second.Header.Get("Retry-After") != "1" {
		t.Fatalf("Retry-After = %q", second.Header.Get("Retry-After"))
	}
	assertEmptyStatus(t, second, http.StatusTooManyRequests)
}

func TestHTTPAcceptedResponse(t *testing.T) {
	config := testConfig()
	service := NewService(config, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))
	service.messageID = func() string { return "message-1" }
	service.now = func() time.Time { return time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC) }
	request := httptest.NewRequest(http.MethodPost, "/api/v1/push", strings.NewReader(`{"request_id":"x","recipient_id":"8860b315-ec86-42eb-a17c-7c163d721ff5","kind":"TRIP_CREATED"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	HTTPHandler(service).ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || response.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("response status=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
	var accepted Accepted
	if err := json.Unmarshal(response.Body.Bytes(), &accepted); err != nil {
		t.Fatal(err)
	}
	if accepted.MessageID != "message-1" || !accepted.AcceptedAt.Equal(service.now()) {
		t.Fatalf("accepted = %+v", accepted)
	}
}

func TestHTTPRequiresJSONAndRejectsNullsAndTrailingValues(t *testing.T) {
	config := testConfig()
	service := NewService(config, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))
	server := httptest.NewServer(HTTPHandler(service))
	defer server.Close()

	validFields := `"request_id":"x","recipient_id":"8860b315-ec86-42eb-a17c-7c163d721ff5","kind":"TRIP_CREATED"`
	tests := []struct {
		path        string
		contentType string
		body        string
	}{
		{path: "/api/v1/push", contentType: "", body: `{` + validFields + `}`},
		{path: "/api/v1/push", contentType: "text/json", body: `{` + validFields + `}`},
		{path: "/api/v1/push", contentType: "application/json", body: `{` + validFields + `} {}`},
		{path: "/api/v1/push", contentType: "application/json", body: `{"request_id":null,"recipient_id":"8860b315-ec86-42eb-a17c-7c163d721ff5","kind":"TRIP_CREATED"}`},
		{path: "/api/v1/push", contentType: "application/json", body: `{"request_id":"x","recipient_id":null,"kind":"TRIP_CREATED"}`},
		{path: "/api/v1/push", contentType: "application/json", body: `{"request_id":"x","recipient_id":"8860b315-ec86-42eb-a17c-7c163d721ff5","kind":null}`},
		{path: "/api/v1/push", contentType: "application/json", body: `{` + validFields + `,"title":null}`},
		{path: "/api/v1/push", contentType: "application/json", body: `{` + validFields + `,"body":null}`},
		{path: "/api/v1/push", contentType: "application/json", body: `{` + validFields + `,"data":null}`},
		{path: "/api/v1/push", contentType: "application/json", body: `{` + validFields + `,"data":{"trip_id":null}}`},
		{path: "/admin/behaviour", contentType: "application/json", body: `{"fail_rate":null}`},
		{path: "/admin/behaviour", contentType: "application/json", body: `{"latency_ms":null}`},
		{path: "/admin/behaviour", contentType: "application/json", body: `{"rate_limit_rps":null}`},
	}
	client := server.Client()
	for _, test := range tests {
		request, err := http.NewRequest(http.MethodPost, server.URL+test.path, strings.NewReader(test.body))
		if err != nil {
			t.Fatal(err)
		}
		if test.contentType != "" {
			request.Header.Set("Content-Type", test.contentType)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		assertEmptyStatus(t, response, http.StatusBadRequest)
	}

	response := postJSONWithType(t, server.URL+"/admin/behaviour", "Application/JSON; charset=utf-8", `{}`)
	assertEmptyStatus(t, response, http.StatusOK)
}

func TestHTTPUnboundedLatencyHasNoWriteTimeout(t *testing.T) {
	server := newHTTPServer(NewService(testConfig(), nil))
	if server.WriteTimeout != 0 {
		t.Fatalf("WriteTimeout = %s, want disabled for unbounded latency", server.WriteTimeout)
	}
}

func TestHTTPSuccessWriteFailureIsLogged(t *testing.T) {
	var output bytes.Buffer
	service := NewService(testConfig(), slog.New(slog.NewJSONHandler(&output, nil)))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/push", strings.NewReader(`{"request_id":"x","recipient_id":"8860b315-ec86-42eb-a17c-7c163d721ff5","kind":"TRIP_CREATED"}`))
	request.Header.Set("Content-Type", "application/json")
	HTTPHandler(service).ServeHTTP(&failingResponseWriter{header: make(http.Header)}, request)
	if !strings.Contains(output.String(), `"msg":"write accepted response"`) {
		t.Fatalf("write failure was not logged: %s", output.String())
	}
}

type failingResponseWriter struct {
	header http.Header
}

func (writer *failingResponseWriter) Header() http.Header { return writer.header }
func (*failingResponseWriter) WriteHeader(int)            {}
func (*failingResponseWriter) Write([]byte) (int, error) {
	return 0, errors.New("injected response write failure")
}

func postJSON(t *testing.T, url, body string) *http.Response {
	t.Helper()
	return postJSONWithType(t, url, "application/json", body)
}

func postJSONWithType(t *testing.T, url, contentType, body string) *http.Response {
	t.Helper()
	response, err := http.Post(url, contentType, strings.NewReader(body)) // #nosec G107 -- test server URL.
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func assertEmptyStatus(t *testing.T, response *http.Response, status int) {
	t.Helper()
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != status {
		t.Fatalf("status = %d, want %d", response.StatusCode, status)
	}
	var body bytes.Buffer
	_, _ = body.ReadFrom(response.Body)
	if body.Len() != 0 {
		t.Fatalf("error body = %q", body.String())
	}
}
