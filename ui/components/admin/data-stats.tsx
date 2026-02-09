"use client";

import { useState, useEffect, useCallback } from "react";
import { adminApi, DataStats } from "@/lib/admin-api";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";

interface DataStatsCardProps {
  adminKey: string;
}

export function DataStatsCard({ adminKey }: DataStatsCardProps) {
  const [stats, setStats] = useState<DataStats | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  const loadStats = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const data = await adminApi.getDataStats(adminKey);
      setStats(data);
    } catch (err: any) {
      setError(err.message);
    } finally {
      setLoading(false);
    }
  }, [adminKey]);

  useEffect(() => {
    loadStats();
  }, [loadStats]);

  return (
    <Card>
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          <CardTitle className="text-lg">Данные в базе</CardTitle>
          <Button variant="outline" size="sm" onClick={loadStats} disabled={loading}>
            {loading ? "Загрузка..." : "Обновить"}
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        {error && (
          <p className="text-sm text-red-500 mb-3">{error}</p>
        )}
        {stats ? (
          <div className="grid grid-cols-2 md:grid-cols-3 gap-4">
            <StatBox label="Аренда (активные)" value={stats.rent_count} color="text-blue-600" />
            <StatBox label="Продажа (активные)" value={stats.sale_count} color="text-emerald-600" />
            <StatBox label="Всего активных" value={stats.total} color="text-purple-600" />

            <StatBox label="Аренда (сырые)" value={stats.rent_raw_count} color="text-blue-400" />
            <StatBox label="Продажа (сырые)" value={stats.sale_raw_count} color="text-emerald-400" />
            <StatBox label="Всего сырых" value={stats.total_raw} color="text-purple-400" />
          </div>
        ) : (
          !loading && <p className="text-sm text-muted-foreground">Нет данных</p>
        )}
      </CardContent>
    </Card>
  );
}

function StatBox({ label, value, color }: { label: string; value: number; color: string }) {
  return (
    <div className="rounded-lg border p-3 text-center">
      <p className={`text-2xl font-bold ${color}`}>
        {value.toLocaleString("ru-RU")}
      </p>
      <p className="text-xs text-muted-foreground mt-1">{label}</p>
    </div>
  );
}
