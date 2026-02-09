const API_BASE = process.env.NEXT_PUBLIC_API_BASE_URL || "http://localhost:8080";

// ==================== TYPES ====================

export interface DataStats {
  rent_count: number;
  sale_count: number;
  rent_raw_count: number;
  sale_raw_count: number;
  total_raw: number;
  total: number;
}

export interface ScrapeParams {
  deal_type: "rent" | "sale";
  city?: string;
  pages?: number;
  max_items?: number;
  parallelism?: number;
  delay_min?: string;
  delay_max?: string;
  max_empty_pages?: number;
  max_retries?: number;
  captcha_cooldown?: string;
  use_cookie?: boolean;
}

export interface ScrapeConfig {
  city: string;
  pages: number;
  max_items: number;
  parallelism: number;
  delay_min: string;
  delay_max: string;
  max_retries: number;
  captcha_cooldown: string;
  max_empty_pages: number;
  deal_type: string;
}

export interface TaskInfo {
  id: string;
  name: string;
  command: string;
  status: "pending" | "running" | "done" | "failed" | "stopped";
  started_at: string;
  ended_at?: string;
  exit_code?: number;
  error?: string;
}

export interface TaskDetail {
  task: TaskInfo;
  logs: string[];
}

export interface CookieData {
  cookie: string;
}

// ==================== HELPERS ====================

function adminHeaders(adminKey: string): HeadersInit {
  return {
    "Content-Type": "application/json",
    "X-Admin-Key": adminKey,
  };
}

async function adminFetch<T>(
  adminKey: string,
  path: string,
  options?: RequestInit
): Promise<T> {
  const resp = await fetch(`${API_BASE}${path}`, {
    ...options,
    headers: {
      ...adminHeaders(adminKey),
      ...(options?.headers || {}),
    },
  });
  if (!resp.ok) {
    const body = await resp.json().catch(() => ({ error: resp.statusText }));
    throw new Error(body.error || `HTTP ${resp.status}`);
  }
  return resp.json();
}

// ==================== API ====================

export const adminApi = {
  // --- Data Stats ---
  getDataStats(adminKey: string): Promise<DataStats> {
    return adminFetch(adminKey, "/api/v1/admin/data/stats");
  },

  // --- Scraping ---
  startScrape(
    adminKey: string,
    params: ScrapeParams
  ): Promise<{ task_id: string; name: string; status: string }> {
    return adminFetch(adminKey, "/api/v1/admin/scrape/start", {
      method: "POST",
      body: JSON.stringify(params),
    });
  },

  getScrapeConfig(adminKey: string): Promise<ScrapeConfig> {
    return adminFetch(adminKey, "/api/v1/admin/scrape/config");
  },

  // --- Scrap Pipeline ---
  runScrapCommand(
    adminKey: string,
    command: string
  ): Promise<{ task_id: string; name: string; status: string }> {
    return adminFetch(adminKey, `/api/v1/admin/scrap/${command}`, {
      method: "POST",
    });
  },

  // --- ML Pipeline ---
  runMLCommand(
    adminKey: string,
    command: string
  ): Promise<{ task_id: string; name: string; status: string }> {
    return adminFetch(adminKey, `/api/v1/admin/ml/${command}`, {
      method: "POST",
    });
  },

  restartMLServer(
    adminKey: string
  ): Promise<{ task_id: string; name: string; status: string }> {
    return adminFetch(adminKey, "/api/v1/admin/ml-server/restart", {
      method: "POST",
    });
  },

  // --- Tasks ---
  listTasks(
    adminKey: string,
    limit = 30
  ): Promise<{ tasks: TaskInfo[] }> {
    return adminFetch(adminKey, `/api/v1/admin/tasks?limit=${limit}`);
  },

  getTask(adminKey: string, taskId: string): Promise<TaskDetail> {
    return adminFetch(adminKey, `/api/v1/admin/tasks/${taskId}`);
  },

  stopTask(adminKey: string, taskId: string): Promise<{ status: string }> {
    return adminFetch(adminKey, `/api/v1/admin/tasks/${taskId}/stop`, {
      method: "POST",
    });
  },

  // SSE log stream URL
  taskLogsURL(adminKey: string, taskId: string): string {
    return `${API_BASE}/api/v1/admin/tasks/${taskId}/logs?key=${encodeURIComponent(adminKey)}`;
  },

  // --- Cookies ---
  getCookies(adminKey: string): Promise<CookieData> {
    return adminFetch(adminKey, "/api/v1/admin/cookies");
  },

  updateCookies(
    adminKey: string,
    cookie: string
  ): Promise<{ status: string }> {
    return adminFetch(adminKey, "/api/v1/admin/cookies", {
      method: "PUT",
      body: JSON.stringify({ cookie }),
    });
  },
};
