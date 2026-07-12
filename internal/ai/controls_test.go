package ai

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTokenBudgetValidateDefaultIsValid(t *testing.T) {
	budget := DefaultTokenBudget()
	if err := budget.Validate(); err != nil {
		t.Fatalf("expected default token budget to be valid: %v", err)
	}
}

func TestTokenBudgetValidateRejectsZeroMaxPromptChars(t *testing.T) {
	budget := DefaultTokenBudget()
	budget.MaxPromptChars = 0
	if err := budget.Validate(); err == nil {
		t.Fatal("expected validation error for zero MaxPromptChars")
	}
}

func TestTokenBudgetValidateRejectsZeroMaxCompletionTokens(t *testing.T) {
	budget := DefaultTokenBudget()
	budget.MaxCompletionTokens = 0
	if err := budget.Validate(); err == nil {
		t.Fatal("expected validation error for zero MaxCompletionTokens")
	}
}

func TestTokenBudgetValidateRejectsZeroMaxEventsPerRequest(t *testing.T) {
	budget := DefaultTokenBudget()
	budget.MaxEventsPerRequest = 0
	if err := budget.Validate(); err == nil {
		t.Fatal("expected validation error for zero MaxEventsPerRequest")
	}
}

func TestTokenBudgetCheckPromptCharsAllowsWithinLimit(t *testing.T) {
	budget := DefaultTokenBudget()
	if err := budget.CheckPromptChars(budget.MaxPromptChars - 1); err != nil {
		t.Fatalf("expected prompt chars within limit to pass: %v", err)
	}
}

func TestTokenBudgetCheckPromptCharsRejectsOverLimit(t *testing.T) {
	budget := DefaultTokenBudget()
	err := budget.CheckPromptChars(budget.MaxPromptChars + 1)
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("expected ErrBudgetExceeded, got %v", err)
	}
}

func TestTokenBudgetCheckEventsAllowsWithinLimit(t *testing.T) {
	budget := DefaultTokenBudget()
	if err := budget.CheckEvents(budget.MaxEventsPerRequest - 1); err != nil {
		t.Fatalf("expected events within limit to pass: %v", err)
	}
}

func TestTokenBudgetCheckEventsRejectsOverLimit(t *testing.T) {
	budget := DefaultTokenBudget()
	err := budget.CheckEvents(budget.MaxEventsPerRequest + 1)
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("expected ErrBudgetExceeded, got %v", err)
	}
}

func TestTokenBudgetCheckEventsAcceptsExactLimit(t *testing.T) {
	budget := DefaultTokenBudget()
	if err := budget.CheckEvents(budget.MaxEventsPerRequest); err != nil {
		t.Fatalf("expected exact limit to pass: %v", err)
	}
}

func TestRetryPolicyValidateDefaultIsValid(t *testing.T) {
	policy := DefaultRetryPolicy()
	if err := policy.Validate(); err != nil {
		t.Fatalf("expected default retry policy to be valid: %v", err)
	}
}

func TestRetryPolicyValidateRejectsZeroMaxAttempts(t *testing.T) {
	policy := DefaultRetryPolicy()
	policy.MaxAttempts = 0
	if err := policy.Validate(); err == nil {
		t.Fatal("expected validation error for zero MaxAttempts")
	}
}

func TestRetryPolicyValidateRejectsSmallBaseBackoff(t *testing.T) {
	policy := DefaultRetryPolicy()
	policy.BaseBackoffMs = 50
	if err := policy.Validate(); err == nil {
		t.Fatal("expected validation error for BaseBackoffMs < 100")
	}
}

func TestRetryPolicyValidateRejectsMaxBackoffLessThanBase(t *testing.T) {
	policy := DefaultRetryPolicy()
	policy.MaxBackoffMs = 500
	policy.BaseBackoffMs = 1000
	if err := policy.Validate(); err == nil {
		t.Fatal("expected validation error for MaxBackoffMs < BaseBackoffMs")
	}
}

func TestRetryPolicyIsRetryableForNotAvailable(t *testing.T) {
	policy := DefaultRetryPolicy()
	if !policy.IsRetryable(ErrProviderNotAvailable) {
		t.Fatal("expected ErrProviderNotAvailable to be retryable")
	}
}

