"use client";

import { useState, useEffect, useCallback } from "react";
import { adminApi, TaskInfo } from "@/lib/admin-api";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";

interface TaskListProps {
  adminKey: string;
  onSelectTask: (taskId: string) => void;
  refreshTrigger?: number;
}

function statusBadge(status: string) {
  const map: Record<string, { label: string; variant: "default" | "secondary" | "destructive" | "outline" }> = {
    running: { label: "Выполняется", variant: "default" },
    done: { label: "Завершено", variant: "secondary" },
    failed: { label: "Ошибка", variant: "destructive" },
    stopped: { label: "Остановлено", variant: "outline" },
    pending: { label: "Ожидание", variant: "outline" },
  };
  const s = map[status] || { label: status, variant: "outline" as const };
  return <Badge variant={s.variant}>{s.label}</Badge>;
}

function timeSince(dateStr: string): string {
  const d = new Date(dateStr);
  const now = new Date();
  const secs = Math.floor((now.getTime() - d.getTime()) / 1000);
  if (secs < 60) return `${secs}с назад`;
  if (secs < 3600) return `${Math.floor(secs / 60)}м назад`;
  if (secs < 86400) return `${Math.floor(secs / 3600)}ч назад`;
  return d.toLocaleString("ru-RU");
}

function duration(startStr: string, endStr?: string): string {
  const start = new Date(startStr);
  const end = endStr ? new Date(endStr) : new Date();
  const secs = Math.floor((end.getTime() - start.getTime()) / 1000);
  if (secs < 60) return `${secs}с`;
  if (secs < 3600) return `${Math.floor(secs / 60)}м ${secs % 60}с`;
  return `${Math.floor(secs / 3600)}ч ${Math.floor((secs % 3600) / 60)}м`;
}

export function TaskList({ adminKey, onSelectTask, refreshTrigger }: TaskListProps) {
  const [tasks, setTasks] = useState<TaskInfo[]>([]);
  const [loading, setLoading] = useState(false);

  const loadTasks = useCallback(async () => {
    setLoading(true);
    try {
      const data = await adminApi.listTasks(adminKey, 30);
      setTasks(data.tasks || []);
    } catch (err: any) {
      console.error(err);
    } finally {
      setLoading(false);
    }
  }, [adminKey]);

  useEffect(() => {
    loadTasks();
  }, [loadTasks, refreshTrigger]);

  // Автообновление для running задач
  useEffect(() => {
    const hasRunning = tasks.some((t) => t.status === "running");
    if (!hasRunning) return;

    const interval = setInterval(loadTasks, 5000);
    return () => clearInterval(interval);
  }, [tasks, loadTasks]);

  return (
    <Card>
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          <CardTitle className="text-lg">История задач</CardTitle>
          <Button variant="outline" size="sm" onClick={loadTasks} disabled={loading}>
            Обновить
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        {tasks.length === 0 ? (
          <p className="text-sm text-muted-foreground text-center py-4">
            Задач пока нет
          </p>
        ) : (
          <div className="space-y-1">
            {tasks.map((task) => (
              <div
                key={task.id}
                className="flex items-center justify-between p-2 rounded-md hover:bg-muted/50 cursor-pointer transition-colors"
                onClick={() => onSelectTask(task.id)}
              >
                <div className="flex items-center gap-3 min-w-0">
                  {task.status === "running" && (
                    <div className="w-2 h-2 bg-blue-500 rounded-full animate-pulse flex-shrink-0" />
                  )}
                  <div className="min-w-0">
                    <p className="text-sm font-medium truncate">{task.name}</p>
                    <p className="text-xs text-muted-foreground">
                      {timeSince(task.started_at)} • {duration(task.started_at, task.ended_at)}
                    </p>
                  </div>
                </div>
                <div className="flex-shrink-0 ml-2">
                  {statusBadge(task.status)}
                </div>
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
