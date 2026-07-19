package console

import (
	"net/http"
)

// mountAPI registers all /admin/v1 management endpoints.
func (s *Server) mountAPI(mux *http.ServeMux) {
	// Wrap state-changing routes with CSRF protection.
	mut := func(p string, h http.HandlerFunc) {
		mux.HandleFunc(p, s.requireSession(s.csrfProtected(h)))
	}
	get := func(p string, h http.HandlerFunc) {
		mux.HandleFunc(p, s.requireSession(h))
	}

	// Status & diagnostics
	get("GET /admin/v1/status", s.handleStatus)
	get("GET /admin/v1/diagnostics", s.handleDiagnostics)
	mut("POST /admin/v1/diagnostics/run", s.handleDiagnosticsRun)

	// Providers
	get("GET /admin/v1/providers", s.handleListProviders)
	mut("POST /admin/v1/providers", s.handleCreateProvider)
	get("GET /admin/v1/providers/{id}", s.handleGetProvider)
	mut("PATCH /admin/v1/providers/{id}", s.handleUpdateProvider)
	mut("DELETE /admin/v1/providers/{id}", s.handleDeleteProvider)
	mut("POST /admin/v1/providers/{id}/test", s.handleTestProvider)
	mut("POST /admin/v1/providers/{id}/models/refresh", s.handleRefreshProviderModels)

	// Models & profiles
	get("GET /admin/v1/models", s.handleListModels)
	get("GET /admin/v1/model-profiles", s.handleListProfiles)
	get("GET /admin/v1/model-profiles/{id}", s.handleGetProfile)
	mut("PUT /admin/v1/model-profiles/{id}", s.handlePutProfile)
	mut("DELETE /admin/v1/model-profiles/{id}/overrides", s.handleResetProfile)
	mut("POST /admin/v1/model-profiles/{id}/validate", s.handleValidateProfile)

	// Model self-assessment
	get("GET /admin/v1/model-profiles/{id}/assessment/preflight", s.handleAssessmentPreflight)
	mut("POST /admin/v1/model-profiles/{id}/assessment/estimate", s.handleAssessmentEstimate)
	mut("POST /admin/v1/model-profiles/{id}/assessments", s.handleStartAssessment)
	get("GET /admin/v1/model-profiles/{id}/assessments", s.handleListAssessments)
	get("GET /admin/v1/model-assessments/{assessment-id}", s.handleGetAssessment)
	mut("POST /admin/v1/model-assessments/{assessment-id}/cancel", s.handleCancelAssessment)
	get("GET /admin/v1/model-assessments/{assessment-id}/proposal", s.handleGetAssessmentProposal)
	mut("POST /admin/v1/model-assessments/{assessment-id}/apply", s.handleApplyAssessmentProposal)

	// Aliases
	get("GET /admin/v1/aliases", s.handleListAliases)
	mut("POST /admin/v1/aliases", s.handleCreateAlias)
	get("GET /admin/v1/aliases/{id}", s.handleGetAlias)
	mut("PATCH /admin/v1/aliases/{id}", s.handleUpdateAlias)
	mut("DELETE /admin/v1/aliases/{id}", s.handleDeleteAlias)

	// Routes
	get("GET /admin/v1/routes", s.handleListRoutes)
	mut("POST /admin/v1/routes", s.handleCreateRoute)
	get("GET /admin/v1/routes/{id}", s.handleGetRoute)
	mut("PATCH /admin/v1/routes/{id}", s.handleUpdateRoute)
	mut("DELETE /admin/v1/routes/{id}", s.handleDeleteRoute)
	mut("POST /admin/v1/routes/{id}/validate", s.handleValidateRoute)
	mut("POST /admin/v1/routes/{id}/test", s.handleTestRoute)

	// Smart routes
	get("GET /admin/v1/smart/status", s.handleSmartStatus)
	mut("POST /admin/v1/smart/classify", s.handleSmartClassify)
	mut("POST /admin/v1/smart/explain", s.handleSmartExplain)
	get("GET /admin/v1/smart/reports", s.handleSmartReports)
	get("GET /admin/v1/smart/routes/{id}/report", s.handleSmartRouteReport)
	mut("POST /admin/v1/smart/routes/{id}/enable-shadow", s.handleSmartEnableShadow)
	mut("POST /admin/v1/smart/routes/{id}/enable-live", s.handleSmartEnableLive)
	mut("POST /admin/v1/smart/routes/{id}/disable", s.handleSmartDisable)

	// Activity & decisions
	get("GET /admin/v1/activity", s.handleActivity)
	get("GET /admin/v1/activity/{requestID}", s.handleActivityByID)
	get("GET /admin/v1/decisions/{requestID}", s.handleDecisionByID)

	// Client keys
	get("GET /admin/v1/client-keys", s.handleListKeys)
	mut("POST /admin/v1/client-keys", s.handleCreateKey)
	mut("POST /admin/v1/client-keys/{id}/rotate", s.handleRotateKey)
	mut("POST /admin/v1/client-keys/{id}/disable", s.handleDisableKey)
	mut("DELETE /admin/v1/client-keys/{id}", s.handleDeleteKey)

	// Configuration
	get("GET /admin/v1/config", s.handleGetConfig)
	mut("POST /admin/v1/config/validate", s.handleValidateConfig)
	mut("POST /admin/v1/config/rollback", s.handleRollbackConfig)
	get("GET /admin/v1/config/history", s.handleConfigHistory)
	get("GET /admin/v1/config/history/{revision}", s.handleConfigHistoryByRevision)
	get("GET /admin/v1/config/export", s.handleConfigExport)

	// Usage
	get("GET /admin/v1/usage/summary", s.handleUsageSummary)
	get("GET /admin/v1/usage/timeseries", s.handleUsageTimeseries)
	get("GET /admin/v1/usage/breakdown", s.handleUsageBreakdown)
}
