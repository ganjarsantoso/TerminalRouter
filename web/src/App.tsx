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
  FileText, 
  Database, 
  Unlock, 
  Cpu, 
  Play, 
  ArrowRight,
  LogOut,
  Clock,
  Layers,
  Info
} from 'lucide-react';

// API helpers
const API_BASE = '/admin/v1';

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
                  <div className="text-[10px] font-semibold text-zinc-500 uppercase tracking-wider px-3 mb-2">Overview</div>
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
                      icon={<Cpu className="h-4 w-4" />} 
                      label="Models & Profiles" 
                      active={currentTab === 'profiles'} 
                      onClick={() => setCurrentTab('profiles')} 
                    />
                    <SidebarItem 
                      icon={<FileText className="h-4 w-4" />} 
                      label="Aliases" 
                      active={currentTab === 'aliases'} 
                      onClick={() => setCurrentTab('aliases')} 
                    />
                    <SidebarItem 
                      icon={<Sliders className="h-4 w-4" />} 
                      label="Routes" 
                      active={currentTab === 'routes'} 
                      onClick={() => setCurrentTab('routes')} 
                    />
                    <SidebarItem 
                      icon={<Activity className="h-4 w-4" />} 
                      label="Smart Routes" 
                      active={currentTab === 'smart'} 
                      onClick={() => setCurrentTab('smart')} 
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
                      label="Diagnostics / History" 
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

              {currentTab === 'aliases' && (
                <AliasesTab 
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

              {currentTab === 'smart' && (
                <SmartRoutesTab 
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
            <p className="text-zinc-400 text-sm">TermRouter uses public aliases to map requests to models. Let's create an alias named <span className="font-semibold text-indigo-400">general</span> that points to a specific model.</p>

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
              When client apps call TermRouter requesting the model <code className="font-mono text-zinc-400">general</code>, it will execute against provider <code className="font-mono text-zinc-400">{providerName}</code> with target model <code className="font-mono text-zinc-400">{selectedModel || customModel || 'gpt-4o'}</code>.
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
            <p className="text-xs text-zinc-500">This key will have access to the <code className="font-mono text-zinc-400">general</code> alias we created.</p>
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
  }'`}
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
  const aliasesCount = Object.keys(config.aliases || {}).length;

  return (
    <div className="space-y-8">
      {/* Top row cards */}
      <div className="grid grid-cols-4 gap-6">
        <StatCard title="Total Providers" value={providersCount} icon={<Database className="h-5 w-5 text-indigo-400" />} />
        <StatCard title="Active Aliases" value={aliasesCount} icon={<FileText className="h-5 w-5 text-teal-400" />} />
        <StatCard title="Request Load (Today)" value={usageSummary.TotalRequests} icon={<Activity className="h-5 w-5 text-indigo-400" />} />
        <StatCard title="Error Rate" value={usageSummary.TotalRequests > 0 ? `${((usageSummary.ErrorCount / usageSummary.TotalRequests) * 100).toFixed(1)}%` : '0%'} icon={<AlertCircle className="h-5 w-5 text-rose-400" />} />
      </div>

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
                  <th className="pb-3">Alias</th>
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
// MODELS & PROFILES TAB
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

function ProfilesTab({ config, discoveredModels, apiCall, fetchConfig, toastSuccess }: any) {
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
      await apiCall(`${API_BASE}/model-profiles/${selectedModel}`, 'PUT', payload);
      toastSuccess(`Capability profile for ${selectedModel} saved`);
      fetchConfig();
    } catch (e) {}
  };

  const handleResetProfile = async () => {
    if (!selectedModel || !confirm('Reset profile to built-in catalog default?')) return;
    try {
      await apiCall(`${API_BASE}/model-profiles/${selectedModel}/overrides`, 'DELETE');
      toastSuccess(`Profile reset to defaults`);
      fetchConfig();
    } catch (e) {}
  };

  // Compile full catalog of configured or discovered models
  const configuredModels = new Set<string>();
  Object.values(config.routes || {}).forEach((r: any) => {
    (r.targets || []).forEach((t: any) => configuredModels.add(t.model));
    (r.candidates || []).forEach((t: any) => configuredModels.add(t.model));
  });
  Object.values(config.aliases || {}).forEach((a: any) => {
    if (a.model) configuredModels.add(a.model);
  });
  
  const allModels = Array.from(new Set([
    ...Array.from(configuredModels),
    ...(discoveredModels?.map((m: any) => m.model_id) || [])
  ])).sort();

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
              return (
                <button
                  key={m}
                  onClick={() => setSelectedModel(m)}
                  className={`w-full text-left p-3 rounded-lg flex items-center justify-between text-xs transition-colors ${isSelected ? 'bg-indigo-600/10 border border-indigo-600/30 text-indigo-200' : 'hover:bg-zinc-900/60 text-zinc-300'}`}
                >
                  <span className="font-mono truncate">{m}</span>
                  <div className="flex items-center gap-1.5 flex-shrink-0">
                    {hasOverride && <span className="bg-emerald-950 text-emerald-400 text-[8px] font-bold px-1 py-0.5 rounded border border-emerald-900">PROFILED</span>}
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
                <button onClick={handleResetProfile} className="rounded-lg border border-zinc-800 hover:bg-zinc-900 px-3 py-1.5 text-xs text-zinc-400 hover:text-zinc-300">Reset Defaults</button>
                <button onClick={handleSaveProfile} className="rounded-lg bg-indigo-600 hover:bg-indigo-500 px-3 py-1.5 text-xs font-semibold">Save Profile</button>
              </div>
            </div>

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
// ALIASES TAB
// ----------------------------------------------------
function AliasesTab({ config, discoveredModels, apiCall, fetchConfig, toastSuccess }: any) {
  const [showAddForm, setShowAddForm] = useState(false);
  const [aliasName, setAliasName] = useState('');
  const [targetType, setTargetType] = useState('route'); // route | direct
  const [targetRoute, setTargetRoute] = useState('');
  const [directProvider, setDirectProvider] = useState('');
  const [directModel, setDirectModel] = useState('');

  // Defaults on load
  useEffect(() => {
    const routeKeys = Object.keys(config.routes || {});
    if (routeKeys.length > 0) setTargetRoute(routeKeys[0]);
    const provKeys = Object.keys(config.providers || {});
    if (provKeys.length > 0) setDirectProvider(provKeys[0]);
  }, [config]);

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!aliasName.trim()) return;

    try {
      const payload: any = {
        name: aliasName.trim().toLowerCase().replace(/[^a-z0-9_-]/g, '')
      };
      if (targetType === 'route') {
        payload.route = targetRoute;
      } else {
        payload.provider = directProvider;
        payload.model = directModel;
      }

      await apiCall(`${API_BASE}/aliases`, 'POST', payload);
      toastSuccess(`Alias "${aliasName}" configured`);
      setAliasName('');
      setDirectModel('');
      setShowAddForm(false);
      fetchConfig();
    } catch (e) {}
  };

  const handleDelete = async (aliasId: string) => {
    if (!confirm(`Remove alias "${aliasId}"?`)) return;
    try {
      await apiCall(`${API_BASE}/aliases/${aliasId}`, 'DELETE');
      toastSuccess(`Alias removed`);
      fetchConfig();
    } catch (e) {}
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between border-b border-zinc-800 pb-4">
        <h3 className="text-lg font-bold">Model & Route Aliases</h3>
        <button 
          onClick={() => setShowAddForm(!showAddForm)}
          className="rounded-xl bg-indigo-600 hover:bg-indigo-500 px-4 py-2 text-sm font-semibold flex items-center gap-1.5 transition-colors"
        >
          {showAddForm ? 'Cancel' : <><Plus className="h-4 w-4" /> Add Alias</>}
        </button>
      </div>

      {showAddForm && (
        <form onSubmit={handleSave} className="glass-panel rounded-2xl border border-zinc-800 p-6 space-y-4 max-w-xl">
          <h4 className="font-bold text-sm text-zinc-300">Create Mapping Alias</h4>
          <div>
            <label className="block text-xs font-semibold text-zinc-400 mb-1">Public Alias Name (e.g. "gpt-4o", "fast")</label>
            <input type="text" required value={aliasName} onChange={(e) => setAliasName(e.target.value)} className="w-full bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-2 text-sm text-zinc-100" />
          </div>

          <div>
            <label className="block text-xs font-semibold text-zinc-400 mb-2">Target Type</label>
            <div className="flex gap-4 text-xs">
              <label className="flex items-center gap-2">
                <input type="radio" name="targetType" checked={targetType === 'route'} onChange={() => setTargetType('route')} />
                Point to Route Group
              </label>
              <label className="flex items-center gap-2">
                <input type="radio" name="targetType" checked={targetType === 'direct'} onChange={() => setTargetType('direct')} />
                Point directly to Provider:Model
              </label>
            </div>
          </div>

          {targetType === 'route' ? (
            <div>
              <label className="block text-xs font-semibold text-zinc-400 mb-1">Select Route</label>
              <select value={targetRoute} onChange={(e) => setTargetRoute(e.target.value)} className="w-full bg-zinc-950 border border-zinc-800 rounded-lg px-3 py-2 text-sm text-zinc-100">
                {Object.keys(config.routes || {}).map(k => <option key={k} value={k}>{k}</option>)}
              </select>
            </div>
          ) : (
            <div className="grid grid-cols-2 gap-4">
              <div>
                <label className="block text-xs font-semibold text-zinc-400 mb-1">Provider</label>
                <select value={directProvider} onChange={(e) => setDirectProvider(e.target.value)} className="w-full bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-2 text-sm text-zinc-100">
                  {Object.keys(config.providers || {}).map(k => <option key={k} value={k}>{k}</option>)}
                </select>
              </div>
              <div>
                <label className="block text-xs font-semibold text-zinc-400 mb-1">Model ID</label>
                <input type="text" required value={directModel} onChange={(e) => setDirectModel(e.target.value)} placeholder="gpt-4o" list={`alias-models`} className="w-full bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-2 text-sm text-zinc-100" />
                <datalist id="alias-models">
                  {availableModels(discoveredModels, directProvider, config).map((m: string) => (
                    <option key={m} value={m} />
                  ))}
                </datalist>
              </div>
            </div>
          )}

          <button type="submit" className="rounded-xl bg-indigo-600 hover:bg-indigo-500 px-4 py-2 text-sm font-semibold">Save Alias</button>
        </form>
      )}

      {/* List Aliases */}
      <div className="glass-panel rounded-2xl border border-zinc-800 shadow-lg p-6">
        <div className="overflow-x-auto">
          <table className="w-full text-left text-xs">
            <thead>
              <tr className="border-b border-zinc-800 text-zinc-400 font-semibold uppercase tracking-wider">
                <th className="pb-3">Alias</th>
                <th className="pb-3">Target Routing Path</th>
                <th className="pb-3">Route Type</th>
                <th className="pb-3 text-right">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-zinc-850">
              {Object.keys(config.aliases || {}).length === 0 ? (
                <tr>
                  <td colSpan={4} className="py-8 text-center text-zinc-500">No aliases configured. Click "Add Alias" to map client names.</td>
                </tr>
              ) : (
                Object.entries(config.aliases || {}).map(([name, a]: any) => (
                  <tr key={name} className="hover:bg-zinc-900/30">
                    <td className="py-4 font-mono font-bold text-zinc-200">{name}</td>
                    <td className="py-4 font-mono text-zinc-300">
                      {a.route ? (
                        <span className="text-indigo-400 font-semibold">routes/{a.route}</span>
                      ) : (
                        `providers/${a.provider} → ${a.model}`
                      )}
                    </td>
                    <td className="py-4">
                      <span className={`px-2 py-0.5 rounded text-[10px] font-bold ${a.route ? 'bg-indigo-950 text-indigo-400 border border-indigo-900' : 'bg-zinc-900 text-zinc-400 border border-zinc-800'}`}>
                        {a.route ? 'ROUTE GROUP' : 'DIRECT TARGET'}
                      </span>
                    </td>
                    <td className="py-4 text-right">
                      <button 
                        onClick={() => handleDelete(name)}
                        className="bg-rose-500/10 hover:bg-rose-500/20 text-rose-400 border border-rose-500/20 p-2 rounded-lg transition-colors inline-flex items-center"
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
// ROUTES TAB
// ----------------------------------------------------
function RoutesTab({ config, discoveredModels, providerHealth, apiCall, fetchConfig, toastSuccess }: any) {
  const [showAddForm, setShowAddForm] = useState(false);
  const [routeName, setRouteName] = useState('');
  const [strategy, setStrategy] = useState('fallback'); // direct | fallback
  
  // Targets list state
  const [targets, setTargets] = useState<any[]>([{ provider: '', model: '', timeout: '' }]);

  useEffect(() => {
    const provs = Object.keys(config.providers || {});
    if (provs.length > 0 && targets.length === 1 && !targets[0].provider) {
      setTargets([{ provider: provs[0], model: '', timeout: '' }]);
    }
  }, [config]);

  const addTargetRow = () => {
    const provs = Object.keys(config.providers || {});
    setTargets([...targets, { provider: provs[0] || '', model: '', timeout: '' }]);
  };

  const removeTargetRow = (idx: number) => {
    setTargets(targets.filter((_, i) => i !== idx));
  };

  const updateTarget = (idx: number, field: string, val: any) => {
    const copy = [...targets];
    copy[idx][field] = val;
    setTargets(copy);
  };

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!routeName.trim() || targets.length === 0) return;

    try {
      const formattedTargets = targets.map(t => {
        const item: any = { provider: t.provider, model: t.model };
        if (t.timeout) item.timeout = t.timeout;
        return item;
      });

      const payload: any = {
        name: routeName.trim().toLowerCase().replace(/[^a-z0-9_-]/g, ''),
        strategy,
        targets: formattedTargets
      };

      await apiCall(`${API_BASE}/routes`, 'POST', payload);
      toastSuccess(`Route group "${routeName}" configured`);
      setRouteName('');
      setTargets([{ provider: Object.keys(config.providers || {})[0] || '', model: '', timeout: '' }]);
      setShowAddForm(false);
      fetchConfig();
    } catch (e) {}
  };

  const handleDelete = async (routeId: string) => {
    if (!confirm(`Delete route group "${routeId}"?`)) return;
    try {
      await apiCall(`${API_BASE}/routes/${routeId}`, 'DELETE');
      toastSuccess(`Route removed`);
      fetchConfig();
    } catch (e) {}
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between border-b border-zinc-800 pb-4">
        <h3 className="text-lg font-bold">Route Target Groups</h3>
        <button 
          onClick={() => setShowAddForm(!showAddForm)}
          className="rounded-xl bg-indigo-600 hover:bg-indigo-500 px-4 py-2 text-sm font-semibold flex items-center gap-1.5 transition-colors"
        >
          {showAddForm ? 'Cancel' : <><Plus className="h-4 w-4" /> Create Route Group</>}
        </button>
      </div>

      {showAddForm && (
        <form onSubmit={handleSave} className="glass-panel rounded-2xl border border-zinc-800 p-6 space-y-4 max-w-2xl">
          <h4 className="font-bold text-sm text-zinc-300">Create Target Group</h4>
          
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-xs font-semibold text-zinc-400 mb-1">Route Name ID</label>
              <input type="text" required value={routeName} onChange={(e) => setRouteName(e.target.value)} placeholder="fast-fallback" className="w-full bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-2 text-sm text-zinc-100" />
            </div>
            <div>
              <label className="block text-xs font-semibold text-zinc-400 mb-1">Routing Strategy</label>
              <select value={strategy} onChange={(e) => setStrategy(e.target.value)} className="w-full bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-2 text-sm text-zinc-100">
                <option value="fallback">Ordered Fallback (Try targets in order on failure)</option>
                <option value="direct">Direct Single Target (Always route to first candidate)</option>
              </select>
            </div>
          </div>

          <div className="space-y-3">
            <div className="flex items-center justify-between border-b border-zinc-850 pb-2">
              <label className="block text-xs font-bold text-zinc-400 uppercase tracking-wider">Targets List (Priority Order)</label>
              <button type="button" onClick={addTargetRow} className="text-xs text-indigo-400 flex items-center gap-1 hover:underline">
                <Plus className="h-3.5 w-3.5" /> Add Target Target
              </button>
            </div>

            {targets.map((t, idx) => (
              <div key={idx} className="flex gap-3 items-center">
                <span className="font-mono text-xs text-zinc-500 w-4">{idx + 1}.</span>
                <select value={t.provider} onChange={(e) => updateTarget(idx, 'provider', e.target.value)} className="bg-zinc-950 border border-zinc-800 rounded-lg px-3 py-1.5 text-xs text-zinc-100 flex-1">
                  {Object.keys(config.providers || {}).map(k => <option key={k} value={k}>{k}</option>)}
                </select>
                <input type="text" required placeholder="Model ID (e.g. gpt-4o-mini)" value={t.model} onChange={(e) => updateTarget(idx, 'model', e.target.value)} list={`target-models-${idx}`} className="bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-1.5 text-xs text-zinc-100 flex-1" />
                <datalist id={`target-models-${idx}`}>
                  {availableModels(discoveredModels, t.provider, config).map((m: string) => (
                    <option key={m} value={m} />
                  ))}
                </datalist>
                <input type="text" placeholder="Timeout (e.g. 30s)" value={t.timeout} onChange={(e) => updateTarget(idx, 'timeout', e.target.value)} className="bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-1.5 text-xs text-zinc-100 w-24 font-mono" />
                <button type="button" disabled={targets.length === 1} onClick={() => removeTargetRow(idx)} className="text-rose-400 hover:text-rose-200 disabled:opacity-40 p-2"><Trash2 className="h-4 w-4" /></button>
              </div>
            ))}
          </div>

          <button type="submit" className="rounded-xl bg-indigo-600 hover:bg-indigo-500 px-4 py-2 text-sm font-semibold">Save Route Group</button>
        </form>
      )}

      {/* List Routes */}
      <div className="grid grid-cols-2 gap-6">
        {Object.entries(config.routes || {}).map(([rName, r]: any) => {
          if (r.strategy === 'smart') return null; // Smart routes in another tab
          return (
            <div key={rName} className="glass-panel rounded-2xl border border-zinc-800 p-5 space-y-4 flex flex-col justify-between shadow-lg">
              <div className="space-y-3">
                <div className="flex items-center justify-between">
                  <h4 className="font-bold text-base text-zinc-205">{rName}</h4>
                  <span className="px-2 py-0.5 rounded text-[10px] font-bold bg-indigo-950 text-indigo-400 border border-indigo-900 uppercase">
                    {r.strategy}
                  </span>
                </div>

                <div className="space-y-2">
                  <span className="text-[10px] text-zinc-500 font-bold uppercase">Targets:</span>
                  <div className="space-y-2">
                    {r.targets?.map((t: any, idx: number) => {
                      const pHealth = providerHealth[t.provider] || {};
                      const isUnhealthy = pHealth.circuit === 'open';
                      return (
                        <div key={idx} className="flex items-center justify-between bg-zinc-900/40 border border-zinc-850 p-2.5 rounded-lg text-xs font-mono">
                          <div className="flex items-center gap-2">
                            <span className="text-zinc-500 font-semibold">{idx + 1}.</span>
                            <span className="font-bold text-zinc-300">{t.provider}</span>
                            <span className="text-zinc-400">/</span>
                            <span className="text-zinc-400">{t.model}</span>
                          </div>
                          <div className="flex items-center gap-2">
                            {t.timeout && <span className="text-[10px] text-zinc-500">t={t.timeout}</span>}
                            <span className={`h-2 w-2 rounded-full ${isUnhealthy ? 'bg-rose-500' : 'bg-emerald-500'}`}></span>
                          </div>
                        </div>
                      );
                    })}
                  </div>
                </div>
              </div>

              <div className="border-t border-zinc-850 pt-3 flex justify-end">
                <button 
                  onClick={() => handleDelete(rName)}
                  className="bg-rose-500/10 hover:bg-rose-500/20 text-rose-400 border border-rose-500/20 px-3 py-1.5 rounded-lg text-xs flex items-center gap-1 transition-colors"
                >
                  <Trash2 className="h-3.5 w-3.5" /> Remove Group
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
// SMART ROUTES TAB
// ----------------------------------------------------
function SmartRoutesTab({ config, discoveredModels, providerHealth, apiCall, fetchConfig, toastSuccess }: any) {
  const [showAddForm, setShowAddForm] = useState(false);
  const [routeName, setRouteName] = useState('');
  const [policy, setPolicy] = useState('balanced');
  const [confidenceThreshold, setConfidenceThreshold] = useState(0.7);
  const [lowConfidenceTarget, setLowConfidenceTarget] = useState('');
  const [candidates, setCandidates] = useState<any[]>([{ provider: '', model: '' }]);
  const [shadowReports, setShadowReports] = useState<any>({});
  const [sessionAffinity, setSessionAffinity] = useState(false);
  const [sessionTTL, setSessionTTL] = useState('15m');

  useEffect(() => {
    const provs = Object.keys(config.providers || {});
    if (provs.length > 0 && candidates.length === 1 && !candidates[0].provider) {
      setCandidates([{ provider: provs[0], model: '' }]);
    }
    // Fetch reports
    apiCall(`${API_BASE}/smart/reports`).then((data: any) => {
      setShadowReports(data.reports || {});
    }).catch(() => {});
  }, [config]);

  const addCandidateRow = () => {
    const provs = Object.keys(config.providers || {});
    setCandidates([...candidates, { provider: provs[0] || '', model: '' }]);
  };

  const removeCandidateRow = (idx: number) => {
    setCandidates(candidates.filter((_, i) => i !== idx));
  };

  const updateCandidate = (idx: number, field: string, val: any) => {
    const copy = [...candidates];
    copy[idx][field] = val;
    setCandidates(copy);
  };

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!routeName.trim() || candidates.length === 0) return;

    try {
      const finalCandidates = candidates.map(c => ({ provider: c.provider, model: c.model }));
      const payload = {
        name: routeName.trim().toLowerCase().replace(/[^a-z0-9_-]/g, ''),
        strategy: 'smart',
        candidates: finalCandidates,
        smart: {
          mode: 'shadow', // default to shadow first
          policy,
          confidence_threshold: Number(confidenceThreshold),
          low_confidence_target: lowConfidenceTarget || undefined,
          session_affinity: {
            enabled: sessionAffinity,
            ttl: sessionTTL
          }
        }
      };

      await apiCall(`${API_BASE}/routes`, 'POST', payload);
      toastSuccess(`Smart Route "${routeName}" configured in shadow mode`);
      setRouteName('');
      setCandidates([{ provider: Object.keys(config.providers || {})[0] || '', model: '' }]);
      setShowAddForm(false);
      fetchConfig();
    } catch (e) {}
  };

  const handleSetMode = async (routeId: string, mode: string) => {
    const endPoint = mode === 'live' ? 'enable-live' : (mode === 'shadow' ? 'enable-shadow' : 'disable');
    
    // Explicit warning for live activation
    if (mode === 'live' && !confirm('Are you sure you want to enable Live Routing? TermRouter will dynamically evaluate prompting tasks to route requests.')) {
      return;
    }

    try {
      const payload = mode === 'live' ? { confirm: true } : {};
      await apiCall(`${API_BASE}/smart/routes/${routeId}/${endPoint}`, 'POST', payload);
      toastSuccess(`Smart Route "${routeId}" set to ${mode} mode`);
      fetchConfig();
    } catch (e) {}
  };

  const handleDelete = async (routeId: string) => {
    if (!confirm(`Delete Smart Route group "${routeId}"?`)) return;
    try {
      await apiCall(`${API_BASE}/routes/${routeId}`, 'DELETE');
      toastSuccess(`Route group removed`);
      fetchConfig();
    } catch (e) {}
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between border-b border-zinc-800 pb-4">
        <div>
          <h3 className="text-lg font-bold">Smart Routes Console</h3>
          <p className="text-xs text-zinc-500 mt-1">Deploy task-aware routing, run shadow evaluations, and configure quality/cost policies.</p>
        </div>
        <button 
          onClick={() => setShowAddForm(!showAddForm)}
          className="rounded-xl bg-indigo-600 hover:bg-indigo-500 px-4 py-2 text-sm font-semibold flex items-center gap-1.5 transition-colors"
        >
          {showAddForm ? 'Cancel' : <><Plus className="h-4 w-4" /> Create Smart Route</>}
        </button>
      </div>

      {showAddForm && (
        <form onSubmit={handleSave} className="glass-panel rounded-2xl border border-zinc-800 p-6 space-y-6 max-w-2xl">
          <h4 className="font-bold text-sm text-zinc-300">Configure Smart Route</h4>

          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-xs font-semibold text-zinc-400 mb-1">Route Name ID</label>
              <input type="text" required value={routeName} onChange={(e) => setRouteName(e.target.value)} placeholder="smart-router" className="w-full bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-2 text-sm text-zinc-100" />
            </div>
            <div>
              <label className="block text-xs font-semibold text-zinc-400 mb-1">Policy Preset</label>
              <select value={policy} onChange={(e) => setPolicy(e.target.value)} className="w-full bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-2 text-sm text-zinc-100">
                <option value="balanced">Balanced (Trade-off cost, quality, latency)</option>
                <option value="quality">Quality-oriented (Prefers highest capability model)</option>
                <option value="economy">Economy-oriented (Prefers lowest cost suitable model)</option>
                <option value="fast">Latency-oriented (Prefers fastest response)</option>
                <option value="private">Privacy-oriented (Prefers local execution)</option>
              </select>
            </div>
          </div>

          <div className="grid grid-cols-2 gap-4 text-xs">
            <div>
              <label className="block text-zinc-400 mb-1">Confidence Threshold: <span className="text-indigo-400 font-bold">{confidenceThreshold}</span></label>
              <input type="range" min="0.1" max="1.0" step="0.05" value={confidenceThreshold} onChange={(e) => setConfidenceThreshold(Number(e.target.value))} className="w-full bg-zinc-800 rounded-lg accent-indigo-500" />
            </div>
            <div>
              <label className="block text-zinc-400 mb-1">Low-Confidence Default Target (e.g. provider:model)</label>
              <input type="text" value={lowConfidenceTarget} onChange={(e) => setLowConfidenceTarget(e.target.value)} placeholder="openai:gpt-4o" className="w-full bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-2 text-sm text-zinc-100 font-mono" />
            </div>
          </div>

          <div className="border-t border-zinc-800 pt-4 grid grid-cols-2 gap-4 text-xs">
            <label className="flex items-center gap-2 border border-zinc-850 bg-zinc-900/20 p-3 rounded-lg cursor-pointer">
              <input type="checkbox" checked={sessionAffinity} onChange={(e) => setSessionAffinity(e.target.checked)} className="rounded bg-zinc-900 border-zinc-800" />
              Enable Session Affinity (Stick to same model per session)
            </label>
            {sessionAffinity && (
              <div>
                <label className="block text-zinc-400 mb-1">Session Expiry TTL</label>
                <input type="text" value={sessionTTL} onChange={(e) => setSessionTTL(e.target.value)} placeholder="15m" className="w-full bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-1.5 text-zinc-100 font-mono" />
              </div>
            )}
          </div>

          <div className="space-y-3">
            <div className="flex items-center justify-between border-b border-zinc-850 pb-2">
              <label className="block text-xs font-bold text-zinc-400 uppercase tracking-wider">Candidate Models</label>
              <button type="button" onClick={addCandidateRow} className="text-xs text-indigo-400 flex items-center gap-1 hover:underline">
                <Plus className="h-3.5 w-3.5" /> Add Candidate
              </button>
            </div>

            {candidates.map((c, idx) => (
              <div key={idx} className="flex gap-3 items-center">
                <span className="font-mono text-xs text-zinc-500 w-4">{idx + 1}.</span>
                <select value={c.provider} onChange={(e) => updateCandidate(idx, 'provider', e.target.value)} className="bg-zinc-950 border border-zinc-800 rounded-lg px-3 py-1.5 text-xs text-zinc-100 flex-1">
                  {Object.keys(config.providers || {}).map(k => <option key={k} value={k}>{k}</option>)}
                </select>
                <input type="text" required placeholder="Model ID (e.g. gpt-4o-mini)" value={c.model} onChange={(e) => updateCandidate(idx, 'model', e.target.value)} list={`candidate-models-${idx}`} className="bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-1.5 text-xs text-zinc-100 flex-1 font-mono" />
                <datalist id={`candidate-models-${idx}`}>
                  {availableModels(discoveredModels, c.provider, config).map((m: string) => (
                    <option key={m} value={m} />
                  ))}
                </datalist>
                <button type="button" disabled={candidates.length === 1} onClick={() => removeCandidateRow(idx)} className="text-rose-400 hover:text-rose-200 disabled:opacity-40 p-2"><Trash2 className="h-4 w-4" /></button>
              </div>
            ))}
          </div>

          <div className="bg-indigo-500/5 border border-indigo-500/10 text-[10px] text-indigo-400 rounded-xl p-3 leading-relaxed">
            <strong>Shadow Mode Default:</strong> Smart routes are created in shadow mode by default. They evaluate incoming client traffic, rank models, and compile decision reports on actual traffic without changing the model that receives the request. Move to live mode after reviewing reports.
          </div>

          <button type="submit" className="rounded-xl bg-indigo-600 hover:bg-indigo-500 px-4 py-2 text-sm font-semibold">Save Smart Route</button>
        </form>
      )}

      {/* List Smart Routes */}
      <div className="space-y-6">
        {Object.entries(config.routes || {}).map(([rName, r]: any) => {
          if (r.strategy !== 'smart') return null;
          const sConf = r.smart || {};
          const mode = sConf.mode || 'off';
          const report = shadowReports[rName] || { analyzed: 0, agreement: '0%', low_confidence: 0 };

          return (
            <div key={rName} className="glass-panel rounded-2xl border border-zinc-800 p-6 space-y-6 shadow-xl">
              <div className="flex items-center justify-between border-b border-zinc-850 pb-4">
                <div>
                  <h4 className="font-bold text-base text-zinc-200 flex items-center gap-3">
                    {rName}
                    <span className={`px-2.5 py-0.5 rounded-full text-[10px] font-bold border ${mode === 'live' ? 'bg-emerald-500/10 text-emerald-400 border-emerald-500/20' : (mode === 'shadow' ? 'bg-amber-500/10 text-amber-400 border-amber-500/20' : 'bg-zinc-800 text-zinc-400 border-zinc-700')}`}>
                      {mode.toUpperCase()} MODE
                    </span>
                  </h4>
                  <p className="text-xs text-zinc-500 mt-1">Policy: <span className="text-indigo-400 font-semibold">{sConf.policy}</span> | Confidence threshold: <span className="font-mono text-zinc-300">{sConf.confidence_threshold}</span></p>
                </div>

                <div className="flex items-center gap-2 bg-zinc-900/55 p-1.5 rounded-xl border border-zinc-850 text-xs">
                  <button 
                    onClick={() => handleSetMode(rName, 'shadow')}
                    className={`px-3 py-1.5 rounded-lg transition-colors ${mode === 'shadow' ? 'bg-amber-600 text-white font-semibold' : 'text-zinc-400 hover:text-zinc-200'}`}
                  >
                    Shadow
                  </button>
                  <button 
                    onClick={() => handleSetMode(rName, 'live')}
                    className={`px-3 py-1.5 rounded-lg transition-colors ${mode === 'live' ? 'bg-emerald-600 text-white font-semibold' : 'text-zinc-400 hover:text-zinc-200'}`}
                  >
                    Go Live
                  </button>
                  <button 
                    onClick={() => handleSetMode(rName, 'off')}
                    className={`px-3 py-1.5 rounded-lg transition-colors ${mode === 'off' ? 'bg-zinc-800 text-zinc-300 font-semibold' : 'text-zinc-400 hover:text-zinc-200'}`}
                  >
                    Disable
                  </button>
                </div>
              </div>

              <div className="grid grid-cols-3 gap-6">
                {/* Candidates */}
                <div className="space-y-2 col-span-2">
                  <span className="text-[10px] text-zinc-500 font-bold uppercase tracking-wider">Eligible Routing Candidates:</span>
                  <div className="grid grid-cols-2 gap-3">
                    {r.candidates?.map((c: any, idx: number) => {
                      const pHealth = providerHealth[c.provider] || {};
                      const isUnhealthy = pHealth.circuit === 'open';
                      return (
                        <div key={idx} className="bg-zinc-950/40 border border-zinc-850 p-3 rounded-xl flex items-center justify-between text-xs font-mono">
                          <div className="flex items-center gap-2">
                            <span className="font-bold text-zinc-300">{c.provider}</span>
                            <span className="text-zinc-505">/</span>
                            <span className="text-zinc-404 truncate max-w-[120px]">{c.model}</span>
                          </div>
                          <span className={`h-2 w-2 rounded-full ${isUnhealthy ? 'bg-rose-500 animate-pulse' : 'bg-emerald-500'}`}></span>
                        </div>
                      );
                    })}
                  </div>
                </div>

                {/* Shadow report */}
                <div className="bg-zinc-900/30 border border-zinc-855 rounded-2xl p-4 space-y-3">
                  <span className="text-[10px] text-zinc-505 font-bold uppercase tracking-wider">Shadow evaluation report:</span>
                  <div className="space-y-2 text-xs">
                    <div className="flex justify-between"><span className="text-zinc-404">Total Analyzed:</span> <span className="font-mono text-zinc-202">{report.analyzed || 0} reqs</span></div>
                    <div className="flex justify-between"><span className="text-zinc-404">Classifier Confidence:</span> <span className="font-mono text-zinc-202">{report.agreement || '100%'}</span></div>
                    <div className="flex justify-between"><span className="text-zinc-404">Low-Confidence Rate:</span> <span className="font-mono text-zinc-202">{report.low_confidence || 0}</span></div>
                  </div>
                </div>
              </div>

              <div className="border-t border-zinc-855 pt-4 flex justify-between">
                <div className="text-xs text-zinc-505 flex items-center gap-1.5">
                  <Clock className="h-4 w-4" />
                  <span>Session Affinity: {sConf.session_affinity?.enabled ? `Enabled (${sConf.session_affinity?.ttl || '15m'})` : 'Disabled'}</span>
                </div>
                <button 
                  onClick={() => handleDelete(rName)}
                  className="bg-rose-500/10 hover:bg-rose-500/20 text-rose-400 border border-rose-500/20 px-3 py-1 text-xs rounded-lg transition-colors flex items-center gap-1"
                >
                  <Trash2 className="h-3.5 w-3.5" /> Remove Group
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
                <th className="pb-3">Requested Alias</th>
                <th className="pb-3">Resolved Route</th>
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
            <label className="block text-xs font-semibold text-zinc-400 uppercase tracking-wider">Restrict to Specific Aliases (Optional)</label>
            <p className="text-[10px] text-zinc-500 leading-normal mb-2">If no aliases are selected, the key can access all configured aliases.</p>
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
                <th className="pb-3">Allowed Aliases</th>
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
                        <span className="text-zinc-500 italic">All aliases</span>
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
    const keys = Object.keys(config.aliases || {});
    if (keys.length > 0) setSelectedAlias(keys[0]);
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
          <label className="block text-xs font-semibold text-zinc-400 mb-1">Select Active Alias</label>
          <select value={selectedAlias} onChange={(e) => setSelectedAlias(e.target.value)} className="w-full bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-2 text-sm text-zinc-100">
            {Object.keys(config.aliases || {}).map(k => <option key={k} value={k}>{k}</option>)}
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
