package completionruntime

import (
	"context"
	"io"
	"net/http"
	"strings"

	"ds2api/internal/auth"
	"ds2api/internal/config"
	"ds2api/internal/httpapi/openai/shared"
)

type StreamRetryOptions struct {
	Surface          string
	Stream           bool
	RetryEnabled     bool
	RetryMaxAttempts int
	MaxAttempts      int
	UsagePrompt      string
}

type StreamRetryHooks struct {
	ConsumeAttempt  func(resp *http.Response, allowDeferEmpty bool) (terminalWritten bool, retryable bool)
	Finalize        func(attempts int)
	ParentMessageID func() int
	OnRetry         func(attempts int)
	OnRetryPrompt   func(prompt string)
	OnRetryFailure  func(status int, message, code string)
	OnTerminal      func(attempts int)
}

func ExecuteStreamWithRetry(ctx context.Context, ds DeepSeekCaller, a *auth.RequestAuth, initialResp *http.Response, payload map[string]any, pow string, opts StreamRetryOptions, hooks StreamRetryHooks) {
	if hooks.ConsumeAttempt == nil {
		return
	}
	surface := strings.TrimSpace(opts.Surface)
	if surface == "" {
		surface = "completion"
	}
	maxAttempts := opts.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	retryMax := opts.RetryMaxAttempts
	if retryMax <= 0 {
		retryMax = shared.EmptyOutputRetryMaxAttempts()
	}

	attempts := 0
	currentResp := initialResp
	for {
		terminalWritten, retryable := hooks.ConsumeAttempt(currentResp, opts.RetryEnabled && attempts < retryMax)
		if terminalWritten {
			if hooks.OnTerminal != nil {
				hooks.OnTerminal(attempts)
			}
			return
		}
		if !retryable || !opts.RetryEnabled || attempts >= retryMax {
			if hooks.Finalize != nil {
				hooks.Finalize(attempts)
			}
			return
		}

		attempts++
		parentMessageID := 0
		if hooks.ParentMessageID != nil {
			parentMessageID = hooks.ParentMessageID()
		}
		config.Logger.Info("[completion_runtime_empty_retry] attempting synthetic retry", "surface", surface, "stream", opts.Stream, "retry_attempt", attempts, "parent_message_id", parentMessageID)
		retryPow, powErr := ds.GetPow(ctx, a, maxAttempts)
		if powErr != nil {
			config.Logger.Warn("[completion_runtime_empty_retry] retry PoW fetch failed, falling back to original PoW", "surface", surface, "stream", opts.Stream, "retry_attempt", attempts, "error", powErr)
			retryPow = pow
		}
		nextResp, err := ds.CallCompletion(ctx, a, shared.ClonePayloadForEmptyOutputRetry(payload, parentMessageID), retryPow, maxAttempts)
		if err != nil {
			if hooks.OnRetryFailure != nil {
				hooks.OnRetryFailure(http.StatusInternalServerError, "Failed to get completion.", "error")
			}
			config.Logger.Warn("[completion_runtime_empty_retry] retry request failed", "surface", surface, "stream", opts.Stream, "retry_attempt", attempts, "error", err)
			return
		}
		if nextResp.StatusCode != http.StatusOK {
			body, readErr := io.ReadAll(nextResp.Body)
			if readErr != nil {
				config.Logger.Warn("[completion_runtime_empty_retry] retry error body read failed", "surface", surface, "stream", opts.Stream, "retry_attempt", attempts, "error", readErr)
			}
			closeRetryBody(surface, nextResp.Body)
			msg := strings.TrimSpace(string(body))
			if msg == "" {
				msg = http.StatusText(nextResp.StatusCode)
			}
			if hooks.OnRetryFailure != nil {
				hooks.OnRetryFailure(nextResp.StatusCode, msg, "error")
			}
			return
		}
		if hooks.OnRetry != nil {
			hooks.OnRetry(attempts)
		}
		if hooks.OnRetryPrompt != nil {
			hooks.OnRetryPrompt(shared.UsagePromptWithEmptyOutputRetry(opts.UsagePrompt, attempts))
		}
		currentResp = nextResp
	}
}

func closeRetryBody(surface string, body io.Closer) {
	if body == nil {
		return
	}
	if err := body.Close(); err != nil {
		config.Logger.Warn("[completion_runtime_empty_retry] retry response body close failed", "surface", surface, "error", err)
	}
}
