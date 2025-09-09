## Подхватываем .env (если есть) и экспортируем переменные в окружение
ifneq (,$(wildcard .env))
include .env
export
endif

## На Windows выполняем рецепты через PowerShell (без нужды экранировать %)
ifeq ($(OS),Windows_NT)
SHELL := powershell.exe
.SHELLFLAGS := -NoProfile -Command
endif

scrape:
	go run ./cmd/scrape_yandex --city=moskva --start=1 --pages=1200 --max-empty-pages=5 --parallel=2 --delay-min=2000ms --delay-max=4000ms --deal=rent --max-items=1000 --cookie '$(COOKIE)'

migrate:
	go run ./cmd/migrate