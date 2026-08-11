// لایه‌ی ارتباط با API بک‌اند

// ————————————————————————————————————————————————————————————
// نوع‌های پاسخ API (مطابق قرارداد بک‌اند Go)
// ————————————————————————————————————————————————————————————

export interface LoginResponse {
  access_token: string;
  token_type: string;
}

export interface ChangePasswordResponse {
  access_token?: string;
  token_type?: string;
}

export interface User {
  id: number;
  username: string;
  index: number;
  quota_bytes: number;
  used_bytes: number;
  remaining_bytes: number | null;
  enabled: boolean;
  is_active: boolean;
  status: string;
  ip_limit: number;
  strikes: number;
  bandwidth_mbps: number;
  expires_at: string | null;
  note: string;
  created_at: string | null;
  ovpn_ip: string;
  wg_ip: string;
  l2tp_ip: string;
  sub_token: string;
}

export interface ResourceStats {
  cpu_pct: number;
  mem_used: number;
  mem_total: number;
  mem_pct: number;
  disk_used: number;
  disk_total: number;
  disk_pct: number;
  net_rx_bps: number;
  net_tx_bps: number;
  cores: number;
  uptime_sec: number;
}

export interface LogEvent {
  id: number;
  created_at: string;
  category: string;
  level: string;
  actor: string;
  message: string;
}

export interface CreateUserPayload {
  username: string;
  quota_gb: number;
  expires_days: number | null;
  note: string;
  ip_limit?: number;
  bandwidth_mbps?: number;
}

export interface SystemStatus {
  users_total: number;
  users_active: number;
  total_used_bytes: number;
  services: Record<string, string>;
  domain: string;
  server_ip: string;
  provisioning_enabled: boolean;
}

export interface ProtocolPeer {
  name: string;
  ip: string;
  online: boolean;
  rx_bytes: number;
  tx_bytes: number;
  last_handshake: number | null;
}

export interface EgressLocation {
  id: number;
  name: string;
  protocol: string;
  country_code: string;
  country_name: string;
  flag: string;
  egress_ip: string;
  address: string;
}

export interface EgressInfo {
  mode: "direct" | "single" | "balancer";
  pool_size: number;
  total: number;
  locations: EgressLocation[];
}

export interface ProtocolStat {
  key: string;
  service: string;
  label: string;
  status: string;
  online: number;
  rx_bytes: number;
  tx_bytes: number;
  total_bytes: number;
  detail: string;
  peers: ProtocolPeer[];
  // set only on the Xray entry: the outbound/egress relays traffic exits through
  egress?: EgressInfo;
}

export interface ProtocolStats {
  protocols: ProtocolStat[];
  total_online: number;
  active_users: number;
  total_bytes: number;
  demo: boolean;
}

export interface L2tpConfig {
  server: string;
  psk: string;
  username: string;
  password: string;
  assigned_ip: string;
  hint: string;
}

export interface Configs {
  username: string;
  openvpn: string;
  wireguard: string;
  l2tp: L2tpConfig;
}

export interface Field {
  key: string;
  label: string;
  type: string;
  group: string;
}

export interface SettingsResponse {
  fields: Field[];
  values: Record<string, string>;
}

export interface Resource {
  id: number;
  kind: string;
  title: string;
  description: string;
  url: string;
  icon: string;
  platform: string;
  sort_order: number;
  enabled: boolean;
}

export type ResourceInput = Omit<Resource, "id">;

export interface Outbound {
  id: number;
  name: string;
  protocol: string;
  address: string;
  is_active: boolean;
  egress_ip: string;
  country_code: string;
  country_name: string;
  flag: string;
  config: string;
  name_count: number;
  balanced: boolean;
  group_size: number;
}

export interface OutboundsResponse {
  items: Outbound[];
  direct: boolean;
  balanced: boolean;
  group_size: number;
  group_name: string;
  strategy: string;
}

export interface OutboundParseResult {
  config: string;
}

export interface OutboundTcpTest {
  ok?: boolean;
  latency_ms?: number | null;
  error?: string;
}

export interface OutboundProxyTest {
  ran?: boolean;
  ok?: boolean;
  latency_ms?: number | null;
  egress_ip?: string;
  country_code?: string;
  country_name?: string;
  error?: string;
}

export interface OutboundTestResult {
  ok: boolean;
  busy?: boolean;
  address: string;
  message: string;
  tcp: OutboundTcpTest;
  proxy: OutboundProxyTest;
}

