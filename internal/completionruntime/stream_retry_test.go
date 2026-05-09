package completionruntime

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"ds2api/internal/auth"
	"ds2api/internal/httpapi/openai/shared"
)

func TestExecuteStreamWithRetryUsesSharedRetryPayloadAndUsagePrompt(t *testing.T) {
	ds := &fakeDeepSeekCaller{responses: []*http.Response{
		sseHTTPResponse(http.StatusOK, `data: {"p":"response/content","v":"ok"}`),
	}}
	initial := sseHTTPResponse(http.StatusOK, `data: {"response_message_id":77,"p":"response/thinking_content","v":"plan"}`)
	payload := map[string]any{"prompt": "original prompt"}
	attemptsSeen := 0
	retryPrompt := ""

	ExecuteStreamWithRetry(context.Background(), ds, &auth.RequestAuth{}, initial, payload, "pow", StreamRetryOptions{
		Surface:      "test.stream",
		Stream:       true,
		RetryEnabled: true,
		UsagePrompt:  "original prompt",
	}, StreamRetryHooks{
		ConsumeAttempt: func(resp *http.Response, allowDeferEmpty bool) (bool, bool) {
			defer func() {
				if err := resp.Body.Close(); err != nil {
					t.Fatalf("close failed: %v", err)
				}
			}()
			_, _ = io.ReadAll(resp.Body)
			attemptsSeen++
			return attemptsSeen == 2, attemptsSeen == 1 && allowDeferEmpty
		},
		ParentMessageID: func() int {
			return 77
		},
		OnRetryPrompt: func(prompt string) {
			retryPrompt = prompt
		},
	})

	if attemptsSeen != 2 {
		t.Fatalf("expected two stream attempts, got %d", attemptsSeen)
	}
	if len(ds.payloads) != 1 {
		t.Fatalf("expected one retry completion call, got %d", len(ds.payloads))
	}
	if got := ds.payloads[0]["parent_message_id"]; got != 77 {
		t.Fatalf("retry parent_message_id mismatch: %#v", got)
	}
	if prompt, _ := ds.payloads[0]["prompt"].(string); !strings.Contains(prompt, shared.EmptyOutputRetrySuffix) {
		t.Fatalf("expected retry suffix in payload prompt, got %q", prompt)
	}
	if !strings.Contains(retryPrompt, shared.EmptyOutputRetrySuffix) {
		t.Fatalf("expected retry suffix in usage prompt, got %q", retryPrompt)
	}
}
