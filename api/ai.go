package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/kannachi323/misty/server/agent"
	"github.com/kannachi323/misty/server/db"
)

const (
	maxAIJSONBodyBytes     = 2 << 20
	maxAIToolJSONBodyBytes = 8 << 20
	maxAISessionTitleRunes = 120
)

type AIService struct {
	database *db.Database
	runtime  *agent.Service
	guard    *AIRequestGuard
}

func NewAIService(database *db.Database, runtime *agent.Service) *AIService {
	return &AIService{database: database, runtime: runtime, guard: NewAIRequestGuard()}
}

func (s *AIService) Status() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := s.requireUser(w, r)
		if !ok {
			return
		}
		tier, err := s.agentTierForUser(userID)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"configured":            s.runtime.AgentConfigured(tier),
			"provider":              "misty",
			"model":                 agent.InitialSelectedModelID,
			"model_name":            agent.InitialSelectedModelName,
			"running":               false,
			"session_id":            nil,
			"error":                 nil,
			"space_scoped_sessions": true,
			"capabilities": map[string]bool{
				"space_scoped_sessions": true,
			},
		})
	}
}

func (s *AIService) CreateSession() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := s.requireUser(w, r)
		if !ok {
			return
		}
		if allowed, retryAfter := s.guard.AllowSession(userID); !allowed {
			writeAIRateLimit(w, retryAfter)
			return
		}
		var body struct {
			AgentID         string `json:"agent_id"`
			AgentJobID      string `json:"agent_job_id"`
			SpaceID         string `json:"space_id"`
			ModelID         string `json:"model_id"`
			ReasoningEffort string `json:"reasoning_effort"`
		}
		if r.ContentLength > 0 && decodeAIJSON(w, r, &body) != nil {
			return
		}
		body.AgentID, body.SpaceID, body.ModelID = strings.TrimSpace(body.AgentID), strings.TrimSpace(body.SpaceID), strings.TrimSpace(body.ModelID)
		// A per-chat effort override (sent by the composer) wins over the agent default.
		requestedReasoningEffort := strings.TrimSpace(body.ReasoningEffort)
		// A chat may pin its own model, distinct from the agent's configured
		// default. When the client sends a model_id it overrides the agent's
		// model for this session only; the agent's stored model is never touched.
		requestedModelID := body.ModelID
		systemPrompt := ""
		reasoningEffort := ""
		allowTools := true
		allowWriteTools := true
		if body.AgentID != "" {
			var personal *db.PersonalAgent
			var lookupErr error
			if body.SpaceID != "" {
				personal, lookupErr = s.database.PersonalAgentForSpace(r.Context(), userID, body.SpaceID, body.AgentID)
			} else {
				personal, lookupErr = s.database.PersonalAgentByID(r.Context(), userID, body.AgentID)
			}
			if lookupErr != nil {
				writeAgentError(w, lookupErr)
				return
			}
			if requestedModelID != "" {
				// Per-chat override: run this session on the requested model
				// regardless of the agent's own model mode.
				body.ModelID = requestedModelID
			} else {
				if personal.ModelMode != "pinned" || strings.TrimSpace(personal.ModelID) == "" {
					writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "agent_model_required"})
					return
				}
				body.ModelID = personal.ModelID
			}
			var toolPolicy struct {
				Write bool `json:"write"`
			}
			if json.Unmarshal(personal.ToolPermissions, &toolPolicy) == nil {
				allowWriteTools = toolPolicy.Write
			}
			reasoningEffort = personal.ReasoningEffort
			systemPrompt = "You are " + personal.Name + ". Follow these owner-provided instructions:\n" + personal.Instructions
			if body.SpaceID != "" {
				contextText, contextErr := s.database.PersonalAgentSpaceContext(r.Context(), userID, body.SpaceID, personal.ContextPermissions)
				if contextErr != nil {
					writeSpaceError(w, contextErr)
					return
				}
				systemPrompt += "\n\nUse only this permission-filtered Space context when relevant:\n" + contextText
				memoryText, memoryErr := s.database.PersonalAgentMemoryContext(r.Context(), userID, body.SpaceID, personal.ID)
				if memoryErr != nil {
					writeAgentError(w, memoryErr)
					return
				}
				if memoryText != "" {
					systemPrompt += "\n\nPrivate memory for this user, agent, and Space. Do not expose it to other members:\n" + memoryText
				}
			}
		} else if body.SpaceID != "" {
			if err := s.database.ValidateAgentSpaceAccess(r.Context(), userID, body.SpaceID, ""); err != nil {
				writeAISessionAccessError(w, err)
				return
			}
			contextText, contextErr := s.database.PersonalAgentSpaceContext(r.Context(), userID, body.SpaceID, json.RawMessage(`{"space_chat":true,"library":true,"notes":true,"tasks":true,"members":true}`))
			if contextErr != nil {
				writeSpaceError(w, contextErr)
				return
			}
			systemPrompt = "Use this permission-filtered Space context when relevant:\n" + contextText
		}
		if body.ModelID == "" {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "agent_model_required"})
			return
		}
		if !agent.GatewayModelAvailable(r.Context(), body.ModelID) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "agent_model_unavailable"})
			return
		}
		allowTools = agent.GatewayModelSupportsTools(r.Context(), body.ModelID)
		if requestedReasoningEffort != "" {
			reasoningEffort = requestedReasoningEffort
		}
		session := s.runtime.CreateSessionWithModel(userID, userID, body.ModelID)
		_ = s.runtime.ConfigureSession(session.ID, userID, systemPrompt, allowTools, allowWriteTools)
		if reasoningEffort != "" {
			_ = s.runtime.SetSessionReasoningEffort(session.ID, userID, reasoningEffort)
		}
		if err := s.database.BindAgentSessionContext(r.Context(), userID, session.ID, body.AgentID, body.SpaceID, body.ModelID, agent.GatewayModelCatalogVersion); err != nil {
			writeAIError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"session_id": session.ID, "agent_id": body.AgentID, "space_id": body.SpaceID, "model_id": body.ModelID,
			"model_catalog_version": agent.GatewayModelCatalogVersion,
		})
	}
}