export interface ErrorResponse {
  detail: string;
}

// ————————————————————————————————————————————————————————————

const TOKEN_KEY = "multivpn_token";

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY);
}
export function setToken(token: string): void {
  localStorage.setItem(TOKEN_KEY, token);
}
export function clearToken(): void {
  localStorage.removeItem(TOKEN_KEY);
}

interface RequestOptions {
  method?: string;
  body?: BodyInit;
  headers?: Record<string, string>;
}

async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = { ...(options.headers || {}) };
  const token = getToken();
  if (token) headers["Authorization"] = `Bearer ${token}`;
  if (options.body && !(options.body instanceof URLSearchParams)) {
    headers["Content-Type"] = "application/json";
  }

  const res = await fetch(path, { ...options, headers });

  if (!res.ok) {
    // نشست منقضی/نامعتبر (نه در خود لاگین) → پاک‌سازی و بارگذاری مجدد
    if (res.status === 401 && !path.endsWith("/login")) {
      clearToken();
      window.location.reload();
      throw new Error("نشست منقضی شد");
    }
    let detail = `خطا ${res.status}`;
    try {
      const data = await res.json();
      if (Array.isArray(data.detail)) {
        // خطای اعتبارسنجی pydantic (۴۲۲) — آرایه‌ای از خطاها
        detail = data.detail.map((e: any) => e.msg || JSON.stringify(e)).join("، ");
      } else if (typeof data.detail === "string") {
        detail = data.detail;
      } else if (data.detail) {
        detail = JSON.stringify(data.detail);
      }
    } catch {
      /* ignore */
    }
    throw new Error(detail);
  }
  if (res.status === 204) return null as T;
  return (await res.json()) as T;
}

