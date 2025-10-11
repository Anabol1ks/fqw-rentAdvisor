# 🚀 Шпаргалка по запуску Scraper

## ✅ Ваша старая команда - РАБОТАЕТ!

```bash
# Точно ваша команда - все работает как раньше + улучшения
go run ./cmd/scrape_yandex --city=moskva --start=1 --pages=12000 --max-empty-pages=5 --parallel=3 --delay-min=2000ms --delay-max=6000ms --deal=rent --max-items=1000 --use-referer=true
```

## 🎯 Быстрые команды

### Для аренды в Москве (ваш случай):
```bash
go run ./cmd/scrape_yandex --city=moskva --deal=rent --max-items=1000 --pages=12000 --parallel=3 --delay-min=2000ms --delay-max=6000ms
```

### Для продажи в СПб:
```bash
go run ./cmd/scrape_yandex --city=peterburg --deal=sale --max-items=500 --pages=5000 --parallel=2
```

### Точно как раньше (без улучшений):
```bash
go run ./cmd/scrape_yandex --city=moskva --deal=rent --max-items=1000 --legacy=true
```

### Максимальная надежность:
```bash
go run ./cmd/scrape_yandex --city=moskva --deal=rent --max-items=1000 --captcha-cooldown=20m --max-retries=10 --error-threshold=3
```

## 📋 Основные параметры

| Параметр | Описание | Пример |
|----------|----------|---------|
| `--city` | Город | `moskva`, `peterburg` |
| `--deal` | Тип сделки | `sale`, `rent` |
| `--max-items` | Цель по записям | `1000` |
| `--pages` | Страниц обойти | `12000` |
| `--parallel` | Потоков | `3` |
| `--delay-min/max` | Задержки | `2000ms`, `6000ms` |

## 🛡️ Новые параметры надежности

| Параметр | По умолчанию | Для чего |
|----------|--------------|----------|
| `--captcha-cooldown` | `10m` | Пауза после капчи |
| `--max-retries` | `5` | Попыток на URL |
| `--error-threshold` | `10` | Лимит ошибок |
| `--legacy` | `false` | Старое поведение |

## 🎛️ Примеры оптимизации вашей команды

### Ваша команда + небольшие улучшения:
```bash
go run ./cmd/scrape_yandex --city=moskva --start=1 --pages=12000 --max-empty-pages=5 --parallel=3 --delay-min=2000ms --delay-max=6000ms --deal=rent --max-items=1000 --use-referer=true --captcha-cooldown=15m
```

### Ваша команда + максимальная надежность:
```bash
go run ./cmd/scrape_yandex --city=moskva --start=1 --pages=12000 --max-empty-pages=5 --parallel=3 --delay-min=2000ms --delay-max=6000ms --deal=rent --max-items=1000 --use-referer=true --captcha-cooldown=20m --max-retries=10 --error-threshold=5
```

## 🔄 Автоматический режим

Если хотите "поставил и забыл":
1. Настройте `.env.resilient`
2. Запустите `monitor_scraper.bat`

## 💡 Главное

**Ваша команда работает точно так же!** Просто теперь scraper стал умнее:
- Переживает капчи и блокировки
- Автоматически повторяет неудачные запросы  
- Собирает больше записей за сессию
- Не падает от ошибок