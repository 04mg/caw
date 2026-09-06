package providers

import (
	"encoding/json"
	"testing"
)

func TestMapQuotaSummaryToResponsePreservesGeminiWeekly(t *testing.T) {
	remGeminiWeekly := 0.899
	remGemini5h := 0.547
	rem3pWeekly := 1.0
	rem3p5h := 1.0

	qs := &QuotaSummary{
		Groups: []QuotaSummaryGroup{
			{
				DisplayName: "Gemini Models",
				Description: "Gemini models",
				Buckets: []QuotaSummaryBucket{
					{
						BucketID:          "gemini-weekly",
						DisplayName:       "Weekly Limit Remaining",
						RemainingFraction: &remGeminiWeekly,
						ResetTime:         "2026-08-30T14:52:26Z",
					},
					{
						BucketID:          "gemini-5h",
						DisplayName:       "Five Hour Limit Remaining",
						RemainingFraction: &remGemini5h,
						ResetTime:         "2026-08-25T04:23:18Z",
					},
				},
			},
			{
				DisplayName: "Claude and GPT models",
				Description: "Claude models",
				Buckets: []QuotaSummaryBucket{
					{
						BucketID:          "3p-weekly",
						DisplayName:       "Weekly Limit Remaining",
						RemainingFraction: &rem3pWeekly,
						ResetTime:         "2026-08-31T23:53:29Z",
					},
					{
						BucketID:          "3p-5h",
						DisplayName:       "Five Hour Limit Remaining",
						RemainingFraction: &rem3p5h,
						ResetTime:         "2026-08-25T04:53:29Z",
					},
				},
			},
		},
	}

	res := mapQuotaSummaryToResponse(qs)
	if res == nil {
		t.Fatal("expected non-nil response")
	}

	// Gemini weekly remaining is 0.899 -> 100 - 89 = 11% or 100 - int(0.899*100) = 11%
	if res.Weekly.Used != 11 {
		t.Fatalf("res.Weekly.Used = %v, want 11", res.Weekly.Used)
	}
	if res.Weekly.ResetTime != "2026-08-30T14:52:26Z" {
		t.Fatalf("res.Weekly.ResetTime = %q, want %q", res.Weekly.ResetTime, "2026-08-30T14:52:26Z")
	}

	// Gemini 5h remaining is 0.547 -> 100 - 54 = 46%
	if res.FiveHour.Used != 46 {
		t.Fatalf("res.FiveHour.Used = %v, want 46", res.FiveHour.Used)
	}
	if res.FiveHour.ResetTime != "2026-08-25T04:23:18Z" {
		t.Fatalf("res.FiveHour.ResetTime = %q, want %q", res.FiveHour.ResetTime, "2026-08-25T04:23:18Z")
	}

	if len(res.Groups) != 0 {
		t.Fatalf("expected 0 groups, got %d", len(res.Groups))
	}
}

func TestMapQuotaSummaryToResponseHigher3pUsageOverrides(t *testing.T) {
	remGeminiWeekly := 0.95 // 5% used
	rem3pWeekly := 0.70     // 30% used

	qs := &QuotaSummary{
		Groups: []QuotaSummaryGroup{
			{
				DisplayName: "Gemini Models",
				Buckets: []QuotaSummaryBucket{
					{
						BucketID:          "gemini-weekly",
						DisplayName:       "Weekly Limit Remaining",
						RemainingFraction: &remGeminiWeekly,
						ResetTime:         "2026-08-30T14:52:26Z",
					},
				},
			},
			{
				DisplayName: "Claude and GPT models",
				Buckets: []QuotaSummaryBucket{
					{
						BucketID:          "3p-weekly",
						DisplayName:       "Weekly Limit Remaining",
						RemainingFraction: &rem3pWeekly,
						ResetTime:         "2026-08-31T23:53:29Z",
					},
				},
			},
		},
	}

	res := mapQuotaSummaryToResponse(qs)
	if res.Weekly.Used != 30 {
		t.Fatalf("res.Weekly.Used = %v, want 30", res.Weekly.Used)
	}
	if res.Weekly.ResetTime != "2026-08-31T23:53:29Z" {
		t.Fatalf("res.Weekly.ResetTime = %q, want %q", res.Weekly.ResetTime, "2026-08-31T23:53:29Z")
	}
}

func TestQuotaSummaryResponseUnmarshalTopLevelGroups(t *testing.T) {
	raw := `{
		"groups": [
			{
				"displayName": "Gemini Models",
				"buckets": [
					{
						"bucketId": "gemini-weekly",
						"displayName": "Weekly Limit",
						"remainingFraction": 0.85
					}
				]
			}
		],
		"description": "Test description"
	}`

	var qResp QuotaSummaryResponse
	if err := json.Unmarshal([]byte(raw), &qResp); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	summary := qResp.QuotaSummary
	if summary == nil {
		summary = qResp.Response
	}
	if summary == nil && len(qResp.Groups) > 0 {
		summary = &QuotaSummary{
			Groups:      qResp.Groups,
			Description: qResp.Description,
		}
	}

	if summary == nil || len(summary.Groups) != 1 {
		t.Fatalf("expected 1 group, got %#v", summary)
	}
	res := mapQuotaSummaryToResponse(summary)
	if res.Weekly.Used != 15 {
		t.Fatalf("res.Weekly.Used = %v, want 15", res.Weekly.Used)
	}
}
