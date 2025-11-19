'use client';

import { useEffect, useState } from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { api, ValuationListItem } from '@/lib/api';
import { Loader2, FileText } from 'lucide-react';
import { toast } from 'sonner';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { ValuationResult } from './valuation-result';

export function ValuationList() {
  const [loading, setLoading] = useState(false);
  const [items, setItems] = useState<ValuationListItem[]>([]);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [selectedData, setSelectedData] = useState<any>(null);

  const loadList = async () => {
    setLoading(true);
    try {
      const response = await api.getValuationList(20, 0);
      setItems(response.items);
    } catch (error) {
      toast.error('Ошибка при загрузке списка');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadList();
  }, []);

  const handleViewDetails = async (id: string) => {
    try {
      const data = await api.getValuationById(id);
      setSelectedData(data);
      setSelectedId(id);
    } catch (error) {
      toast.error('Ошибка при загрузке данных');
    }
  };

  const formatPrice = (price: number) => {
    return new Intl.NumberFormat('ru-RU', {
      style: 'currency',
      currency: 'RUB',
      maximumFractionDigits: 0,
    }).format(price);
  };

  const formatDate = (dateString: string) => {
    return new Date(dateString).toLocaleDateString('ru-RU', {
      year: 'numeric',
      month: 'long',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    });
  };

  if (loading) {
    return (
      <Card>
        <CardContent className="flex items-center justify-center py-12">
          <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
        </CardContent>
      </Card>
    );
  }

  return (
    <>
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <CardTitle>Сохраненные отчеты</CardTitle>
            <Button variant="outline" size="sm" onClick={loadList}>
              Обновить
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          {items.length === 0 ? (
            <p className="text-center text-muted-foreground py-8">
              Нет сохраненных отчетов
            </p>
          ) : (
            <div className="space-y-3">
              {items.map((item) => (
                <div
                  key={item.id}
                  className="p-4 rounded-lg border bg-card hover:bg-accent/50 transition-colors cursor-pointer"
                  onClick={() => handleViewDetails(item.id)}
                >
                  <div className="flex justify-between items-start gap-4">
                    <div className="flex-1 space-y-1">
                      <div className="flex items-center gap-2">
                        <FileText className="h-4 w-4 text-muted-foreground" />
                        <p className="font-medium">{item.address}</p>
                      </div>
                      <p className="text-sm text-muted-foreground">{item.city}</p>
                      {item.summary_short && (
                        <p className="text-xs text-muted-foreground line-clamp-2">
                          {item.summary_short}
                        </p>
                      )}
                      <p className="text-xs text-muted-foreground">
                        {formatDate(item.created_at)}
                      </p>
                    </div>
                    <div className="text-right space-y-1">
                      <p className="text-lg font-bold text-primary">
                        {formatPrice(item.prediction_rub)}
                      </p>
                      <p className="text-xs text-muted-foreground">
                        {formatPrice(item.interval_low_rub)} — {formatPrice(item.interval_high_rub)}
                      </p>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      <Dialog open={!!selectedId} onOpenChange={() => setSelectedId(null)}>
        <DialogContent className="max-w-4xl max-h-[90vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>Детали отчета</DialogTitle>
          </DialogHeader>
          {selectedData && <ValuationResult data={selectedData} />}
        </DialogContent>
      </Dialog>
    </>
  );
}
