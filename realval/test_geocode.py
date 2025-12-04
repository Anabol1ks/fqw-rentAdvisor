import sys
sys.path.insert(0, "src")

from realval.cli import _nominatim_geocode, CITY_CENTERS
import requests

address = "Тверская улица, 10"
city = "Москва"

headers = {
    "User-Agent": "realval/0.1 (+grigorogannisyan.12@gmail.com)",
    "Accept-Language": "ru",
}
url = "https://nominatim.openstreetmap.org/search"

params = {
    "street": address,
    "city": city,
    "countrycodes": "ru",
    "format": "jsonv2",
    "addressdetails": 1,
    "limit": 5,
}

print("Запрос к Nominatim...")
r = requests.get(url, params=params, headers=headers, timeout=20)
print(f"Статус: {r.status_code}")
print(f"Ответ: {r.text[:500]}")
if r.status_code != 200:
    print(f"ОШИБКА: {r.status_code}")
    sys.exit(1)
data = r.json() or []
print(f"Получено записей: {len(data)}")

# Проверяем фильтрацию
print("\nПроверка фильтрации по городу:")
filtered = []
for it in data:
    addr = it.get("address") or {}
    addr_city = addr.get("city") or addr.get("town") or addr.get("state") or ""
    print(f"  Найденный город: '{addr_city}', искомый: '{city}'")
    if city and isinstance(addr_city, str) and city.lower() in addr_city.lower():
        print(f"    ✓ Подходит")
        filtered.append(it)
    else:
        print(f"    ✗ Не подходит")

print(f"\nПосле фильтрации: {len(filtered)} записей")
print(f"Использовать: {len(filtered or data)} записей")

# Проверяем _pick_best
center = CITY_CENTERS.get(city)
print(f"\nЦентр города: {center}")

if filtered or data:
    candidates = filtered or data
    print(f"Обработка {len(candidates)} кандидатов:")
    for idx, it in enumerate(candidates):
        lat = it.get("lat")
        lon = it.get("lon")
        print(f"  {idx}: lat={lat}, lon={lon}, display_name={it.get('display_name', '')[:50]}")

print("\n\nВызов _nominatim_geocode:")
result = _nominatim_geocode(address, city)
print(f"Результат: {result}")
