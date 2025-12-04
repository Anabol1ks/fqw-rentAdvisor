import sys
import os
sys.path.insert(0, "src")

# Загружаем переменные окружения
from pathlib import Path
from dotenv import load_dotenv
load_dotenv(Path(".env"))

from realval.cli import _yandex_geocode

address = "улица Льва Толстого, 16"
city = "Москва"

print(f"Тестируем Yandex геокодер...")
print(f"API Key: {os.environ.get('YANDEX_GEO_API_KEY')}")
print(f"Адрес: {address}, город: {city}")

result = _yandex_geocode(address, city)
print(f"\nРезультат: {result}")

if result:
    lat, lon = result
    print(f"Широта: {lat}, Долгота: {lon}")
    print(f"✓ Геокодирование успешно!")
else:
    print(f"✗ Геокодирование не удалось")
