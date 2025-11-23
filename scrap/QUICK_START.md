# Краткое руководство по Resilient Scraper

## Основные возможности
Улучшенный scraper с механизмами надежности:
- ✅ Автоматический retry с exponential backoff
- ✅ Обнаружение и восстановление после капчи  
- ✅ Ротация User-Agent и прокси
- ✅ Защитный режим при ошибках
- ✅ Сохранение прогресса
- ✅ Подробная статистика

## Быстрый запуск

### 1. Настройка (отредактируйте .env.resilient)
```env
CITY=moskva
MAX_ITEMS=1000
PAGES=100
PARALLELISM=2
CAPTCHA_COOLDOWN=10m
ERROR_THRESHOLD=10
DEAL_TYPE=sale
```

### 2. Запуск с мониторингом (рекомендуется)
```cmd
monitor_scraper.bat
```

### 3. Ручной запуск
```cmd
run_resilient_scraper.bat
```

### 4. Продвинутый запуск
```cmd
scrape_yandex.exe -city=moskva -max-items=500 -captcha-cooldown=15m -error-threshold=5
```

## Ключевые параметры

- `-max-items N` - Цель по новым записям
- `-captcha-cooldown 10m` - Пауза после капчи
- `-error-threshold 10` - Лимит ошибок для защитного режима
- `-max-retries 5` - Попыток на URL
- `-user-agent-rotation` - Ротация браузеров
- `-enable-progress` - Сохранение прогресса

## Советы для максимального результата

### Для больших объемов (1000+ записей):
```cmd
scrape_yandex.exe ^
    -max-items=2000 ^
    -pages=200 ^
    -captcha-cooldown=15m ^
    -error-threshold=5 ^
    -protective-delay=60s ^
    -recovery-delay=5m
```

### При частых блокировках:
```cmd
scrape_yandex.exe ^
    -parallel=1 ^
    -delay-min=3000ms ^
    -delay-max=5000ms ^
    -captcha-cooldown=20m ^
    -user-agent-rotation=true
```

### С прокси:
```env
PROXY_LIST=http://proxy1:8080,http://proxy2:8080
```

## Мониторинг результатов

После запуска проверьте:
- Логи в консоли
- Файлы прогресса в папке `progress/`
- Базу данных на новые записи

Scraper автоматически:
- Возобновит работу после капчи
- Переключит прокси при блокировке
- Сохранит прогресс для восстановления
- Выведет подробную статистику

## При проблемах

1. Увеличьте `captcha-cooldown`
2. Уменьшите `parallelism`
3. Добавьте прокси в `PROXY_LIST`
4. Используйте legacy режим: `-legacy=true`