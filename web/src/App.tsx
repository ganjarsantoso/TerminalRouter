import React, { useState, useEffect } from 'react';
import { 
  Activity, 
  Settings, 
  Shield, 
  Sliders, 
  Terminal, 
  RefreshCw, 
  CheckCircle, 
  AlertCircle, 
  Trash2, 
  Plus, 
  Key, 
  ChevronRight, 
  Database, 
  Unlock, 
  Cpu, 
  Play, 
  ArrowRight,
  LogOut,
  Clock,
  Layers,
  Info,
  Pencil,
  XCircle
} from 'lucide-react';

// API helpers
const API_BASE = '/admin/v1';

// Build unified route list from existing config structures.
// Groups aliases pointing to the same route, detects direct aliases as Single Model,
// and surfaces unassigned internal routes.
function buildUnifiedRoutes(config: any, providerHealth: any): any[] {
  const routes: any[] = [];
  const aliasByRoute: Record<string, { name: string; isPrimary: boolean }[]> = {};
  const directAliases: { name: string; provider: string; model: string }[] = [];

  Object.entries(config.aliases || {}).forEach(([name, a]: any) => {
    if (a.route) {
      if (!aliasByRoute[a.route]) aliasByRoute[a.route] = [];
      aliasByRoute[a.route].push({ name, isPrimary: aliasByRoute[a.route].length === 0 });
    } else {
      directAliases.push({ name, provider: a.provider, model: a.model });
    }
  });

  Object.entries(config.routes || {}).forEach(([rName, r]: any) => {
    const aliases = aliasByRoute[rName] || [];
    const primaryAlias = aliases.find((a: any) => a.isPrimary) || aliases[0];
    const additionalAliases = aliases.filter((a: any) => a !== primaryAlias).map((a: any) => a.name);

    if (r.strategy === 'smart') {
      const sConf = r.smart || {};
      const candidateCount = r.candidates?.length || 0;
      routes.push({
        id: rName,
        name: primaryAlias?.name || rName,
        aliases: additionalAliases,
        mode: 'smart',
        enabled: sConf.mode !== 'off',
        internalId: rName,
        summary: `${candidateCount} candidates · ${sConf.policy || 'balanced'} policy`,
        health: `${candidateCount} candidates`,
        status: sConf.mode || 'off',
        smart: sConf,
        candidates: r.candidates || []
      });
    } else {
      const targetCount = r.targets?.length || 0;
      const healthyTargets = (r.targets || []).filter((t: any) => {
        const ph = providerHealth?.[t.provider] || {};
        return ph.circuit !== 'open';
      }).length;
      routes.push({
        id: rName,
        name: primaryAlias?.name || rName,
        aliases: additionalAliases,
        mode: 'fallback',
        enabled: true,
        internalId: rName,
        summary: `${targetCount} ordered targets`,
        health: `${healthyTargets} of ${targetCount} targets available`,
        status: healthyTargets > 0 ? 'healthy' : 'degraded',
        fallback: { targets: r.targets || [] }
      });
    }
  });

  directAliases.forEach((d) => {
    const ph = providerHealth?.[d.provider] || {};
    const isHealthy = ph.circuit !== 'open';
    routes.push({
      id: d.name,
      name: d.name,
      aliases: [],
      mode: 'single',
      enabled: true,
      internalId: null,
      summary: `${d.provider} / ${d.model}`,
      health: isHealthy ? 'Healthy' : 'Unhealthy',
      status: isHealthy ? 'healthy' : 'degraded',
      single: { provider: d.provider, model: d.model }
    });
  });

  return routes;
}

// Compute available model IDs for a given provider by merging discovered
// models (from the /models endpoint) with models already in config.
function availableModels(discoveredModels: any[], providerName: string, config: any): string[] {
  const discovered = discoveredModels
    .filter((m: any) => m.provider === providerName)
    .map((m: any) => m.model);
  const configured: string[] = [];
  Object.values(config.routes || {}).forEach((r: any) => {
    (r.targets || []).forEach((t: any) => { if (t.provider === providerName && t.model) configured.push(t.model); });
    (r.candidates || []).forEach((c: any) => { if (c.provider === providerName && c.model) configured.push(c.model); });
  });
  Object.values(config.aliases || {}).forEach((a: any) => {
    if (a.provider === providerName && a.model) configured.push(a.model);
  });
  return [...new Set([...discovered, ...configured])].sort();
}

export default function App() {
  // Authentication & Session
  const [isLoggedIn, setIsLoggedIn] = useState<boolean | null>(null);
  const [csrfToken, setCsrfToken] = useState<string>('');
  const [loading, setLoading] = useState(true);

  // App Navigation & UI State
  const [currentTab, setCurrentTab] = useState('overview');
  const [showWizard, setShowWizard] = useState(false);
  const [globalError, setGlobalError] = useState<string | null>(null);
  const [globalSuccess, setGlobalSuccess] = useState<string | null>(null);

  // Config & Database States
  const [config, setConfig] = useState<any>({
    server: {},
    credentials: { backend: 'vault' },
    providers: {},
    routes: {},
    aliases: {},
    model_profiles: {},
    logging: {}
  });
  const [configRevision, setConfigRevision] = useState<number>(0);
  const [configHistory, setConfigHistory] = useState<any[]>([]);
  const [providerHealth, setProviderHealth] = useState<any>({});
  const [discoveredModels, setDiscoveredModels] = useState<any[]>([]);
  const [clientKeys, setClientKeys] = useState<any[]>([]);
  const [activities, setActivities] = useState<any[]>([]);
  const [diagnostics, setDiagnostics] = useState<any>({ ok: [], issues: [] });
  const [activeRequestsCount, setActiveRequestsCount] = useState<number>(0);
  const [usageSummary, setUsageSummary] = useState<any>({
    TotalRequests: 0,
    SuccessCount: 0,
    ErrorCount: 0,
    InputTokens: 0,
    OutputTokens: 0,
    AvgLatencyMs: 0,
    ByProvider: {}
  });

  // Fetch CSRF and Check Session on Mount
  useEffect(() => {
    // Check if URL has token
    const params = new URLSearchParams(window.location.search);
    const token = params.get('token');
    if (token) {
      handleBootstrapLogin(token);
    } else {
      checkSession();
    }
  }, []);

  // Poll active stats when logged in
  useEffect(() => {
    if (!isLoggedIn) return;
    fetchConfig();
    fetchKeys();
    fetchActivity();
    fetchDiagnostics();
    fetchUsage();

    const interval = setInterval(() => {
      fetchDiagnostics();
      fetchUsage();
      fetchActivity();
    }, 10000);
    return () => clearInterval(interval);
  }, [isLoggedIn]);

  const checkSession = async () => {
    try {
      const res = await fetch(`${API_BASE}/session`);
      if (res.ok) {
        setIsLoggedIn(true);
        // Fetch CSRF token
        const csrfRes = await fetch(`${API_BASE}/csrf`);
        if (csrfRes.ok) {
          const csrfData = await csrfRes.json();
          setCsrfToken(csrfData.csrf_token);
        }
      } else {
        setIsLoggedIn(false);
      }
    } catch (e) {
      setIsLoggedIn(false);
    } finally {
      setLoading(false);
    }
  };

  const handleBootstrapLogin = async (token: string) => {
    try {
      const res = await fetch(`${API_BASE}/session/bootstrap`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ token })
      });
      if (res.ok) {
        setIsLoggedIn(true);
        // Clear token from URL
        window.history.replaceState({}, document.title, window.location.pathname);
        checkSession();
      } else {
        setGlobalError('Invalid or expired login token.');
        setIsLoggedIn(false);
      }
    } catch (e) {
      setGlobalError('Connection to TermRouter failed.');
      setIsLoggedIn(false);
    } finally {
      setLoading(false);
    }
  };

  const handleLogout = async () => {
    try {
      await fetch(`${API_BASE}/session`, {
        method: 'DELETE',
        headers: { 'X-CSRF-Token': csrfToken }
      });
      setIsLoggedIn(false);
      setCsrfToken('');
    } catch (e) {
      setIsLoggedIn(false);
    }
  };

  // API Call Wrapper with CSRF header
  const apiCall = async (url: string, method = 'GET', body: any = null) => {
    const headers: any = {
      'Content-Type': 'application/json',
    };
    if (csrfToken) {
      headers['X-CSRF-Token'] = csrfToken;
    }
    const opts: any = { method, headers };
    if (body) {
      opts.body = JSON.stringify(body);
    }

    try {
      const res = await fetch(url, opts);
      if (res.status === 409) {
        throw new Error('Revision Conflict: Configuration changed by another user. Reloading page.');
      }
      if (!res.ok) {
        const errData = await res.json().catch(() => ({}));
        throw new Error(errData.error?.message || `HTTP ${res.status} error`);
      }
      return res.json().catch(() => ({}));
    } catch (e: any) {
      setGlobalError(e.message);
      throw e;
    }
  };

  // Data Fetchers
  const fetchConfig = async () => {
    try {
      const data = await apiCall(`${API_BASE}/config`);
      setConfig(data.config);
      setConfigRevision(data.revision);
      fetchHistory();
      
      // Update wizard state if no providers/keys configured
      if (Object.keys(data.config.providers || {}).length === 0) {
        setShowWizard(true);
      }
    } catch (e) {}
  };

  const fetchHistory = async () => {
    try {
      const data = await apiCall(`${API_BASE}/config/history`);
      setConfigHistory(data.history || []);
    } catch (e) {}
  };

  const fetchKeys = async () => {
    try {
      const data = await apiCall(`${API_BASE}/client-keys`);
      setClientKeys(data.keys || []);
    } catch (e) {}
  };

  const fetchActivity = async () => {
    try {
      const data = await apiCall(`${API_BASE}/activity`);
      setActivities(data.requests || []);
      
      // Calc active streams
      const active = (data.requests || []).filter((r: any) => r.status_code === 0 || (r.stream && r.latency_ms === 0)).length || 0;
      setActiveRequestsCount(active);
    } catch (e) {}
  };

  const fetchDiagnostics = async () => {
    try {
      const data = await apiCall(`${API_BASE}/diagnostics`);
      const checks = data.checks || [];
      setDiagnostics({
        ok: checks.filter((c: any) => c.status === 'ok').map((c: any) => c.detail || c.name),
        issues: checks.filter((c: any) => c.status !== 'ok').map((c: any) => `${c.name}: ${c.detail}`),
        checks
      });
      // Fetch status to get provider health states
      const statusData = await apiCall(`${API_BASE}/status`);
      setProviderHealth(statusData.provider_health || statusData.health || {});
      // Fetch discovered models from the dedicated models endpoint
      try {
        const modelsData = await apiCall(`${API_BASE}/models`);
        setDiscoveredModels(modelsData.models || []);
      } catch (_) {}
    } catch (e) {}
  };

  const fetchUsage = async () => {
    try {
      const data = await apiCall(`${API_BASE}/usage/summary`);
      setUsageSummary(data.summary || {
        TotalRequests: 0,
        SuccessCount: 0,
        ErrorCount: 0,
        InputTokens: 0,
        OutputTokens: 0,
        AvgLatencyMs: 0,
        ByProvider: {}
      });
    } catch (e) {}
  };

  // Show status toasts
  const toastSuccess = (msg: string) => {
    setGlobalSuccess(msg);
    setTimeout(() => setGlobalSuccess(null), 4000);
  };

  // Main UI skeleton
  if (loading) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-zinc-950 text-zinc-50">
        <div className="text-center">
          <RefreshCw className="mx-auto h-10 w-10 animate-spin text-indigo-500" />
          <p className="mt-4 text-zinc-400">Connecting to TermRouter Console...</p>
        </div>
      </div>
    );
  }

  if (isLoggedIn === false) {
    return <LoginView onLogin={(token) => handleBootstrapLogin(token)} error={globalError} />;
  }

  return (
    <div className="flex h-screen bg-zinc-950 text-zinc-50 overflow-hidden font-sans">
      {/* Toast notifications */}
      {globalSuccess && (
        <div className="fixed top-6 right-6 z-50 flex items-center gap-2 rounded-lg bg-emerald-500/10 border border-emerald-500/20 px-4 py-3 text-emerald-400 shadow-xl backdrop-blur-md animate-float">
          <CheckCircle className="h-5 w-5" />
          <span>{globalSuccess}</span>
        </div>
      )}
      {globalError && (
        <div className="fixed top-6 right-6 z-50 flex items-center gap-2 rounded-lg bg-rose-500/10 border border-rose-500/20 px-4 py-3 text-rose-400 shadow-xl backdrop-blur-md animate-float">
          <AlertCircle className="h-5 w-5" />
          <span>{globalError}</span>
          <button onClick={() => setGlobalError(null)} className="ml-2 hover:text-rose-200">×</button>
        </div>
      )}

      {showWizard ? (
        <SetupWizard 
          onClose={() => {
            setShowWizard(false);
            fetchConfig();
          }}
          apiCall={apiCall}
          toastSuccess={toastSuccess}
        />
      ) : (
        <>
          {/* Sidebar */}
          <aside className="w-64 bg-zinc-900/60 border-r border-zinc-800 flex flex-col justify-between flex-shrink-0 backdrop-blur-md">
            <div>
              {/* Logo / Header */}
              <div className="p-6 border-b border-zinc-800 flex items-center gap-3">
                <Cpu className="h-6 w-6 text-indigo-400 animate-pulse-glow" />
                <div>
                  <h1 className="font-bold text-sm tracking-wide text-zinc-100 uppercase">TermRouter</h1>
                  <span className="text-xs text-zinc-500">Console v{configRevision ? `1.0 (rev ${configRevision})` : '1.0'}</span>
                </div>
              </div>

              {/* Navigation Items */}
              <nav className="p-4 space-y-6">
                <div>
                  <SidebarItem 
                    icon={<Layers className="h-4 w-4" />} 
                    label="Dashboard" 
                    active={currentTab === 'overview'} 
                    onClick={() => setCurrentTab('overview')} 
                  />
                </div>

                <div>
                  <div className="text-[10px] font-semibold text-zinc-500 uppercase tracking-wider px-3 mb-2">Configure</div>
                  <div className="space-y-1">
                    <SidebarItem 
                      icon={<Database className="h-4 w-4" />} 
                      label="Providers" 
                      active={currentTab === 'providers'} 
                      onClick={() => setCurrentTab('providers')} 
                    />
                    <SidebarItem 
                      icon={<Sliders className="h-4 w-4" />} 
                      label="Routes" 
                      active={currentTab === 'routes'} 
                      onClick={() => setCurrentTab('routes')} 
                    />
                    <SidebarItem 
                      icon={<Cpu className="h-4 w-4" />} 
                      label="Model Profiles" 
                      active={currentTab === 'profiles'} 
                      onClick={() => setCurrentTab('profiles')} 
                    />
                  </div>
                </div>

                <div>
                  <div className="text-[10px] font-semibold text-zinc-500 uppercase tracking-wider px-3 mb-2">Operate</div>
                  <div className="space-y-1">
                    <SidebarItem 
                      icon={<Clock className="h-4 w-4" />} 
                      label="Activity" 
                      active={currentTab === 'activity'} 
                      onClick={() => setCurrentTab('activity')} 
                    />
                    <SidebarItem 
                      icon={<Key className="h-4 w-4" />} 
                      label="Client Keys" 
                      active={currentTab === 'keys'} 
                      onClick={() => setCurrentTab('keys')} 
                    />
                    <SidebarItem 
                      icon={<Terminal className="h-4 w-4" />} 
                      label="Playground" 
                      active={currentTab === 'playground'} 
                      onClick={() => setCurrentTab('playground')} 
                    />
                  </div>
                </div>

                <div>
                  <div className="text-[10px] font-semibold text-zinc-500 uppercase tracking-wider px-3 mb-2">System</div>
                  <div className="space-y-1">
                    <SidebarItem 
                      icon={<Settings className="h-4 w-4" />} 
                      label="Settings" 
                      active={currentTab === 'system'} 
                      onClick={() => setCurrentTab('system')} 
                    />
                  </div>
                </div>
              </nav>
            </div>

            {/* Sidebar Footer */}
            <div className="p-4 border-t border-zinc-800 bg-zinc-950/40 space-y-3">
              <div className="flex items-center justify-between text-xs text-zinc-500 px-2">
                <span className="flex items-center gap-1.5">
                  <span className={`h-2 w-2 rounded-full ${diagnostics.issues?.length === 0 ? 'bg-emerald-500' : 'bg-amber-500'}`}></span>
                  System {diagnostics.issues?.length === 0 ? 'Healthy' : 'Warnings'}
                </span>
                <span className="bg-zinc-800 text-zinc-400 px-1.5 py-0.5 rounded text-[10px]">LOCAL</span>
              </div>
              <button 
                onClick={handleLogout}
                className="w-full flex items-center justify-center gap-2 rounded-lg border border-zinc-800 bg-zinc-900/50 hover:bg-zinc-800 px-3 py-2 text-sm text-zinc-300 hover:text-zinc-50 transition-colors"
              >
                <LogOut className="h-4 w-4" />
                Sign Out
              </button>
            </div>
          </aside>

          {/* Main Content Area */}
          <main className="flex-1 flex flex-col min-w-0 overflow-y-auto bg-gradient-to-br from-zinc-950 via-zinc-900/40 to-zinc-950">
            {/* Topbar / Status Header */}
            <header className="h-16 border-b border-zinc-800/80 flex items-center justify-between px-8 flex-shrink-0 bg-zinc-950/60 backdrop-blur-md">
              <div className="flex items-center gap-4">
                <h2 className="text-lg font-bold text-zinc-200 capitalize">{currentTab.replace('-', ' ')}</h2>
                <div className="h-4 w-px bg-zinc-800"></div>
                <div className="flex items-center gap-2 text-xs text-zinc-400">
                  <span className="font-semibold text-zinc-300">Gateway:</span>
                  <a href={`http://${config.server?.host || '127.0.0.1'}:${config.server?.port || 8787}`} target="_blank" className="font-mono text-indigo-400 hover:underline">
                    {config.server?.host || '127.0.0.1'}:{config.server?.port || 8787}
                  </a>
                </div>
              </div>
              <div className="flex items-center gap-3">
                <button 
                  onClick={() => setShowWizard(true)}
                  className="rounded-lg bg-zinc-900 border border-zinc-800 hover:border-zinc-700 px-3 py-1.5 text-xs text-zinc-300 hover:text-zinc-50 transition-colors flex items-center gap-1.5"
                >
                  <Sliders className="h-3.5 w-3.5" />
                  Run Setup Wizard
                </button>
                {activeRequestsCount > 0 && (
                  <span className="flex items-center gap-1.5 rounded-full bg-indigo-500/10 border border-indigo-500/30 px-3 py-1 text-xs text-indigo-400 animate-pulse">
                    <span className="h-1.5 w-1.5 rounded-full bg-indigo-500"></span>
                    {activeRequestsCount} Active Request(s)
                  </span>
                )}
              </div>
            </header>

            {/* Tab Contents */}
            <div className="p-8 max-w-7xl w-full mx-auto space-y-8 flex-1">
              {currentTab === 'overview' && (
                <OverviewTab 
                  config={config} 
                  providerHealth={providerHealth}
                  usageSummary={usageSummary} 
                  activities={activities}
                  diagnostics={diagnostics}
                  setCurrentTab={setCurrentTab}
                />
              )}

              {currentTab === 'providers' && (
                <ProvidersTab 
                  config={config} 
                  providerHealth={providerHealth}
                  discoveredModels={discoveredModels}
                  apiCall={apiCall}
                  fetchConfig={fetchConfig}
                  toastSuccess={toastSuccess}
                />
              )}

              {currentTab === 'profiles' && (
                <ProfilesTab 
                  config={config}
                  discoveredModels={discoveredModels}
                  apiCall={apiCall}
                  fetchConfig={fetchConfig}
                  toastSuccess={toastSuccess}
                />
              )}

              {currentTab === 'routes' && (
                <RoutesTab 
                  config={config}
                  discoveredModels={discoveredModels}
                  providerHealth={providerHealth}
                  apiCall={apiCall}
                  fetchConfig={fetchConfig}
                  toastSuccess={toastSuccess}
                />
              )}

              {currentTab === 'activity' && (
                <ActivityTab 
                  activities={activities}
                  apiCall={apiCall}
                />
              )}

              {currentTab === 'keys' && (
                <KeysTab 
                  clientKeys={clientKeys}
                  apiCall={apiCall}
                  fetchKeys={fetchKeys}
                  toastSuccess={toastSuccess}
                  config={config}
                />
              )}

              {currentTab === 'playground' && (
                <PlaygroundTab 
                  config={config}
                  apiCall={apiCall}
                />
              )}

              {currentTab === 'system' && (
                <SystemTab 
                  diagnostics={diagnostics}
                  configHistory={configHistory}
                  apiCall={apiCall}
                  fetchConfig={fetchConfig}
                  fetchDiagnostics={fetchDiagnostics}
                  toastSuccess={toastSuccess}
                />
              )}
            </div>
          </main>
        </>
      )}
    </div>
  );
}

