package pushservice

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/time/rate"
)

// PushRequest is the transport-neutral command accepted by the stub.
type PushRequest struct {
	RequestID   string            `json:"request_id"`
	RecipientID string            `json:"recipient_id"`
	Kind        string            `json:"kind"`
	Title       string            `json:"title,omitempty"`
	Body        string            `json:"body,omitempty"`
	Data        map[string]string `json:"data,omitempty"`
}

// Accepted is returned after successful processing.
type Accepted struct {
	MessageID  string    `json:"message_id"`
	AcceptedAt time.Time `json:"accepted_at"`
}

// Behaviour is the mutable in-memory fault configuration.
type Behaviour struct {
	FailRate     float64
	Latency      time.Duration
	RateLimitRPS int
}

// BehaviourPatch applies only fields supplied by the admin caller.
type BehaviourPatch struct {
	FailRate     *float64 `json:"fail_rate"`
	LatencyMS    *int64   `json:"latency_ms"`
	RateLimitRPS *int     `json:"rate_limit_rps"`
}

// FailureKind is a transport-neutral processing failure.
type FailureKind string

const (
	// FailureInvalid marks semantic or transport-level validation errors.
	FailureInvalid FailureKind = "invalid"
	// FailureRateLimited marks rejection by the global token bucket.
	FailureRateLimited FailureKind = "rate_limited"
	// FailureInjected marks a probabilistic configured failure.
	FailureInjected FailureKind = "injected_failure"
	// FailureCanceled marks context cancellation during processing.
	FailureCanceled    FailureKind = "canceled"
	processingResultOK string      = "accepted"
)

// Failure contains a mapped failure and optional retry delay.
type Failure struct {
	Kind       FailureKind
	RetryAfter time.Duration
	Err        error
}

func (f *Failure) Error() string {
	if f == nil || f.Err == nil {
		return string(f.Kind)
	}
	return f.Err.Error()
}

// Service owns the single global limiter and mutable behaviour.
type Service struct {
	mu        sync.Mutex
	behaviour Behaviour
	burst     int
	limiter   *rate.Limiter
	logger    *slog.Logger
	now       func() time.Time
	random    func() float64
	messageID func() string
}

// NewService creates one isolated Push Service instance.
func NewService(config Config, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	service := &Service{
		behaviour: Behaviour{FailRate: config.FailRate, Latency: config.Latency, RateLimitRPS: config.RateLimitRPS},
		burst:     config.RateLimitBurst, logger: logger, now: time.Now,
		random: rand.Float64, messageID: uuid.NewString,
	}
	service.resetLimiterLocked()
	return service
}

// Behaviour returns a concurrency-safe snapshot.
func (s *Service) Behaviour() Behaviour {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.behaviour
}

// PatchBehaviour validates and atomically applies an in-memory partial patch.
func (s *Service) PatchBehaviour(patch BehaviourPatch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.behaviour
	if patch.FailRate != nil {
		next.FailRate = *patch.FailRate
	}
	if patch.LatencyMS != nil {
		if *patch.LatencyMS < 0 || *patch.LatencyMS > int64((time.Duration(1<<63-1))/time.Millisecond) {
			return errors.New("latency_ms is outside the supported duration range")
		}
		next.Latency = time.Duration(*patch.LatencyMS) * time.Millisecond
	}
	if patch.RateLimitRPS != nil {
		next.RateLimitRPS = *patch.RateLimitRPS
	}
	if math.IsNaN(next.FailRate) || math.IsInf(next.FailRate, 0) || next.FailRate < 0 || next.FailRate > 1 || next.Latency < 0 || next.RateLimitRPS < 0 {
		return errors.New("behaviour values are outside the supported range")
	}
	rateChanged := next.RateLimitRPS != s.behaviour.RateLimitRPS
	s.behaviour = next
	if rateChanged {
		s.resetLimiterLocked()
	}
	return nil
}

// Process executes validation -> rate limit -> latency -> injected failure -> success.
func (s *Service) Process(ctx context.Context, transport string, request PushRequest) (accepted Accepted, failure *Failure) {
	result := processingResultOK
	defer func() { s.logAttempt(ctx, transport, request, result) }()

	if err := validatePush(request); err != nil {
		result = string(FailureInvalid)
		return Accepted{}, &Failure{Kind: FailureInvalid, Err: err}
	}
	behaviour, allowed, retryAfter := s.snapshotAndAllow(s.now())
	if !allowed {
		result = string(FailureRateLimited)
		return Accepted{}, &Failure{Kind: FailureRateLimited, RetryAfter: retryAfter, Err: errors.New("global push rate limit exceeded")}
	}
	if behaviour.Latency > 0 {
		timer := time.NewTimer(behaviour.Latency)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			result = string(FailureCanceled)
			return Accepted{}, &Failure{Kind: FailureCanceled, Err: ctx.Err()}
		case <-timer.C:
		}
	} else {
		select {
		case <-ctx.Done():
			result = string(FailureCanceled)
			return Accepted{}, &Failure{Kind: FailureCanceled, Err: ctx.Err()}
		default:
		}
	}
	if s.random() < behaviour.FailRate {
		result = string(FailureInjected)
		return Accepted{}, &Failure{Kind: FailureInjected, Err: errors.New("injected push failure")}
	}
	return Accepted{MessageID: s.messageID(), AcceptedAt: s.now().UTC()}, nil
}

func (s *Service) logAttempt(ctx context.Context, transport string, request PushRequest, result string) {
	s.logger.InfoContext(ctx, "push attempt",
		"request_id", request.RequestID,
		"recipient_id", request.RecipientID,
		"kind", request.Kind,
		"transport", transport,
		"result", result,
	)
}

func (s *Service) logResponseError(ctx context.Context, operation string, err error) {
	s.logger.ErrorContext(ctx, operation, "error", err)
}

func (s *Service) snapshotAndAllow(now time.Time) (Behaviour, bool, time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.behaviour
	if current.RateLimitRPS == 0 {
		return current, true, 0
	}
	if s.limiter.AllowN(now, 1) {
		return current, true, 0
	}
	retry := time.Duration(math.Ceil(1/float64(current.RateLimitRPS))) * time.Second
	if retry < time.Second {
		retry = time.Second
	}
	return current, false, retry
}

func (s *Service) resetLimiterLocked() {
	if s.behaviour.RateLimitRPS == 0 {
		s.limiter = nil
		return
	}
	s.limiter = rate.NewLimiter(rate.Limit(s.behaviour.RateLimitRPS), s.burst)
}

func validatePush(request PushRequest) error {
	if request.RequestID == "" {
		return errors.New("request_id is required")
	}
	if _, err := uuid.Parse(request.RecipientID); err != nil {
		return errors.New("recipient_id must be a UUID")
	}
	switch request.Kind {
	case "REQUEST_POSITION", "TRIP_CREATED", "TRIP_COMPLETED":
	default:
		return errors.New("kind is required and must be supported")
	}
	if request.Kind == "REQUEST_POSITION" {
		if _, err := uuid.Parse(request.Data["trip_id"]); err != nil {
			return errors.New("data.trip_id must be a UUID for REQUEST_POSITION")
		}
	}
	return nil
}
