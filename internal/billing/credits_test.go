package billing

import (
	"testing"

	agent "github.com/kannachi323/misty/server/internal/agents"
)

func TestHostedAIChargeUsesModelRateCardAndSafetyMultiplier(t *testing.T) {
	usage := agent.ModelUsage{InputTokens: 10_000, CachedInputTokens: 2_000, OutputTokens: 1_000}
	if got := creditsForUsage("gpt-5.6-luna", usage); got != 17_750 {
		t.Fatalf("creditsForUsage() = %d, want 17750", got)
	}
	if got := creditsForUsage("gpt-5.5", usage); got != 88_750 {
		t.Fatalf("flagship creditsForUsage() = %d, want 88750", got)
	}
	if got := creditsForUsage("gemini-3.5-flash", usage); got != 26_625 {
		t.Fatalf("Gemini creditsForUsage() = %d, want 26625", got)
	}
}

func TestHostedAIChargeIsGranularForSmallRequests(t *testing.T) {
	usage := agent.ModelUsage{InputTokens: 1_000, OutputTokens: 300}
	if got := creditsForUsage("gemini-2.5-flash-lite", usage); got != 275 {
		t.Fatalf("creditsForUsage() = %d, want 275", got)
	}
	usage.OutputTokens = 600
	if got := creditsForUsage("gemini-2.5-flash-lite", usage); got != 425 {
		t.Fatalf("larger creditsForUsage() = %d, want 425", got)
	}
}

func TestHostedAIChargeDiscountsCachedInput(t *testing.T) {
	uncached := agent.ModelUsage{InputTokens: 10_000}
	cached := agent.ModelUsage{InputTokens: 10_000, CachedInputTokens: 10_000}
	if got, want := creditsForUsage("gemini-2.5-flash", cached), creditsForUsage("gemini-2.5-flash", uncached)/10; got != want {
		t.Fatalf("cached credits = %d, want %d", got, want)
	}
}

func TestHostedAIChargeKeepsEmptyUsageAtMicrousdPrecision(t *testing.T) {
	if got := creditsForUsage("gpt-5.6-luna", agent.ModelUsage{}); got != 2 {
		t.Fatalf("creditsForUsage(empty) = %d, want 2", got)
	}
}

func TestHostedAIModelRatesAreConfigurable(t *testing.T) {
	t.Setenv("MISTY_HOSTED_AI_MODEL_RATES_JSON", `{"custom/model":{"input":2000,"cached_input":200,"output":8000}}`)
	usage := agent.ModelUsage{InputTokens: 1_000}
	if got := creditsForUsage("custom/model", usage); got != 2_500 {
		t.Fatalf("configured hosted AI charge = %d, want 2500", got)
	}
}