// Sidebar Navigation Item Component
interface SidebarItemProps {
  icon: React.ReactNode;
  label: string;
  active: boolean;
  onClick: () => void;
}

function SidebarItem({ icon, label, active, onClick }: SidebarItemProps) {
  return (
    <button
      onClick={onClick}
      className={`w-full flex items-center gap-3 px-3 py-2 rounded-lg text-sm transition-all duration-150 ${
        active 
          ? 'bg-indigo-600 text-white font-medium shadow-md shadow-indigo-600/20' 
          : 'text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800/40'
      }`}
    >
      {icon}
      <span>{label}</span>
    </button>
  );
}

// Login view
function LoginView({ onLogin, error }: { onLogin: (t: string) => void, error: string | null }) {
  const [tokenInput, setTokenInput] = useState('');

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!tokenInput.trim()) return;
    onLogin(tokenInput.trim());
  };

  return (
    <div className="min-h-screen flex items-center justify-center bg-zinc-950 text-zinc-50 font-sans p-6">
      <div className="max-w-md w-full glass-panel rounded-2xl border border-zinc-800 p-8 shadow-2xl relative overflow-hidden">
        <div className="absolute top-0 left-0 right-0 h-1 bg-gradient-to-r from-indigo-500 via-purple-500 to-indigo-500 animate-pulse"></div>
        <div className="text-center mb-8">
          <div className="inline-flex p-3 rounded-xl bg-indigo-500/10 border border-indigo-500/20 mb-3 animate-float">
            <Cpu className="h-8 w-8 text-indigo-400" />
          </div>
          <h2 className="text-2xl font-bold tracking-tight text-zinc-100">TermRouter Console</h2>
          <p className="text-zinc-400 text-sm mt-1">Provide your local bootstrap token to authenticate</p>
        </div>

        {error && (
          <div className="mb-6 rounded-lg bg-rose-500/10 border border-rose-500/20 px-4 py-3 text-sm text-rose-400 flex items-start gap-2.5">
            <AlertCircle className="h-5 w-5 flex-shrink-0 mt-0.5" />
            <div>
              <p className="font-semibold">Authentication Failed</p>
              <p className="text-xs text-rose-300/80 mt-0.5">{error}</p>
            </div>
          </div>
        )}

        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="block text-xs font-semibold text-zinc-400 uppercase tracking-wider mb-2">Bootstrap Token</label>
            <div className="relative">
              <input
                type="password"
                value={tokenInput}
                onChange={(e) => setTokenInput(e.target.value)}
                placeholder="Enter token from terminal log..."
                className="w-full bg-zinc-900 border border-zinc-800 rounded-xl px-4 py-3 text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500/50 focus:border-indigo-500 text-zinc-100 font-mono"
              />
            </div>
            <p className="text-[10px] text-zinc-500 mt-2 leading-relaxed">
              When TermRouter Console starts, it logs a link like <code className="font-mono text-zinc-400">/login?token=...</code> to your terminal. Copy the token parameter value above.
            </p>
          </div>

          <button
            type="submit"
            className="w-full flex items-center justify-center gap-2 rounded-xl bg-indigo-600 hover:bg-indigo-500 active:bg-indigo-700 px-4 py-3 font-semibold text-sm shadow-lg shadow-indigo-600/20 transition-all"
          >
            <Unlock className="h-4 w-4" />
            Unlock Console
          </button>
        </form>
      </div>
    </div>
  );
}

// ----------------------------------------------------
// WIZARD VIEW
// ----------------------------------------------------
function SetupWizard({ onClose, apiCall, toastSuccess }: { onClose: () => void, apiCall: any, toastSuccess: any }) {
  const [step, setStep] = useState(1);
  const [backend, setBackend] = useState('vault');
  const [providerType, setProviderType] = useState('openai');
  const [providerName, setProviderName] = useState('openai');
  const [baseURL, setBaseURL] = useState('https://api.openai.com/v1');
  const [credMethod, setCredMethod] = useState('key'); // key | env
  const [apiKey, setApiKey] = useState('');
  const [envName, setEnvName] = useState('OPENAI_API_KEY');
  const [testResult, setTestResult] = useState<any>(null);
  const [testing, setTesting] = useState(false);
  const [selectedModel, setSelectedModel] = useState('');
  const [customModel, setCustomModel] = useState('');
  const [clientKeyName, setClientKeyName] = useState('dev-key');
  const [generatedKey, setGeneratedKey] = useState('');
  const [keyConfirmed, setKeyConfirmed] = useState(false);
  
  // Update fields when provider type changes
  useEffect(() => {
    if (providerType === 'openai') {
      setProviderName('openai');
      setBaseURL('https://api.openai.com/v1');
      setEnvName('OPENAI_API_KEY');
    } else if (providerType === 'anthropic') {
      setProviderName('anthropic');
      setBaseURL('https://api.anthropic.com');
      setEnvName('ANTHROPIC_API_KEY');
    } else {
      setProviderName('custom-server');
      setBaseURL('http://localhost:11434/v1');
      setEnvName('CUSTOM_API_KEY');
    }
  }, [providerType]);

  const handleTestProvider = async () => {
    setTesting(true);
    setTestResult(null);
    try {
      const credential = credMethod === 'env'
        ? { method: 'env', value: envName }
        : { method: 'vault', value: apiKey };
      
      const payload: any = {
        name: providerName,
        type: providerType,
        base_url: baseURL,
        credential,
        enabled: true
      };

      await apiCall(`${API_BASE}/providers`, 'POST', payload);
      
      // Trigger test
      const testData = await apiCall(`${API_BASE}/providers/${providerName}/test`, 'POST');
      setTestResult({ ok: true, models: testData.models || [] });
      if (testData.models && testData.models.length > 0) {
        setSelectedModel(testData.models[0]);
      }
    } catch (e: any) {
      setTestResult({ ok: false, error: e.message });
    } finally {
      setTesting(false);
    }
  };

  const handleFinishSetup = async () => {
    try {
      // Save route
      const finalModel = selectedModel || customModel || 'gpt-4o';
      const routePayload = {
        name: 'default',
        strategy: 'direct',
        targets: [{ provider: providerName, model: finalModel }]
      };
      await apiCall(`${API_BASE}/routes`, 'POST', routePayload);

      // Create alias
      const aliasPayload = {
        name: 'general',
        route: 'default'
      };
      await apiCall(`${API_BASE}/aliases`, 'POST', aliasPayload);

      // Create client key
      const keyPayload = {
        name: clientKeyName,
        aliases: ['general']
      };
      const keyData = await apiCall(`${API_BASE}/client-keys`, 'POST', keyPayload);
      setGeneratedKey(keyData.key);
      setStep(6); // Go to end step
    } catch (e) {}
  };

  return (
    <div className="min-h-screen flex items-center justify-center bg-zinc-950 text-zinc-50 font-sans p-6 w-full">
      <div className="max-w-2xl w-full glass-panel rounded-2xl border border-zinc-800 shadow-2xl p-8 space-y-8">
        {/* Header */}
        <div className="flex items-center justify-between border-b border-zinc-800 pb-4">
          <div className="flex items-center gap-3">
            <Cpu className="h-6 w-6 text-indigo-400" />
            <h2 className="text-lg font-bold">TermRouter Setup Wizard</h2>
          </div>
          <div className="text-xs text-zinc-500">Step {step} of 6</div>
        </div>

        {/* Progress Bar */}
        <div className="w-full bg-zinc-800 h-1.5 rounded-full overflow-hidden">
          <div className="bg-indigo-600 h-full transition-all duration-300" style={{ width: `${(step / 6) * 100}%` }}></div>
        </div>

        {/* Wizard Steps */}
        {step === 1 && (
          <div className="space-y-6">
            <h3 className="text-xl font-semibold">Welcome to TermRouter</h3>
            <p className="text-zinc-400 text-sm leading-relaxed">
              TermRouter is a lightweight, local-first API gateway and routing layer for AI models. It aggregates multiple providers (OpenAI, Anthropic, Ollama, etc.) and routes client requests intelligently.
            </p>
            <div className="bg-indigo-500/5 border border-indigo-500/20 rounded-xl p-4 text-xs text-indigo-300 leading-relaxed">
              <strong>Local-First Design:</strong> All your API keys are stored locally in your operating system keyring, an encrypted SQLite database vault, or environment variables. No secrets ever leave your machine.
            </div>
            <div>
              <label className="block text-xs font-semibold text-zinc-400 uppercase tracking-wider mb-2">Select Credential Storage Backend</label>
              <div className="grid grid-cols-3 gap-4">
                <label className={`flex flex-col p-4 rounded-xl border cursor-pointer hover:bg-zinc-900/50 transition-colors ${backend === 'vault' ? 'border-indigo-500 bg-indigo-500/5' : 'border-zinc-800'}`}>
                  <input type="radio" name="backend" value="vault" checked={backend === 'vault'} onChange={() => setBackend('vault')} className="sr-only" />
                  <span className="font-semibold text-sm">Encrypted Vault</span>
                  <span className="text-[10px] text-zinc-500 mt-1">Recommended. Encrypted file-based storage.</span>
                </label>
                <label className={`flex flex-col p-4 rounded-xl border cursor-pointer hover:bg-zinc-900/50 transition-colors ${backend === 'keyring' ? 'border-indigo-500 bg-indigo-500/5' : 'border-zinc-800'}`}>
                  <input type="radio" name="backend" value="keyring" checked={backend === 'keyring'} onChange={() => setBackend('keyring')} className="sr-only" />
                  <span className="font-semibold text-sm">OS Keyring</span>
                  <span className="text-[10px] text-zinc-500 mt-1">Uses Windows Credential Manager or macOS Keychain.</span>
                </label>
                <label className={`flex flex-col p-4 rounded-xl border cursor-pointer hover:bg-zinc-900/50 transition-colors ${backend === 'env' ? 'border-indigo-500 bg-indigo-500/5' : 'border-zinc-800'}`}>
                  <input type="radio" name="backend" value="env" checked={backend === 'env'} onChange={() => setBackend('env')} className="sr-only" />
                  <span className="font-semibold text-sm">Env References</span>
                  <span className="text-[10px] text-zinc-500 mt-1">Loads references directly from environment variables.</span>
                </label>
              </div>
            </div>
          </div>
        )}

        {step === 2 && (
          <div className="space-y-6">
            <h3 className="text-xl font-semibold">Add a Model Provider</h3>
            <p className="text-zinc-400 text-sm">Let's connect your first LLM provider. Select the type below:</p>
            <div className="grid grid-cols-3 gap-4">
              <label className={`flex flex-col items-center justify-center p-4 rounded-xl border cursor-pointer hover:bg-zinc-900/50 transition-colors ${providerType === 'openai' ? 'border-indigo-500 bg-indigo-500/5' : 'border-zinc-800'}`}>
                <input type="radio" name="pType" value="openai" checked={providerType === 'openai'} onChange={() => setProviderType('openai')} className="sr-only" />
                <span className="font-bold text-sm">OpenAI</span>
              </label>
              <label className={`flex flex-col items-center justify-center p-4 rounded-xl border cursor-pointer hover:bg-zinc-900/50 transition-colors ${providerType === 'anthropic' ? 'border-indigo-500 bg-indigo-500/5' : 'border-zinc-800'}`}>
                <input type="radio" name="pType" value="anthropic" checked={providerType === 'anthropic'} onChange={() => setProviderType('anthropic')} className="sr-only" />
                <span className="font-bold text-sm">Anthropic</span>
              </label>
              <label className={`flex flex-col items-center justify-center p-4 rounded-xl border cursor-pointer hover:bg-zinc-900/50 transition-colors ${providerType === 'openai-compatible' ? 'border-indigo-500 bg-indigo-500/5' : 'border-zinc-800'}`}>
                <input type="radio" name="pType" value="openai-compatible" checked={providerType === 'openai-compatible'} onChange={() => setProviderType('openai-compatible')} className="sr-only" />
                <span className="font-bold text-sm">Custom Compatible</span>
              </label>
            </div>

            <div className="grid grid-cols-2 gap-4">
              <div>
                <label className="block text-xs font-semibold text-zinc-400 mb-1">Provider ID (lowercase slug)</label>
                <input type="text" value={providerName} onChange={(e) => setProviderName(e.target.value)} className="w-full bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-2 text-sm text-zinc-100" />
              </div>
              <div>
                <label className="block text-xs font-semibold text-zinc-400 mb-1">Base URL</label>
                <input type="text" value={baseURL} onChange={(e) => setBaseURL(e.target.value)} className="w-full bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-2 text-sm text-zinc-100 font-mono" />
              </div>
            </div>

            <div className="border-t border-zinc-800 pt-4 space-y-4">
              <div>
                <label className="block text-xs font-semibold text-zinc-400 mb-2">Authentication Method</label>
                <div className="flex gap-4 text-xs">
                  <label className="flex items-center gap-2">
                    <input type="radio" name="cred" checked={credMethod === 'key'} onChange={() => setCredMethod('key')} />
                    Enter API Key directly
                  </label>
                  <label className="flex items-center gap-2">
                    <input type="radio" name="cred" checked={credMethod === 'env'} onChange={() => setCredMethod('env')} />
                    Reference Env Var name
                  </label>
                </div>
              </div>

              {credMethod === 'key' ? (
                <div>
                  <label className="block text-xs font-semibold text-zinc-400 mb-1">API Key</label>
                  <input type="password" value={apiKey} onChange={(e) => setApiKey(e.target.value)} placeholder="sk-..." className="w-full bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-2 text-sm text-zinc-100 font-mono" />
                </div>
              ) : (
                <div>
                  <label className="block text-xs font-semibold text-zinc-400 mb-1">Environment Variable Name</label>
                  <input type="text" value={envName} onChange={(e) => setEnvName(e.target.value)} placeholder="OPENAI_API_KEY" className="w-full bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-2 text-sm text-zinc-100 font-mono" />
                </div>
              )}
            </div>
          </div>
        )}

        {step === 3 && (
          <div className="space-y-6">
            <h3 className="text-xl font-semibold">Test Connection</h3>
            <p className="text-zinc-400 text-sm">We will now perform a network ping and model discovery probe to verify connectivity to <span className="font-semibold text-zinc-200">{providerName}</span>.</p>

            <button 
              onClick={handleTestProvider} 
              disabled={testing}
              className="w-full flex items-center justify-center gap-2 rounded-xl bg-indigo-600 hover:bg-indigo-500 px-4 py-3 font-semibold text-sm transition-all"
            >
              {testing ? <RefreshCw className="h-4 w-4 animate-spin" /> : <Play className="h-4 w-4" />}
              {testing ? 'Testing Connection...' : 'Run Connectivity Test'}
            </button>

            {testResult && (
              <div className={`p-4 rounded-xl border text-sm ${testResult.ok ? 'bg-emerald-500/10 border-emerald-500/20 text-emerald-400' : 'bg-rose-500/10 border-rose-500/20 text-rose-400'}`}>
                {testResult.ok ? (
                  <div className="space-y-2">
                    <div className="flex items-center gap-2 font-semibold">
                      <CheckCircle className="h-5 w-5" />
                      Connection Successful!
                    </div>
                    <p className="text-xs text-zinc-400">Discovered models: {testResult.models?.join(', ') || 'none'}</p>
                  </div>
                ) : (
                  <div className="space-y-2">
                    <div className="flex items-center gap-2 font-semibold">
                      <AlertCircle className="h-5 w-5" />
                      Test Failed
                    </div>
                    <p className="text-xs text-zinc-300">{testResult.error}</p>
                  </div>
                )}
              </div>
            )}
          </div>
        )}

        {step === 4 && (
          <div className="space-y-6">
            <h3 className="text-xl font-semibold">Model Discovery & Routing</h3>
            <p className="text-zinc-400 text-sm">TermRouter uses public model names to map requests to models. Let's create a route named <span className="font-semibold text-indigo-400">general</span> that points to a specific model.</p>

            {testResult?.models && testResult.models.length > 0 ? (
              <div>
                <label className="block text-xs font-semibold text-zinc-400 mb-1">Select Discovered Model</label>
                <select value={selectedModel} onChange={(e) => setSelectedModel(e.target.value)} className="w-full bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-2 text-sm text-zinc-100">
                  {testResult.models.map((m: string) => <option key={m} value={m}>{m}</option>)}
                </select>
              </div>
            ) : (
              <div>
                <label className="block text-xs font-semibold text-zinc-400 mb-1">Enter Model ID</label>
                <input type="text" value={customModel} onChange={(e) => setCustomModel(e.target.value)} placeholder="gpt-4o" className="w-full bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-2 text-sm text-zinc-100" />
              </div>
            )}
            <p className="text-xs text-zinc-500 leading-relaxed">
              When client apps call TermRouter requesting the model <code className="font-mono text-zinc-400">general</code> (Single Model route), it will always execute against provider <code className="font-mono text-zinc-400">{providerName}</code> with target model <code className="font-mono text-zinc-400">{selectedModel || customModel || 'gpt-4o'}</code>.
            </p>
          </div>
        )}

        {step === 5 && (
          <div className="space-y-6">
            <h3 className="text-xl font-semibold">Generate Router Client Key</h3>
            <p className="text-zinc-400 text-sm">Your applications need a TermRouter client key to authenticate against the gateway. Let's create one.</p>
            <div>
              <label className="block text-xs font-semibold text-zinc-400 mb-1">Client Key Name</label>
              <input type="text" value={clientKeyName} onChange={(e) => setClientKeyName(e.target.value)} className="w-full bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-2 text-sm text-zinc-100" />
            </div>
            <p className="text-xs text-zinc-500">This key will have access to the <code className="font-mono text-zinc-400">general</code> route we created.</p>
            <button 
              onClick={handleFinishSetup}
              className="w-full flex items-center justify-center gap-2 rounded-xl bg-indigo-600 hover:bg-indigo-500 px-4 py-3 font-semibold text-sm transition-all"
            >
              Generate Key & Finish Setup
            </button>
          </div>
        )}

        {step === 6 && (
          <div className="space-y-6">
            <h3 className="text-xl font-semibold text-emerald-400">Setup Complete!</h3>
            <p className="text-zinc-400 text-sm">TermRouter is successfully configured and ready to handle model requests!</p>

            <div className="p-4 rounded-xl bg-zinc-900 border border-zinc-800 space-y-4">
              <div>
                <label className="block text-xs font-semibold text-zinc-500 mb-1">YOUR CLIENT API KEY (SAVE THIS!):</label>
                <div className="flex gap-2">
                  <input type="text" readOnly value={generatedKey} className="flex-1 bg-zinc-950 border border-zinc-800 rounded-lg px-3 py-2 text-sm font-mono text-emerald-400" />
                  <button onClick={() => {
                    navigator.clipboard.writeText(generatedKey);
                    toastSuccess('Copied API key to clipboard');
                  }} className="bg-zinc-850 hover:bg-zinc-800 px-3 py-2 rounded-lg text-xs">Copy</button>
                </div>
                <p className="text-[10px] text-amber-400 mt-1">This key is only shown once. We store only its cryptographic hash.</p>
              </div>

              <div className="space-y-2">
                <label className="flex items-center gap-2 text-xs font-semibold text-zinc-300">
                  <input type="checkbox" checked={keyConfirmed} onChange={(e) => setKeyConfirmed(e.target.checked)} />
                  I confirm that I have copied my client API key.
                </label>
              </div>
            </div>

            <div className="space-y-2">
              <label className="block text-xs font-semibold text-zinc-400 uppercase tracking-wider">Example client invocation:</label>
              <pre className="bg-zinc-950 border border-zinc-800 rounded-lg p-3 text-xs font-mono text-zinc-300 overflow-x-auto">
{`curl http://127.0.0.1:8787/v1/chat/completions \\
  -H "Authorization: Bearer ${generatedKey || 'tr_live_...'}" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "general",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'  
# "general" is the public route name → resolves to ${providerName}/${selectedModel || customModel || 'gpt-4o'}`}
              </pre>
            </div>

            <button 
              disabled={!keyConfirmed}
              onClick={onClose}
              className="w-full flex items-center justify-center gap-2 rounded-xl bg-indigo-600 hover:bg-indigo-500 disabled:opacity-40 disabled:hover:bg-indigo-600 px-4 py-3 font-semibold text-sm transition-all"
            >
              Enter Console Dashboard
              <ArrowRight className="h-4 w-4" />
            </button>
          </div>
        )}

        {/* Nav Buttons */}
        {step < 6 && (
          <div className="flex justify-between border-t border-zinc-800 pt-4">
            <button 
              disabled={step === 1}
              onClick={() => setStep(step - 1)}
              className="rounded-lg border border-zinc-800 hover:bg-zinc-900 px-4 py-2 text-sm disabled:opacity-40"
            >
              Back
            </button>
            <button 
              disabled={
                step === 3 && !testResult?.ok ||
                step === 5
              }
              onClick={() => setStep(step + 1)}
              className="rounded-lg bg-indigo-600 hover:bg-indigo-500 px-4 py-2 text-sm disabled:opacity-40"
            >
              Next
            </button>
          </div>
        )}
      </div>
    </div>
  );
}

