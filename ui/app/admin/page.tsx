"use client";

import { useState, useCallback, useEffect } from "react";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { DataStatsCard } from "@/components/admin/data-stats";
import { ScrapePanel } from "@/components/admin/scrape-panel";
import { MLPipeline } from "@/components/admin/ml-pipeline";
import { TaskList } from "@/components/admin/task-list";
import { TaskLogViewer } from "@/components/admin/task-log-viewer";
import { CookieEditor } from "@/components/admin/cookie-editor";

const ADMIN_KEY_STORAGE = "rentadvisor_admin_key";
const ADMIN_KEY_EXPIRY = "rentadvisor_admin_key_expiry";
const ONE_DAY_MS = 24 * 60 * 60 * 1000;

export default function AdminPage() {
  const [adminKey, setAdminKey] = useState("");
  const [inputKey, setInputKey] = useState("");
  const [authenticated, setAuthenticated] = useState(false);
  const [activeTaskId, setActiveTaskId] = useState<string | null>(null);
  const [refreshTrigger, setRefreshTrigger] = useState(0);

  // Загрузка ключа из localStorage при монтировании
  useEffect(() => {
    if (typeof window === "undefined") return;

    const storedKey = localStorage.getItem(ADMIN_KEY_STORAGE);
    const storedExpiry = localStorage.getItem(ADMIN_KEY_EXPIRY);

    if (storedKey && storedExpiry) {
      const expiry = parseInt(storedExpiry, 10);
      if (Date.now() < expiry) {
        setAdminKey(storedKey);
        setAuthenticated(true);
      } else {
        // Ключ истёк
        localStorage.removeItem(ADMIN_KEY_STORAGE);
        localStorage.removeItem(ADMIN_KEY_EXPIRY);
      }
    }
  }, []);

  const handleLogin = () => {
    if (inputKey.trim()) {
      const key = inputKey.trim();
      const expiry = Date.now() + ONE_DAY_MS;

      setAdminKey(key);
      setAuthenticated(true);

      // Сохраняем в localStorage
      localStorage.setItem(ADMIN_KEY_STORAGE, key);
      localStorage.setItem(ADMIN_KEY_EXPIRY, expiry.toString());
    }
  };

  const handleLogout = () => {
    setAuthenticated(false);
    setAdminKey("");
    setInputKey("");
    localStorage.removeItem(ADMIN_KEY_STORAGE);
    localStorage.removeItem(ADMIN_KEY_EXPIRY);
  };

  const handleTaskStarted = useCallback((taskId: string) => {
    setActiveTaskId(taskId);
    setRefreshTrigger((prev) => prev + 1);
  }, []);

  // Экран входа
  if (!authenticated) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-gradient-to-br from-zinc-50 to-zinc-100 dark:from-zinc-900 dark:to-zinc-950 p-4">
        <Card className="w-full max-w-md">
          <CardHeader className="text-center">
            <CardTitle className="text-2xl">🔐 Админ-панель</CardTitle>
            <p className="text-sm text-muted-foreground mt-1">
              RentAdvisor — управление данными и ML-пайплайном
            </p>
          </CardHeader>
          <CardContent className="space-y-4">
            <div>
              <Input
                type="password"
                placeholder="Введите ключ администратора"
                value={inputKey}
                onChange={(e) => setInputKey(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && handleLogin()}
              />
            </div>
            <Button onClick={handleLogin} className="w-full" disabled={!inputKey.trim()}>
              Войти
            </Button>
            <p className="text-xs text-muted-foreground text-center">
              Ключ задаётся переменной окружения <code>ADMIN_KEY</code> в Go-бэкенде
            </p>
          </CardContent>
        </Card>
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-gradient-to-br from-zinc-50 to-zinc-100 dark:from-zinc-900 dark:to-zinc-950">
      {/* Header */}
      <header className="border-b bg-white/80 dark:bg-zinc-900/80 backdrop-blur-sm sticky top-0 z-50">
        <div className="container mx-auto px-4 py-3 flex items-center justify-between">
          <div className="flex items-center gap-3">
            <h1 className="text-xl font-bold">⚙️ RentAdvisor Admin</h1>
          </div>
          <div className="flex items-center gap-2">
            <a href="/" className="text-sm text-muted-foreground hover:text-foreground transition-colors">
              ← На сайт
            </a>
            <Button variant="ghost" size="sm" onClick={handleLogout}>
              Выйти
            </Button>
          </div>
        </div>
      </header>

      <main className="container mx-auto px-4 py-6 max-w-6xl">
        <Tabs defaultValue="data" className="space-y-4">
          <TabsList className="grid w-full grid-cols-5">
            <TabsTrigger value="data">📊 Данные</TabsTrigger>
            <TabsTrigger value="scraping">🕷️ Скрапинг</TabsTrigger>
            <TabsTrigger value="ml">🧠 ML Пайплайн</TabsTrigger>
            <TabsTrigger value="tasks">📋 Задачи</TabsTrigger>
            <TabsTrigger value="settings">⚙️ Настройки</TabsTrigger>
          </TabsList>

          {/* Tab: Данные */}
          <TabsContent value="data" className="space-y-4">
            <DataStatsCard adminKey={adminKey} />
          </TabsContent>

          {/* Tab: Скрапинг */}
          <TabsContent value="scraping" className="space-y-4">
            <ScrapePanel adminKey={adminKey} onTaskStarted={handleTaskStarted} />
            {activeTaskId && (
              <TaskLogViewer
                adminKey={adminKey}
                taskId={activeTaskId}
                onClose={() => setActiveTaskId(null)}
              />
            )}
          </TabsContent>

          {/* Tab: ML Пайплайн */}
          <TabsContent value="ml" className="space-y-4">
            <MLPipeline adminKey={adminKey} onTaskStarted={handleTaskStarted} />
            {activeTaskId && (
              <TaskLogViewer
                adminKey={adminKey}
                taskId={activeTaskId}
                onClose={() => setActiveTaskId(null)}
              />
            )}
          </TabsContent>

          {/* Tab: Задачи */}
          <TabsContent value="tasks" className="space-y-4">
            <TaskList adminKey={adminKey} onSelectTask={setActiveTaskId} refreshTrigger={refreshTrigger} />
            {activeTaskId && (
              <TaskLogViewer
                adminKey={adminKey}
                taskId={activeTaskId}
                onClose={() => setActiveTaskId(null)}
              />
            )}
          </TabsContent>

          {/* Tab: Настройки */}
          <TabsContent value="settings" className="space-y-4">
            <CookieEditor adminKey={adminKey} />
          </TabsContent>
        </Tabs>
      </main>
    </div>
  );
}
