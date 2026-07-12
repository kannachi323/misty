package billing

import (
	"errors"
	"math"
	"strings"
	"time"

	"github.com/kannachi323/misty/server/agent"
	"github.com/kannachi323/misty/server/db"
)

type CreditMeter struct {
	database *db.Database
	now      func() time.Time
}

func NewCreditMeter(database *db.Database) *CreditMeter {
	return &CreditMeter{database: database, now: time.Now}
}

type modelRates struct {
	input, cachedInput, output int64 // thousandths of USD per one million tokens
}

const (
	creditCostBufferPercent int64 = 15
)

func ratesForModel(model string) modelRates {
	model = strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.Contains(model, "gemini-3.5-flash"):
		return modelRates{input: 1500, cachedInput: 150, output: 9000}
	case strings.Contains(model, "gemini-3.1-pro"):
		return modelRates{input: 2000, cachedInput: 200, output: 12000}
	case strings.Contains(model, "gemini-2.5-pro"):
		return modelRates{input: 1250, cachedInput: 125, output: 10000}
	case strings.Contains(model, "gemini-2.5-flash-lite"):
		return modelRates{input: 100, cachedInput: 10, output: 400}
	case strings.Contains(model, "gemini-2.5-flash"):
		return modelRates{input: 300, cachedInput: 30, output: 2500}
	case strings.Contains(model, "5.6-sol"):
		return modelRates{input: 5000, cachedInput: 500, output: 30000}
	case strings.Contains(model, "5.6-terra"):
		return modelRates{input: 2500, cachedInput: 250, output: 15000}
	case strings.Contains(model, "5.6-luna"):
		return modelRates{input: 1000, cachedInput: 100, output: 6000}
	case strings.Contains(model, "5.5-pro"), strings.Contains(model, "5.4-pro"):
		return modelRates{input: 30000, cachedInput: 30000, output: 180000}
	case strings.Contains(model, "5.5"):
		return modelRates{input: 5000, cachedInput: 500, output: 30000}
	case strings.Contains(model, "5.4-mini"):
		return modelRates{input: 750, cachedInput: 75, output: 4500}
	case strings.Contains(model, "5.4-nano"):
		return modelRates{input: 200, cachedInput: 20, output: 1250}
	case strings.Contains(model, "5.4"):
		return modelRates{input: 2500, cachedInput: 250, output: 15000}
	case strings.Contains(model, "gpt-5"):
		return modelRates{input: 1250, cachedInput: 125, output: 10000}
	default:
		// Conservative fallback matching the current flagship rate class.
		return modelRates{input: 5000, cachedInput: 500, output: 30000}
	}
}

func creditsForUsage(model string, usage agent.ModelUsage) int64 {
	rates := ratesForModel(model)
	noncachedInput := usage.InputTokens - usage.CachedInputTokens
	if noncachedInput < 0 {
		noncachedInput = 0
	}
	numerator := int64(0)
	numerator = saturatingTokenCost(numerator, noncachedInput, rates.input)
	numerator = saturatingTokenCost(numerator, usage.CachedInputTokens, rates.cachedInput)
	numerator = saturatingTokenCost(numerator, usage.OutputTokens, rates.output)

	// One credit represents one millionth of a dollar of weighted provider cost.
	// Rates are stored as thousandths of USD per million tokens, so dividing the
	// weighted numerator by 1,000 converts it to micro-credits. The 15% buffer
	// covers provider price drift and operating overhead without exposing the
	// concrete provider or model to Mika users.
	denominator := int64(100 * db.CreditDenominationScale)
	multiplier := int64(100 + creditCostBufferPercent)
	whole := numerator / denominator
	remainder := numerator % denominator
	if whole > math.MaxInt64/multiplier {
		return math.MaxInt64
	}
	credits := whole * multiplier
	if remainder > 0 {
		credits += (remainder*multiplier + denominator - 1) / denominator
	}
	if credits < 1 {
		return 1
	}
	return credits
}

func saturatingTokenCost(total, tokens, rate int64) int64 {
	if tokens <= 0 || rate <= 0 {
		return total
	}
	if tokens > (math.MaxInt64-total)/rate {
		return math.MaxInt64
	}
	return total + tokens*rate
}

func (meter *CreditMeter) Reserve(userID, idempotencyKey, usageMeter, provider, model string, estimatedInputTokens, maxOutputTokens int64) (*agent.UsageReservation, error) {
	license, err := meter.database.GetLicenseByUserID(userID)
	if err != nil {
		return nil, err
	}
	tier := db.TierBasic
	if license != nil {
		tier = license.Tier
	}
	estimate := agent.ModelUsage{InputTokens: estimatedInputTokens, OutputTokens: maxOutputTokens, Estimated: true}
	credits := creditsForUsage(model, estimate)
	reservation, wallet, err := meter.database.ReserveCredits(userID, tier, usageMeter, idempotencyKey, credits, meter.now())
	if err != nil {
		var insufficient db.InsufficientCreditsError
		if errors.As(err, &insufficient) {
			resetAt := time.Time{}
			if wallet != nil {
				resetAt = wallet.AllowanceResetAt
			}
			return nil, agent.CreditsExhaustedError{Required: insufficient.Required, Available: insufficient.Available, ResetAt: resetAt}
		}
		return nil, err
	}
	return &agent.UsageReservation{ID: reservation.ID, ReservedCredits: reservation.ReservedCredits}, nil
}

func (meter *CreditMeter) Settle(reservation *agent.UsageReservation, idempotencyKey, usageMeter, provider, model string, usage agent.ModelUsage) (agent.UsageSettlement, error) {
	if usage.InputTokens == 0 && usage.OutputTokens == 0 {
		// Some providers do not expose usage. Charge the conservative reservation rather than allowing unmetered managed AI.
		usage.Estimated = true
		usage.OutputTokens = 1
	}
	credits := creditsForUsage(model, usage)
	if usage.Estimated && credits < reservation.ReservedCredits {
		credits = reservation.ReservedCredits
	}
	if credits > reservation.ReservedCredits {
		credits = reservation.ReservedCredits
	}
	wallet, err := meter.database.SettleCreditReservation(reservation.ID, idempotencyKey, db.CreditUsage{
		Provider: provider, Model: model, InputTokens: usage.InputTokens,
		CachedInputTokens: usage.CachedInputTokens, OutputTokens: usage.OutputTokens,
		ReasoningTokens: usage.ReasoningTokens, Credits: credits,
	})
	if err != nil {
		return agent.UsageSettlement{}, err
	}
	return agent.UsageSettlement{CreditsUsed: credits, CreditsRemaining: wallet.Available()}, nil
}

func (meter *CreditMeter) Release(reservation *agent.UsageReservation) error {
	if reservation == nil {
		return nil
	}
	return meter.database.ReleaseCreditReservation(reservation.ID)
}
