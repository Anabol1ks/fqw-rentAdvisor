'use client';

import { useState } from 'react';
import { MLStatusIndicator } from '@/components/ml-status-indicator';
import { ValuationForm } from '@/components/valuation-form';
import { ValuationResult } from '@/components/valuation-result';
import { ValuationList } from '@/components/valuation-list';
import { AILoading } from '@/components/ai-loading';
import { LoadingSkeleton } from '@/components/loading-skeleton';
import { AddressValuationResponse } from '@/lib/api';
import { Button } from '@/components/ui/button';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { BrainCircuit, List } from 'lucide-react';
import { Toaster } from 'sonner';

export default function Home() {
  const [result, setResult] = useState<AddressValuationResponse | null>(null);
  const [activeTab, setActiveTab] = useState('form');
  const [isLoading, setIsLoading] = useState(false);

  const handleValuationSuccess = (response: AddressValuationResponse) => {
    setResult(response);
    setIsLoading(false);
  };

  const handleValuationStart = () => {
    setIsLoading(true);
    setResult(null);
  };

  return (
    <>
      <Toaster position="top-right" />
      <div className="min-h-screen bg-background relative">
        {/* Dotted background pattern */}
        <div
          className="absolute inset-0 -z-10 opacity-100"
          style={{
            backgroundImage:
              'radial-gradient(circle, rgba(0, 0, 0, 0.12) 1px, transparent 1px)',
            backgroundSize: '20px 20px',
          }}
        />
        <header className="border-b relative z-10">
          <div className="container mx-auto px-4 py-4">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-3">
                <BrainCircuit className="h-8 w-8 text-primary" />
                <div>
                  <h1 className="text-2xl font-bold">RentAdvisor</h1>
                  <p className="text-sm text-muted-foreground">
                    Нейросетевой прогноз стоимости аренды
                  </p>
                </div>
              </div>
              <MLStatusIndicator />
            </div>
          </div>
        </header>

        <main className="container mx-auto px-4 py-8 relative z-10">
          <div className="max-w-6xl mx-auto space-y-8">
            <section className="text-center space-y-4">
              <h2 className="text-3xl font-bold tracking-tight">
                Оценка рыночной стоимости аренды квартир в Москве
              </h2>
              <p className="text-lg text-muted-foreground max-w-3xl mx-auto">
                RentAdvisor использует современные алгоритмы машинного обучения для точного прогнозирования
                стоимости аренды недвижимости. Система анализирует множество факторов: расположение,
                площадь, состояние квартиры, близость к метро и другие параметры для расчета оптимальной
                рыночной ставки.
              </p>
              <div className="grid grid-cols-1 md:grid-cols-3 gap-6 pt-6">
                <div className="p-6 rounded-lg border bg-card">
                  <h3 className="font-semibold mb-2">Точный прогноз</h3>
                  <p className="text-sm text-muted-foreground">
                    Нейронная сеть обучена на тысячах объявлений о аренде в Москве
                  </p>
                </div>
                <div className="p-6 rounded-lg border bg-card">
                  <h3 className="font-semibold mb-2">Диапазон уверенности</h3>
                  <p className="text-sm text-muted-foreground">
                    Получите не только точечный прогноз, но и доверительный интервал цены
                  </p>
                </div>
                <div className="p-6 rounded-lg border bg-card">
                  <h3 className="font-semibold mb-2">Сопоставимые объекты</h3>
                  <p className="text-sm text-muted-foreground">
                    Сравните прогноз с реальными предложениями на рынке
                  </p>
                </div>
              </div>
              <p className="text-xs text-muted-foreground pt-4">
                Дипломный проект РТУ МИРЭА | Кафедра Вычислительной техники
              </p>
            </section>

            <Tabs value={activeTab} onValueChange={setActiveTab} className="w-full">
              <TabsList className="grid w-full max-w-md mx-auto grid-cols-2">
                <TabsTrigger value="form">
                  <BrainCircuit className="mr-2 h-4 w-4" />
                  Новая оценка
                </TabsTrigger>
                <TabsTrigger value="list">
                  <List className="mr-2 h-4 w-4" />
                  Общий список
                </TabsTrigger>
              </TabsList>

              <TabsContent value="form" className="space-y-6">
                {!isLoading && !result && (
                  <ValuationForm
                    onSuccess={handleValuationSuccess}
                    onLoadingStart={handleValuationStart}
                  />
                )}
                {isLoading && <AILoading />}
                {result && !isLoading && (
                  <div className="space-y-4">
                    <ValuationResult data={result} />
                    <div className="flex justify-center">
                      <Button
                        variant="outline"
                        onClick={() => {
                          setResult(null);
                          setIsLoading(false);
                        }}
                      >
                        Создать новую оценку
                      </Button>
                    </div>
                  </div>
                )}
              </TabsContent>

              <TabsContent value="list">
                <ValuationList />
              </TabsContent>
            </Tabs>
          </div>
        </main>

        <footer className="border-t mt-16">
          <div className="container mx-auto px-4 py-6">
            <p className="text-center text-sm text-muted-foreground">
              © 2026 RentAdvisor | РТУ МИРЭА | Все права защищены
            </p>
          </div>
        </footer>
      </div>
    </>
  );
}
