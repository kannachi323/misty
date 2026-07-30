package api

import (
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	agent "github.com/kannachi323/misty/server/internal/agents"
	db "github.com/kannachi323/misty/server/internal/platform/postgres"

	"github.com/go-chi/chi/v5"
)

func (s *AIService) Complete() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := s.requireUser(w, r)
		if !ok {
			return
		}
		var body struct {
			Prompt string `json:"prompt"`
		}
		if err := decodeAIJSON(w, r, &body); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		release, ok := s.acquireProviderCall(w, userID)
		if !ok {
			return
		}
		defer release()
		text, usage, err := s.runtime.CompleteWithModelContext(r.Context(), userID, body.Prompt, db.CreditMeterAutomationAI, agent.InitialSelectedModelID)
		if err != nil {
			writeAIError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"text": text, "model": agent.InitialSelectedModelID, "hosted_ai_used_ratio": usage.UsedRatio, "hosted_ai_reset_at": usage.ResetAt})
	}
}

func (s *AIService) SendMessage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := s.requireUser(w, r)
		if !ok {
			return
		}
		sessionID := strings.TrimSpace(chi.URLParam(r, "sessionID"))
		var body agent.AgentMessageRequest
		if err := decodeAIJSON(w, r, &body); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if messageRequestsDocumentTool(body) && !agentDocumentsEnabled() {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "document_agents_disabled"})
			return
		}
		bound, err := s.database.ValidateAgentSessionAccess(r.Context(), userID, sessionID)
		if err != nil {
			writeAISessionAccessError(w, err)
			return
		}
		// The session's Space comes from the session row, never from body.SpaceID.
		// A client that sends a different Space is ignored rather than trusted.
		if err := s.applySpaceContext(r.Context(), userID, sessionID, bound, &body); err != nil {
			writeSpaceError(w, err)
			return
		}
		release, ok := s.acquireProviderCall(w, userID)
		if !ok {
			return
		}
		defer release()
		tier, err := s.agentTierForUser(userID)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if err := s.runtime.SendMessageWithTierContext(r.Context(), sessionID, userID, body, tier); err != nil {
			writeAIError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func (s *AIService) Events() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := s.requireUser(w, r)
		if !ok {
			return
		}
		sessionID := strings.TrimSpace(chi.URLParam(r, "sessionID"))
		if _, err := s.database.ValidateAgentSessionAccess(r.Context(), userID, sessionID); err != nil {
			writeAISessionAccessError(w, err)
			return
		}
		after, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("after")), 10, 64)
		events, err := s.runtime.Events(sessionID, userID, after)
		if err != nil {
			writeAIError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"events": events})
	}
}

func (s *AIService) SubmitToolResults() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := s.requireUser(w, r)
		if !ok {
			return
		}
		sessionID := strings.TrimSpace(chi.URLParam(r, "sessionID"))
		var body struct {
			Results []agent.ToolResult `json:"results"`
		}
		if err := decodeAIJSONWithLimit(w, r, &body, maxAIToolJSONBodyBytes); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if toolResultsContainDocuments(body.Results) && !agentDocumentsEnabled() {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "document_agents_disabled"})
			return
		}
		if _, err := s.database.ValidateAgentSessionAccess(r.Context(), userID, sessionID); err != nil {
			writeAISessionAccessError(w, err)
			return
		}
		release, ok := s.acquireProviderCall(w, userID)
		if !ok {
			return
		}
		defer release()
		tier, err := s.agentTierForUser(userID)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if err := s.runtime.SubmitToolResultsWithTierContext(r.Context(), sessionID, userID, body.Results, tier); err != nil {
			writeAIError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func agentDocumentsEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("MISTY_AGENT_DOCUMENTS_ENABLED")), "true")
}

func messageRequestsDocumentTool(request agent.AgentMessageRequest) bool {
	for _, tool := range request.Capabilities.Tools {
		if tool.Name == agent.ToolPreviewFile {
			return true
		}
	}
	return false
}

func toolResultsContainDocuments(results []agent.ToolResult) bool {
	for _, result := range results {
		if result.Name == agent.ToolPreviewFile {
			return true
		}
	}
	return false
}

func (s *AIService) acquireProviderCall(w http.ResponseWriter, userID string) (func(), bool) {
	release, retryAfter, allowed := s.guard.AcquireProviderCall(userID)
	if !allowed {
		writeAIRateLimit(w, retryAfter)
		return nil, false
	}
	return release, true
}

func writeAIRateLimit(w http.ResponseWriter, retryAfter time.Duration) {
	seconds := retryAfterSeconds(retryAfter)
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	writeJSON(w, http.StatusTooManyRequests, map[string]any{
		"code": "rate_limited", "message": "Agent request limit reached. Try again later.",
		"retry_after_seconds": seconds,
	})
}

func (s *AIService) agentTierForUser(userID string) (agent.AgentTier, error) {
	license, err := s.database.GetLicenseByUserID(userID)
	if err != nil {
		return agent.TierLow, err
	}
	if license == nil {
		return agent.TierLow, nil
	}
	return agentTierForLicenseTier(license.Tier), nil
}

// agentTierForLicenseTier deliberately routes every paid plan to the same tier;
// TestAutomaticRoutingIsTheSameForEveryPlan guards that. The parameter is the
// seam for per-plan routing if tiers are ever priced differently, so it stays
// even though it is currently unused.
func agentTierForLicenseTier(_ db.Tier) agent.AgentTier {
	return agent.TierMed
}

func (s *AIService) Cancel() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := s.requireUser(w, r)
		if !ok {
			return
		}
		sessionID := strings.TrimSpace(chi.URLParam(r, "sessionID"))
		if err := s.runtime.Cancel(sessionID, userID); err != nil {
			writeAIError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func (s *AIService) DeleteConversation() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := s.requireUser(w, r)
		if !ok {
			return
		}
		sessionID := strings.TrimSpace(chi.URLParam(r, "sessionID"))
		if err := s.database.DeleteAgentConversation(r.Context(), userID, sessionID); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		_ = s.runtime.Forget(sessionID, userID)
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *AIService) requireUser(w http.ResponseWriter, r *http.Request) (string, bool) {
	userID, err := sessionUserID(r, s.database)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return "", false
	}
	if userID == "" {
		http.Error(w, "not authenticated", http.StatusUnauthorized)
		return "", false
	}
	return userID, true
}

func decodeAIJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	return decodeAIJSONWithLimit(w, r, dst, maxAIJSONBodyBytes)
}