func TestRetryPolicyIsRetryableForTimeout(t *testing.T) {
	policy := DefaultRetryPolicy()
	if !policy.IsRetryable(ErrProviderTimeout) {
		t.Fatal("expected ErrProviderTimeout to be retryable")
	}
}

func TestRetryPolicyIsNotRetryableForRejected(t *testing.T) {
	policy := DefaultRetryPolicy()
	if policy.IsRetryable(ErrProviderRejected) {
		t.Fatal("expected ErrProviderRejected NOT to be retryable")
	}
}

func TestRetryPolicyIsNotRetryableForNil(t *testing.T) {
	policy := DefaultRetryPolicy()
	if policy.IsRetryable(nil) {
		t.Fatal("expected nil NOT to be retryable")
	}
}

func TestRetryPolicyIsNotRetryableForRandomError(t *testing.T) {
	policy := DefaultRetryPolicy()
	if policy.IsRetryable(errors.New("random network blip")) {
		t.Fatal("expected random error NOT to be retryable by default")
	}
}

func TestRetryPolicyIsNotRetryableForContextCanceled(t *testing.T) {
	policy := DefaultRetryPolicy()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if policy.IsRetryable(ctx.Err()) {
		t.Fatal("expected context.Canceled NOT to be retryable")
	}
}

func TestRetryPolicyBackoffExponential(t *testing.T) {
	policy := RetryPolicy{MaxAttempts: 5, BaseBackoffMs: 200, MaxBackoffMs: 5000}
	if b := policy.Backoff(1); b != 200*time.Millisecond {
		t.Fatalf("expected attempt 1 backoff 200ms, got %v", b)
	}
	if b := policy.Backoff(2); b != 400*time.Millisecond {
		t.Fatalf("expected attempt 2 backoff 400ms, got %v", b)
	}
	if b := policy.Backoff(3); b != 800*time.Millisecond {
		t.Fatalf("expected attempt 3 backoff 800ms, got %v", b)
	}
}

func TestRetryPolicyBackoffCapsAtMax(t *testing.T) {
	policy := RetryPolicy{MaxAttempts: 10, BaseBackoffMs: 1000, MaxBackoffMs: 3000}
	for attempt := 1; attempt <= 5; attempt++ {
		b := policy.Backoff(attempt)
		if b > 3000*time.Millisecond {
			t.Fatalf("attempt %d backoff %v exceeds max 3s", attempt, b)
		}
	}
}

func TestRetryPolicyBackoffZeroForAttemptZero(t *testing.T) {
	policy := DefaultRetryPolicy()
	if b := policy.Backoff(0); b != 0 {
		t.Fatalf("expected zero backoff for attempt 0, got %v", b)
	}
}

func TestRateLimiterNewSessionAlwaysAllowed(t *testing.T) {
	rl := NewRateLimiter(1, 1)
	allowed, retryAfter := rl.Allow("session-new")
	if !allowed {
		t.Fatalf("expected new session to be allowed, retryAfter=%v", retryAfter)
	}
}

func TestRateLimiterBlocksWhenBurstExhausted(t *testing.T) {
	rl := NewRateLimiter(1, 1)
	rl.Allow("session-burst")
	allowed, _ := rl.Allow("session-burst")
	if allowed {
		t.Fatal("expected second call to be blocked after burst exhausted")
	}
}

func TestRateLimiterRefillsAfterWait(t *testing.T) {
	rl := NewRateLimiter(1000, 1)
	rl.Allow("session-refill")
	time.Sleep(5 * time.Millisecond)
	allowed, _ := rl.Allow("session-refill")
	if !allowed {
		t.Fatal("expected call to be allowed after rate refill")
	}
}

func TestRateLimiterDifferentSessionsIndependent(t *testing.T) {
	rl := NewRateLimiter(1, 1)
	rl.Allow("session-a")
	allowedA, _ := rl.Allow("session-a")
	if allowedA {
		t.Fatal("expected session-a second call to be blocked")
	}
	allowedB, _ := rl.Allow("session-b")
	if !allowedB {
		t.Fatal("expected session-b to be allowed independently")
	}
}