// ----------------------------------------------------
// DASHBOARD VIEW
// ----------------------------------------------------
function OverviewTab({ config, providerHealth, usageSummary, activities, diagnostics, setCurrentTab }: any) {
  const providersCount = Object.keys(config.providers || {}).length;

  const unifiedRoutes = buildUnifiedRoutes(config, {});
  const singleCount = unifiedRoutes.filter((r: any) => r.mode === 'single').length;
  const fallbackCount = unifiedRoutes.filter((r: any) => r.mode === 'fallback').length;
  const smartCount = unifiedRoutes.filter((r: any) => r.mode === 'smart').length;
  const publicRouteCount = unifiedRoutes.length;

  return (
    <div className="space-y-8">
      {/* Top row cards */}
      <div className="grid grid-cols-4 gap-6">
        <StatCard title="Total Providers" value={providersCount} icon={<Database className="h-5 w-5 text-indigo-400" />} />
        <StatCard title="Public Routes" value={publicRouteCount} icon={<Sliders className="h-5 w-5 text-teal-400" />} />
        <StatCard title="Request Load (Today)" value={usageSummary.TotalRequests} icon={<Activity className="h-5 w-5 text-indigo-400" />} />
        <StatCard title="Error Rate" value={usageSummary.TotalRequests > 0 ? `${((usageSummary.ErrorCount / usageSummary.TotalRequests) * 100).toFixed(1)}%` : '0%'} icon={<AlertCircle className="h-5 w-5 text-rose-400" />} />
      </div>

      {/* Route mode breakdown */}
      {publicRouteCount > 0 && (
        <div className="flex items-center gap-4 text-xs text-zinc-400">
          <span className="font-semibold text-zinc-300">Route breakdown:</span>
          <span className="flex items-center gap-1">
            <span className="h-2 w-2 rounded-full bg-indigo-500"></span>
            Single Model: {singleCount}
          </span>
          <span className="flex items-center gap-1">
            <span className="h-2 w-2 rounded-full bg-amber-500"></span>
            Fallback: {fallbackCount}
          </span>
          <span className="flex items-center gap-1">
            <span className="h-2 w-2 rounded-full bg-emerald-500"></span>
            Smart: {smartCount}
          </span>
        </div>
      )}

      <div className="grid grid-cols-3 gap-8">
        {/* Provider Health list */}
        <div className="col-span-2 glass-panel rounded-2xl border border-zinc-800/80 p-6 space-y-4">
          <div className="flex items-center justify-between border-b border-zinc-800 pb-3">
            <h3 className="font-bold text-sm tracking-wide uppercase text-zinc-300">Provider Health Status</h3>
            <button onClick={() => setCurrentTab('providers')} className="text-xs text-indigo-400 hover:underline">Manage Providers</button>
          </div>
          {providersCount === 0 ? (
            <div className="text-center py-6 text-zinc-500 text-sm">No providers configured yet.</div>
          ) : (
            <div className="divide-y divide-zinc-800/50">
              {Object.entries(config.providers || {}).map(([name, p]: any) => {
                const health = providerHealth[name] || {};
                const isEnabled = p.enabled !== false;
                const status = !isEnabled ? 'disabled' : (health.circuit === 'open' ? 'open' : 'healthy');
                return (
                  <div key={name} className="flex items-center justify-between py-3.5 first:pt-0 last:pb-0">
                    <div>
                      <div className="font-semibold text-zinc-100 flex items-center gap-2">
                        {name}
                        <span className="text-[10px] bg-zinc-800 text-zinc-400 px-1.5 py-0.5 rounded font-mono uppercase">{p.type}</span>
                      </div>
                      <div className="text-xs text-zinc-500 mt-0.5 font-mono">{p.base_url}</div>
                    </div>
                    <div className="flex items-center gap-3">
                      {status === 'disabled' && <span className="text-xs rounded-full bg-zinc-800 border border-zinc-700 px-2.5 py-1 text-zinc-400">Disabled</span>}
                      {status === 'open' && <span className="text-xs rounded-full bg-rose-500/10 border border-rose-500/20 px-2.5 py-1 text-rose-400 animate-pulse">Open (Cooldown)</span>}
                      {status === 'healthy' && <span className="text-xs rounded-full bg-emerald-500/10 border border-emerald-500/20 px-2.5 py-1 text-emerald-400">Healthy</span>}
                      <span className="text-xs font-mono text-zinc-400">{health.latency_ms ? `${health.latency_ms}ms` : '—'}</span>
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </div>

        {/* Diagnostics warnings */}
        <div className="glass-panel rounded-2xl border border-zinc-800/80 p-6 space-y-4">
          <div className="flex items-center justify-between border-b border-zinc-800 pb-3">
            <h3 className="font-bold text-sm tracking-wide uppercase text-zinc-300">System Doctor</h3>
            <button onClick={() => setCurrentTab('system')} className="text-xs text-indigo-400 hover:underline">Diagnostics</button>
          </div>
          {diagnostics.issues?.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-6 text-center space-y-2">
              <CheckCircle className="h-8 w-8 text-emerald-500" />
              <p className="text-sm font-medium text-zinc-300">All checks passed</p>
              <p className="text-xs text-zinc-500">TermRouter is running in optimal state.</p>
            </div>
          ) : (
            <div className="space-y-3">
              {diagnostics.issues?.slice(0, 3).map((issue: string, idx: number) => (
                <div key={idx} className="flex gap-2.5 text-xs text-amber-400 bg-amber-500/5 border border-amber-500/10 rounded-lg p-2.5">
                  <AlertCircle className="h-4 w-4 flex-shrink-0 mt-0.5" />
                  <span>{issue}</span>
                </div>
              ))}
              {diagnostics.issues?.length > 3 && (
                <p className="text-xs text-zinc-500 text-center font-medium">+{diagnostics.issues.length - 3} more issues. View diagnostics tab.</p>
              )}
            </div>
          )}
        </div>
      </div>

      {/* Recent activity */}
      <div className="glass-panel rounded-2xl border border-zinc-800/80 p-6 space-y-4">
        <div className="flex items-center justify-between border-b border-zinc-800 pb-3">
          <h3 className="font-bold text-sm tracking-wide uppercase text-zinc-300">Recent Request Stream</h3>
          <button onClick={() => setCurrentTab('activity')} className="text-xs text-indigo-400 hover:underline">All Activity</button>
        </div>
        {activities.length === 0 ? (
          <div className="text-center py-8 text-zinc-500 text-sm">No recent requests recorded.</div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-xs">
              <thead>
                <tr className="border-b border-zinc-800 text-zinc-400 font-semibold uppercase tracking-wider">
                  <th className="pb-3">Time</th>
                  <th className="pb-3">Public Name</th>
                  <th className="pb-3">Selected Provider</th>
                  <th className="pb-3">Selected Model</th>
                  <th className="pb-3">Status</th>
                  <th className="pb-3">Latency</th>
                  <th className="pb-3">Tokens</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-zinc-800/50">
                {activities.slice(0, 5).map((r: any) => (
                  <tr key={r.id} className="hover:bg-zinc-900/30">
                    <td className="py-3.5 text-zinc-400 font-mono">{new Date(r.timestamp).toLocaleTimeString()}</td>
                    <td className="py-3.5 font-semibold text-zinc-200">{r.alias || r.requested_model}</td>
                    <td className="py-3.5 text-zinc-300">{r.provider || '—'}</td>
                    <td className="py-3.5 text-zinc-300 font-mono">{r.upstream_model || '—'}</td>
                    <td className="py-3.5">
                      <span className={`px-2 py-0.5 rounded text-[10px] font-bold ${r.status_code >= 200 && r.status_code < 400 ? 'bg-emerald-500/10 text-emerald-400' : 'bg-rose-500/10 text-rose-400'}`}>
                        {r.status_code === 0 ? 'STREAMING' : r.status_code}
                      </span>
                    </td>
                    <td className="py-3.5 font-mono text-zinc-400">{r.latency_ms}ms</td>
                    <td className="py-3.5 font-mono text-zinc-500">
                      {r.input_tokens || 0} / {r.output_tokens || 0}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  );
}

function StatCard({ title, value, icon }: { title: string, value: any, icon: React.ReactNode }) {
  return (
    <div className="glass-panel rounded-2xl border border-zinc-800/80 p-5 flex items-center justify-between shadow-lg">
      <div className="space-y-1">
        <span className="text-xs font-semibold text-zinc-500 uppercase tracking-wider">{title}</span>
        <h4 className="text-2xl font-extrabold text-zinc-100">{value}</h4>
      </div>
      <div className="p-3 rounded-xl bg-zinc-900/80 border border-zinc-800/60 shadow-inner">
        {icon}
      </div>
    </div>
  );
}

// ----------------------------------------------------
// PROVIDERS TAB
// ----------------------------------------------------
function ProvidersTab({ config, providerHealth, discoveredModels, apiCall, fetchConfig, toastSuccess }: any) {
  const [showAddForm, setShowAddForm] = useState(false);
  const [name, setName] = useState('');
  const [type, setType] = useState('openai');
  const [baseURL, setBaseURL] = useState('https://api.openai.com/v1');
  const [credMethod, setCredMethod] = useState('key');
  const [apiKey, setApiKey] = useState('');
  const [envName, setEnvName] = useState('OPENAI_API_KEY');
  const [testingId, setTestingId] = useState<string | null>(null);

  const handleAdd = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return;

    try {
      const credential = credMethod === 'env' 
        ? { method: 'env', value: envName }
        : { method: 'vault', value: apiKey };
      const payload: any = {
        name: name.trim(),
        type,
        base_url: baseURL,
        credential,
        enabled: true
      };

      await apiCall(`${API_BASE}/providers`, 'POST', payload);
      toastSuccess(`Provider ${name} added successfully`);
      setName('');
      setApiKey('');
      setShowAddForm(false);
      fetchConfig();
    } catch (e) {}
  };

  const handleToggle = async (providerId: string, enabled: boolean) => {
    try {
      const payload = { enabled };
      await apiCall(`${API_BASE}/providers/${providerId}`, 'PATCH', payload);
      toastSuccess(`Provider ${providerId} ${enabled ? 'enabled' : 'disabled'}`);
      fetchConfig();
    } catch (e) {}
  };

  const handleDelete = async (providerId: string) => {
    if (!confirm(`Are you sure you want to remove provider "${providerId}"?`)) return;

    try {
      await apiCall(`${API_BASE}/providers/${providerId}`, 'DELETE');
      toastSuccess(`Provider ${providerId} removed`);
      fetchConfig();
    } catch (e) {}
  };

  const handleTest = async (providerId: string) => {
    setTestingId(providerId);
    try {
      const res = await apiCall(`${API_BASE}/providers/${providerId}/test`, 'POST');
      if (res.ok) {
        alert(`Provider test passed! Discovered models: ${res.models?.join(', ') || 'none'}`);
      } else {
        alert(`Test failed: ${res.error}`);
      }
    } catch (e) {} finally {
      setTestingId(null);
    }
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between border-b border-zinc-800 pb-4">
        <h3 className="text-lg font-bold">Manage Provider Connections</h3>
        <button 
          onClick={() => setShowAddForm(!showAddForm)}
          className="rounded-xl bg-indigo-600 hover:bg-indigo-500 px-4 py-2 text-sm font-semibold flex items-center gap-1.5 transition-colors"
        >
          {showAddForm ? 'Cancel' : <><Plus className="h-4 w-4" /> Add Provider</>}
        </button>
      </div>

      {showAddForm && (
        <form onSubmit={handleAdd} className="glass-panel rounded-2xl border border-zinc-800 p-6 space-y-4">
          <h4 className="font-bold text-sm text-zinc-300">Add New Connection</h4>
          <div className="grid grid-cols-3 gap-4">
            <div>
              <label className="block text-xs font-semibold text-zinc-400 mb-1">Provider ID (lowercase slug)</label>
              <input type="text" required value={name} onChange={(e) => setName(e.target.value.toLowerCase().replace(/[^a-z0-9_-]/g, ''))} placeholder="openai-custom" className="w-full bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-2 text-sm text-zinc-100" />
            </div>
            <div>
              <label className="block text-xs font-semibold text-zinc-400 mb-1">Provider Type</label>
              <select value={type} onChange={(e) => {
                setType(e.target.value);
                if (e.target.value === 'openai') setBaseURL('https://api.openai.com/v1');
                else if (e.target.value === 'anthropic') setBaseURL('https://api.anthropic.com');
                else setBaseURL('http://localhost:11434/v1');
              }} className="w-full bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-2 text-sm text-zinc-100">
                <option value="openai">OpenAI</option>
                <option value="anthropic">Anthropic</option>
                <option value="openai-compatible">OpenAI-Compatible (Ollama, local, vLLM)</option>
              </select>
            </div>
            <div>
              <label className="block text-xs font-semibold text-zinc-400 mb-1">Base URL</label>
              <input type="text" required value={baseURL} onChange={(e) => setBaseURL(e.target.value)} className="w-full bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-2 text-sm text-zinc-100 font-mono" />
            </div>
          </div>

          <div className="border-t border-zinc-800 pt-4 grid grid-cols-2 gap-4">
            <div>
              <label className="block text-xs font-semibold text-zinc-400 mb-2">Auth Method</label>
              <div className="flex gap-4 text-xs">
                <label className="flex items-center gap-2">
                  <input type="radio" name="wiz_cred" checked={credMethod === 'key'} onChange={() => setCredMethod('key')} />
                  Plaintext API Key (Stored securely)
                </label>
                <label className="flex items-center gap-2">
                  <input type="radio" name="wiz_cred" checked={credMethod === 'env'} onChange={() => setCredMethod('env')} />
                  Environment Variable name
                </label>
              </div>
            </div>

            {credMethod === 'key' ? (
              <div>
                <label className="block text-xs font-semibold text-zinc-400 mb-1">API Key</label>
                <input type="password" value={apiKey} onChange={(e) => setApiKey(e.target.value)} placeholder="sk-..." className="w-full bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-2 text-sm text-zinc-100 font-mono" />
              </div>
            ) : (
              <div>
                <label className="block text-xs font-semibold text-zinc-400 mb-1">Env Var Name</label>
                <input type="text" value={envName} onChange={(e) => setEnvName(e.target.value)} placeholder="OPENAI_API_KEY" className="w-full bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-2 text-sm text-zinc-100 font-mono" />
              </div>
            )}
          </div>

          <button type="submit" className="rounded-xl bg-indigo-600 hover:bg-indigo-500 px-4 py-2 text-sm font-semibold">Save Provider</button>
        </form>
      )}

      {/* Grid of Providers */}
      <div className="grid grid-cols-2 gap-6">
        {Object.entries(config.providers || {}).map(([pName, p]: any) => {
          const health = providerHealth[pName] || {};
          const isEnabled = p.enabled !== false;
          const status = !isEnabled ? 'disabled' : (health.circuit === 'open' ? 'open' : 'healthy');
          const myModels = discoveredModels?.filter((m: any) => m.provider_id === pName) || [];

          return (
            <div key={pName} className="glass-panel rounded-2xl border border-zinc-800 p-6 space-y-4 flex flex-col justify-between shadow-lg relative overflow-hidden">
              <div className="space-y-3">
                <div className="flex items-center justify-between">
                  <div>
                    <h4 className="font-bold text-base text-zinc-100">{pName}</h4>
                    <span className="text-[10px] font-mono text-zinc-400 uppercase bg-zinc-800 px-2 py-0.5 rounded">{p.type}</span>
                  </div>
                  <label className="relative inline-flex items-center cursor-pointer">
                    <input type="checkbox" checked={isEnabled} onChange={(e) => handleToggle(pName, e.target.checked)} className="sr-only peer" />
                    <div className="w-9 h-5 bg-zinc-800 rounded-full peer peer-focus:ring-2 peer-focus:ring-indigo-500/50 peer-checked:after:translate-x-full after:content-[''] after:absolute after:top-0.5 after:left-[2px] after:bg-zinc-400 after:border-zinc-300 after:border after:rounded-full after:h-4 after:w-4 after:transition-all peer-checked:bg-indigo-600 peer-checked:after:bg-white"></div>
                  </label>
                </div>

                <div className="space-y-1 text-xs">
                  <div className="flex justify-between"><span className="text-zinc-500">Endpoint:</span> <span className="font-mono text-zinc-300 max-w-[200px] truncate">{p.base_url}</span></div>
                  <div className="flex justify-between"><span className="text-zinc-500">Credential Ref:</span> <span className="font-mono text-zinc-300">{p.credential?.source || 'None'}</span></div>
                  <div className="flex justify-between">
                    <span className="text-zinc-500">Circuit State:</span> 
                    <span className={`font-semibold uppercase ${status === 'healthy' ? 'text-emerald-400' : (status === 'open' ? 'text-rose-400 animate-pulse' : 'text-zinc-500')}`}>{status}</span>
                  </div>
                  {health.last_error && (
                    <div className="bg-rose-500/5 border border-rose-500/10 text-[10px] text-rose-400 rounded p-2 overflow-y-auto max-h-12 font-mono">
                      {health.last_error}
                    </div>
                  )}
                </div>

                {myModels.length > 0 && (
                  <div className="pt-2">
                    <span className="text-[10px] text-zinc-500 uppercase font-semibold">Discovered Models ({myModels.length}):</span>
                    <div className="flex flex-wrap gap-1 mt-1 max-h-16 overflow-y-auto">
                      {myModels.map((m: any) => (
                        <span key={m.model_id} className="text-[9px] bg-zinc-900 text-zinc-400 border border-zinc-850 px-1.5 py-0.5 rounded font-mono">{m.model_id}</span>
                      ))}
                    </div>
                  </div>
                )}
              </div>

              <div className="flex items-center gap-2 border-t border-zinc-800/80 pt-4 mt-2">
                <button 
                  disabled={testingId === pName}
                  onClick={() => handleTest(pName)}
                  className="flex-1 bg-zinc-900 border border-zinc-800 hover:border-zinc-700 px-3 py-1.5 rounded-lg text-xs text-zinc-300 flex items-center justify-center gap-1 transition-colors"
                >
                  {testingId === pName ? <RefreshCw className="h-3 w-3 animate-spin" /> : <Play className="h-3 w-3" />}
                  Test
                </button>
                <button 
                  onClick={() => handleDelete(pName)}
                  className="bg-rose-500/10 hover:bg-rose-500/20 text-rose-400 border border-rose-500/20 px-3 py-1.5 rounded-lg text-xs flex items-center gap-1 transition-colors"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                  Remove
                </button>
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}

// ----------------------------------------------------
// MODEL PROFILES TAB
// ----------------------------------------------------
const CAP_LABELS = {
  general: 'General Capabilities',
  reasoning: 'Reasoning',
  analysis: 'Analysis',
  coding: 'Coding',
  writing: 'Writing',
  tool_use: 'Tool Use',
  instruction_following: 'Instruction Following',
  structured_output: 'Structured Output',
  long_context: 'Long Context Support',
  multilingual: 'Multilingual Support',
  mathematics: 'Mathematics',
  summarization: 'Summarization',
  extraction: 'Extraction'
};

const PRIV_LABELS = {
  local: 'Local Execution (100% Private)',
  'private-cloud': 'Private Cloud (GDPR/Compliance)',
  cloud: 'Public Cloud (Shared API)'
};

function ProfilesTab({ config, apiCall, fetchConfig, toastSuccess }: any) {
  const [selectedModel, setSelectedModel] = useState<string | null>(null);
  
  // Custom Profile Editor values
  const [capabilities, setCapabilities] = useState<any>({});
  const [vision, setVision] = useState(false);
  const [tools, setTools] = useState(false);
  const [parallelTools, setParallelTools] = useState(false);
  const [structuredOutput, setStructuredOutput] = useState(false);
  const [streaming, setStreaming] = useState(true);
  const [contextWindow, setContextWindow] = useState(128000);
  const [maxOutputTokens, setMaxOutputTokens] = useState(4096);
  const [costTier, setCostTier] = useState(1);
  const [latencyTier, setLatencyTier] = useState(1);
  const [privacy, setPrivacy] = useState('cloud');

  // Self-assessment state
  const [assessView, setAssessView] = useState<'none' | 'setup' | 'running' | 'review'>('none');
  const [assessDepth, setAssessDepth] = useState('standard');
  const [assessCategories, setAssessCategories] = useState<string[]>([]);
  const [assessEstimate, setAssessEstimate] = useState<any>(null);
  const [assessId, setAssessId] = useState<string | null>(null);
  const [assessStatus, setAssessStatus] = useState<any>(null);
  const [assessProposal, setAssessProposal] = useState<any>(null);
  const [assessLoading, setAssessLoading] = useState(false);

  // Load properties when selectedModel changes
  useEffect(() => {
    if (!selectedModel) return;
    const mp = config.model_profiles?.[selectedModel] || {};
    const defaultCaps: any = {};
    Object.keys(CAP_LABELS).forEach(k => {
      defaultCaps[k] = mp.capabilities?.[k] || 0;
    });
    setCapabilities(defaultCaps);
    setVision(mp.properties?.vision || false);
    setTools(mp.properties?.tools || false);
    setParallelTools(mp.properties?.parallel_tools || false);
    setStructuredOutput(mp.properties?.structured_output || false);
    setStreaming(mp.properties?.streaming !== false);
    setContextWindow(mp.properties?.context_window || 128000);
    setMaxOutputTokens(mp.properties?.max_output_tokens || 4096);
    setCostTier(mp.properties?.cost_tier || 1);
    setLatencyTier(mp.properties?.latency_tier || 1);
    setPrivacy(mp.properties?.privacy || 'cloud');
  }, [selectedModel, config]);

  const handleSaveProfile = async () => {
    if (!selectedModel) return;
    try {
      const payload = {
        capabilities,
        properties: {
          vision,
          tools,
          parallel_tools: parallelTools,
          structured_output: structuredOutput,
          streaming,
          context_window: Number(contextWindow),
          max_output_tokens: Number(maxOutputTokens),
          cost_tier: Number(costTier),
          latency_tier: Number(latencyTier),
          privacy
        }
      };
      await apiCall(`${API_BASE}/model-profiles/${encodeURIComponent(selectedModel)}`, 'PUT', payload);
      toastSuccess(`Capability profile for ${selectedModel} saved`);
      fetchConfig();
    } catch (e) {}
  };

  const handleResetProfile = async () => {
    if (!selectedModel || !confirm('Reset profile to built-in catalog default?')) return;
    try {
      await apiCall(`${API_BASE}/model-profiles/${encodeURIComponent(selectedModel)}/overrides`, 'DELETE');
      toastSuccess(`Profile reset to defaults`);
      fetchConfig();
    } catch (e) {}
  };

  // Self-assessment handlers
  const handlePreflightEstimate = async () => {
    if (!selectedModel) return;
    setAssessLoading(true);
    try {
      const est = await apiCall(`${API_BASE}/model-profiles/${encodeURIComponent(selectedModel)}/assessment/estimate`, 'POST', {
        depth: assessDepth, categories: assessCategories.length > 0 ? assessCategories : undefined
      });
      setAssessEstimate(est);
    } catch (e) {}
    setAssessLoading(false);
  };

  const handleStartAssessment = async () => {
    if (!selectedModel) return;
    setAssessLoading(true);
    try {
      const res = await apiCall(`${API_BASE}/model-profiles/${encodeURIComponent(selectedModel)}/assessments`, 'POST', {
        depth: assessDepth, categories: assessCategories.length > 0 ? assessCategories : undefined
      });
      if (!res || !res.assessment_id) {
        console.error('Assessment POST response missing assessment_id, body:', res, 'URL:', `${API_BASE}/model-profiles/${encodeURIComponent(selectedModel)}/assessments`);
        setAssessView('none');
        return;
      }
      setAssessId(res.assessment_id);
      setAssessView('running');
      setAssessStatus(res);
      toastSuccess(`Assessment ${res.assessment_id} started`);
      
      // Poll for completion
      const poll = setInterval(async () => {
        try {
          const status = await apiCall(`${API_BASE}/model-assessments/${res.assessment_id}`);
          setAssessStatus(status);
          if (status.status === 'completed' || status.status === 'failed' || status.status === 'cancelled') {
            clearInterval(poll);
            if (status.status === 'completed') {
              const prop = await apiCall(`${API_BASE}/model-assessments/${res.assessment_id}/proposal`);
              setAssessProposal(prop);
              setAssessView('review');
            }
          }
        } catch (e) { clearInterval(poll); }
      }, 2000);
    } catch (e) {}
    setAssessLoading(false);
  };

  const handleStartAssessSetup = async () => {
    setAssessDepth('standard');
    setAssessCategories([]);
    setAssessEstimate(null);
    setAssessView('setup');
    // Get estimate
    setTimeout(handlePreflightEstimate, 100);
  };

  const handleApplyProposal = async (acceptedFields?: string[]) => {
    if (!assessId) return;
    setAssessLoading(true);
    try {
      await apiCall(`${API_BASE}/model-assessments/${assessId}/apply`, 'POST', {
        accepted_fields: acceptedFields || [],
        preserve_user_overrides: true
      });
      toastSuccess('Assessment proposal applied');
      setAssessView('none');
      setAssessProposal(null);
      setAssessId(null);
      fetchConfig();
    } catch (e) {}
    setAssessLoading(false);
  };

  const handleCancelAssessment = async () => {
    if (!assessId) return;
    try {
      await apiCall(`${API_BASE}/model-assessments/${assessId}/cancel`, 'POST', {});
      toastSuccess('Assessment cancelled');
      setAssessView('none');
    } catch (e) {}
  };

  const handleDismissAssessment = () => {
    setAssessView('none');
    setAssessProposal(null);
    setAssessId(null);
    setAssessStatus(null);
    setAssessEstimate(null);
  };

  // Compile full catalog of configured models (provider/model format)
  const configuredModels = new Set<string>();
  Object.values(config.routes || {}).forEach((r: any) => {
    (r.targets || []).forEach((t: any) => {
      if (t.provider && t.model) configuredModels.add(`${t.provider}/${t.model}`);
    });
    (r.candidates || []).forEach((t: any) => {
      if (t.provider && t.model) configuredModels.add(`${t.provider}/${t.model}`);
    });
  });
  Object.values(config.aliases || {}).forEach((a: any) => {
    if (a.provider && a.model) configuredModels.add(`${a.provider}/${a.model}`);
  });
  
  const allModels = Array.from(configuredModels).sort();

  return (
    <div className="grid grid-cols-3 gap-8">
      {/* Left panel: List models */}
      <div className="glass-panel rounded-2xl border border-zinc-800 p-5 space-y-4">
        <h3 className="font-bold text-sm tracking-wide uppercase text-zinc-300 border-b border-zinc-800 pb-2">Model Directory</h3>
        <div className="divide-y divide-zinc-855 max-h-[600px] overflow-y-auto space-y-1 pr-1">
          {allModels.length === 0 ? (
            <div className="text-zinc-500 text-xs py-4 text-center">No models detected. Try adding a provider and running a test.</div>
          ) : (
            allModels.map((m: string) => {
              const hasOverride = !!config.model_profiles?.[m];
              const isSelected = selectedModel === m;
              const src = config.model_profiles?.[m]?.source || '';
              const statusBadge = src === 'self-assessment' ? 'ASSESSED'
                : src === 'user' ? 'MODIFIED'
                : hasOverride ? 'PROFILED' : '';
              const badgeColor = src === 'self-assessment' ? 'bg-indigo-950 text-indigo-400 border-indigo-900'
                : src === 'user' ? 'bg-amber-950 text-amber-400 border-amber-900'
                : 'bg-emerald-950 text-emerald-400 border-emerald-900';
              return (
                <button
                  key={m}
                  onClick={() => setSelectedModel(m)}
                  className={`w-full text-left p-3 rounded-lg flex items-center justify-between text-xs transition-colors ${isSelected ? 'bg-indigo-600/10 border border-indigo-600/30 text-indigo-200' : 'hover:bg-zinc-900/60 text-zinc-300'}`}
                >
                  <span className="font-mono truncate">{m}</span>
                  <div className="flex items-center gap-1.5 flex-shrink-0">
                    {statusBadge && <span className={`text-[8px] font-bold px-1 py-0.5 rounded border ${badgeColor}`}>{statusBadge}</span>}
                    <ChevronRight className="h-3.5 w-3.5" />
                  </div>
                </button>
              );
            })
          )}
        </div>
      </div>

      {/* Right panel: Profile details & editor */}
      <div className="col-span-2 space-y-6">
        {!selectedModel ? (
          <div className="glass-panel rounded-2xl border border-zinc-800 p-8 text-center flex flex-col items-center justify-center min-h-[300px]">
            <Cpu className="h-10 w-10 text-zinc-600 animate-float mb-4" />
            <h4 className="font-bold text-zinc-300">Select a Model</h4>
            <p className="text-xs text-zinc-500 mt-1 max-w-sm">Select a model from the directory list on the left to view or customize its capabilities for smart routing.</p>
          </div>
        ) : (
          <div className="glass-panel rounded-2xl border border-zinc-800 p-6 space-y-6">
            <div className="flex items-center justify-between border-b border-zinc-800 pb-4">
              <div>
                <h3 className="font-bold text-base text-zinc-100 font-mono">{selectedModel}</h3>
                <p className="text-xs text-zinc-500 mt-1">Specify model scores (0-5) to inform the smart route task classifier.</p>
              </div>
              <div className="flex items-center gap-2">
                <button onClick={handleStartAssessSetup} className="rounded-lg border border-indigo-600/40 hover:bg-indigo-600/10 px-3 py-1.5 text-xs text-indigo-400 flex items-center gap-1.5">
                  <Play className="h-3 w-3" /> Assess Model
                </button>
                <button onClick={handleResetProfile} className="rounded-lg border border-zinc-800 hover:bg-zinc-900 px-3 py-1.5 text-xs text-zinc-400 hover:text-zinc-300">Reset Defaults</button>
                <button onClick={handleSaveProfile} className="rounded-lg bg-indigo-600 hover:bg-indigo-500 px-3 py-1.5 text-xs font-semibold">Save Profile</button>
              </div>
            </div>
            
            {/* Assessment modals */}
            {assessView === 'setup' && selectedModel && (
              <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
                <div className="bg-zinc-900 border border-zinc-800 rounded-2xl p-6 max-w-lg w-full mx-4 space-y-5 shadow-2xl">
                  <div className="flex items-center justify-between border-b border-zinc-800 pb-3">
                    <h4 className="font-bold text-sm text-zinc-100">Self-Assessment Setup</h4>
                    <button onClick={() => setAssessView('none')} className="text-zinc-500 hover:text-zinc-300 text-lg">&times;</button>
                  </div>
                  
                  <div>
                    <label className="block text-xs font-semibold text-zinc-400 mb-2">Assessment Depth</label>
                    <div className="grid grid-cols-3 gap-2">
                      {['quick', 'standard', 'comprehensive'].map(d => (
                        <button key={d} onClick={() => { setAssessDepth(d); setTimeout(handlePreflightEstimate, 100); }}
                          className={`p-3 rounded-xl border text-xs text-left transition-all ${assessDepth === d ? 'border-indigo-500 bg-indigo-500/10' : 'border-zinc-800 hover:border-zinc-700'}`}>
                          <div className="font-bold text-zinc-200 capitalize mb-1">{d}</div>
                          <div className="text-[10px] text-zinc-500">
                            {d === 'quick' ? '1-3 min' : d === 'standard' ? '5-15 min' : '15-30 min'}
                          </div>
                        </button>
                      ))}
                    </div>
                  </div>

                  {assessEstimate && (
                    <div className="bg-zinc-800/60 rounded-xl p-3 space-y-1.5 text-xs">
                      <div className="flex justify-between text-zinc-300">
                        <span>Estimated requests</span>
                        <span className="font-mono">{assessEstimate.request_count}</span>
                      </div>
                      <div className="flex justify-between text-zinc-300">
                        <span>Estimated tokens</span>
                        <span className="font-mono">{assessEstimate.estimated_tokens?.toLocaleString()}</span>
                      </div>
                      <div className="flex justify-between text-zinc-300">
                        <span>Estimated cost</span>
                        <span className="font-mono">{assessEstimate.cost_known ? `$${assessEstimate.estimated_cost?.toFixed(4)}` : 'Unknown'}</span>
                      </div>
                      <div className="flex justify-between text-zinc-300">
                        <span>Leaves local machine</span>
                        <span className={assessEstimate.leaves_local ? 'text-amber-400' : 'text-emerald-400'}>{assessEstimate.leaves_local ? 'Yes' : 'No'}</span>
                      </div>
                    </div>
                  )}

                  <div className="flex justify-end gap-2 border-t border-zinc-800 pt-3">
                    <button onClick={() => setAssessView('none')} className="rounded-lg border border-zinc-800 hover:bg-zinc-800 px-3 py-1.5 text-xs">Cancel</button>
                    <button onClick={handleStartAssessment} disabled={assessLoading}
                      className="rounded-lg bg-indigo-600 hover:bg-indigo-500 px-4 py-1.5 text-xs font-semibold disabled:opacity-50 flex items-center gap-1">
                      {assessLoading && <RefreshCw className="h-3 w-3 animate-spin" />}
                      Start Assessment
                    </button>
                  </div>
                </div>
              </div>
            )}

            {assessView === 'running' && assessStatus && (
              <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
                <div className="bg-zinc-900 border border-zinc-800 rounded-2xl p-6 max-w-md w-full mx-4 space-y-5 shadow-2xl">
                  <div className="flex items-center justify-between border-b border-zinc-800 pb-3">
                    <h4 className="font-bold text-sm text-zinc-100 flex items-center gap-2">
                      <RefreshCw className="h-4 w-4 animate-spin text-indigo-400" />
                      Assessing {selectedModel}
                    </h4>
                  </div>
                  
                  <div className="space-y-2">
                    <div className="flex justify-between text-xs text-zinc-400">
                      <span>Status</span>
                      <span className="font-semibold text-indigo-400 capitalize">{assessStatus.status}</span>
                    </div>
                    <div className="flex justify-between text-xs text-zinc-400">
                      <span>Assessment ID</span>
                      <span className="font-mono text-[10px]">{assessStatus.assessment_id}</span>
                    </div>
                    <div className="flex justify-between text-xs text-zinc-400">
                      <span>Depth</span>
                      <span className="capitalize">{assessStatus.depth}</span>
                    </div>
                  </div>

                  {assessStatus.categories?.length > 0 && (
                    <div className="space-y-1.5">
                      <h5 className="text-xs font-semibold text-zinc-400 uppercase tracking-wider">Categories</h5>
                      {assessStatus.categories.map((cat: any) => (
                        <div key={cat.name} className="flex items-center justify-between text-xs">
                          <span className="text-zinc-300 capitalize">{cat.name.replace(/_/g, ' ')}</span>
                          <span className={`flex items-center gap-1 ${
                            cat.status === 'completed' ? 'text-emerald-400' :
                            cat.status === 'running' ? 'text-indigo-400' :
                            cat.status === 'pending' ? 'text-zinc-500' : 'text-amber-400'
                          }`}>
                            {cat.status === 'completed' && <CheckCircle className="h-3 w-3" />}
                            {cat.status === 'running' && <RefreshCw className="h-3 w-3 animate-spin" />}
                            {cat.status}
                          </span>
                        </div>
                      ))}
                    </div>
                  )}

                  <div className="flex justify-end gap-2 border-t border-zinc-800 pt-3">
                    <button onClick={handleCancelAssessment} className="rounded-lg border border-rose-500/30 hover:bg-rose-500/10 px-3 py-1.5 text-xs text-rose-400">Cancel</button>
                    <button onClick={() => setAssessView('none')} className="rounded-lg border border-zinc-800 hover:bg-zinc-800 px-3 py-1.5 text-xs">Run in Background</button>
                  </div>
                </div>
              </div>
            )}

            {assessView === 'review' && assessProposal && (
              <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm overflow-y-auto py-8">
                <div className="bg-zinc-900 border border-zinc-800 rounded-2xl p-6 max-w-2xl w-full mx-4 space-y-5 shadow-2xl">
                  <div className="flex items-center justify-between border-b border-zinc-800 pb-3">
                    <h4 className="font-bold text-sm text-zinc-100">Assessment Proposal Review</h4>
                    <button onClick={handleDismissAssessment} className="text-zinc-500 hover:text-zinc-300 text-lg">&times;</button>
                  </div>

                  <div className="bg-zinc-800/40 rounded-xl p-3 text-xs text-zinc-400 space-y-1">
                    <div className="flex justify-between"><span>Model</span><span className="font-mono text-zinc-200">{assessProposal.provider_id}/{assessProposal.model_id}</span></div>
                    <div className="flex justify-between"><span>Benchmark</span><span className="font-mono text-zinc-200">{assessProposal.benchmark_version}</span></div>
                    <div className="flex justify-between"><span>Overall Confidence</span><span className="font-semibold text-indigo-400">{(assessProposal.overall_confidence * 100).toFixed(0)}%</span></div>
                  </div>

                  {assessProposal.differences?.length > 0 ? (
                    <div className="space-y-2">
                      <h5 className="text-xs font-semibold text-zinc-400 uppercase tracking-wider">Proposed Changes</h5>
                      {assessProposal.differences.map((diff: any) => (
                        <div key={diff.field} className="flex items-center justify-between bg-zinc-800/30 rounded-lg p-3 text-xs">
                          <div className="flex-1">
                            <span className="font-semibold text-zinc-200 capitalize">{diff.field.replace(/_/g, ' ')}</span>
                            <div className="text-[10px] text-zinc-500 mt-0.5">
                              Current: <span className="text-zinc-400">{diff.current_value || 0}/5</span>
                              {' → '}
                              Proposed: <span className="text-indigo-400 font-semibold">{diff.proposed_value}/5</span>
                            </div>
                          </div>
                          <div className="flex items-center gap-2">
                            <span className={`text-[10px] px-1.5 py-0.5 rounded ${
                              diff.confidence >= 0.7 ? 'bg-emerald-950 text-emerald-400' :
                              diff.confidence >= 0.4 ? 'bg-amber-950 text-amber-400' :
                              'bg-zinc-800 text-zinc-400'
                            }`}>
                              {(diff.confidence * 100).toFixed(0)}%
                            </span>
                            {diff.recommended && <CheckCircle className="h-3.5 w-3.5 text-emerald-500" />}
                          </div>
                        </div>
                      ))}
                    </div>
                  ) : (
                    <div className="text-center py-4 text-zinc-500 text-xs">No changes proposed (current profile matches assessment)</div>
                  )}

                  {assessProposal.category_results?.length > 0 && (
                    <div className="space-y-1.5">
                      <h5 className="text-xs font-semibold text-zinc-400 uppercase tracking-wider">Category Scores</h5>
                      <div className="grid grid-cols-2 gap-2">
                        {assessProposal.category_results.map((cat: any) => (
                          <div key={cat.name} className="flex justify-between bg-zinc-800/20 rounded-lg p-2 text-xs">
                            <span className="text-zinc-300 capitalize truncate mr-2">{cat.name.replace(/_/g, ' ')}</span>
                            <span className="font-bold text-indigo-400">{cat.score}/5</span>
                          </div>
                        ))}
                      </div>
                    </div>
                  )}

                  <div className="flex justify-end gap-2 border-t border-zinc-800 pt-3 flex-wrap">
                    <button onClick={handleDismissAssessment} className="rounded-lg border border-zinc-800 hover:bg-zinc-800 px-3 py-1.5 text-xs">Discard</button>
                    <button onClick={() => handleApplyProposal([])} disabled={assessLoading}
                      className="rounded-lg bg-indigo-600 hover:bg-indigo-500 px-4 py-1.5 text-xs font-semibold disabled:opacity-50 flex items-center gap-1">
                      {assessLoading && <RefreshCw className="h-3 w-3 animate-spin" />}
                      Apply Proposal
                    </button>
                  </div>
                </div>
              </div>
            )}

            {/* Disclaimer */}
            <div className="bg-zinc-900/60 border border-zinc-800/80 rounded-xl p-3 text-[10px] text-zinc-400 flex items-start gap-2">
              <Info className="h-4 w-4 text-indigo-400 flex-shrink-0" />
              <span>Capability profiles guide routing and are not objective universal rankings. Behavior depends on model version, provider, context, and tools.</span>
            </div>

            {/* Slider capabilities Grid */}
            <div>
              <h4 className="text-xs font-semibold text-zinc-400 uppercase tracking-wider mb-4 border-b border-zinc-850 pb-1">Capability Dimensions</h4>
              <div className="grid grid-cols-2 gap-x-6 gap-y-4">
                {Object.entries(CAP_LABELS).map(([key, label]) => (
                  <div key={key} className="space-y-1.5 text-xs">
                    <div className="flex justify-between">
                      <span className="text-zinc-300 font-medium">{label}</span>
                      <span className="font-bold text-indigo-400">
                        {capabilities[key] === 0 ? 'Unknown (0)' : `${capabilities[key]}/5`}
                      </span>
                    </div>
                    <input
                      type="range"
                      min="0"
                      max="5"
                      value={capabilities[key] || 0}
                      onChange={(e) => setCapabilities({ ...capabilities, [key]: Number(e.target.value) })}
                      className="w-full h-1.5 bg-zinc-800 rounded-lg appearance-none cursor-pointer accent-indigo-500"
                    />
                  </div>
                ))}
              </div>
            </div>

            {/* Hard Constraints & properties */}
            <div className="space-y-4">
              <h4 className="text-xs font-semibold text-zinc-400 uppercase tracking-wider border-b border-zinc-855 pb-1">Technical Properties</h4>
              
              <div className="grid grid-cols-4 gap-4">
                <label className="flex items-center gap-2 text-xs border border-zinc-855 bg-zinc-900/20 p-2.5 rounded-lg cursor-pointer">
                  <input type="checkbox" checked={vision} onChange={(e) => setVision(e.target.checked)} className="rounded bg-zinc-900 border-zinc-850" />
                  Vision Support
                </label>
                <label className="flex items-center gap-2 text-xs border border-zinc-855 bg-zinc-900/20 p-2.5 rounded-lg cursor-pointer">
                  <input type="checkbox" checked={tools} onChange={(e) => setTools(e.target.checked)} className="rounded bg-zinc-900 border-zinc-855" />
                  Tool Support
                </label>
                <label className="flex items-center gap-2 text-xs border border-zinc-855 bg-zinc-900/20 p-2.5 rounded-lg cursor-pointer">
                  <input type="checkbox" checked={parallelTools} onChange={(e) => setParallelTools(e.target.checked)} className="rounded bg-zinc-900 border-zinc-855" />
                  Parallel Tools
                </label>
                <label className="flex items-center gap-2 text-xs border border-zinc-855 bg-zinc-900/20 p-2.5 rounded-lg cursor-pointer">
                  <input type="checkbox" checked={structuredOutput} onChange={(e) => setStructuredOutput(e.target.checked)} className="rounded bg-zinc-900 border-zinc-855" />
                  Structured Output
                </label>
              </div>

              <div className="grid grid-cols-3 gap-4 text-xs">
                <div>
                  <label className="block text-zinc-400 mb-1">Context Window</label>
                  <input type="number" value={contextWindow} onChange={(e) => setContextWindow(Number(e.target.value))} className="w-full bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-1.5 font-mono" />
                </div>
                <div>
                  <label className="block text-zinc-400 mb-1">Max Output Tokens</label>
                  <input type="number" value={maxOutputTokens} onChange={(e) => setMaxOutputTokens(Number(e.target.value))} className="w-full bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-1.5 font-mono" />
                </div>
                <div>
                  <label className="block text-zinc-400 mb-1">Privacy Tier</label>
                  <select value={privacy} onChange={(e) => setPrivacy(e.target.value)} className="w-full bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-1.5">
                    {Object.entries(PRIV_LABELS).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
                  </select>
                </div>
              </div>

              <div className="grid grid-cols-2 gap-4 text-xs">
                <div>
                  <label className="block text-zinc-400 mb-1">Cost Tier (1-5)</label>
                  <select value={costTier} onChange={(e) => setCostTier(Number(e.target.value))} className="w-full bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-1.5">
                    <option value="1">1 - Free/Micro (Ollama, local)</option>
                    <option value="2">2 - Low Cost (gpt-4o-mini, haiku)</option>
                    <option value="3">3 - Medium Cost (gpt-4o, flash)</option>
                    <option value="4">4 - High Cost (pro, sonnet)</option>
                    <option value="5">5 - Premium/Enterprise (opus)</option>
                  </select>
                </div>
                <div>
                  <label className="block text-zinc-400 mb-1">Latency Tier (1-5)</label>
                  <select value={latencyTier} onChange={(e) => setLatencyTier(Number(e.target.value))} className="w-full bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-1.5">
                    <option value="1">1 - Realtime (local, tiny)</option>
                    <option value="2">2 - Fast (gpt-4o-mini, haiku)</option>
                    <option value="3">3 - Normal (gpt-4o, sonnet)</option>
                    <option value="4">4 - High (heavy reasoning models)</option>
                    <option value="5">5 - Slow (deep search / reasoning)</option>
                  </select>
                </div>
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

// ----------------------------------------------------
// UNIFIED ROUTES TAB
// ----------------------------------------------------
function RoutesTab({ config, discoveredModels, providerHealth, apiCall, fetchConfig, toastSuccess }: any) {
  const [wizardStep, setWizardStep] = useState(0);
  const [showUnassigned, setShowUnassigned] = useState(false);
  
  // Wizard state
  const [formName, setFormName] = useState('');
  const [formDescription, setFormDescription] = useState('');
  const [formMode, setFormMode] = useState('single');
  const [formAliases, setFormAliases] = useState('');
  
  // Single Model fields
  const [singleProvider, setSingleProvider] = useState('');
  const [singleModel, setSingleModel] = useState('');
  
  // Fallback fields
  const [fbTargets, setFbTargets] = useState<any[]>([{ provider: '', model: '' }]);
  
  // Smart fields
  const [smartCandidates, setSmartCandidates] = useState<any[]>([{ provider: '', model: '' }]);
  const [smartPolicy, setSmartPolicy] = useState('balanced');
  const [smartDefaultTarget, setSmartDefaultTarget] = useState('');
  const [smartConfidence, setSmartConfidence] = useState(0.7);
  const [smartSession, setSmartSession] = useState(false);
  const [smartSessionTTL, setSmartSessionTTL] = useState('60m');
  const [smartMode, setSmartMode] = useState('shadow');
  const [editingRoute, setEditingRoute] = useState<any | null>(null);
  const [candidateTests, setCandidateTests] = useState<Record<string, { loading: boolean; ok?: boolean; error?: string }>>({});

  const [shadowReports] = useState<any>({});

  const [filterMode, setFilterMode] = useState('all');

  useEffect(() => {
    const provs = Object.keys(config.providers || {});
    if (provs.length > 0) {
      if (!singleProvider) setSingleProvider(provs[0]);
      if (fbTargets.length === 1 && !fbTargets[0].provider) setFbTargets([{ provider: provs[0], model: '' }]);
      if (smartCandidates.length === 1 && !smartCandidates[0].provider) setSmartCandidates([{ provider: provs[0], model: '' }]);
    }
  }, [config]);

  const unifiedRoutes = buildUnifiedRoutes(config, providerHealth);
  const unusedShadowRef = shadowReports; void unusedShadowRef;
  const unassignedRoutes = Object.entries(config.routes || {}).filter(([rName]: any) => {
    return !Object.values(config.aliases || {}).some((a: any) => a.route === rName);
  });

  const filteredRoutes = filterMode === 'all' 
    ? unifiedRoutes 
    : unifiedRoutes.filter((r: any) => r.mode === filterMode);

  const resetForm = () => {
    setWizardStep(0);
    setFormName('');
    setFormDescription('');
    setFormMode('single');
    setFormAliases('');
    setSingleModel('');
    setSingleProvider(Object.keys(config.providers || {})[0] || '');
    setFbTargets([{ provider: Object.keys(config.providers || {})[0] || '', model: '' }]);
    setSmartCandidates([{ provider: Object.keys(config.providers || {})[0] || '', model: '' }]);
    setSmartPolicy('balanced');
    setSmartDefaultTarget('');
    setSmartConfidence(0.7);
    setSmartSession(false);
    setSmartSessionTTL('60m');
    setSmartMode('shadow');
    setEditingRoute(null);
    setCandidateTests({});
  };

  const handleDeleteRoute = async (route: any) => {
    const msg = route.aliases?.length > 0 
      ? `Delete route "${route.name}" and its ${route.aliases.length} additional alias(es)?`
      : `Delete route "${route.name}"?`;
    if (!confirm(msg)) return;

    try {
      // Delete aliases first, then the route
      const allNames = [route.name, ...(route.aliases || [])];
      for (const name of allNames) {
        if (config.aliases?.[name]) {
          await apiCall(`${API_BASE}/aliases/${name}`, 'DELETE').catch(() => {});
        }
      }
      if (route.internalId && config.routes?.[route.internalId]) {
        await apiCall(`${API_BASE}/routes/${route.internalId}`, 'DELETE').catch(() => {});
      }
      toastSuccess(`Route "${route.name}" deleted`);
      fetchConfig();
    } catch (e) {}
  };

  const handleToggleRoute = async (route: any, enabled: boolean) => {
    if (route.mode === 'smart') {
      try {
        await apiCall(`${API_BASE}/smart/routes/${route.internalId}/${enabled ? 'enable-shadow' : 'disable'}`, 'POST', {});
        toastSuccess(`Route "${route.name}" ${enabled ? 'enabled' : 'disabled'}`);
        fetchConfig();
      } catch (e) {}
    } else {
      // For non-smart routes, toggle via the alias enable state if possible
      toastSuccess(`Route state updated`);
    }
  };

  const handleTestCandidate = async (key: string, provider: string, model: string) => {
    if (!provider || !model) return;
    setCandidateTests(prev => ({ ...prev, [key]: { loading: true } }));
    try {
      const res = await apiCall(`${API_BASE}/providers/${encodeURIComponent(provider)}/test`, 'POST');
      const models: string[] = res.models || [];
      const found = models.some((m: string) => m === model || m.endsWith('/' + model));
      setCandidateTests(prev => ({ ...prev, [key]: { loading: false, ok: found, error: found ? undefined : `Model "${model}" not found in provider listing` } }));
    } catch (e: any) {
      setCandidateTests(prev => ({ ...prev, [key]: { loading: false, ok: false, error: e.message } }));
    }
  };

  const handleEditRoute = (route: any) => {
    setFormName(route.name);
    setFormMode(route.mode);
    setEditingRoute(route);

    if (route.mode === 'single') {
      setSingleProvider(route.single?.provider || '');
      setSingleModel(route.single?.model || '');
      setFbTargets([{ provider: '', model: '' }]);
      setSmartCandidates([{ provider: '', model: '' }]);
    } else if (route.mode === 'fallback') {
      setFbTargets(route.fallback?.targets?.length > 0
        ? route.fallback.targets.map((t: any) => ({ provider: t.provider, model: t.model }))
        : [{ provider: '', model: '' }]);
      setSmartCandidates([{ provider: '', model: '' }]);
    } else if (route.mode === 'smart') {
      setSmartCandidates(route.candidates?.length > 0
        ? route.candidates.map((c: any) => ({ provider: c.provider, model: c.model }))
        : [{ provider: '', model: '' }]);
      setSmartPolicy(route.smart?.policy || 'balanced');
      setSmartConfidence(route.smart?.confidence_threshold ?? 0.7);
      setSmartSession(route.smart?.session_affinity?.enabled || false);
      setSmartSessionTTL(route.smart?.session_affinity?.ttl || '60m');
      setSmartMode(route.smart?.mode || 'shadow');
      setSmartDefaultTarget(route.smart?.low_confidence_target || '');
    }

    setWizardStep(1);
  };

  // Create route wizard
  const handleCreateRoute = async () => {
    const safeName = formName.trim().toLowerCase().replace(/[^a-z0-9_-]/g, '');
    const extraAliases = formAliases.split(',').map(a => a.trim()).filter(Boolean);
    const isEdit = editingRoute !== null;

    try {
      if (formMode === 'single') {
        const payload = {
          name: safeName,
          provider: singleProvider,
          model: singleModel
        };
        if (isEdit) {
          await apiCall(`${API_BASE}/aliases/${safeName}`, 'PATCH', { provider: singleProvider, model: singleModel });
        } else {
          await apiCall(`${API_BASE}/aliases`, 'POST', payload);
          for (const alias of extraAliases) {
            if (alias !== safeName) {
              await apiCall(`${API_BASE}/aliases`, 'POST', { name: alias, provider: singleProvider, model: singleModel }).catch(() => {});
            }
          }
        }
        toastSuccess(`Single Model route "${formName}" ${isEdit ? 'updated' : 'created'}`);
      } else if (formMode === 'fallback') {
        const routePayload: any = {
          strategy: 'fallback',
          targets: fbTargets.filter(t => t.provider && t.model).map(t => ({ provider: t.provider, model: t.model }))
        };
        if (isEdit && editingRoute.internalId) {
          await apiCall(`${API_BASE}/routes/${editingRoute.internalId}`, 'PATCH', routePayload);
        } else {
          routePayload.name = safeName + '-route';
          const routeResult = await apiCall(`${API_BASE}/routes`, 'POST', routePayload);
          const routeId = routeResult.name || safeName + '-route';
          await apiCall(`${API_BASE}/aliases`, 'POST', { name: safeName, route: routeId });
          for (const alias of extraAliases) {
            if (alias !== safeName) {
              await apiCall(`${API_BASE}/aliases`, 'POST', { name: alias, route: routeId }).catch(() => {});
            }
          }
        }
        toastSuccess(`Fallback route "${formName}" ${isEdit ? 'updated' : 'created'}`);
      } else if (formMode === 'smart') {
        const routePayload: any = {
          strategy: 'smart',
          candidates: smartCandidates.filter(c => c.provider && c.model),
          smart: {
            mode: smartMode,
            policy: smartPolicy,
            confidence_threshold: Number(smartConfidence),
            session_affinity: smartSession ? { enabled: true, ttl: smartSessionTTL } : { enabled: false },
            low_confidence_target: smartDefaultTarget || undefined
          }
        };
        if (isEdit && editingRoute.internalId) {
          await apiCall(`${API_BASE}/routes/${editingRoute.internalId}`, 'PATCH', routePayload);
        } else {
          routePayload.name = safeName + '-smart';
          const routeResult = await apiCall(`${API_BASE}/routes`, 'POST', routePayload);
          const routeId = routeResult.name || safeName + '-smart';
          await apiCall(`${API_BASE}/aliases`, 'POST', { name: safeName, route: routeId });
          for (const alias of extraAliases) {
            if (alias !== safeName) {
              await apiCall(`${API_BASE}/aliases`, 'POST', { name: alias, route: routeId }).catch(() => {});
            }
          }
        }
        toastSuccess(`Smart route "${formName}" ${isEdit ? 'updated' : 'created'} in ${smartMode} mode`);
      }
      resetForm();
      fetchConfig();
    } catch (e) {}
  };

  // ------ MAIN RENDER ------
  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between border-b border-zinc-800 pb-4">
        <div>
          <h3 className="text-lg font-bold">Public Routes</h3>
          <p className="text-xs text-zinc-500 mt-1">Configure how clients reach models through public model names.</p>
        </div>
        <button 
          onClick={() => { resetForm(); setWizardStep(1); }}
          className="rounded-xl bg-indigo-600 hover:bg-indigo-500 px-4 py-2 text-sm font-semibold flex items-center gap-1.5 transition-colors"
        >
          <Plus className="h-4 w-4" /> Create Route
        </button>
      </div>

      {/* Create Route Wizard */}
      {wizardStep > 0 && (
        <div className="glass-panel rounded-2xl border border-zinc-800 p-6 space-y-6">
          <div className="flex items-center justify-between border-b border-zinc-800 pb-3">
            <h4 className="font-bold text-sm text-zinc-300">
              {editingRoute ? 'Edit Route' : 'Create Route'}
              {' · '}
              {wizardStep === 1 && 'Public Identity'}
              {wizardStep === 2 && 'Routing Behavior'}
              {wizardStep === 3 && `Configure ${formMode === 'single' ? 'Single Model' : formMode === 'fallback' ? 'Fallback' : 'Smart'} Settings`}
              {wizardStep === 4 && 'Review & Save'}
            </h4>
            <div className="text-xs text-zinc-500">Step {wizardStep} of 4</div>
          </div>

          {/* Progress */}
          <div className="w-full bg-zinc-800 h-1 rounded-full overflow-hidden">
            <div className="bg-indigo-600 h-full transition-all duration-300" style={{ width: `${(wizardStep / 4) * 100}%` }}></div>
          </div>

          {/* Step 1: Public Identity */}
          {wizardStep === 1 && (
            <div className="space-y-4 max-w-lg">
              <div>
                <label className="block text-xs font-semibold text-zinc-400 mb-1">Public model name</label>
                <input type="text" required value={formName} onChange={(e) => setFormName(e.target.value)} placeholder="e.g. coding" className="w-full bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-2 text-sm text-zinc-100 font-mono" />
                <p className="text-[10px] text-zinc-500 mt-1">Applications send this name in the <code className="font-mono text-zinc-400">model</code> field.</p>
              </div>
              <div>
                <label className="block text-xs font-semibold text-zinc-400 mb-1">Description — optional</label>
                <input type="text" value={formDescription} onChange={(e) => setFormDescription(e.target.value)} placeholder="Primary model used by coding agents" className="w-full bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-2 text-sm text-zinc-100" />
              </div>
              <div>
                <label className="block text-xs font-semibold text-zinc-400 mb-1">Additional aliases — optional</label>
                <input type="text" value={formAliases} onChange={(e) => setFormAliases(e.target.value)} placeholder="code, developer" className="w-full bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-2 text-sm text-zinc-100" />
                <p className="text-[10px] text-zinc-500 mt-1">Comma-separated. All names resolve to the same route.</p>
              </div>
            </div>
          )}

          {/* Step 2: Mode Selection */}
          {wizardStep === 2 && (
            <div>
              <p className="text-xs text-zinc-400 mb-4">How should TermRouter decide where requests go?</p>
              <div className="grid grid-cols-3 gap-4">
                <button 
                  onClick={() => setFormMode('single')}
                  className={`p-5 rounded-2xl border text-left transition-all ${formMode === 'single' ? 'border-indigo-500 bg-indigo-500/10 shadow-lg shadow-indigo-500/10' : 'border-zinc-800 bg-zinc-900/30 hover:border-zinc-700'}`}
                >
                  <h5 className="font-bold text-sm text-zinc-100 mb-2">Single Model</h5>
                  <p className="text-xs text-zinc-400 mb-3">Always send requests to one provider and model.</p>
                  <div className="text-[10px] text-zinc-500 space-y-1">
                    <div className="flex items-center gap-1"><CheckCircle className="h-3 w-3 text-emerald-500" /> Simple setup</div>
                    <div className="flex items-center gap-1"><CheckCircle className="h-3 w-3 text-emerald-500" /> Local models</div>
                    <div className="flex items-center gap-1"><CheckCircle className="h-3 w-3 text-emerald-500" /> Predictable behavior</div>
                  </div>
                </button>
                <button 
                  onClick={() => setFormMode('fallback')}
                  className={`p-5 rounded-2xl border text-left transition-all ${formMode === 'fallback' ? 'border-indigo-500 bg-indigo-500/10 shadow-lg shadow-indigo-500/10' : 'border-zinc-800 bg-zinc-900/30 hover:border-zinc-700'}`}
                >
                  <h5 className="font-bold text-sm text-zinc-100 mb-2">Fallback</h5>
                  <p className="text-xs text-zinc-400 mb-3">Try targets in priority order on eligible failures.</p>
                  <div className="text-[10px] text-zinc-500 space-y-1">
                    <div className="flex items-center gap-1"><CheckCircle className="h-3 w-3 text-emerald-500" /> Higher availability</div>
                    <div className="flex items-center gap-1"><CheckCircle className="h-3 w-3 text-emerald-500" /> Rate-limit recovery</div>
                    <div className="flex items-center gap-1"><CheckCircle className="h-3 w-3 text-emerald-500" /> Primary + backup</div>
                  </div>
                </button>
                <button 
                  onClick={() => setFormMode('smart')}
                  className={`p-5 rounded-2xl border text-left transition-all ${formMode === 'smart' ? 'border-indigo-500 bg-indigo-500/10 shadow-lg shadow-indigo-500/10' : 'border-zinc-800 bg-zinc-900/30 hover:border-zinc-700'}`}
                >
                  <h5 className="font-bold text-sm text-zinc-100 mb-2">Smart</h5>
                  <p className="text-xs text-zinc-400 mb-3">Analyze requests and select the best candidate.</p>
                  <div className="text-[10px] text-zinc-500 space-y-1">
                    <div className="flex items-center gap-1"><CheckCircle className="h-3 w-3 text-emerald-500" /> Mixed workloads</div>
                    <div className="flex items-center gap-1"><CheckCircle className="h-3 w-3 text-emerald-500" /> Cost-quality balance</div>
                    <div className="flex items-center gap-1"><CheckCircle className="h-3 w-3 text-emerald-500" /> Task specialization</div>
                  </div>
                </button>
              </div>
            </div>
          )}

          {/* Step 3: Mode-specific config */}
          {wizardStep === 3 && formMode === 'single' && (
            <div className="space-y-4 max-w-lg">
              <div>
                <label className="block text-xs font-semibold text-zinc-400 mb-1">Provider</label>
                <select value={singleProvider} onChange={(e) => setSingleProvider(e.target.value)} className="w-full bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-2 text-sm text-zinc-100">
                  {Object.keys(config.providers || {}).map(k => <option key={k} value={k}>{k}</option>)}
                </select>
              </div>
              <div>
                <label className="block text-xs font-semibold text-zinc-400 mb-1">Model</label>
                <div className="flex gap-2">
                  <input type="text" required value={singleModel} onChange={(e) => setSingleModel(e.target.value)} placeholder="gpt-4o-mini" list="route-single-models" className="flex-1 bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-2 text-sm text-zinc-100 font-mono" />
                  <button type="button" onClick={() => handleTestCandidate('single', singleProvider, singleModel)}
                    disabled={!singleProvider || !singleModel || candidateTests['single']?.loading}
                    className={`p-2 rounded-lg transition-colors border ${
                      candidateTests['single']?.loading ? 'bg-zinc-800 text-zinc-500 border-zinc-700 animate-pulse' :
                      candidateTests['single']?.ok === true ? 'bg-emerald-500/10 text-emerald-400 border-emerald-500/20' :
                      candidateTests['single']?.ok === false ? 'bg-rose-500/10 text-rose-400 border-rose-500/20' :
                      'bg-zinc-800/50 text-zinc-500 border-zinc-800 hover:bg-zinc-800'
                    }`}
                    title={candidateTests['single']?.error || (candidateTests['single']?.ok ? 'Model reachable' : 'Test model')}>
                    {candidateTests['single']?.loading ? <RefreshCw className="h-4 w-4 animate-spin" /> :
                     candidateTests['single']?.ok === true ? <CheckCircle className="h-4 w-4" /> :
                     candidateTests['single']?.ok === false ? <XCircle className="h-4 w-4" /> :
                     <Play className="h-4 w-4" />}
                  </button>
                </div>
                <datalist id="route-single-models">
                  {availableModels(discoveredModels, singleProvider, config).map((m: string) => (
                    <option key={m} value={m} />
                  ))}
                </datalist>
              </div>
              <div className="bg-indigo-500/5 border border-indigo-500/10 rounded-xl p-3 text-xs text-indigo-300">
                Requests for <code className="font-mono font-bold">{formName || '...'}</code> always go to <code className="font-mono font-bold">{singleProvider || '...'} / {singleModel || '...'}</code>.
              </div>
            </div>
          )}

          {wizardStep === 3 && formMode === 'fallback' && (
            <div className="space-y-4 max-w-2xl">
              <div className="flex items-center justify-between">
                <span className="text-xs font-bold text-zinc-400 uppercase tracking-wider">Ordered Targets</span>
                <button type="button" onClick={() => setFbTargets([...fbTargets, { provider: Object.keys(config.providers || {})[0] || '', model: '' }])} className="text-xs text-indigo-400 flex items-center gap-1 hover:underline">
                  <Plus className="h-3.5 w-3.5" /> Add Target
                </button>
              </div>
              {fbTargets.map((t, idx) => (
                <div key={idx} className="flex gap-3 items-center">
                  <span className="font-mono text-xs text-zinc-500 w-5">{idx + 1}.</span>
                  <select value={t.provider} onChange={(e) => {
                    const copy = [...fbTargets]; copy[idx].provider = e.target.value; setFbTargets(copy);
                  }} className="bg-zinc-950 border border-zinc-800 rounded-lg px-3 py-1.5 text-xs text-zinc-100 flex-1">
                    {Object.keys(config.providers || {}).map(k => <option key={k} value={k}>{k}</option>)}
                  </select>
                  <input type="text" required placeholder="Model ID" value={t.model} onChange={(e) => {
                    const copy = [...fbTargets]; copy[idx].model = e.target.value; setFbTargets(copy);
                  }} list={`fb-models-${idx}`} className="bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-1.5 text-xs text-zinc-100 flex-1" />
                  <datalist id={`fb-models-${idx}`}>
                    {availableModels(discoveredModels, t.provider, config).map((m: string) => (
                      <option key={m} value={m} />
                    ))}
                  </datalist>
                  <button type="button" onClick={() => handleTestCandidate(`fb-${idx}`, t.provider, t.model)}
                    disabled={!t.provider || !t.model || candidateTests[`fb-${idx}`]?.loading}
                    className={`p-1.5 rounded-lg transition-colors border text-xs flex items-center gap-1 ${
                      candidateTests[`fb-${idx}`]?.loading ? 'bg-zinc-800 text-zinc-500 border-zinc-700 animate-pulse' :
                      candidateTests[`fb-${idx}`]?.ok === true ? 'bg-emerald-500/10 text-emerald-400 border-emerald-500/20' :
                      candidateTests[`fb-${idx}`]?.ok === false ? 'bg-rose-500/10 text-rose-400 border-rose-500/20' :
                      'bg-zinc-800/50 text-zinc-500 border-zinc-800 hover:bg-zinc-800'
                    }`}
                    title={candidateTests[`fb-${idx}`]?.error || (candidateTests[`fb-${idx}`]?.ok ? 'Model reachable' : 'Test candidate')}>
                    {candidateTests[`fb-${idx}`]?.loading ? <RefreshCw className="h-3.5 w-3.5 animate-spin" /> :
                     candidateTests[`fb-${idx}`]?.ok === true ? <CheckCircle className="h-3.5 w-3.5" /> :
                     candidateTests[`fb-${idx}`]?.ok === false ? <XCircle className="h-3.5 w-3.5" /> :
                     <Play className="h-3.5 w-3.5" />}
                  </button>
                  {fbTargets.length > 1 && (
                    <button type="button" onClick={() => setFbTargets(fbTargets.filter((_, i) => i !== idx))} className="text-rose-400 hover:text-rose-200 p-2">
                      <Trash2 className="h-4 w-4" />
                    </button>
                  )}
                </div>
              ))}
            </div>
          )}

          {wizardStep === 3 && formMode === 'smart' && (
            <div className="space-y-6 max-w-2xl">
              <div className="space-y-3">
                <div className="flex items-center justify-between">
                  <span className="text-xs font-bold text-zinc-400 uppercase tracking-wider">Candidate Pool</span>
                  <button type="button" onClick={() => setSmartCandidates([...smartCandidates, { provider: Object.keys(config.providers || {})[0] || '', model: '' }])} className="text-xs text-indigo-400 flex items-center gap-1 hover:underline">
                    <Plus className="h-3.5 w-3.5" /> Add Candidate
                  </button>
                </div>
                {smartCandidates.map((c, idx) => (
                  <div key={idx} className="flex gap-3 items-center">
                    <select value={c.provider} onChange={(e) => {
                      const copy = [...smartCandidates]; copy[idx].provider = e.target.value; setSmartCandidates(copy);
                    }} className="bg-zinc-950 border border-zinc-800 rounded-lg px-3 py-1.5 text-xs text-zinc-100 flex-1">
                      {Object.keys(config.providers || {}).map(k => <option key={k} value={k}>{k}</option>)}
                    </select>
                    <input type="text" required placeholder="Model ID" value={c.model} onChange={(e) => {
                      const copy = [...smartCandidates]; copy[idx].model = e.target.value; setSmartCandidates(copy);
                    }} list={`sc-models-${idx}`} className="bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-1.5 text-xs text-zinc-100 flex-1" />
                    <datalist id={`sc-models-${idx}`}>
                      {availableModels(discoveredModels, c.provider, config).map((m: string) => (
                        <option key={m} value={m} />
                      ))}
                    </datalist>
                    <button type="button" onClick={() => handleTestCandidate(`sc-${idx}`, c.provider, c.model)}
                      disabled={!c.provider || !c.model || candidateTests[`sc-${idx}`]?.loading}
                      className={`p-1.5 rounded-lg transition-colors border text-xs flex items-center gap-1 ${
                        candidateTests[`sc-${idx}`]?.loading ? 'bg-zinc-800 text-zinc-500 border-zinc-700 animate-pulse' :
                        candidateTests[`sc-${idx}`]?.ok === true ? 'bg-emerald-500/10 text-emerald-400 border-emerald-500/20' :
                        candidateTests[`sc-${idx}`]?.ok === false ? 'bg-rose-500/10 text-rose-400 border-rose-500/20' :
                        'bg-zinc-800/50 text-zinc-500 border-zinc-800 hover:bg-zinc-800'
                      }`}
                      title={candidateTests[`sc-${idx}`]?.error || (candidateTests[`sc-${idx}`]?.ok ? 'Model reachable' : 'Test candidate')}>
                      {candidateTests[idx]?.loading ? <RefreshCw className="h-3.5 w-3.5 animate-spin" /> :
                       candidateTests[idx]?.ok === true ? <CheckCircle className="h-3.5 w-3.5" /> :
                       candidateTests[idx]?.ok === false ? <XCircle className="h-3.5 w-3.5" /> :
                       <Play className="h-3.5 w-3.5" />}
                    </button>
                    {smartCandidates.length > 1 && (
                      <button type="button" onClick={() => setSmartCandidates(smartCandidates.filter((_, i) => i !== idx))} className="text-rose-400 hover:text-rose-200 p-2">
                        <Trash2 className="h-4 w-4" />
                      </button>
                    )}
                  </div>
                ))}
              </div>

              <div className="grid grid-cols-2 gap-4 text-xs">
                <div>
                  <label className="block text-zinc-400 mb-1">Policy Preset</label>
                  <select value={smartPolicy} onChange={(e) => setSmartPolicy(e.target.value)} className="w-full bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-1.5 text-zinc-100">
                    <option value="balanced">Balanced</option>
                    <option value="quality">Quality-oriented</option>
                    <option value="economy">Economy-oriented</option>
                    <option value="fast">Latency-oriented</option>
                    <option value="private">Privacy-oriented</option>
                  </select>
                </div>
                <div>
                  <label className="block text-zinc-400 mb-1">Default target (fallback)</label>
                  <input type="text" value={smartDefaultTarget} onChange={(e) => setSmartDefaultTarget(e.target.value)} placeholder="provider:model" className="w-full bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-1.5 text-zinc-100 font-mono" />
                </div>
              </div>

              <div className="grid grid-cols-2 gap-4 text-xs">
                <div>
                  <label className="block text-zinc-400 mb-1">Operating Mode</label>
                  <div className="flex gap-2">
                    <button onClick={() => setSmartMode('shadow')} className={`flex-1 px-3 py-2 rounded-lg border text-xs font-semibold transition-colors ${smartMode === 'shadow' ? 'bg-amber-600 text-white border-amber-500' : 'bg-zinc-900 text-zinc-400 border-zinc-800'}`}>Shadow</button>
                    <button onClick={() => setSmartMode('live')} className={`flex-1 px-3 py-2 rounded-lg border text-xs font-semibold transition-colors ${smartMode === 'live' ? 'bg-emerald-600 text-white border-emerald-500' : 'bg-zinc-900 text-zinc-400 border-zinc-800'}`}>Live</button>
                  </div>
                </div>
                <div>
                  <label className="block text-zinc-400 mb-1">Confidence: <span className="text-indigo-400 font-bold">{smartConfidence}</span></label>
                  <input type="range" min="0.1" max="1.0" step="0.05" value={smartConfidence} onChange={(e) => setSmartConfidence(Number(e.target.value))} className="w-full bg-zinc-800 rounded-lg accent-indigo-500" />
                </div>
              </div>

              <label className="flex items-center gap-2 text-xs border border-zinc-800 bg-zinc-900/20 p-3 rounded-lg cursor-pointer">
                <input type="checkbox" checked={smartSession} onChange={(e) => setSmartSession(e.target.checked)} className="rounded bg-zinc-900 border-zinc-800" />
                <span>Enable session affinity <span className="text-zinc-500">(stick to same model per session)</span></span>
              </label>
              {smartSession && (
                <div className="max-w-xs">
                  <label className="block text-xs text-zinc-400 mb-1">Session TTL</label>
                  <input type="text" value={smartSessionTTL} onChange={(e) => setSmartSessionTTL(e.target.value)} placeholder="60m" className="w-full bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-1.5 text-sm text-zinc-100 font-mono" />
                </div>
              )}
            </div>
          )}

          {/* Step 4: Review */}
          {wizardStep === 4 && (
            <div className="space-y-4 max-w-lg">
              <div className="bg-zinc-900/40 border border-zinc-800 rounded-xl p-4 space-y-3 text-sm">
                <div className="flex justify-between"><span className="text-zinc-400">Public name:</span> <span className="font-mono font-bold text-zinc-100">{formName || '...'}</span></div>
                {formAliases && <div className="flex justify-between"><span className="text-zinc-400">Additional aliases:</span> <span className="font-mono text-zinc-300">{formAliases}</span></div>}
                <div className="flex justify-between"><span className="text-zinc-400">Mode:</span> <span className="font-bold text-indigo-400 capitalize">{formMode}</span></div>
                
                {formMode === 'single' && (
                  <div className="flex justify-between"><span className="text-zinc-400">Target:</span> <span className="font-mono text-zinc-300">{singleProvider} / {singleModel}</span></div>
                )}
                {formMode === 'fallback' && (
                  <div><span className="text-zinc-400 block mb-1">Targets:</span>
                    {fbTargets.filter(t => t.provider && t.model).map((t, i) => (
                      <div key={i} className="font-mono text-zinc-300 text-xs ml-2">{i + 1}. {t.provider} / {t.model}</div>
                    ))}
                  </div>
                )}
                {formMode === 'smart' && (
                  <div><span className="text-zinc-400 block mb-1">Candidates ({smartCandidates.filter(c => c.provider && c.model).length}):</span>
                    {smartCandidates.filter(c => c.provider && c.model).map((c, i) => (
                      <div key={i} className="font-mono text-zinc-300 text-xs ml-2">{c.provider} / {c.model}</div>
                    ))}
                    <div className="mt-2 text-xs text-zinc-400">Policy: <span className="font-semibold text-indigo-400 capitalize">{smartPolicy}</span> | Mode: <span className="font-semibold text-amber-400 uppercase">{smartMode}</span></div>
                  </div>
                )}
              </div>
            </div>
          )}

          {/* Wizard nav buttons */}
          <div className="flex justify-between border-t border-zinc-800 pt-4">
            <button 
              onClick={() => { if (wizardStep === 1) { setWizardStep(0); } else { setWizardStep(wizardStep - 1); } }}
              className="rounded-lg border border-zinc-800 hover:bg-zinc-900 px-4 py-2 text-sm"
            >
              {wizardStep === 1 ? 'Cancel' : 'Back'}
            </button>
            {wizardStep < 4 ? (
              <button 
                onClick={() => setWizardStep(wizardStep + 1)}
                disabled={wizardStep === 1 && !formName.trim()}
                className="rounded-lg bg-indigo-600 hover:bg-indigo-500 px-5 py-2 text-sm font-semibold disabled:opacity-40"
              >
                Next
              </button>
            ) : (
              <button 
                onClick={handleCreateRoute}
                className="rounded-lg bg-indigo-600 hover:bg-indigo-500 px-5 py-2 text-sm font-semibold"
              >
                Save Route
              </button>
            )}
          </div>
        </div>
      )}

      {/* Filter tabs */}
      <div className="flex items-center gap-2 text-xs">
        {['all', 'single', 'fallback', 'smart'].map(m => (
          <button 
            key={m} 
            onClick={() => setFilterMode(m)}
            className={`px-3 py-1.5 rounded-lg border font-semibold transition-colors ${filterMode === m ? 'bg-indigo-600 text-white border-indigo-500' : 'bg-zinc-900 text-zinc-400 border-zinc-800 hover:text-zinc-200'}`}
          >
            {m === 'all' ? 'All' : m === 'single' ? 'Single Model' : m.charAt(0).toUpperCase() + m.slice(1)}
          </button>
        ))}
      </div>

      {/* Route table */}
      <div className="glass-panel rounded-2xl border border-zinc-800 shadow-lg overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-left text-xs">
            <thead>
              <tr className="border-b border-zinc-800 text-zinc-400 font-semibold uppercase tracking-wider bg-zinc-900/30">
                <th className="px-4 py-3">Public Name</th>
                <th className="px-4 py-3">Mode</th>
                <th className="px-4 py-3">Destination / Candidates</th>
                <th className="px-4 py-3">Status</th>
                <th className="px-4 py-3">Health</th>
                <th className="px-4 py-3 text-right">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-zinc-800/50">
              {filteredRoutes.length === 0 ? (
                <tr>
                  <td colSpan={6} className="px-4 py-12 text-center text-zinc-500">
                    <div className="flex flex-col items-center gap-2">
                      <Sliders className="h-8 w-8 text-zinc-600" />
                      <p className="font-medium">No public routes yet</p>
                      <p className="text-[10px] text-zinc-600">Create a public model name and choose how TermRouter should serve it.</p>
                    </div>
                  </td>
                </tr>
              ) : (
                filteredRoutes.map((route: any) => (
                  <tr key={route.id} className="hover:bg-zinc-900/30 transition-colors">
                    <td className="px-4 py-4">
                      <div className="font-bold text-zinc-100 font-mono">{route.name}</div>
                      {route.aliases?.length > 0 && (
                        <div className="text-[10px] text-zinc-500 mt-0.5">
                          +{route.aliases.length} alias(es)
                        </div>
                      )}
                    </td>
                    <td className="px-4 py-4">
                      {route.mode === 'single' && (
                        <span className="px-2 py-0.5 rounded text-[10px] font-bold bg-indigo-950 text-indigo-400 border border-indigo-900">Single Model</span>
                      )}
                      {route.mode === 'fallback' && (
                        <span className="px-2 py-0.5 rounded text-[10px] font-bold bg-amber-950 text-amber-400 border border-amber-900">Fallback</span>
                      )}
                      {route.mode === 'smart' && (
                        <span className="px-2 py-0.5 rounded text-[10px] font-bold bg-emerald-950 text-emerald-400 border border-emerald-900">Smart</span>
                      )}
                    </td>
                    <td className="px-4 py-4 text-zinc-300 font-mono text-[11px] max-w-[250px] truncate">
                      {route.summary}
                    </td>
                    <td className="px-4 py-4">
                      {route.mode === 'smart' ? (
                        <span className={`px-2 py-0.5 rounded text-[10px] font-bold border ${route.status === 'live' ? 'bg-emerald-500/10 text-emerald-400 border-emerald-500/20' : route.status === 'shadow' ? 'bg-amber-500/10 text-amber-400 border-amber-500/20' : 'bg-zinc-800 text-zinc-400 border-zinc-700'}`}>
                          {(route.status || 'off').toUpperCase()}
                        </span>
                      ) : (
                        <span className={`h-2 w-2 rounded-full inline-block ${route.status === 'healthy' ? 'bg-emerald-500' : 'bg-rose-500'}`}></span>
                      )}
                    </td>
                    <td className="px-4 py-4 text-zinc-400 text-[11px]">{route.health}</td>
                    <td className="px-4 py-4 text-right">
                      <div className="flex items-center justify-end gap-1">
                        {route.mode === 'smart' && (
                          <>
                            <button 
                              onClick={() => handleToggleRoute(route, route.status === 'off')}
                              className={`px-2 py-1 rounded text-[10px] font-semibold border transition-colors ${route.status === 'live' ? 'bg-emerald-500/10 text-emerald-400 border-emerald-500/20' : route.status === 'shadow' ? 'bg-amber-500/10 text-amber-400 border-amber-500/20' : 'bg-zinc-800 text-zinc-400 border-zinc-700'}`}
                            >
                              {route.status === 'live' ? 'Live' : route.status === 'shadow' ? 'Shadow' : 'Disabled'}
                            </button>
                          </>
                        )}
                        <button 
                          onClick={() => handleEditRoute(route)}
                          className="bg-zinc-700/50 hover:bg-zinc-600/50 text-zinc-400 border border-zinc-700 p-1.5 rounded-lg transition-colors"
                          title="Edit"
                        >
                          <Pencil className="h-3.5 w-3.5" />
                        </button>
                        <button 
                          onClick={() => handleDeleteRoute(route)}
                          className="bg-rose-500/10 hover:bg-rose-500/20 text-rose-400 border border-rose-500/20 p-1.5 rounded-lg transition-colors"
                          title="Delete"
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </button>
                      </div>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>

      {/* Unassigned routes section */}
      {unassignedRoutes.length > 0 && (
        <div className="glass-panel rounded-2xl border border-zinc-800/60 p-5 space-y-3">
          <button 
            onClick={() => setShowUnassigned(!showUnassigned)} 
            className="flex items-center justify-between w-full text-left"
          >
            <h4 className="text-sm font-bold text-zinc-400">Unassigned Internal Routes ({unassignedRoutes.length})</h4>
            <span className="text-xs text-zinc-500">{showUnassigned ? 'Hide' : 'Show'}</span>
          </button>
          {showUnassigned && (
            <div className="space-y-2">
              <p className="text-xs text-zinc-500">These routes exist but clients cannot request them through a public model name.</p>
              {unassignedRoutes.map(([rName, r]: any) => (
                <div key={rName} className="flex items-center justify-between bg-zinc-900/30 border border-zinc-800 rounded-lg p-3">
                  <div className="flex items-center gap-3">
                    <span className="font-mono text-sm text-zinc-300">{rName}</span>
                    <span className="text-[10px] bg-zinc-800 text-zinc-400 px-1.5 py-0.5 rounded font-mono">{r.strategy}</span>
                  </div>
                  <button 
                    onClick={async () => {
                      const name = prompt('Assign a public model name:');
                      if (name) {
                        try {
                          await apiCall(`${API_BASE}/aliases`, 'POST', { name: name.trim(), route: rName });
                          toastSuccess(`Alias "${name}" created for route "${rName}"`);
                          fetchConfig();
                        } catch (e) {}
                      }
                    }}
                    className="bg-indigo-600/10 hover:bg-indigo-600/20 text-indigo-400 border border-indigo-500/20 px-3 py-1 rounded-lg text-xs font-semibold"
                  >
                    Assign public name
                  </button>
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

// ----------------------------------------------------
// ACTIVITY TAB
// ----------------------------------------------------
function ActivityTab({ activities, apiCall }: any) {
  const [selectedRequest, setSelectedRequest] = useState<any>(null);
  const [decisionDetails, setDecisionDetails] = useState<any>(null);

  const handleRowClick = async (r: any) => {
    setSelectedRequest(r);
    setDecisionDetails(null);
    if (r.alias) {
      try {
        const data = await apiCall(`${API_BASE}/decisions/${r.id}`);
        setDecisionDetails(data.decision);
      } catch (e) {}
    }
  };

  return (
    <div className="grid grid-cols-3 gap-8">
      {/* List */}
      <div className="col-span-2 glass-panel rounded-2xl border border-zinc-800 p-6 space-y-4 shadow-xl">
        <h3 className="font-bold text-sm tracking-wide uppercase text-zinc-300">Request Activity Stream</h3>
        <div className="overflow-x-auto">
          <table className="w-full text-left text-xs">
            <thead>
              <tr className="border-b border-zinc-800 text-zinc-400 font-semibold uppercase tracking-wider">
                <th className="pb-3">Time</th>
                <th className="pb-3">Public Name</th>
                <th className="pb-3">Selected Provider/Model</th>
                <th className="pb-3">Latency</th>
                <th className="pb-3">Status</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-zinc-850">
              {activities.length === 0 ? (
                <tr>
                  <td colSpan={5} className="py-8 text-center text-zinc-500">No requests recorded yet. Initiate completions via playground or client integration.</td>
                </tr>
              ) : (
                activities.map((r: any) => {
                  const isSelected = selectedRequest?.id === r.id;
                  return (
                    <tr 
                      key={r.id} 
                      onClick={() => handleRowClick(r)}
                      className={`hover:bg-zinc-900/30 cursor-pointer transition-colors ${isSelected ? 'bg-indigo-600/5 text-indigo-200 border-l-2 border-l-indigo-600' : ''}`}
                    >
                      <td className="py-3.5 font-mono text-zinc-400">{new Date(r.timestamp).toLocaleTimeString()}</td>
                      <td className="py-3.5 font-semibold text-zinc-100">{r.requested_model}</td>
                      <td className="py-3.5 font-mono text-zinc-303">{r.alias ? `routes/${r.alias}` : `${r.provider}/${r.upstream_model}`}</td>
                      <td className="py-3.5 font-mono text-zinc-400">{r.latency_ms}ms</td>
                      <td className="py-3.5">
                        <span className={`px-2 py-0.5 rounded text-[10px] font-bold ${r.status_code >= 200 && r.status_code < 400 ? 'bg-emerald-500/10 text-emerald-400' : 'bg-rose-500/10 text-rose-400'}`}>
                          {r.status_code === 0 ? 'STREAMING' : r.status_code}
                        </span>
                      </td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </div>
      </div>

      {/* Detail side drawer */}
      <div className="glass-panel rounded-2xl border border-zinc-800 p-6 space-y-6 shadow-xl">
        <h3 className="font-bold text-sm tracking-wide uppercase text-zinc-300 border-b border-zinc-800 pb-2">Request Details</h3>
        {!selectedRequest ? (
          <div className="text-center py-12 text-zinc-505 text-xs">Select a request from the activity log to inspect details and routing explanations.</div>
        ) : (
          <div className="space-y-6 text-xs">
            <div className="space-y-2">
              <div className="flex justify-between"><span className="text-zinc-505">Request ID:</span> <span className="font-mono text-zinc-302">{selectedRequest.id}</span></div>
              <div className="flex justify-between"><span className="text-zinc-505">Timestamp:</span> <span className="text-zinc-302">{new Date(selectedRequest.timestamp).toLocaleString()}</span></div>
              <div className="flex justify-between"><span className="text-zinc-505">Protocol:</span> <span className="font-mono uppercase text-zinc-302">{selectedRequest.protocol}</span></div>
              <div className="flex justify-between"><span className="text-zinc-505">Selected Provider:</span> <span className="font-mono text-zinc-302">{selectedRequest.provider || '—'}</span></div>
              <div className="flex justify-between"><span className="text-zinc-505">Selected Model:</span> <span className="font-mono text-zinc-302">{selectedRequest.upstream_model || '—'}</span></div>
              <div className="flex justify-between"><span className="text-zinc-505">Tokens In/Out:</span> <span className="font-mono text-zinc-302">{selectedRequest.input_tokens} / {selectedRequest.output_tokens}</span></div>
              <div className="flex justify-between"><span className="text-zinc-505">Stream mode:</span> <span className="text-zinc-302">{selectedRequest.stream ? 'Yes' : 'No'}</span></div>
            </div>

            {selectedRequest.fallback_reason && (
              <div className="bg-amber-500/5 border border-amber-500/10 rounded-lg p-3 space-y-1">
                <span className="font-bold text-[10px] text-amber-400 uppercase">Fallback Occurred:</span>
                <p className="text-zinc-303 text-[11px] font-mono leading-relaxed">{selectedRequest.fallback_reason}</p>
              </div>
            )}

            {/* Smart routing decision explanation */}
            {decisionDetails && (
              <div className="border-t border-zinc-800 pt-4 space-y-4">
                <h4 className="font-bold text-xs text-zinc-400 uppercase tracking-wider">Smart Route Analysis</h4>
                <div className="space-y-2">
                  <div className="flex justify-between"><span className="text-zinc-505">Primary Task:</span> <span className="bg-indigo-950 text-indigo-400 border border-indigo-900 px-2 py-0.5 rounded text-[10px] font-bold font-mono uppercase">{decisionDetails.TaskPrimaryType}</span></div>
                  <div className="flex justify-between"><span className="text-zinc-505">Complexity:</span> <span className="font-mono text-zinc-302">{decisionDetails.TaskComplexity}</span></div>
                  <div className="flex justify-between"><span className="text-zinc-505">Confidence:</span> <span className="font-mono text-zinc-302">{(decisionDetails.Confidence * 100).toFixed(0)}%</span></div>
                  <div className="flex justify-between"><span className="text-zinc-505">Session Match:</span> <span className="text-zinc-302">{decisionDetails.SessionAffinityHit ? 'Yes' : 'No'}</span></div>
                </div>

                {decisionDetails.EvaluationsJSON && (
                  <div className="space-y-2 text-[10px]">
                    <span className="font-semibold text-zinc-505 block uppercase">Candidate Rankings:</span>
                    <div className="space-y-1.5 font-mono">
                      {JSON.parse(decisionDetails.EvaluationsJSON).map((ev: any, idx: number) => (
                        <div key={idx} className="flex justify-between items-center bg-zinc-900/30 p-2 rounded border border-zinc-850">
                          <span className="text-zinc-303 max-w-[140px] truncate">{ev.provider}/{ev.model}</span>
                          <span className="font-bold text-indigo-400">Score: {ev.score?.toFixed(2) || '—'}</span>
                        </div>
                      ))}
                    </div>
                  </div>
                )}
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

// ----------------------------------------------------
// CLIENT KEYS TAB
// ----------------------------------------------------
function KeysTab({ clientKeys, apiCall, fetchKeys, toastSuccess, config }: any) {
  const [showAddForm, setShowAddForm] = useState(false);
  const [keyName, setKeyName] = useState('');
  const [allowedAliases, setAllowedAliases] = useState<string[]>([]);
  const [createdKey, setCreatedKey] = useState<string | null>(null);

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!keyName.trim()) return;

    try {
      const data = await apiCall(`${API_BASE}/client-keys`, 'POST', {
        name: keyName.trim(),
        aliases: allowedAliases.length > 0 ? allowedAliases : null
      });
      setCreatedKey(data.key);
      setKeyName('');
      setAllowedAliases([]);
      setShowAddForm(false);
      fetchKeys();
    } catch (e) {}
  };

  const handleDisable = async (keyId: string) => {
    if (!confirm('Disable this client key? Connected apps will lose access immediately.')) return;
    try {
      await apiCall(`${API_BASE}/client-keys/${keyId}/disable`, 'POST');
      toastSuccess('Client key disabled');
      fetchKeys();
    } catch (e) {}
  };

  const handleDelete = async (keyId: string) => {
    if (!confirm('Permanently delete this client key? This action is irreversible.')) return;
    try {
      await apiCall(`${API_BASE}/client-keys/${keyId}`, 'DELETE');
      toastSuccess('Client key deleted');
      fetchKeys();
    } catch (e) {}
  };

  const toggleAliasRestriction = (aName: string) => {
    if (allowedAliases.includes(aName)) {
      setAllowedAliases(allowedAliases.filter(a => a !== aName));
    } else {
      setAllowedAliases([...allowedAliases, aName]);
    }
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between border-b border-zinc-800 pb-4">
        <h3 className="text-lg font-bold">Client API Keys</h3>
        <button 
          onClick={() => {
            setShowAddForm(!showAddForm);
            setCreatedKey(null);
          }}
          className="rounded-xl bg-indigo-600 hover:bg-indigo-500 px-4 py-2 text-sm font-semibold flex items-center gap-1.5 transition-colors"
        >
          {showAddForm ? 'Cancel' : <><Plus className="h-4 w-4" /> Generate Client Key</>}
        </button>
      </div>

      {createdKey && (
        <div className="p-4 rounded-xl bg-emerald-500/10 border border-emerald-500/20 text-emerald-400 space-y-3 max-w-xl">
          <div className="flex items-center gap-2 font-bold text-sm">
            <CheckCircle className="h-5 w-5" />
            Client API Key Generated Successfully!
          </div>
          <div className="flex gap-2">
            <input type="text" readOnly value={createdKey} className="flex-1 bg-zinc-950 border border-zinc-800 rounded-lg px-3 py-2 text-sm font-mono text-emerald-400 focus:outline-none" />
            <button onClick={() => {
              navigator.clipboard.writeText(createdKey);
              toastSuccess('Copied to clipboard');
            }} className="bg-zinc-850 hover:bg-zinc-800 px-3 py-2 rounded-lg text-xs font-semibold">Copy</button>
          </div>
          <p className="text-[10px] text-zinc-400">Save this key now. It will not be shown again. We only store its secure cryptographic hash.</p>
        </div>
      )}

      {showAddForm && (
        <form onSubmit={handleCreate} className="glass-panel rounded-2xl border border-zinc-800 p-6 space-y-4 max-w-xl">
          <h4 className="font-bold text-sm text-zinc-300">Generate Client Token</h4>
          <div>
            <label className="block text-xs font-semibold text-zinc-400 mb-1">Key Name (e.g. "prod-app", "local-dev")</label>
            <input type="text" required value={keyName} onChange={(e) => setKeyName(e.target.value)} className="w-full bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-2 text-sm text-zinc-100" />
          </div>

          <div className="space-y-2">
            <label className="block text-xs font-semibold text-zinc-400 uppercase tracking-wider">Restrict to Specific Public Routes (Optional)</label>
            <p className="text-[10px] text-zinc-500 leading-normal mb-2">If no routes are selected, the key can access all configured public routes.</p>
            <div className="flex flex-wrap gap-2">
              {Object.keys(config.aliases || {}).map((aName: string) => {
                const isSelected = allowedAliases.includes(aName);
                return (
                  <button
                    type="button"
                    key={aName}
                    onClick={() => toggleAliasRestriction(aName)}
                    className={`px-3 py-1.5 rounded-lg text-xs transition-colors border ${isSelected ? 'bg-indigo-600/10 border-indigo-600/30 text-indigo-300 font-semibold' : 'bg-zinc-900 border-zinc-800 text-zinc-400 hover:text-zinc-300'}`}
                  >
                    {aName}
                  </button>
                );
              })}
            </div>
          </div>

          <button type="submit" className="rounded-xl bg-indigo-600 hover:bg-indigo-500 px-4 py-2 text-sm font-semibold">Generate Client Key</button>
        </form>
      )}

      {/* Grid of keys */}
      <div className="glass-panel rounded-2xl border border-zinc-800 shadow-lg p-6">
        <div className="overflow-x-auto">
          <table className="w-full text-left text-xs">
            <thead>
              <tr className="border-b border-zinc-800 text-zinc-400 font-semibold uppercase tracking-wider">
                <th className="pb-3">Name</th>
                <th className="pb-3">Prefix</th>
                <th className="pb-3">Allowed Routes</th>
                <th className="pb-3">Status</th>
                <th className="pb-3">Created</th>
                <th className="pb-3 text-right">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-zinc-850">
              {clientKeys.length === 0 ? (
                <tr>
                  <td colSpan={6} className="py-8 text-center text-zinc-500">No client keys configured. Generate a key to authorize client applications.</td>
                </tr>
              ) : (
                clientKeys.map((k: any) => (
                  <tr key={k.id} className="hover:bg-zinc-900/30">
                    <td className="py-4 font-bold text-zinc-200">{k.name}</td>
                    <td className="py-4 font-mono text-zinc-400">{k.key_prefix || 'tr_live_...'}</td>
                    <td className="py-4">
                      {k.allowed_aliases && k.allowed_aliases.length > 0 ? (
                        <div className="flex gap-1 flex-wrap">
                          {k.allowed_aliases.map((a: string) => <span key={a} className="bg-zinc-800 text-zinc-300 text-[10px] px-1.5 py-0.5 rounded font-mono">{a}</span>)}
                        </div>
                      ) : (
                          <span className="text-zinc-500 italic">All routes</span>
                      )}
                    </td>
                    <td className="py-4">
                      <span className={`px-2 py-0.5 rounded text-[10px] font-bold ${k.enabled ? 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/20' : 'bg-rose-500/10 text-rose-400 border border-rose-500/20'}`}>
                        {k.enabled ? 'ACTIVE' : 'DISABLED'}
                      </span>
                    </td>
                    <td className="py-4 text-zinc-505">{new Date(k.created_at).toLocaleDateString()}</td>
                    <td className="py-4 text-right flex justify-end gap-2">
                      {k.enabled && (
                        <button 
                          onClick={() => handleDisable(k.id)}
                          className="bg-zinc-800 border border-zinc-700 hover:border-zinc-600 px-2.5 py-1.5 rounded-lg text-[10px] text-zinc-300 font-semibold transition-colors"
                        >
                          Disable
                        </button>
                      )}
                      <button 
                        onClick={() => handleDelete(k.id)}
                        className="bg-rose-500/10 hover:bg-rose-500/20 text-rose-400 border border-rose-500/20 p-2 rounded-lg transition-colors flex items-center"
                      >
                        <Trash2 className="h-4 w-4" />
                      </button>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}

// ----------------------------------------------------
// PLAYGROUND TAB
// ----------------------------------------------------
function PlaygroundTab({ config, apiCall }: any) {
  const [selectedAlias, setSelectedAlias] = useState('');
  const [stream, setStream] = useState(false);
  const [prompt, setPrompt] = useState('');
  const [systemPrompt, setSystemPrompt] = useState('');
  
  // Results
  const [response, setResponse] = useState('');
  const [latency, setLatency] = useState<number | null>(null);
  const [decision, setDecision] = useState<any>(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    const routes = buildUnifiedRoutes(config, {});
    if (routes.length > 0) setSelectedAlias(routes[0].name);
  }, [config]);

  const handleSend = async () => {
    if (!selectedAlias || !prompt.trim()) return;

    setLoading(true);
    setResponse('');
    setLatency(null);
    setDecision(null);

    const start = Date.now();
    try {
      const payload = {
        model: selectedAlias,
        messages: [
          ...(systemPrompt ? [{ role: 'system', content: systemPrompt }] : []),
          { role: 'user', content: prompt }
        ],
        stream
      };

      // Since we don't store plaintext client keys on the server, the admin console session itself can request playground runs.
      // We route this through our custom admin playground API that executes the query using the configured resolver.
      const res = await apiCall(`${API_BASE}/routes/${selectedAlias}/test`, 'POST', payload);
      setLatency(Date.now() - start);
      setResponse(res.response || JSON.stringify(res, null, 2));
      setDecision(res.decision);
    } catch (e: any) {
      setResponse(`Error executing request: ${e.message}`);
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="grid grid-cols-3 gap-8">
      {/* Parameters Form */}
      <div className="glass-panel rounded-2xl border border-zinc-800 p-6 space-y-4 shadow-xl">
        <h3 className="font-bold text-sm tracking-wide uppercase text-zinc-300 border-b border-zinc-800 pb-2">Diagnostic Playground</h3>
        
        <div>
          <label className="block text-xs font-semibold text-zinc-400 mb-1">Select Public Route</label>
          <select value={selectedAlias} onChange={(e) => setSelectedAlias(e.target.value)} className="w-full bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-2 text-sm text-zinc-100">
            {buildUnifiedRoutes(config, {}).map((r: any) => <option key={r.id} value={r.name}>{r.name} ({r.mode})</option>)}
          </select>
        </div>

        <div>
          <label className="block text-xs font-semibold text-zinc-400 mb-1">System Message (Optional)</label>
          <textarea value={systemPrompt} onChange={(e) => setSystemPrompt(e.target.value)} placeholder="You are a coding helper..." rows={2} className="w-full bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-2 text-xs text-zinc-100 font-mono" />
        </div>

        <div>
          <label className="block text-xs font-semibold text-zinc-400 mb-1">Prompt / Input Payload</label>
          <textarea required value={prompt} onChange={(e) => setPrompt(e.target.value)} placeholder="Type representative task prompt here..." rows={4} className="w-full bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-2 text-xs text-zinc-100 font-mono" />
        </div>

        <div className="flex items-center gap-2 text-xs text-zinc-400">
          <input type="checkbox" checked={stream} onChange={(e) => setStream(e.target.checked)} className="rounded bg-zinc-900 border-zinc-800" />
          <span>Use Gateway Streaming (text/event-stream)</span>
        </div>

        <button 
          onClick={handleSend}
          disabled={loading || !prompt.trim()}
          className="w-full flex items-center justify-center gap-2 rounded-xl bg-indigo-600 hover:bg-indigo-500 disabled:opacity-40 disabled:hover:bg-indigo-600 px-4 py-3 font-semibold text-sm transition-all"
        >
          {loading ? <RefreshCw className="h-4 w-4 animate-spin" /> : <Play className="h-4 w-4" />}
          {loading ? 'Executing...' : 'Send Completion Request'}
        </button>

        <p className="text-[10px] text-zinc-550 leading-normal">
          <strong>Note:</strong> Prompts sent here are processed directly via the gateway and stored in the request logs for audit. Prompt body content is not cached in local storage.
        </p>
      </div>

      {/* Response Box */}
      <div className="col-span-2 flex flex-col gap-6">
        <div className="glass-panel rounded-2xl border border-zinc-800 p-6 flex-1 flex flex-col justify-between shadow-xl min-h-[300px]">
          <div className="space-y-3 flex-1 flex flex-col">
            <div className="flex items-center justify-between border-b border-zinc-800 pb-2">
              <span className="text-xs font-bold text-zinc-405 uppercase tracking-wider">Gateway Response Output</span>
              {latency && <span className="text-xs font-mono text-zinc-550">Latency: {latency}ms</span>}
            </div>
            
            <div className="flex-1 bg-zinc-950 border border-zinc-900 rounded-xl p-4 font-mono text-xs text-zinc-303 overflow-y-auto whitespace-pre-wrap select-text">
              {response || (loading ? 'Awaiting stream chunk responses...' : 'Response token stream output will appear here...')}
            </div>
          </div>
        </div>

        {/* Explain results */}
        {decision && (
          <div className="glass-panel rounded-2xl border border-zinc-800 p-5 space-y-4 shadow-xl">
            <h4 className="font-bold text-xs text-zinc-405 uppercase tracking-wider border-b border-zinc-855 pb-2 flex items-center gap-2">
              <Shield className="h-4 w-4 text-indigo-400" />
              Routing Execution Explanation
            </h4>
            <div className="grid grid-cols-4 gap-4 text-xs">
              <div className="space-y-1">
                <span className="text-zinc-505">Strategy:</span>
                <span className="block font-mono text-zinc-202 uppercase font-semibold">{decision.mode ? 'Smart Route' : 'Direct alias'}</span>
              </div>
              <div className="space-y-1">
                <span className="text-zinc-505">Classifier:</span>
                <span className="block font-mono text-zinc-202 uppercase">{decision.task_primary_type || 'general_chat'}</span>
              </div>
              <div className="space-y-1">
                <span className="text-zinc-550">Selected Provider:</span>
                <span className="block font-mono text-indigo-400 font-semibold">{decision.selected_provider}</span>
              </div>
              <div className="space-y-1">
                <span className="text-zinc-550">Target Model:</span>
                <span className="block font-mono text-indigo-400 font-semibold">{decision.selected_model}</span>
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

// ----------------------------------------------------
// SYSTEM / DIAGNOSTICS & HISTORY TAB
// ----------------------------------------------------
function SystemTab({ diagnostics, configHistory, apiCall, fetchConfig, fetchDiagnostics, toastSuccess }: any) {
  const [diffRev, setDiffRev] = useState<number | null>(null);
  const [diffRecord, setDiffRecord] = useState<any>(null);
  const [runningDiag, setRunningDiag] = useState(false);

  const handleRunDoctor = async () => {
    setRunningDiag(true);
    try {
      await apiCall(`${API_BASE}/diagnostics/run`, 'POST');
      toastSuccess('Diagnostics checks completed');
      fetchDiagnostics();
    } catch (e) {} finally {
      setRunningDiag(false);
    }
  };

  const handleShowDiff = async (rev: number) => {
    try {
      const data = await apiCall(`${API_BASE}/config/history/${rev}`);
      setDiffRev(rev);
      setDiffRecord(data.record);
    } catch (e) {}
  };

  const handleRollback = async (rev: number) => {
    if (!confirm(`Rollback entire configuration to Revision ${rev}? This will replace config.yaml.`)) return;

    try {
      await apiCall(`${API_BASE}/config/rollback`, 'POST', { revision: rev });
      toastSuccess(`Config rolled back to revision ${rev}`);
      setDiffRev(null);
      setDiffRecord(null);
      fetchConfig();
    } catch (e) {}
  };

  return (
    <div className="grid grid-cols-2 gap-8">
      {/* Diagnostics panel */}
      <div className="glass-panel rounded-2xl border border-zinc-800 p-6 space-y-6 shadow-xl">
        <div className="flex items-center justify-between border-b border-zinc-800 pb-3">
          <div>
            <h3 className="font-bold text-sm tracking-wide uppercase text-zinc-300">Diagnostics Check list</h3>
            <p className="text-xs text-zinc-500 mt-0.5">Doctor checks equivalent to "termrouter doctor" command.</p>
          </div>
          <button 
            onClick={handleRunDoctor} 
            disabled={runningDiag}
            className="rounded-lg bg-zinc-900 border border-zinc-800 hover:border-zinc-700 px-3 py-1.5 text-xs text-zinc-300 flex items-center gap-1.5 transition-colors"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${runningDiag ? 'animate-spin' : ''}`} />
            Run Check
          </button>
        </div>

        <div className="space-y-4 max-h-[500px] overflow-y-auto pr-1">
          {/* Issues */}
          {diagnostics.issues?.length > 0 && (
            <div className="space-y-2">
              <span className="text-[10px] text-rose-500 font-bold uppercase tracking-wider">Identified Issues:</span>
              {diagnostics.issues.map((i: string, idx: number) => (
                <div key={idx} className="flex gap-2.5 text-xs text-rose-400 bg-rose-500/5 border border-rose-500/10 rounded-xl p-3">
                  <AlertCircle className="h-4 w-4 flex-shrink-0 mt-0.5" />
                  <span>{i}</span>
                </div>
              ))}
            </div>
          )}

          {/* Passed */}
          <div className="space-y-2">
            <span className="text-[10px] text-emerald-500 font-bold uppercase tracking-wider">Checks Passed:</span>
            {diagnostics.ok?.length === 0 ? (
              <div className="text-zinc-505 text-xs py-2">No completed checks.</div>
            ) : (
              diagnostics.ok?.map((o: string, idx: number) => (
                <div key={idx} className="flex gap-2.5 text-xs text-zinc-303 bg-zinc-900/30 border border-zinc-850 rounded-xl p-3">
                  <CheckCircle className="h-4 w-4 text-emerald-500 flex-shrink-0 mt-0.5" />
                  <span>{o}</span>
                </div>
              ))
            )}
          </div>
        </div>
      </div>

      {/* History and Rollback panel */}
      <div className="space-y-6">
        <div className="glass-panel rounded-2xl border border-zinc-800 p-6 space-y-4 shadow-xl">
          <div>
            <h3 className="font-bold text-sm tracking-wide uppercase text-zinc-300">Configuration History</h3>
            <p className="text-xs text-zinc-500 mt-0.5">Audit log of configuration changes. Roll back anytime.</p>
          </div>

          <div className="divide-y divide-zinc-850 max-h-[400px] overflow-y-auto space-y-1 pr-1">
            {configHistory.length === 0 ? (
              <div className="text-center py-6 text-zinc-500 text-xs">No configuration changes logged.</div>
            ) : (
              configHistory.map((h: any) => {
                const isSelected = diffRev === h.revision;
                return (
                  <div key={h.revision} className={`p-3 rounded-lg flex items-center justify-between transition-colors text-xs ${isSelected ? 'bg-zinc-900 border border-zinc-800' : 'hover:bg-zinc-900/30'}`}>
                    <div>
                      <div className="font-semibold text-zinc-200 flex items-center gap-1.5">
                        Revision {h.revision}
                        <span className="text-[9px] bg-indigo-950 text-indigo-400 border border-indigo-900 px-1 rounded uppercase font-bold">{h.change_type}</span>
                      </div>
                      <div className="text-[10px] text-zinc-550 mt-1 flex items-center gap-1.5">
                        <Clock className="h-3 w-3" />
                        {new Date(h.timestamp).toLocaleString()}
                      </div>
                    </div>
                    <div className="flex gap-2">
                      <button onClick={() => handleShowDiff(h.revision)} className="bg-zinc-800 hover:bg-zinc-700 text-zinc-300 px-2 py-1 rounded text-[10px]">Diff</button>
                      <button onClick={() => handleRollback(h.revision)} className="bg-indigo-600/10 hover:bg-indigo-600/20 text-indigo-400 border border-indigo-500/20 px-2 py-1 rounded text-[10px] font-semibold">Rollback</button>
                    </div>
                  </div>
                );
              })
            )}
          </div>
        </div>

        {/* Diff view */}
        {diffRecord && (
          <div className="glass-panel rounded-2xl border border-zinc-800 p-6 space-y-4 shadow-xl">
            <div className="flex items-center justify-between border-b border-zinc-800 pb-2">
              <h4 className="font-bold text-xs text-zinc-405 uppercase tracking-wider">Revision {diffRev} Configuration Yaml</h4>
              <button onClick={() => setDiffRecord(null)} className="text-zinc-500 hover:text-zinc-300 text-xs">Close</button>
            </div>
            <pre className="bg-zinc-950 border border-zinc-900 rounded-xl p-4 text-[10px] font-mono text-zinc-303 overflow-x-auto max-h-[300px] overflow-y-auto select-text leading-relaxed">
              {diffRecord.sanitized_yaml}
            </pre>
          </div>
        )}
      </div>
    </div>
  );
}
