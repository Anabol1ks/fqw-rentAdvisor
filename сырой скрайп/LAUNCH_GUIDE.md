# 📚 Полное руководство по запуску Resilient Scraper

## 🔄 Обратная совместимость - ДА!

**Хорошие новости!** Ваша старая команда работает точно так же:

```bash
# Ваша старая команда - работает как раньше!
go run ./cmd/scrape_yandex --city=moskva --start=1 --pages=12000 --max-empty-pages=5 --parallel=3 --delay-min=2000ms --delay-max=6000ms --deal=rent --max-items=1000 --use-referer=true
```

**Что изменилось:** теперь по умолчанию используется **resilient режим** с умными retry и восстановлением, но все ваши параметры работают так же.

## 🎯 Все способы запуска

### 1. **Старый способ (go run) - РАБОТАЕТ!**
```bash
# Точно ваша команда
go run ./cmd/scrape_yandex --city=moskva --start=1 --pages=12000 --max-empty-pages=5 --parallel=3 --delay-min=2000ms --delay-max=6000ms --deal=rent --max-items=1000 --use-referer=true

# Если хотите ТОЧНО старое поведение (без resilient функций)
go run ./cmd/scrape_yandex --city=moskva --start=1 --pages=12000 --max-empty-pages=5 --parallel=3 --delay-min=2000ms --delay-max=6000ms --deal=rent --max-items=1000 --use-referer=true --legacy=true
```

### 2. **Новый способ (скомпилированный exe)**
```bash
# Сначала собираем
go build -o scrape_yandex.exe ./cmd/scrape_yandex

# Затем запускаем точно так же
.\scrape_yandex.exe --city=moskva --start=1 --pages=12000 --max-empty-pages=5 --parallel=3 --delay-min=2000ms --delay-max=6000ms --deal=rent --max-items=1000 --use-referer=true
```

### 3. **Автоматический режим (рекомендуется для больших объемов)**
```bash
# Редактируем .env.resilient под ваши настройки
# Затем запускаем автоматический мониторинг
monitor_scraper.bat
```

### 4. **Батч-скрипт с оптимальными настройками**
```bash
run_resilient_scraper.bat
```

## 🆚 Сравнение режимов

### Legacy Mode (`--legacy=true`)
- Работает **точно как раньше**
- Без retry, без восстановления после капчи
- Останавливается при первой серьезной проблеме

### Resilient Mode (по умолчанию)
- **Умные retry** с exponential backoff
- **Автоматическое восстановление** после капчи (ждет 10 минут и продолжает)
- **Защитный режим** при ошибках (замедляется, но не останавливается)
- **Ротация User-Agent** для обхода блокировок

## 🎛️ Новые полезные параметры

Добавьте к вашей команде для улучшения:

```bash
# Ваша команда + новые параметры для надежности
go run ./cmd/scrape_yandex \
    --city=moskva \
    --start=1 \
    --pages=12000 \
    --max-empty-pages=5 \
    --parallel=3 \
    --delay-min=2000ms \
    --delay-max=6000ms \
    --deal=rent \
    --max-items=1000 \
    --use-referer=true \
    --captcha-cooldown=15m \
    --max-retries=10 \
    --error-threshold=5
```

### Что означают новые параметры:
- `--captcha-cooldown=15m` - пауза после капчи (по умолчанию 10m)
- `--max-retries=10` - сколько раз пытаться повторить неудачный запрос
- `--error-threshold=5` - после скольких ошибок подряд замедлиться
- `--protective-delay=60s` - задержка в защитном режиме
- `--user-agent-rotation=true` - ротация браузеров (по умолчанию включена)

## 💡 Рекомендации для ваших параметров

Ваша команда с оптимизацией для 1000 записей аренды:

```bash
# Оптимизированная версия вашей команды
go run ./cmd/scrape_yandex \
    --city=moskva \
    --start=1 \
    --pages=12000 \
    --max-empty-pages=5 \
    --parallel=3 \
    --delay-min=2000ms \
    --delay-max=6000ms \
    --deal=rent \
    --max-items=1000 \
    --use-referer=true \
    --captcha-cooldown=12m \
    --max-retries=8 \
    --error-threshold=8 \
    --protective-delay=45s
```

## 🔧 Настройка через .env файл

Создайте файл `.env.resilient` с вашими параметрами:

```env
CITY=moskva
START_PAGE=1
PAGES=12000
MAX_ITEMS=1000
PARALLELISM=3
DEAL_TYPE=rent
DELAY_MIN=2000ms
DELAY_MAX=6000ms
MAX_EMPTY_PAGES=5
USE_REFERER=true

# Новые resilient параметры
CAPTCHA_COOLDOWN=12m
MAX_RETRIES=8
ERROR_THRESHOLD=8
PROTECTIVE_MODE_DELAY=45s
```

Тогда можно запускать просто:
```bash
go run ./cmd/scrape_yandex
# Все параметры возьмутся из .env.resilient
```

## 🚀 Какой способ выбрать?

### Для разовых тестов:
```bash
go run ./cmd/scrape_yandex --city=moskva --deal=rent --max-items=100
```

### Для сбора как раньше (ваша команда):
```bash
go run ./cmd/scrape_yandex --city=moskva --start=1 --pages=12000 --max-empty-pages=5 --parallel=3 --delay-min=2000ms --delay-max=6000ms --deal=rent --max-items=1000 --use-referer=true
```

### Для максимальной надежности и больших объемов:
```bash
# Настройте .env.resilient
# Запустите автоматический мониторинг
monitor_scraper.bat
```

## ⚠️ Важные моменты

1. **Все старые параметры работают** - ничего не сломалось
2. **По умолчанию resilient режим** - больше записей, меньше сбоев
3. **Флаг `--legacy=true`** вернет точно старое поведение
4. **Новые параметры опциональны** - можете не использовать

## 🎯 Итого

**Простой ответ:** Ваша команда работает точно так же, но теперь scraper стал намного умнее и надежнее!

```bash
# Ваша команда - работает как раньше, но лучше!
go run ./cmd/scrape_yandex --city=moskva --start=1 --pages=12000 --max-empty-pages=5 --parallel=3 --delay-min=2000ms --delay-max=6000ms --deal=rent --max-items=1000 --use-referer=true
```