func TestRateLimiterRetryAfterProvidedWhenBlocked(t *testing.T) {
	rl := NewRateLimiter(1, 1)
	rl.Allow("session-retry")
	_, retryAfter := rl.Allow("session-retry")
	if retryAfter <= 0 {
		t.Fatal("expected positive retryAfter when blocked")
	}
}

func TestRateLimiterZeroRateDefaults(t *testing.T) {
	rl := NewRateLimiter(0, 0)
	allowed, _ := rl.Allow("session-defaults")
	if !allowed {
		t.Fatal("expected zero-rate default to still allow initial calls")
	}
}

func TestCostEstimatorDefault(t *testing.T) {
	ce := DefaultCostEstimator()
	if ce.PerPromptToken <= 0 || ce.PerCompletionToken <= 0 {
		t.Fatal("expected positive cost estimates")
	}
}

func TestCostEstimatorEstimateCost(t *testing.T) {
	ce := CostEstimator{PerPromptToken: 1.0, PerCompletionToken: 2.0}
	cost := ce.EstimateCost(1000, 500)
	expected := 1.0 + 1.0
	if cost != expected {
		t.Fatalf("expected cost %.4f, got %.4f", expected, cost)
	}
}

func TestCostEstimatorZeroTokens(t *testing.T) {
	ce := DefaultCostEstimator()
	cost := ce.EstimateCost(0, 0)
	if cost != 0 {
		t.Fatalf("expected zero cost for zero tokens, got %.6f", cost)
	}
}

func TestCostEstimatorOnlyPromptTokens(t *testing.T) {
	ce := CostEstimator{PerPromptToken: 0.50, PerCompletionToken: 0}
	cost := ce.EstimateCost(2000, 0)
	if cost != 1.0 {
		t.Fatalf("expected 1.0 for 2000 prompt tokens at 0.50/1K, got %.4f", cost)
	}
}

func TestUsageAccountRecordAndRetrieve(t *testing.T) {
	acc := NewUsageAccount()
	acc.Record("session-1", 100, 50, 0.05)
	usage, ok := acc.SessionUsage("session-1")
	if !ok {
		t.Fatal("expected session usage to exist")
	}
	if usage.TotalRequests != 1 {
		t.Fatalf("expected 1 request, got %d", usage.TotalRequests)
	}
	if usage.TotalPromptTokens != 100 {
		t.Fatalf("expected 100 prompt tokens, got %d", usage.TotalPromptTokens)
	}
	if usage.TotalCompletionTokens != 50 {
		t.Fatalf("expected 50 completion tokens, got %d", usage.TotalCompletionTokens)
	}
	if usage.TotalCost != 0.05 {
		t.Fatalf("expected cost 0.05, got %.4f", usage.TotalCost)
	}
}

func TestUsageAccountAccumulatesMultipleCalls(t *testing.T) {
	acc := NewUsageAccount()
	acc.Record("session-acc", 100, 50, 0.05)
	acc.Record("session-acc", 200, 100, 0.10)
	usage, ok := acc.SessionUsage("session-acc")
	if !ok {
		t.Fatal("expected session usage to exist")
	}
	if usage.TotalRequests != 2 {
		t.Fatalf("expected 2 requests, got %d", usage.TotalRequests)
	}
	if usage.TotalPromptTokens != 300 {
		t.Fatalf("expected 300 prompt tokens, got %d", usage.TotalPromptTokens)
	}
	if usage.TotalCompletionTokens != 150 {
		t.Fatalf("expected 150 completion tokens, got %d", usage.TotalCompletionTokens)
	}
	if usage.TotalCost < 0.149 || usage.TotalCost > 0.151 {
		t.Fatalf("expected cost ~0.15, got %.6f", usage.TotalCost)
	}
}

func TestUsageAccountMissingSession(t *testing.T) {
	acc := NewUsageAccount()
	_, ok := acc.SessionUsage("session-nonexistent")
	if ok {
		t.Fatal("expected missing session to return false")
	}
}