// Sessions lists the account's retained agent sessions so a client can rebuild
// its session list on a new device, or after losing local state.
func (s *AIService) Sessions() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := s.requireUser(w, r)
		if !ok {
			return
		}
		sessions, err := s.database.ListAgentSessions(r.Context(), userID)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
	}
}

// Transcript returns a session's messages so a device that has never seen the
// session can render it. Read-only: it neither resumes generation nor replays
// tool requests.
func (s *AIService) Transcript() http.HandlerFunc {
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
		messages, err := s.runtime.Transcript(r.Context(), sessionID, userID)
		if err != nil {
			writeAIError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"messages": messages})
	}
}

// RenameSession labels a session. Titles are display-only, so an over-long or
// blank title is normalised rather than rejected.
func (s *AIService) RenameSession() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := s.requireUser(w, r)
		if !ok {
			return
		}
		sessionID := strings.TrimSpace(chi.URLParam(r, "sessionID"))
		var body struct {
			Title string `json:"title"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, maxAIJSONBodyBytes)).Decode(&body); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		title := strings.TrimSpace(body.Title)
		if len([]rune(title)) > maxAISessionTitleRunes {
			title = string([]rune(title)[:maxAISessionTitleRunes])
		}
		if err := s.database.RenameAgentSession(r.Context(), userID, sessionID, title); err != nil {
			if errors.Is(err, agent.ErrPersistedSessionNotFound) {
				http.Error(w, "session not found", http.StatusNotFound)
				return
			}
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"id": sessionID, "title": title})
	}
}

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

func decodeAIJSONWithLimit(w http.ResponseWriter, r *http.Request, dst any, limit int64) error {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return errInvalidJSON
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errInvalidJSON
	}
	return nil
}

func writeAIError(w http.ResponseWriter, err error) {
	var exhausted agent.HostedAILimitReachedError
	switch {
	case errors.Is(err, context.Canceled):
		writeJSON(w, 499, map[string]any{"code": "request_canceled", "message": "Agent request canceled."})
	case errors.As(err, &exhausted):
		writeJSON(w, http.StatusPaymentRequired, map[string]any{
			"code": "hosted_ai_limit_reached", "message": "Weekly hosted AI usage is fully used.",
			"reset_at": exhausted.ResetAt, "upgrade_available": true,
		})
	case errors.Is(err, agent.ErrSessionNotFound):
		http.Error(w, "session not found", http.StatusNotFound)
	case errors.Is(err, agent.ErrModelUnavailable):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "agent_model_unavailable", "message": "The selected model is unavailable. Choose another model or Automatic."})
	case isAIInvalidRequest(err):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

func writeAISessionAccessError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, agent.ErrPersistedSessionNotFound):
		http.Error(w, "session not found", http.StatusNotFound)
	case errors.Is(err, db.ErrPersonalAgentNotFound), errors.Is(err, db.ErrSpaceForbidden), errors.Is(err, db.ErrLibraryForbidden):
		writeJSON(w, http.StatusForbidden, map[string]string{"code": "forbidden"})
	default:
		writeSpaceError(w, err)
	}
}

func isAIInvalidRequest(err error) bool {
	var invalid agent.ErrInvalidRequest
	return errors.As(err, &invalid)
}
