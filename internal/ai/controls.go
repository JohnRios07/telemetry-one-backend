package ai

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"
)

var (
	ErrBudgetExceeded = errors.New("AI gateway budget exceeded")
	ErrRateLimited    = errors.New("AI gateway rate limit exceeded")
	ErrRetryExhausted = errors.New("AI gateway retry attempts exhausted")
)

type TokenBudget struct {
	MaxPromptChars      int
	MaxCompletionTokens int
	MaxEventsPerRequest int
}

func DefaultTokenBudget() TokenBudget {
	return TokenBudget{
		MaxPromptChars:      40000,
		MaxCompletionTokens: 2000,
		MaxEventsPerRequest: 50,
	}
}

func (b TokenBudget) Validate() error {
	if b.MaxPromptChars <= 0 {
		return errors.New("TokenBudget MaxPromptChars must be > 0")
	}
	if b.MaxCompletionTokens <= 0 {
		return errors.New("TokenBudget MaxCompletionTokens must be > 0")
	}
	if b.MaxEventsPerRequest <= 0 {
		return errors.New("TokenBudget MaxEventsPerRequest must be > 0")
	}
	return nil
}

func (b TokenBudget) CheckPromptChars(chars int) error {
	if chars > b.MaxPromptChars {
		return fmt.Errorf("%w: prompt has %d chars, max is %d", ErrBudgetExceeded, chars, b.MaxPromptChars)
	}
	return nil
}

func (b TokenBudget) CheckEvents(count int) error {
	if count > b.MaxEventsPerRequest {
		return fmt.Errorf("%w: request has %d events, max is %d", ErrBudgetExceeded, count, b.MaxEventsPerRequest)
	}
	return nil
}

type RetryPolicy struct {
	MaxAttempts   int
	BaseBackoffMs int
	MaxBackoffMs  int
}

func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts:   3,
		BaseBackoffMs: 1000,
		MaxBackoffMs:  10000,
	}
}

func (p RetryPolicy) Validate() error {
	if p.MaxAttempts < 1 {
		return errors.New("RetryPolicy MaxAttempts must be >= 1")
	}
	if p.BaseBackoffMs < 100 {
		return errors.New("RetryPolicy BaseBackoffMs must be >= 100")
	}
	if p.MaxBackoffMs < p.BaseBackoffMs {
		return errors.New("RetryPolicy MaxBackoffMs must be >= BaseBackoffMs")
	}
	return nil
}

var retryableProviderErrors = []error{ErrProviderNotAvailable, ErrProviderTimeout}

func (p RetryPolicy) IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, ErrProviderRejected) {
		return false
	}
	for _, retryable := range retryableProviderErrors {
		if errors.Is(err, retryable) {
			return true
		}
	}
	return false
}

func (p RetryPolicy) Backoff(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}
	backoff := float64(p.BaseBackoffMs) * math.Pow(2, float64(attempt-1))
	if backoff > float64(p.MaxBackoffMs) {
		backoff = float64(p.MaxBackoffMs)
	}
	return time.Duration(backoff) * time.Millisecond
}

type RateLimiter struct {
	mu       sync.Mutex
	sessions map[string]*tokenBucket
	rate     float64
	burst    int
}

type tokenBucket struct {
	tokens     float64
	lastRefill time.Time
}

func NewRateLimiter(ratePerSec float64, burst int) *RateLimiter {
	if ratePerSec <= 0 {
		ratePerSec = 10
	}
	if burst <= 0 {
		burst = 5
	}
	return &RateLimiter{
		sessions: make(map[string]*tokenBucket),
		rate:     ratePerSec,
		burst:    burst,
	}
}

func (rl *RateLimiter) Allow(sessionID string) (bool, time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	bucket, ok := rl.sessions[sessionID]
	if !ok {
		rl.sessions[sessionID] = &tokenBucket{
			tokens:     float64(rl.burst) - 1,
			lastRefill: time.Now(),
		}
		return true, 0
	}

	now := time.Now()
	elapsed := now.Sub(bucket.lastRefill).Seconds()
	bucket.tokens = math.Min(float64(rl.burst), bucket.tokens+elapsed*rl.rate)
	bucket.lastRefill = now

	if bucket.tokens >= 1 {
		bucket.tokens--
		return true, 0
	}

	retryAfter := time.Duration((1 - bucket.tokens) / rl.rate * float64(time.Second))
	return false, retryAfter
}

type CostEstimator struct {
	PerPromptToken     float64
	PerCompletionToken float64
}

func DefaultCostEstimator() CostEstimator {
	return CostEstimator{
		PerPromptToken:     0.00015,
		PerCompletionToken: 0.00060,
	}
}

func (e CostEstimator) EstimateCost(promptTokens, completionTokens int) float64 {
	promptCost := float64(promptTokens) / 1000.0 * e.PerPromptToken
	completionCost := float64(completionTokens) / 1000.0 * e.PerCompletionToken
	return promptCost + completionCost
}

type SessionUsage struct {
	TotalRequests         int
	TotalPromptTokens     int
	TotalCompletionTokens int
	TotalCost             float64
}

type UsageAccount struct {
	mu       sync.Mutex
	sessions map[string]*SessionUsage
}