func TestUsageAccountMultipleSessionsIndependent(t *testing.T) {
	acc := NewUsageAccount()
	acc.Record("session-a", 100, 50, 0.05)
	acc.Record("session-b", 10, 5, 0.01)
	usageA, _ := acc.SessionUsage("session-a")
	usageB, _ := acc.SessionUsage("session-b")
	if usageA.TotalRequests != 1 || usageB.TotalRequests != 1 {
		t.Fatalf("expected 1 request each, got A=%d B=%d", usageA.TotalRequests, usageB.TotalRequests)
	}
	if usageA.TotalPromptTokens != 100 || usageB.TotalPromptTokens != 10 {
		t.Fatalf("expected different prompt tokens, got A=%d B=%d", usageA.TotalPromptTokens, usageB.TotalPromptTokens)
	}
}

func TestObservabilityHooksNilFunctionsDoNotPanic(t *testing.T) {
	hooks := ObservabilityHooks{}
	ctx := context.Background()
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("unexpected panic from nil hooks: %v", r)
			}
		}()
		if hooks.AfterRequest != nil {
			hooks.AfterRequest(ctx, "s", "e", 0, 0, 0, nil)
		}
		if hooks.BudgetExceeded != nil {
			hooks.BudgetExceeded(ctx, "s", "reason", DefaultTokenBudget())
		}
		if hooks.RateLimited != nil {
			hooks.RateLimited(ctx, "s", 0)
		}
		if hooks.RetryAttempt != nil {
			hooks.RetryAttempt(ctx, "s", 1, nil)
		}
	}()
}

func TestControllerPreCheckBudgetExceededPromptChars(t *testing.T) {
	ctrl := Controller{Budget: TokenBudget{MaxPromptChars: 100, MaxCompletionTokens: 2000, MaxEventsPerRequest: 50}}
	err := ctrl.PreCheck(context.Background(), "session-1", 200, 1)
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("expected ErrBudgetExceeded for prompt char budget, got %v", err)
	}
}

func TestControllerPreCheckBudgetExceededEvents(t *testing.T) {
	ctrl := Controller{Budget: TokenBudget{MaxPromptChars: 99999, MaxCompletionTokens: 2000, MaxEventsPerRequest: 5}}
	err := ctrl.PreCheck(context.Background(), "session-1", 100, 10)
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("expected ErrBudgetExceeded for event budget, got %v", err)
	}
}

func TestControllerPreCheckRateLimitBlocks(t *testing.T) {
	ctrl := Controller{
		Budget:      DefaultTokenBudget(),
		RateLimiter: NewRateLimiter(1, 1),
	}
	ctrl.PreCheck(context.Background(), "session-rateblock", 100, 1)
	err := ctrl.PreCheck(context.Background(), "session-rateblock", 100, 1)
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited on second call, got %v", err)
	}
}

func TestControllerPreCheckPassesWithinBudgetAndRate(t *testing.T) {
	ctrl := Controller{
		Budget:      DefaultTokenBudget(),
		RateLimiter: NewRateLimiter(1000, 10),
	}
	err := ctrl.PreCheck(context.Background(), "session-ok", 1000, 10)
	if err != nil {
		t.Fatalf("expected PreCheck to pass, got %v", err)
	}
}

func TestControllerPreCheckPassesWithoutRateLimiter(t *testing.T) {
	ctrl := Controller{Budget: DefaultTokenBudget()}
	err := ctrl.PreCheck(context.Background(), "session-nolimit", 1000, 5)
	if err != nil {
		t.Fatalf("expected PreCheck to pass without rate limiter, got %v", err)
	}
}

func TestControllerPreCheckHooksBudgetExceeded(t *testing.T) {
	called := false
	hooks := ObservabilityHooks{
		BudgetExceeded: func(_ context.Context, sessionID string, reason string, budget TokenBudget) {
			called = true
			if sessionID != "session-hook-budget" {
				t.Fatalf("expected session-hook-budget, got %s", sessionID)
			}
			if !strings.Contains(reason, "budget") {
				t.Fatalf("expected reason to mention budget, got %s", reason)
			}
		},
	}
	ctrl := Controller{
		Budget: TokenBudget{MaxPromptChars: 50, MaxCompletionTokens: 2000, MaxEventsPerRequest: 50},
		Hooks:  hooks,
	}
	ctrl.PreCheck(context.Background(), "session-hook-budget", 100, 1)
	if !called {
		t.Fatal("expected BudgetExceeded hook to be called")
	}
}

