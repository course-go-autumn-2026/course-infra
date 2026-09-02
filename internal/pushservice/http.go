package pushservice

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"mime"
	"net/http"
	"strconv"
	"strings"
)

const maxRequestBody = 1 << 20

// HTTPHandler returns the strict public/admin HTTP API.
func HTTPHandler(service *Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("POST /api/v1/push", func(writer http.ResponseWriter, request *http.Request) {
		var push PushRequest
		if err := decodeStrictJSON(writer, request, &push); err != nil {
			service.logAttempt(request.Context(), "http", push, string(FailureInvalid))
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		accepted, failure := service.Process(request.Context(), "http", push)
		if failure != nil {
			switch failure.Kind {
			case FailureInvalid:
				writer.WriteHeader(http.StatusBadRequest)
			case FailureRateLimited:
				seconds := int(math.Ceil(failure.RetryAfter.Seconds()))
				if seconds < 1 {
					seconds = 1
				}
				writer.Header().Set("Retry-After", strconv.Itoa(seconds))
				writer.WriteHeader(http.StatusTooManyRequests)
			case FailureInjected, FailureCanceled:
				writer.WriteHeader(http.StatusServiceUnavailable)
			default:
				writer.WriteHeader(http.StatusServiceUnavailable)
			}
			return
		}
		var payload bytes.Buffer
		if err := json.NewEncoder(&payload).Encode(accepted); err != nil {
			service.logResponseError(request.Context(), "encode accepted response", err)
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusAccepted)
		if _, err := writer.Write(payload.Bytes()); err != nil {
			service.logResponseError(request.Context(), "write accepted response", err)
		}
	})
	mux.HandleFunc("POST /admin/behaviour", func(writer http.ResponseWriter, request *http.Request) {
		var patch BehaviourPatch
		if err := decodeStrictJSON(writer, request, &patch); err != nil {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		if err := service.PatchBehaviour(patch); err != nil {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		writer.WriteHeader(http.StatusOK)
	})
	return mux
}

func decodeStrictJSON(writer http.ResponseWriter, request *http.Request, target any) error {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		return errors.New("Content-Type must be application/json")
	}
	request.Body = http.MaxBytesReader(writer, request.Body, maxRequestBody)
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values are not allowed")
		}
		return err
	}
	return rejectExplicitNulls(body, target)
}

func rejectExplicitNulls(body []byte, target any) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(body, &object); err != nil {
		return err
	}
	var fields []string
	switch target.(type) {
	case *PushRequest:
		fields = []string{"request_id", "recipient_id", "kind", "title", "body", "data"}
	case *BehaviourPatch:
		fields = []string{"fail_rate", "latency_ms", "rate_limit_rps"}
	default:
		return errors.New("unsupported strict JSON target")
	}
	for _, field := range fields {
		if raw, present := object[field]; present && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return errors.New(field + " must not be null")
		}
	}
	if _, ok := target.(*PushRequest); ok {
		if raw, present := object["data"]; present {
			var values map[string]json.RawMessage
			if err := json.Unmarshal(raw, &values); err != nil {
				return err
			}
			for name, value := range values {
				if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
					return errors.New("data." + name + " must not be null")
				}
			}
		}
	}
	return nil
}
