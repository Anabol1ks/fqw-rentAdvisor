x# 🏠 Автодополнение адресов

## Описание функционала

Система предоставляет автодополнение при вводе адреса с использованием геокодеров:

- ✅ **Yandex Geocoder API** - рекомендуемый вариант для России
- ✅ **Nominatim (OpenStreetMap)** - бесплатная альтернатива без API ключа

### ⚠️ Важное ограничение

**Yandex Geocoder API не предоставляет данные о доме** (этажность, год постройки, материал).

Согласно [официальной документации](https://yandex.ru/maps-api/docs/geocoder-api/response.html), API возвращает только:
- Адрес (formatted)
- Описание (description) 
- Координаты (Point.pos)
- Метаданные адреса (Components)

Для получения данных о зданиях нужны другие API:
- 🔹 **Yandex Maps Places API** (коммерческий)
- 🔹 **Overpass API** (OpenStreetMap)
- 🔹 **Росреестр API** (если доступен)

## Источники данных

### 1. Yandex Geocoder API (рекомендуется)
- **Ключ**: Получить на https://developer.tech.yandex.ru/
- **Преимущества**: Более точные данные для России
- **Настройка**: Установить `YANDEX_GEO_API_KEY` в `go_back/.env`
- **Документация**: https://yandex.ru/maps-api/docs/geocoder-api/

### 2. Nominatim (OpenStreetMap) - бесплатная альтернатива
- **Преимущества**: Бесплатно, без ключа API
- **Использование**: Автоматически, если Yandex ключ не указан

## Как это работает

### Backend (Go)

1. **Структуры данных** (`go_back/internal/geocode/yandex.go`):
```go
type AddressSuggestion struct {
    Address     string  `json:"address"`
    Description string  `json:"description"`
    Latitude    float64 `json:"latitude"`
    Longitude   float64 `json:"longitude"`
}
```

2. **API Endpoint**: `GET /api/v1/geocode/suggest?query=<адрес>&limit=5`

3. **Кэширование**: Redis (24 часа) для ускорения повторных запросов

### Frontend (React/TypeScript)

1. **Компонент** `<AddressAutocomplete>`:
   - Debounce 500ms для оптимизации запросов
   - Keyboard navigation (↑↓ Enter Esc)
   - MapPin иконки, loading spinner
   - Click-outside-to-close

2. **Использование**:
   ```tsx
   <AddressAutocomplete
     value={address}
     onChange={(value) => setAddress(value)}
     onSelect={(suggestion) => {
       // suggestion содержит: address, description, latitude, longitude
       console.log(suggestion);
     }}
   />
   ```

## Настройка

### 1. Получение Yandex API ключа

```bash
# 1. Откройте https://developer.tech.yandex.ru/services
# 2. Войдите в аккаунт
# 3. Найдите "Геокодер" (Geocoder API)
# 4. Нажмите "Получить ключ"
# 5. Скопируйте API-ключ
```

### 2. Конфигурация `.env`

```env
# go_back/.env
YANDEX_GEO_API_KEY=ваш_ключ_здесь

# Опционально (Redis для кэширования)
REDIS_ADDR=localhost:6379
REDIS_PASSWORD=12341
REDIS_DB=0
```

### 3. Запуск Redis (опционально, но рекомендуется)

```powershell
docker run -d -p 6379:6379 redis:7-alpine --requirepass 12341
```

## Визуальные индикаторы

Поля, заполненные автоматически, отмечены иконкой ✨ (Sparkles):

```
Этажность дома ✨    [31]
Год постройки ✨     [2015]
Материал дома ✨     [Монолит]
```

## Уведомления

При выборе адреса с автозаполнением показывается toast-уведомление:
```
✅ Автоматически заполнено: 3 поля
```

## Ограничения

⚠️ **Yandex Geocoder:**
- Задержка активации ключа: 15-20 минут после создания
- **НЕ предоставляет данные о доме** (этажность, год, материал) - только адрес и координаты
- Для данных о зданиях нужны другие API (Places, Overpass, Росреестр)

⚠️ **Nominatim (OpenStreetMap):**
- Данные зависят от наполненности OSM базы
- Меньше метаданных по сравнению с Yandex
- Rate limit: ~1 запрос/сек (соблюдается через debounce)

## Тестирование

```powershell
# 1. Запустите backend
cd go_back
go run cmd/service/main.go

# 2. Запустите frontend  
cd ui
npm run dev

# 3. Откройте http://localhost:3000
# 4. Начните вводить адрес, например "Нежинская улица"
# 5. Выберите адрес из подсказок ✅
```

## API Response пример

```json
{
  "suggestions": [
    {
      "address": "Россия, Москва, Нежинская улица, 1к1",
      "description": "Выхино-Жулебино",
      "latitude": 55.706123,
      "longitude": 37.816456
    }
  ],
  "from_cache": false
}
```

## Отладка

Для отладки проверьте логи:
- **Backend**: Вывод в консоль `go run cmd/service/main.go`
- **Frontend**: Консоль браузера (F12)
- **Redis**: `redis-cli KEYS "rentadvisor:*"`

---

**Обновлено**: 20 ноября 2025 г.  
**Версия**: 1.1.0 - Исправлена документация согласно официальному Yandex Geocoder API
