'use client';

import { useEffect, useState } from 'react';
import { Activity } from 'lucide-react';
import { api } from '@/lib/api';
import { cn } from '@/lib/utils';

export function MLStatusIndicator() {
  const [status, setStatus] = useState<'checking' | 'online' | 'offline'>('checking');

  useEffect(() => {
    const checkStatus = async () => {
      const isHealthy = await api.checkMLHealth();
      setStatus(isHealthy ? 'online' : 'offline');
    };

    checkStatus();
    const interval = setInterval(checkStatus, 30000);

    return () => clearInterval(interval);
  }, []);

  return (
    <div className="flex items-center gap-2 px-3 py-1.5 rounded-md bg-background/50 border">
      <Activity className={cn(
        'h-4 w-4',
        status === 'online' && 'text-green-500',
        status === 'offline' && 'text-red-500',
        status === 'checking' && 'text-yellow-500'
      )} />
      <span className="text-sm">
        ML сервис: {status === 'online' ? 'В сети' : status === 'offline' ? 'Отключен' : 'Проверка...'}
      </span>
    </div>
  );
}
