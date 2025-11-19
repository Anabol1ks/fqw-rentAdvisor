'use client';

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { AddressValuationResponse, api } from '@/lib/api';
import { Download, ExternalLink, MapPin } from 'lucide-react';
import { Separator } from '@/components/ui/separator';

interface ValuationResultProps {
  data: AddressValuationResponse;
}

export function ValuationResult({ data }: ValuationResultProps) {
  const handleDownloadPdf = () => {
    const url = api.getValuationPdfUrl(data.report_id);
    window.open(url, '_blank');
  };

  const formatPrice = (price: number) => {
    return new Intl.NumberFormat('ru-RU', {
      style: 'currency',
      currency: 'RUB',
      maximumFractionDigits: 0,
    }).format(price);
  };

  const formatRooms = (rooms: number) => {
    return rooms === 0 ? 'студия' : `${rooms} комн.`;
  };

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader>
          <CardTitle>Результат оценки</CardTitle>
          <CardDescription>
            {data.object.address}, {data.object.city}
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-6">
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            <div className="space-y-2 p-4 rounded-lg bg-muted/50">
              <p className="text-sm text-muted-foreground">Прогноз стоимости</p>
              <p className="text-2xl font-bold text-primary">
                {formatPrice(data.price.prediction_rub)}
              </p>
              <p className="text-xs text-muted-foreground">в месяц</p>
            </div>

            <div className="space-y-2 p-4 rounded-lg bg-muted/50">
              <p className="text-sm text-muted-foreground">Минимум диапазона</p>
              <p className="text-2xl font-bold">
                {formatPrice(data.price.interval_low_rub)}
              </p>
              <p className="text-xs text-muted-foreground">в месяц</p>
            </div>

            <div className="space-y-2 p-4 rounded-lg bg-muted/50">
              <p className="text-sm text-muted-foreground">Максимум диапазона</p>
              <p className="text-2xl font-bold">
                {formatPrice(data.price.interval_high_rub)}
              </p>
              <p className="text-xs text-muted-foreground">в месяц</p>
            </div>
          </div>

          <Separator />

          <div className="space-y-4">
            <h3 className="text-lg font-semibold">Характеристики объекта</h3>
            <div className="grid grid-cols-2 md:grid-cols-3 gap-4">
              <div>
                <p className="text-sm text-muted-foreground">Комнат</p>
                <p className="font-medium">{formatRooms(data.object.rooms)}</p>
              </div>
              <div>
                <p className="text-sm text-muted-foreground">Площадь</p>
                <p className="font-medium">{data.object.area_total} м²</p>
              </div>
              {data.object.floor && (
                <div>
                  <p className="text-sm text-muted-foreground">Этаж</p>
                  <p className="font-medium">
                    {data.object.floor} из {data.object.floors_total || '—'}
                  </p>
                </div>
              )}
              {data.object.year_built && (
                <div>
                  <p className="text-sm text-muted-foreground">Год постройки</p>
                  <p className="font-medium">{data.object.year_built}</p>
                </div>
              )}
              {data.object.house_material && (
                <div>
                  <p className="text-sm text-muted-foreground">Материал дома</p>
                  <p className="font-medium">{data.object.house_material}</p>
                </div>
              )}
            </div>
          </div>

          {data.text && (
            <>
              <Separator />
              <div className="space-y-4">
                <h3 className="text-lg font-semibold">Анализ</h3>
                <div className="space-y-2">
                  <p className="text-sm">{data.text.summary_short}</p>
                  {data.text.summary_long && (
                    <p className="text-sm text-muted-foreground">{data.text.summary_long}</p>
                  )}
                </div>
                {data.text.factors_summary && data.text.factors_summary.length > 0 && (
                  <div className="space-y-2">
                    <h4 className="text-sm font-semibold">Ключевые факторы:</h4>
                    <ul className="list-disc list-inside space-y-1">
                      {data.text.factors_summary.map((factor, index) => (
                        <li key={index} className="text-sm text-muted-foreground">
                          {factor}
                        </li>
                      ))}
                    </ul>
                  </div>
                )}
              </div>
            </>
          )}

          {data.comparables && data.comparables.length > 0 && (
            <>
              <Separator />
              <div className="space-y-4">
                <h3 className="text-lg font-semibold">Сопоставимые объекты</h3>
                <div className="space-y-3">
                  {data.comparables.slice(0, 5).map((comp, index) => (
                    <div
                      key={index}
                      className="p-3 rounded-lg border bg-card hover:bg-accent/50 transition-colors"
                    >
                      <div className="flex justify-between items-start gap-4">
                        <div className="flex-1 space-y-1">
                          <div className="flex items-center gap-2">
                            <MapPin className="h-4 w-4 text-muted-foreground" />
                            <p className="text-sm font-medium">{comp.metro_station}</p>
                            <span className="text-xs text-muted-foreground">
                              ({comp.distance_km.toFixed(2)} км)
                            </span>
                          </div>
                          <p className="text-sm text-muted-foreground">
                            {comp.rooms} комн., {comp.area_total} м²
                          </p>
                        </div>
                        <div className="flex items-center gap-3">
                          <p className="text-sm font-bold">{formatPrice(comp.price_rub)}</p>
                          {comp.url && (
                            <Button variant="ghost" size="sm" asChild>
                              <a href={comp.url} target="_blank" rel="noopener noreferrer">
                                <ExternalLink className="h-4 w-4" />
                              </a>
                            </Button>
                          )}
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            </>
          )}

          <Button onClick={handleDownloadPdf} className="w-full">
            <Download className="mr-2 h-4 w-4" />
            Скачать PDF отчет
          </Button>
        </CardContent>
      </Card>
    </div>
  );
}
