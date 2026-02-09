"use client";

import { useState } from "react";
import { adminApi } from "@/lib/admin-api";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Separator } from "@/components/ui/separator";

interface MLPipelineProps {
  adminKey: string;
  onTaskStarted: (taskId: string) => void;
}

const PIPELINE_STEPS = [
  {
    id: "export",
    name: "1. Экспорт из БД",
    desc: "Выгрузка данных аренды из PostgreSQL в Parquet",
    icon: "📥",
  },
  {
    id: "ingest",
    name: "2. Загрузка и очистка",
    desc: "Импорт, очистка аномалий, приведение типов",
    icon: "🧹",
  },
  {
    id: "features",
    name: "3. Генерация признаков",
    desc: "H3-индексы, rooms_bucket, локальная статистика",
    icon: "⚙️",
  },
  {
    id: "split",
    name: "4. Разделение данных",
    desc: "Split на train/valid по времени (15 дней, 30%)",
    icon: "✂️",
  },
  {
    id: "train",
    name: "5. Обучение модели",
    desc: "Обучение LightGBM с интервалами и locstat",
    icon: "🧠",
  },
  {
    id: "predict",
    name: "6. Тестовый прогноз",
    desc: "Прогноз на sample_object.json для проверки",
    icon: "🎯",
  },
];

export function MLPipeline({ adminKey, onTaskStarted }: MLPipelineProps) {
  const [loading, setLoading] = useState<string | null>(null);
  const [restartLoading, setRestartLoading] = useState(false);
  const [fullPipelineLoading, setFullPipelineLoading] = useState(false);

  const runStep = async (command: string) => {
    setLoading(command);
    try {
      const res = await adminApi.runMLCommand(adminKey, command);
      onTaskStarted(res.task_id);
    } catch (err: any) {
      alert("Ошибка: " + err.message);
    } finally {
      setLoading(null);
    }
  };

  const runFullPipeline = async () => {
    setFullPipelineLoading(true);
    try {
      // Запускаем шаги последовательно — первый шаг  
      const res = await adminApi.runMLCommand(adminKey, "export");
      onTaskStarted(res.task_id);
      alert("Запущен шаг 'Экспорт'. После его завершения запустите следующие шаги последовательно.");
    } catch (err: any) {
      alert("Ошибка: " + err.message);
    } finally {
      setFullPipelineLoading(false);
    }
  };

  const restartServer = async () => {
    setRestartLoading(true);
    try {
      const res = await adminApi.restartMLServer(adminKey);
      onTaskStarted(res.task_id);
    } catch (err: any) {
      alert("Ошибка: " + err.message);
    } finally {
      setRestartLoading(false);
    }
  };

  return (
    <div className="space-y-4">
      {/* Pipeline Steps */}
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-lg">ML Пайплайн (RealVal)</CardTitle>
          <CardDescription>
            Последовательные шаги подготовки и обучения модели LightGBM для оценки аренды
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-2">
          {PIPELINE_STEPS.map((step, idx) => (
            <div key={step.id}>
              <div className="flex items-center justify-between py-2">
                <div className="flex items-center gap-3">
                  <span className="text-xl">{step.icon}</span>
                  <div>
                    <p className="font-medium text-sm">{step.name}</p>
                    <p className="text-xs text-muted-foreground">{step.desc}</p>
                  </div>
                </div>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={loading === step.id}
                  onClick={() => runStep(step.id)}
                >
                  {loading === step.id ? "Запуск..." : "Запустить"}
                </Button>
              </div>
              {idx < PIPELINE_STEPS.length - 1 && (
                <div className="ml-4 border-l-2 border-dashed border-muted h-2" />
              )}
            </div>
          ))}
        </CardContent>
      </Card>

      {/* Server Management */}
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-lg">ML Сервер</CardTitle>
          <CardDescription>
            Управление FastAPI сервером (uvicorn) для прогнозирования
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="flex gap-3">
            <Button
              variant="default"
              onClick={restartServer}
              disabled={restartLoading}
              className="flex-1"
            >
              {restartLoading ? "Перезапуск..." : "🔄 Перезапустить ML-сервер"}
            </Button>
          </div>
          <p className="text-xs text-muted-foreground mt-2">
            Перезапускает uvicorn с моделью из models/artefacts/rent_lgbm.joblib
          </p>
        </CardContent>
      </Card>
    </div>
  );
}
