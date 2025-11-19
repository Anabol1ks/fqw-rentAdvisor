'use client';

import { useEffect, useState } from 'react';
import { Card, CardContent } from '@/components/ui/card';
import { BrainCircuit, Sparkles, TrendingUp, MapPin, Home } from 'lucide-react';

const loadingMessages = [
  'Анализируем данные о рынке аренды...',
  'Обрабатываем геолокацию объекта...',
  'Сравниваем с аналогичными квартирами...',
  'Рассчитываем рыночную стоимость...',
  'Строим прогнозную модель...',
  'Генерируем отчет и рекомендации...',
];

const icons = [BrainCircuit, Sparkles, TrendingUp, MapPin, Home];

export function AILoading() {
  const [messageIndex, setMessageIndex] = useState(0);
  const [IconComponent, setIconComponent] = useState(() => icons[0]);

  useEffect(() => {
    const interval = setInterval(() => {
      setMessageIndex((prev) => {
        const next = (prev + 1) % loadingMessages.length;
        setIconComponent(() => icons[next % icons.length]);
        return next;
      });
    }, 2000);

    return () => clearInterval(interval);
  }, []);

  return (
    <Card className="border-2 border-primary/20">
      <CardContent className="flex flex-col items-center justify-center py-12 space-y-6">
        {/* Animated AI Icon */}
        <div className="relative">
          <div className="absolute inset-0 animate-ping">
            <div className="h-20 w-20 rounded-full bg-primary/20" />
          </div>
          <div className="relative h-20 w-20 rounded-full bg-primary/10 flex items-center justify-center animate-pulse">
            <IconComponent className="h-10 w-10 text-primary animate-bounce" />
          </div>
        </div>

        {/* Loading Message */}
        <div className="text-center space-y-2">
          <h3 className="text-lg font-semibold">Нейросеть работает...</h3>
          <p className="text-sm text-muted-foreground animate-pulse">
            {loadingMessages[messageIndex]}
          </p>
        </div>

        {/* Progress Dots */}
        <div className="flex gap-2">
          {[0, 1, 2, 3, 4].map((i) => (
            <div
              key={i}
              className={`h-2 w-2 rounded-full transition-all duration-300 ${
                i === messageIndex % 5
                  ? 'bg-primary scale-150'
                  : 'bg-primary/30'
              }`}
              style={{
                animationDelay: `${i * 0.1}s`,
              }}
            />
          ))}
        </div>

        {/* Neural Network Animation */}
        <div className="relative w-48 h-12">
          <div className="absolute inset-0 flex items-center justify-between">
            {[...Array(5)].map((_, i) => (
              <div
                key={i}
                className="relative"
                style={{
                  animationDelay: `${i * 0.2}s`,
                }}
              >
                <div className="h-3 w-3 rounded-full bg-primary/60 animate-pulse" />
                {i < 4 && (
                  <div
                    className="absolute top-1/2 left-3 w-8 h-0.5 bg-gradient-to-r from-primary/60 to-primary/20"
                    style={{
                      animation: 'pulse 1s ease-in-out infinite',
                      animationDelay: `${i * 0.2}s`,
                    }}
                  />
                )}
              </div>
            ))}
          </div>
        </div>

        <p className="text-xs text-muted-foreground">
          Это может занять несколько секунд
        </p>
      </CardContent>
    </Card>
  );
}