func TestControllerPreCheckHooksRateLimited(t *testing.T) {
	called := false
	hooks := ObservabilityHooks{
		RateLimited: func(_ context.Context, sessionID string, retryAfter time.Duration) {
			called = true
			if sessionID != "session-hook-rate" {
				t.Fatalf("expected session-hook-rate, got %s", sessionID)
			}
			if retryAfter <= 0 {
				t.Fatal("expected positive retryAfter in hook")
			}
		},
	}
	ctrl := Controller{
		Budget:      DefaultTokenBudget(),
		RateLimiter: NewRateLimiter(1, 1),
		Hooks:       hooks,
	}
	ctrl.PreCheck(context.Background(), "session-hook-rate", 1, 1)
	ctrl.PreCheck(context.Background(), "session-hook-rate", 1, 1)
	if !called {
		t.Fatal("expected RateLimited hook to be called")
	}
}

func TestControllerValidateValidConfig(t *testing.T) {
	ctrl := Controller{
		Budget: DefaultTokenBudget(),
		Retry:  DefaultRetryPolicy(),
	}
	if err := ctrl.Validate(); err != nil {
		t.Fatalf("expected valid controller config: %v", err)
	}
}

func TestControllerValidateInvalidBudget(t *testing.T) {
	ctrl := Controller{
		Budget: TokenBudget{},
		Retry:  DefaultRetryPolicy(),
	}
	if err := ctrl.Validate(); err == nil {
		t.Fatal("expected validation error for invalid budget")
	}
}

func TestControllerValidateInvalidRetry(t *testing.T) {
	ctrl := Controller{
		Budget: DefaultTokenBudget(),
		Retry:  RetryPolicy{},
	}
	if err := ctrl.Validate(); err == nil {
		t.Fatal("expected validation error for invalid retry policy")
	}
}

type controlledTestAdapter struct {
	callCount int
	failWith  error
}

func (a *controlledTestAdapter) Analyze(_ context.Context, _ ProviderRequest) (ProviderResponse, error) {
	a.callCount++
	if a.failWith != nil {
		return ProviderResponse{}, a.failWith
	}
	return ProviderResponse{
		Content:      "analysis result",
		Model:        "test-model",
		FinishReason: "stop",
		Usage:        UsageInfo{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150},
	}, nil
}

