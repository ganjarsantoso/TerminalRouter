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
	get("GET /admin/v1/model-assessments/{assessmentID}", s.handleGetAssessment)
	mut("POST /admin/v1/model-assessments/{assessmentID}/cancel", s.handleCancelAssessment)
	get("GET /admin/v1/model-assessments/{assessmentID}/proposal", s.handleGetAssessmentProposal)
	mut("POST /admin/v1/model-assessments/{assessmentID}/apply", s.handleApplyAssessmentProposal)

	// Independent benchmark profile import (external consensus)
	get("GET /admin/v1/external-registry", s.handleExternalRegistryInfo)
	get("GET /admin/v1/model-profiles/{id}/external-evidence", s.handleExternalEvidenceSearch)
	mut("POST /admin/v1/model-profiles/{id}/external-evidence/proposal", s.handleExternalEvidenceProposal)
	get("GET /admin/v1/external-profile-proposals", s.handleListExternalProposals)
	get("GET /admin/v1/external-profile-proposals/{proposalID}", s.handleGetExternalProposal)
	mut("POST /admin/v1/external-profile-proposals/{proposalID}/dismiss", s.handleDismissExternalProposal)
	mut("POST /admin/v1/external-profile-proposals/{proposalID}/apply", s.handleApplyExternalProposal)
	get("GET /admin/v1/external-profile-imports", s.handleExternalImportHistory)

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

	// Playground — real completion via admin session
	mut("POST /admin/v1/playground", s.handlePlayground)

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

	// Token optimization
	get("GET /admin/v1/optimization/status", s.handleOptimizationStatus)
	mut("POST /admin/v1/optimization/analyze", s.handleOptimizationAnalyze)
	mut("POST /admin/v1/optimization/dry-run", s.handleOptimizationDryRun)
	mut("POST /admin/v1/optimization/compare", s.handleOptimizationCompare)
	get("GET /admin/v1/optimization/report", s.handleOptimizationReport)
	get("GET /admin/v1/optimization/plugins", s.handleOptimizationPlugins)
	mut("POST /admin/v1/optimization/plugins/{name}/test", s.handleOptimizationPluginsTest)

	// LUI v0.1 semantic interchange
	mut("POST /admin/v1/lui/validate", s.handleLUIValidate)
	mut("POST /admin/v1/lui/render", s.handleLUIRender)
	get("GET /admin/v1/lui/inspect/{requestID}", s.handleLUIInspect)

	// Quota & Usage (Revision 6)
	get("GET /admin/v1/quota/summary", s.handleQuotaSummary)
	get("GET /admin/v1/quota/windows", s.handleQuotaWindows)
	get("GET /admin/v1/quota/events", s.handleQuotaEvents)
	mut("POST /admin/v1/quota/refresh", s.handleQuotaRefresh)
	get("GET /admin/v1/quota/recommendations", s.handleQuotaRecommendations)

	// Provider accounts
	get("GET /admin/v1/providers/{provider}/accounts", s.handleListAccounts)
	mut("POST /admin/v1/providers/{provider}/accounts", s.handleCreateAccount)
	get("GET /admin/v1/providers/{provider}/accounts/{account}", s.handleGetAccount)
	mut("PATCH /admin/v1/providers/{provider}/accounts/{account}", s.handleUpdateAccount)
	mut("POST /admin/v1/providers/{provider}/accounts/{account}/test", s.handleTestAccount)
	mut("POST /admin/v1/providers/{provider}/accounts/{account}/drain", s.handleDrainAccount)
	mut("POST /admin/v1/providers/{provider}/accounts/{account}/resume", s.handleResumeAccount)

	// Analytics
	get("GET /admin/v1/analytics/usage", s.handleAnalyticsUsage)
	get("GET /admin/v1/analytics/cost", s.handleAnalyticsCost)
	get("GET /admin/v1/analytics/models", s.handleAnalyticsModels)
	get("GET /admin/v1/analytics/providers", s.handleAnalyticsProviders)
	get("GET /admin/v1/analytics/trends", s.handleAnalyticsTrends)
	get("GET /admin/v1/analytics/export", s.handleAnalyticsExport)
}