export const api = {
  async login(username: string, password: string): Promise<LoginResponse> {
    const body = new URLSearchParams({ username, password });
    const data = await request<LoginResponse>("/api/auth/login", { method: "POST", body });
    setToken(data.access_token);
    return data;
  },
  logout(): void {
    clearToken();
  },
  async changePassword(oldPassword: string, newPassword: string): Promise<ChangePasswordResponse> {
    const data = await request<ChangePasswordResponse>("/api/auth/change-password", {
      method: "POST",
      body: JSON.stringify({ old_password: oldPassword, new_password: newPassword }),
    });
    if (data?.access_token) setToken(data.access_token); // توکن تازه با نسخه‌ی جدید
    return data;
  },
  status: (): Promise<SystemStatus> => request<SystemStatus>("/api/system/status"),
  protocolStats: (): Promise<ProtocolStats> => request<ProtocolStats>("/api/system/protocols"),
  listUsers: (): Promise<User[]> => request<User[]>("/api/users"),
  createUser: (payload: CreateUserPayload): Promise<User> =>
    request<User>("/api/users", { method: "POST", body: JSON.stringify(payload) }),
  updateUser: (id: number, payload: Partial<User>): Promise<User> =>
    request<User>(`/api/users/${id}`, { method: "PATCH", body: JSON.stringify(payload) }),
  deleteUser: (id: number): Promise<void> => request<void>(`/api/users/${id}`, { method: "DELETE" }),
  resetUser: (id: number): Promise<User> => request<User>(`/api/users/${id}/reset`, { method: "POST" }),
  getConfigs: (id: number): Promise<Configs> => request<Configs>(`/api/users/${id}/configs`),
  rotateSub: (id: number): Promise<User> => request<User>(`/api/users/${id}/rotate-sub`, { method: "POST" }),
  getSettings: (): Promise<SettingsResponse> => request<SettingsResponse>("/api/settings"),
  updateSettings: (data: Record<string, string>): Promise<SettingsResponse> =>
    request<SettingsResponse>("/api/settings", { method: "PATCH", body: JSON.stringify(data) }),
  getProtocols: (): Promise<SettingsResponse> => request<SettingsResponse>("/api/protocols"),
  updateProtocols: (data: Record<string, string>): Promise<SettingsResponse> =>
    request<SettingsResponse>("/api/protocols", { method: "PATCH", body: JSON.stringify(data) }),
  getResources: (): Promise<Resource[]> => request<Resource[]>("/api/resources"),
  createResource: (payload: ResourceInput): Promise<Resource> =>
    request<Resource>("/api/resources", { method: "POST", body: JSON.stringify(payload) }),
  updateResource: (id: number, payload: Partial<ResourceInput>): Promise<Resource> =>
    request<Resource>(`/api/resources/${id}`, { method: "PATCH", body: JSON.stringify(payload) }),
  deleteResource: (id: number): Promise<void> => request<void>(`/api/resources/${id}`, { method: "DELETE" }),
  getOutbounds: (): Promise<OutboundsResponse> => request<OutboundsResponse>("/api/outbounds"),
  parseOutbound: (config: string): Promise<OutboundParseResult> =>
    request<OutboundParseResult>("/api/outbounds/parse", { method: "POST", body: JSON.stringify({ config }) }),
  addOutbound: (name: string, config: string): Promise<Outbound> =>
    request<Outbound>("/api/outbounds", { method: "POST", body: JSON.stringify({ name, config }) }),
  updateOutbound: (id: number, payload: Partial<Outbound>): Promise<Outbound> =>
    request<Outbound>(`/api/outbounds/${id}`, { method: "PATCH", body: JSON.stringify(payload) }),
  activateOutbound: (id: number): Promise<void> => request<void>(`/api/outbounds/${id}/activate`, { method: "POST" }),
  activateAllOutbounds: (): Promise<void> => request<void>(`/api/outbounds/activate-all`, { method: "POST" }),
  testOutbound: (id: number): Promise<OutboundTestResult> =>
    request<OutboundTestResult>(`/api/outbounds/${id}/test`, { method: "POST" }),
  useDirect: (): Promise<void> => request<void>("/api/outbounds/direct", { method: "POST" }),
  getIranDirect: (): Promise<{ enabled: boolean }> => request<{ enabled: boolean }>("/api/outbounds/iran-direct"),
  setIranDirect: (enabled: boolean): Promise<{ enabled: boolean }> =>
    request<{ enabled: boolean }>("/api/outbounds/iran-direct", { method: "POST", body: JSON.stringify({ enabled }) }),
  deleteOutbound: (id: number): Promise<void> => request<void>(`/api/outbounds/${id}`, { method: "DELETE" }),
  getLogs: (category = "all", limit = 300): Promise<LogEvent[]> =>
    request<LogEvent[]>(`/api/logs?category=${encodeURIComponent(category)}&limit=${limit}`),
  clearLogs: (): Promise<{ ok: boolean }> => request<{ ok: boolean }>("/api/logs", { method: "DELETE" }),
  getApiNotes: (): Promise<{ notes: string }> => request<{ notes: string }>("/api/api-notes"),
  setApiNotes: (notes: string): Promise<{ notes: string }> =>
    request<{ notes: string }>("/api/api-notes", { method: "PUT", body: JSON.stringify({ notes }) }),
  getResourceStats: (): Promise<ResourceStats> => request<ResourceStats>("/api/system/resources"),
  downloadBackup: async (): Promise<void> => {
    const res = await fetch("/api/backup", { headers: { Authorization: `Bearer ${getToken()}` } });
    if (!res.ok) throw new Error("دانلود بکاپ ناموفق بود");
    const blob = await res.blob();
    const cd = res.headers.get("Content-Disposition") || "";
    const m = cd.match(/filename="([^"]+)"/);
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = m ? m[1] : "multivpn-backup.db";
    a.click();
    URL.revokeObjectURL(url);
  },
  restoreBackup: async (file: File): Promise<{ ok: boolean; message: string }> => {
    const fd = new FormData();
    fd.append("file", file);
    const res = await fetch("/api/restore", {
      method: "POST",
      headers: { Authorization: `Bearer ${getToken()}` },
      body: fd,
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(data.detail || "بازیابی ناموفق بود");
    return data as { ok: boolean; message: string };
  },
};

// آدرس پایه‌ی صفحه‌ی اشتراک (از تنظیمات یا origin فعلی)
export function subUrl(token: string, baseUrl?: string): string {
  let base = (baseUrl || "").trim().replace(/\/+$/, "");
  if (base) {
    // فقط http(s) پذیرفته می‌شود — جلوگیری از javascript:/data: در href
    try {
      const u = new URL(base);
      if (u.protocol !== "http:" && u.protocol !== "https:") base = "";
    } catch {
      base = "";
    }
  }
  if (!base) base = window.location.origin;
  return `${base}/sub/${token}`;
}
