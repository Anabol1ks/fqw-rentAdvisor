"use client";

import { useState, useEffect } from "react";
import { adminApi, ScrapeParams, ScrapeConfig } from "@/lib/admin-api";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Separator } from "@/components/ui/separator";
import { Badge } from "@/components/ui/badge";

interface ScrapePanelProps {
  adminKey: string;
  onTaskStarted: (taskId: string) => void;
}

const SCRAP_COMMANDS = [
  { id: "normalize", name: "Нормализация", desc: "Нормализация сырых данных" },
  { id: "migrate", name: "Миграция", desc: "Миграция схемы БД" },
  { id: "import-metro", name: "Импорт метро", desc: "Импорт станций метро из CSV" },
  { id: "geocode", name: "Геокодирование", desc: "Геокодирование адресов" },
  { id: "dedupe", name: "Дедупликация", desc: "Удаление дубликатов" },
  { id: "enrich", name: "Обогащение", desc: "Обогащение данных (метро, расстояния)" },
  { id: "deactivate", name: "Деактивация", desc: "Деактивация устаревших" },
  { id: "fix-seq", name: "Фикс последовательностей", desc: "Исправление PK sequences" },
  { id: "pipeline", name: "Полный пайплайн", desc: "Нормализация → Обогащение → Дедупликация" },
];

export function ScrapePanel({ adminKey, onTaskStarted }: ScrapePanelProps) {
  const [loading, setLoading] = useState(false);
  const [config, setConfig] = useState<ScrapeConfig | null>(null);

  // Форма скрапинга
  const [dealType, setDealType] = useState<"rent" | "sale">("rent");
  const [city, setCity] = useState("moskva");
  const [pages, setPages] = useState(100);
  const [maxItems, setMaxItems] = useState(1000);
  const [parallelism, setParallelism] = useState(2);
  const [delayMin, setDelayMin] = useState("1200ms");
  const [delayMax, setDelayMax] = useState("2000ms");
  const [maxEmpty, setMaxEmpty] = useState(2);
  const [maxRetries, setMaxRetries] = useState(3);
  const [captchaCd, setCaptchaCd] = useState("2m");
  const [useCookie, setUseCookie] = useState(false);

  const [cmdLoading, setCmdLoading] = useState<string | null>(null);

  useEffect(() => {
    adminApi.getScrapeConfig(adminKey).then((cfg) => {
      setConfig(cfg);
      setCity(cfg.city);
      setPages(cfg.pages);
      setMaxItems(cfg.max_items);
      setParallelism(cfg.parallelism);
      setDelayMin(cfg.delay_min);
      setDelayMax(cfg.delay_max);
      setMaxRetries(cfg.max_retries);
      setCaptchaCd(cfg.captcha_cooldown);
      setMaxEmpty(cfg.max_empty_pages);
    }).catch(() => {});
  }, [adminKey]);

  const startScrape = async () => {
    setLoading(true);
    try {
      const params: ScrapeParams = {
        deal_type: dealType,
        city,
        pages,
        max_items: maxItems,
        parallelism,
        delay_min: delayMin,
        delay_max: delayMax,
        max_empty_pages: maxEmpty,
        max_retries: maxRetries,
        captcha_cooldown: captchaCd,
        use_cookie: useCookie,
      };
      const res = await adminApi.startScrape(adminKey, params);
      onTaskStarted(res.task_id);
    } catch (err: any) {
      alert("Ошибка: " + err.message);
    } finally {
      setLoading(false);
    }
  };

  const runCommand = async (command: string) => {
    setCmdLoading(command);
    try {
      const res = await adminApi.runScrapCommand(adminKey, command);
      onTaskStarted(res.task_id);
    } catch (err: any) {
      alert("Ошибка: " + err.message);
    } finally {
      setCmdLoading(null);
    }
  };

  return (
    <div className="space-y-4">
      {/* Запуск скрапинга */}
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-lg">Запуск скрапинга</CardTitle>
          <CardDescription>Настройте параметры и запустите сбор данных с Яндекс.Недвижимость</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {/* Тип сделки */}
          <div className="flex gap-2">
            <Button
              variant={dealType === "rent" ? "default" : "outline"}
              size="sm"
              onClick={() => setDealType("rent")}
            >
              Аренда (rent)
            </Button>
            <Button
              variant={dealType === "sale" ? "default" : "outline"}
              size="sm"
              onClick={() => setDealType("sale")}
            >
              Продажа (sale)
            </Button>
          </div>

          {/* Параметры */}
          <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-3">
            <div>
              <Label className="text-xs">Город</Label>
              <Input value={city} onChange={(e) => setCity(e.target.value)} className="h-8 text-sm" />
            </div>
            <div>
              <Label className="text-xs">Страницы</Label>
              <Input type="number" value={pages} onChange={(e) => setPages(+e.target.value)} className="h-8 text-sm" />
            </div>
            <div>
              <Label className="text-xs">Макс. объявлений</Label>
              <Input type="number" value={maxItems} onChange={(e) => setMaxItems(+e.target.value)} className="h-8 text-sm" />
            </div>
            <div>
              <Label className="text-xs">Параллелизм</Label>
              <Input type="number" value={parallelism} onChange={(e) => setParallelism(+e.target.value)} className="h-8 text-sm" />
            </div>
            <div>
              <Label className="text-xs">Мин. задержка</Label>
              <Input value={delayMin} onChange={(e) => setDelayMin(e.target.value)} className="h-8 text-sm" />
            </div>
            <div>
              <Label className="text-xs">Макс. задержка</Label>
              <Input value={delayMax} onChange={(e) => setDelayMax(e.target.value)} className="h-8 text-sm" />
            </div>
            <div>
              <Label className="text-xs">Макс. пустых стр.</Label>
              <Input type="number" value={maxEmpty} onChange={(e) => setMaxEmpty(+e.target.value)} className="h-8 text-sm" />
            </div>
            <div>
              <Label className="text-xs">Макс. повторов</Label>
              <Input type="number" value={maxRetries} onChange={(e) => setMaxRetries(+e.target.value)} className="h-8 text-sm" />
            </div>
            <div>
              <Label className="text-xs">Captcha cooldown</Label>
              <Input value={captchaCd} onChange={(e) => setCaptchaCd(e.target.value)} className="h-8 text-sm" />
            </div>
          </div>

          {/* Куки */}
          <div className="flex items-center gap-2">
            <Switch checked={useCookie} onCheckedChange={setUseCookie} />
            <Label className="text-sm">Использовать куки из .env.resilient</Label>
          </div>

          <Button onClick={startScrape} disabled={loading} className="w-full">
            {loading ? "Запуск..." : `Запустить скрапинг (${dealType})`}
          </Button>
        </CardContent>
      </Card>

      {/* Команды обработки */}
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-lg">Обработка данных</CardTitle>
          <CardDescription>Команды постобработки спарсенных данных</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-2">
            {SCRAP_COMMANDS.map((cmd) => (
              <Button
                key={cmd.id}
                variant="outline"
                size="sm"
                className="justify-start h-auto py-2 px-3"
                disabled={cmdLoading === cmd.id}
                onClick={() => runCommand(cmd.id)}
              >
                <div className="text-left">
                  <div className="font-medium text-sm">
                    {cmdLoading === cmd.id ? "Запуск..." : cmd.name}
                  </div>
                  <div className="text-xs text-muted-foreground">{cmd.desc}</div>
                </div>
              </Button>
            ))}
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