func TestControllerExecuteWithRetriesSuccessFirstAttempt(t *testing.T) {
	adapter := &controlledTestAdapter{}
	ctrl := Controller{Retry: DefaultRetryPolicy()}
	resp, err := ctrl.ExecuteWithRetries(context.Background(), "session-retry-ok", func(ctx context.Context) (ProviderResponse, error) {
		return adapter.Analyze(ctx, ProviderRequest{})
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if resp.Content != "analysis result" {
		t.Fatalf("expected analysis result, got %s", resp.Content)
	}
	if adapter.callCount != 1 {
		t.Fatalf("expected 1 call, got %d", adapter.callCount)
	}
}

func TestControllerExecuteWithRetriesRetriesOnRetryableThenSucceeds(t *testing.T) {
	adapter := &controlledTestAdapter{failWith: ErrProviderNotAvailable}
	ctrl := Controller{
		Retry: RetryPolicy{MaxAttempts: 3, BaseBackoffMs: 5, MaxBackoffMs: 50},
	}
	ctx := context.Background()
	resp, err := ctrl.ExecuteWithRetries(ctx, "session-retry-recover", func(ctx context.Context) (ProviderResponse, error) {
		if adapter.callCount < 2 {
			return adapter.Analyze(ctx, ProviderRequest{})
		}
		return ProviderResponse{
			Content:      "recovered result",
			Model:        "test-model",
			FinishReason: "stop",
			Usage:        UsageInfo{PromptTokens: 50, CompletionTokens: 25, TotalTokens: 75},
		}, nil
	})
	if err != nil {
		t.Fatalf("expected eventual success, got %v", err)
	}
	if resp.Content != "recovered result" {
		t.Fatalf("expected recovered result, got %s", resp.Content)
	}
}

func TestControllerExecuteWithRetriesExhaustsRetries(t *testing.T) {
	adapter := &controlledTestAdapter{failWith: ErrProviderNotAvailable}
	ctrl := Controller{
		Retry: RetryPolicy{MaxAttempts: 2, BaseBackoffMs: 5, MaxBackoffMs: 50},
	}
	_, err := ctrl.ExecuteWithRetries(context.Background(), "session-retry-exhaust", func(ctx context.Context) (ProviderResponse, error) {
		return adapter.Analyze(ctx, ProviderRequest{})
	})
	if !errors.Is(err, ErrRetryExhausted) {
		t.Fatalf("expected ErrRetryExhausted, got %v", err)
	}
	if adapter.callCount != 2 {
		t.Fatalf("expected 2 attempts, got %d", adapter.callCount)
	}
}

func TestControllerExecuteWithRetriesNoRetryOnNonRetryable(t *testing.T) {
	adapter := &controlledTestAdapter{failWith: ErrProviderRejected}
	ctrl := Controller{
		Retry: RetryPolicy{MaxAttempts: 3, BaseBackoffMs: 5, MaxBackoffMs: 50},
	}
	_, err := ctrl.ExecuteWithRetries(context.Background(), "session-retry-noretry", func(ctx context.Context) (ProviderResponse, error) {
		return adapter.Analyze(ctx, ProviderRequest{})
	})
	if !errors.Is(err, ErrProviderRejected) {
		t.Fatalf("expected ErrProviderRejected (no retry), got %v", err)
	}
	if adapter.callCount != 1 {
		t.Fatalf("expected only 1 attempt for non-retryable error, got %d", adapter.callCount)
	}
}

func TestControllerExecuteWithRetriesRespectsContextCancel(t *testing.T) {
	adapter := &controlledTestAdapter{failWith: ErrProviderNotAvailable}
	ctrl := Controller{
		Retry: RetryPolicy{MaxAttempts: 5, BaseBackoffMs: 100, MaxBackoffMs: 500},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ctrl.ExecuteWithRetries(ctx, "session-retry-cancel", func(ctx context.Context) (ProviderResponse, error) {
		return adapter.Analyze(ctx, ProviderRequest{})
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestControllerExecuteWithRetriesHooksCalledOnRetry(t *testing.T) {
	var hookAttempts []int
	hooks := ObservabilityHooks{
		RetryAttempt: func(_ context.Context, sessionID string, attempt int, err error) {
			hookAttempts = append(hookAttempts, attempt)
			if sessionID != "session-retry-hook" {
				t.Fatalf("expected session-retry-hook, got %s", sessionID)
			}
		},
	}
	adapter := &controlledTestAdapter{failWith: ErrProviderNotAvailable}
	ctrl := Controller{
		Retry: RetryPolicy{MaxAttempts: 3, BaseBackoffMs: 5, MaxBackoffMs: 50},
		Hooks: hooks,
	}
	ctrl.ExecuteWithRetries(context.Background(), "session-retry-hook", func(ctx context.Context) (ProviderResponse, error) {
		return adapter.Analyze(ctx, ProviderRequest{})
	})
	if len(hookAttempts) != 2 {
		t.Fatalf("expected 2 retry hook calls (max 3 attempts = 2 retries), got %d (%v)", len(hookAttempts), hookAttempts)
	}
	if hookAttempts[0] != 1 {
		t.Fatalf("expected first retry hook attempt=1, got %d", hookAttempts[0])
	}
	if hookAttempts[1] != 2 {
		t.Fatalf("expected second retry hook attempt=2, got %d", hookAttempts[1])
	}
}

func TestControllerRecordResultRecordsUsageOnSuccess(t *testing.T) {
	acc := NewUsageAccount()
	ctrl := Controller{Account: acc, CostEstimator: DefaultCostEstimator()}
	resp := &ProviderResponse{
		Content: "test",
		Usage:   UsageInfo{PromptTokens: 200, CompletionTokens: 100, TotalTokens: 300},
	}
	ctrl.RecordResult(context.Background(), "session-record", "engineer", time.Second, resp, nil)
	usage, ok := acc.SessionUsage("session-record")
	if !ok {
		t.Fatal("expected usage to be recorded")
	}
	if usage.TotalRequests != 1 || usage.TotalPromptTokens != 200 || usage.TotalCompletionTokens != 100 {
		t.Fatalf("unexpected usage: %+v", usage)
	}
	if usage.TotalCost <= 0 {
		t.Fatal("expected positive cost")
	}
}

func TestControllerRecordResultDoesNotRecordUsageOnError(t *testing.T) {
	acc := NewUsageAccount()
	ctrl := Controller{Account: acc, CostEstimator: DefaultCostEstimator()}
	ctrl.RecordResult(context.Background(), "session-record-err", "engineer", time.Second, nil, ErrProviderNotAvailable)
	_, ok := acc.SessionUsage("session-record-err")
	if ok {
		t.Fatal("expected no usage recorded on error")
	}
}

func TestControllerRecordResultFiresAfterRequestHookOnSuccess(t *testing.T) {
	called := false
	hooks := ObservabilityHooks{
		AfterRequest: func(_ context.Context, sessionID string, mode string, duration time.Duration, promptTokens int, completionTokens int, err error) {
			called = true
			if sessionID != "session-hook-success" {
				t.Fatalf("expected session-hook-success, got %s", sessionID)
			}
			if mode != "engineer" {
				t.Fatalf("expected engineer mode, got %s", mode)
			}
			if promptTokens != 100 || completionTokens != 50 {
				t.Fatalf("expected token counts 100/50, got %d/%d", promptTokens, completionTokens)
			}
			if err != nil {
				t.Fatalf("expected nil error on success, got %v", err)
			}
			if duration <= 0 {
				t.Fatal("expected positive duration")
			}
		},
	}
	ctrl := Controller{Hooks: hooks}
	resp := &ProviderResponse{Usage: UsageInfo{PromptTokens: 100, CompletionTokens: 50}}
	ctrl.RecordResult(context.Background(), "session-hook-success", "engineer", 100*time.Millisecond, resp, nil)
	if !called {
		t.Fatal("expected AfterRequest hook to be called on success")
	}
}

func TestControllerRecordResultFiresAfterRequestHookOnError(t *testing.T) {
	called := false
	hooks := ObservabilityHooks{
		AfterRequest: func(_ context.Context, sessionID string, mode string, duration time.Duration, promptTokens int, completionTokens int, err error) {
			called = true
			if !errors.Is(err, ErrProviderTimeout) {
				t.Fatalf("expected ErrProviderTimeout, got %v", err)
			}
		},
	}
	ctrl := Controller{Hooks: hooks}
	ctrl.RecordResult(context.Background(), "session-hook-err", "coach", 200*time.Millisecond, nil, ErrProviderTimeout)
	if !called {
		t.Fatal("expected AfterRequest hook to be called on error")
	}
}

func TestControllerExecuteComposeEndToEndSuccess(t *testing.T) {
	acc := NewUsageAccount()
	ctrl := Controller{
		Budget:        DefaultTokenBudget(),
		Retry:         DefaultRetryPolicy(),
		Account:       acc,
		CostEstimator: DefaultCostEstimator(),
	}
	adapter := &controlledTestAdapter{}
	resp, err := ctrl.Execute(context.Background(), "session-e2e", "engineer", 100, 5, ProviderRequest{Model: "test-model", Messages: []Message{{Role: "user", Content: "analyze"}}}, adapter)
	if err != nil {
		t.Fatalf("expected e2e success, got %v", err)
	}
	if resp.Content != "analysis result" {
		t.Fatalf("expected analysis result, got %s", resp.Content)
	}
	usage, ok := acc.SessionUsage("session-e2e")
	if !ok {
		t.Fatal("expected usage to be recorded after e2e call")
	}
	if usage.TotalRequests != 1 {
		t.Fatalf("expected 1 recorded request, got %d", usage.TotalRequests)
	}
}

func TestControllerExecuteBudgetExceededBeforeAdapterCall(t *testing.T) {
	acc := NewUsageAccount()
	ctrl := Controller{
		Budget:        TokenBudget{MaxPromptChars: 10, MaxCompletionTokens: 2000, MaxEventsPerRequest: 50},
		Account:       acc,
		CostEstimator: DefaultCostEstimator(),
	}
	adapter := &controlledTestAdapter{}
	_, err := ctrl.Execute(context.Background(), "session-e2e-budget", "engineer", 100, 1, ProviderRequest{}, adapter)
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("expected ErrBudgetExceeded before adapter call, got %v", err)
	}
	if adapter.callCount != 0 {
		t.Fatalf("expected adapter NOT to be called when budget exceeded, got %d calls", adapter.callCount)
	}
	_, ok := acc.SessionUsage("session-e2e-budget")
	if ok {
		t.Fatal("expected no usage recorded when budget exceeded")
	}
}

func TestControllerExecuteNonRetryableErrorNoRetry(t *testing.T) {
	ctrl := Controller{
		Budget: DefaultTokenBudget(),
		Retry:  RetryPolicy{MaxAttempts: 3, BaseBackoffMs: 5, MaxBackoffMs: 50},
	}
	adapter := &controlledTestAdapter{failWith: ErrProviderRejected}
	_, err := ctrl.Execute(context.Background(), "session-nonretry", "engineer", 100, 1, ProviderRequest{}, adapter)
	if !errors.Is(err, ErrProviderRejected) {
		t.Fatalf("expected ErrProviderRejected, got %v", err)
	}
	if adapter.callCount != 1 {
		t.Fatalf("expected only 1 attempt for non-retryable, got %d", adapter.callCount)
	}
}

func TestControllerExecuteHooksInvokedEndToEnd(t *testing.T) {
	var afterCalled bool
	hooks := ObservabilityHooks{
		AfterRequest: func(_ context.Context, sessionID string, mode string, duration time.Duration, promptTokens int, completionTokens int, err error) {
			afterCalled = true
		},
	}
	ctrl := Controller{
		Budget: DefaultTokenBudget(),
		Retry:  DefaultRetryPolicy(),
		Hooks:  hooks,
	}
	adapter := &controlledTestAdapter{}
	_, err := ctrl.Execute(context.Background(), "session-hooks-e2e", "coach", 100, 5, ProviderRequest{}, adapter)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !afterCalled {
		t.Fatal("expected AfterRequest hook to be called in e2e flow")
	}
}

func TestControllerExecuteRateLimitedBlocks(t *testing.T) {
	ctrl := Controller{
		Budget:      DefaultTokenBudget(),
		Retry:       RetryPolicy{MaxAttempts: 1, BaseBackoffMs: 100, MaxBackoffMs: 100},
		RateLimiter: NewRateLimiter(1, 1),
	}
	adapter := &controlledTestAdapter{}
	ctrl.Execute(context.Background(), "session-e2e-rate", "engineer", 100, 1, ProviderRequest{}, adapter)
	_, err := ctrl.Execute(context.Background(), "session-e2e-rate", "engineer", 100, 1, ProviderRequest{}, adapter)
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited on second call, got %v", err)
	}
	if adapter.callCount != 1 {
		t.Fatalf("expected adapter called only once for rate-limited session, got %d", adapter.callCount)
	}
}

func TestControllerExecuteNoRateLimiterDoesNotBlock(t *testing.T) {
	ctrl :=Controller{
		Budget: DefaultTokenBudget(),
		Retry:  RetryPolicy{MaxAttempts: 1, BaseBackoffMs: 100, MaxBackoffMs: 100},
	}
	adapter := &controlledTestAdapter{}
	for i := 0; i < 3; i++ {
		_, err := ctrl.Execute(context.Background(), "session-norate", "engineer", 100, 1, ProviderRequest{}, adapter)
		if err != nil {
			t.Fatalf("expected no rate limit without rate limiter on call %d: %v", i+1, err)
		}
	}
	if adapter.callCount != 3 {
		t.Fatalf("expected 3 adapter calls, got %d", adapter.callCount)
	}
}
