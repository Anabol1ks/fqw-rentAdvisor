"use client";

import { useState, useEffect, useRef, useCallback } from "react";
import { adminApi, TaskInfo } from "@/lib/admin-api";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";

interface TaskLogViewerProps {
  adminKey: string;
  taskId: string | null;
  onClose: () => void;
}

function statusColor(status: string) {
  switch (status) {
    case "running":
      return "bg-blue-500";
    case "done":
      return "bg-green-500";
    case "failed":
      return "bg-red-500";
    case "stopped":
      return "bg-yellow-500";
    default:
      return "bg-gray-500";
  }
}

function statusLabel(status: string) {
  switch (status) {
    case "running":
      return "Выполняется";
    case "done":
      return "Завершено";
    case "failed":
      return "Ошибка";
    case "stopped":
      return "Остановлено";
    case "pending":
      return "Ожидание";
    default:
      return status;
  }
}

export function TaskLogViewer({ adminKey, taskId, onClose }: TaskLogViewerProps) {
  const [task, setTask] = useState<TaskInfo | null>(null);
  const [logs, setLogs] = useState<string[]>([]);
  const [autoScroll, setAutoScroll] = useState(true);
  const scrollRef = useRef<HTMLDivElement>(null);
  const eventSourceRef = useRef<EventSource | null>(null);

  const scrollToBottom = useCallback(() => {
    if (autoScroll && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [autoScroll]);

  useEffect(() => {
    if (!taskId) return;

    // Загружаем текущее состояние
    adminApi.getTask(adminKey, taskId).then((data) => {
      setTask(data.task);
      setLogs(data.logs || []);
    });

    // Подключаем SSE
    const url = adminApi.taskLogsURL(adminKey, taskId);
    const es = new EventSource(url);
    eventSourceRef.current = es;

    es.onmessage = (e) => {
      setLogs((prev) => [...prev, e.data]);
    };

    es.addEventListener("done", (e) => {
      es.close();
      // Обновляем статус задачи
      adminApi.getTask(adminKey, taskId).then((data) => {
        setTask(data.task);
      });
    });

    es.onerror = () => {
      es.close();
    };

    return () => {
      es.close();
    };
  }, [taskId, adminKey]);

  useEffect(() => {
    scrollToBottom();
  }, [logs, scrollToBottom]);

  const handleStop = async () => {
    if (!taskId) return;
    try {
      await adminApi.stopTask(adminKey, taskId);
      setTask((prev) => (prev ? { ...prev, status: "stopped" } : prev));
    } catch (err: any) {
      console.error(err);
    }
  };

  if (!taskId) return null;

  return (
    <Card className="mt-4">
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <CardTitle className="text-base">
              {task?.name || "Задача"}
            </CardTitle>
            {task && (
              <Badge variant="outline" className={`${statusColor(task.status)} text-white border-0`}>
                {statusLabel(task.status)}
              </Badge>
            )}
          </div>
          <div className="flex gap-2">
            {task?.status === "running" && (
              <Button variant="destructive" size="sm" onClick={handleStop}>
                Остановить
              </Button>
            )}
            <Button variant="outline" size="sm" onClick={onClose}>
              Закрыть
            </Button>
          </div>
        </div>
        {task?.command && (
          <p className="text-xs text-muted-foreground font-mono mt-1 truncate">
            {task.command}
          </p>
        )}
      </CardHeader>
      <CardContent>
        <div
          ref={scrollRef}
          className="bg-zinc-950 text-green-400 font-mono text-xs p-3 rounded-md h-[400px] overflow-y-auto"
        >
          {logs.length === 0 ? (
            <p className="text-zinc-500">Ожидание вывода...</p>
          ) : (
            logs.map((line, i) => (
              <div key={i} className={`py-0.5 ${line.startsWith("[system]") ? "text-yellow-400" : ""}`}>
                {line}
              </div>
            ))
          )}
        </div>
        <div className="flex items-center gap-2 mt-2">
          <label className="flex items-center gap-1.5 text-xs text-muted-foreground cursor-pointer">
            <input
              type="checkbox"
              checked={autoScroll}
              onChange={(e) => setAutoScroll(e.target.checked)}
              className="rounded"
            />
            Автопрокрутка
          </label>
          <span className="text-xs text-muted-foreground ml-auto">
            {logs.length} строк
          </span>
        </div>
      </CardContent>
    </Card>
  );
}