func NewUsageAccount() *UsageAccount {
	return &UsageAccount{sessions: make(map[string]*SessionUsage)}
}

func (a *UsageAccount) Record(sessionID string, promptTokens, completionTokens int, cost float64) {
	a.mu.Lock()
	defer a.mu.Unlock()

	usage, ok := a.sessions[sessionID]
	if !ok {
		usage = &SessionUsage{}
		a.sessions[sessionID] = usage
	}
	usage.TotalRequests++
	usage.TotalPromptTokens += promptTokens
	usage.TotalCompletionTokens += completionTokens
	usage.TotalCost += cost
}

func (a *UsageAccount) SessionUsage(sessionID string) (SessionUsage, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	usage, ok := a.sessions[sessionID]
	if !ok {
		return SessionUsage{}, false
	}
	return *usage, true
}

type ObservabilityHooks struct {
	AfterRequest   func(ctx context.Context, sessionID string, mode string, duration time.Duration, promptTokens int, completionTokens int, err error)
	BudgetExceeded func(ctx context.Context, sessionID string, reason string, budget TokenBudget)
	RateLimited    func(ctx context.Context, sessionID string, retryAfter time.Duration)
	RetryAttempt   func(ctx context.Context, sessionID string, attempt int, err error)
}

type Controller struct {
	Budget        TokenBudget
	Retry         RetryPolicy
	RateLimiter   *RateLimiter
	Account       *UsageAccount
	CostEstimator CostEstimator
	Hooks         ObservabilityHooks
}

func (c *Controller) Validate() error {
	if err := c.Budget.Validate(); err != nil {
		return fmt.Errorf("token budget: %w", err)
	}
	if err := c.Retry.Validate(); err != nil {
		return fmt.Errorf("retry policy: %w", err)
	}
	return nil
}

func (c *Controller) PreCheck(ctx context.Context, sessionID string, promptCharCount int, eventCount int) error {
	if err := c.Budget.CheckPromptChars(promptCharCount); err != nil {
		if c.Hooks.BudgetExceeded != nil {
			c.Hooks.BudgetExceeded(ctx, sessionID, err.Error(), c.Budget)
		}
		return err
	}
	if err := c.Budget.CheckEvents(eventCount); err != nil {
		if c.Hooks.BudgetExceeded != nil {
			c.Hooks.BudgetExceeded(ctx, sessionID, err.Error(), c.Budget)
		}
		return err
	}
	if c.RateLimiter != nil {
		allowed, retryAfter := c.RateLimiter.Allow(sessionID)
		if !allowed {
			if c.Hooks.RateLimited != nil {
				c.Hooks.RateLimited(ctx, sessionID, retryAfter)
			}
			return fmt.Errorf("%w: retry after %v", ErrRateLimited, retryAfter)
		}
	}
	return nil
}

func (c *Controller) ExecuteWithRetries(ctx context.Context, sessionID string, fn func(context.Context) (ProviderResponse, error)) (ProviderResponse, error) {
	maxAttempts := c.Retry.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			if ctx.Err() != nil {
				return ProviderResponse{}, ctx.Err()
			}
			backoff := c.Retry.Backoff(attempt)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return ProviderResponse{}, ctx.Err()
			}
		}
		resp, err := fn(ctx)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if attempt > 0 && c.Hooks.RetryAttempt != nil {
			c.Hooks.RetryAttempt(ctx, sessionID, attempt, err)
		}
		if !c.Retry.IsRetryable(err) {
			return ProviderResponse{}, err
		}
	}
	return ProviderResponse{}, fmt.Errorf("%w after %d attempts: %v", ErrRetryExhausted, maxAttempts, lastErr)
}

func (c *Controller) RecordResult(ctx context.Context, sessionID string, mode string, duration time.Duration, resp *ProviderResponse, err error) {
	if err != nil {
		if c.Hooks.AfterRequest != nil {
			promptTokens := 0
			completionTokens := 0
			if resp != nil {
				promptTokens = resp.Usage.PromptTokens
				completionTokens = resp.Usage.CompletionTokens
			}
			c.Hooks.AfterRequest(ctx, sessionID, mode, duration, promptTokens, completionTokens, err)
		}
		return
	}
	if resp != nil && c.Account != nil {
		cost := c.CostEstimator.EstimateCost(resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
		c.Account.Record(sessionID, resp.Usage.PromptTokens, resp.Usage.CompletionTokens, cost)
	}
	if c.Hooks.AfterRequest != nil && resp != nil {
		c.Hooks.AfterRequest(ctx, sessionID, mode, duration, resp.Usage.PromptTokens, resp.Usage.CompletionTokens, nil)
	}
}

func (c *Controller) Execute(ctx context.Context, sessionID string, mode string, promptCharCount int, eventCount int, req ProviderRequest, adapter ProviderAdapter) (ProviderResponse, error) {
	start := time.Now()

	if err := c.PreCheck(ctx, sessionID, promptCharCount, eventCount); err != nil {
		return ProviderResponse{}, err
	}

	resp, err := c.ExecuteWithRetries(ctx, sessionID, func(ctx context.Context) (ProviderResponse, error) {
		return adapter.Analyze(ctx, req)
	})

	c.RecordResult(ctx, sessionID, mode, time.Since(start), &resp, err)
	return resp, err
}
