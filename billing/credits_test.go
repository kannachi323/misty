package billing

import (
	"testing"

	"github.com/kannachi323/misty/server/agent"
)

func TestCreditsForUsageUsesModelRateCard(t *testing.T) {
	usage := agent.ModelUsage{InputTokens: 10_000, CachedInputTokens: 2_000, OutputTokens: 1_000}
	if got := creditsForUsage("gpt-5.6-luna", usage); got != 16_330 {
		t.Fatalf("creditsForUsage() = %d, want 16330", got)
	}
	if got := creditsForUsage("gpt-5.5", usage); got != 81_650 {
		t.Fatalf("flagship creditsForUsage() = %d, want 81650", got)
	}
	if got := creditsForUsage("gemini-3.5-flash", usage); got != 24_495 {
		t.Fatalf("Gemini creditsForUsage() = %d, want 24495", got)
	}
}

func TestCreditsForUsageIsGranularForSmallRequests(t *testing.T) {
	usage := agent.ModelUsage{InputTokens: 1_000, OutputTokens: 300}
	if got := creditsForUsage("gemini-2.5-flash-lite", usage); got != 253 {
		t.Fatalf("creditsForUsage() = %d, want 253", got)
	}
	usage.OutputTokens = 600
	if got := creditsForUsage("gemini-2.5-flash-lite", usage); got != 391 {
		t.Fatalf("larger creditsForUsage() = %d, want 391", got)
	}
}

func TestCreditsForUsageDiscountsCachedInput(t *testing.T) {
	uncached := agent.ModelUsage{InputTokens: 10_000}
	cached := agent.ModelUsage{InputTokens: 10_000, CachedInputTokens: 10_000}
	if got, want := creditsForUsage("gemini-2.5-flash", cached), creditsForUsage("gemini-2.5-flash", uncached)/10; got != want {
		t.Fatalf("cached credits = %d, want %d", got, want)
	}
}

func TestCreditsForUsageHasOneCreditMinimum(t *testing.T) {
	if got := creditsForUsage("gpt-5.6-luna", agent.ModelUsage{}); got != 1 {
		t.Fatalf("creditsForUsage(empty) = %d, want 1", got)
	}
}
