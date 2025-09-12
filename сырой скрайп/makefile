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

COOKIE_FILE ?= cookie.txt

scrape:
	# Cookie берём из отдельного файла (полный raw header, как в браузере). Символы # не режутся make.
	# Создай файл cookie.txt рядом с makefile и вставь строку без оборачивающих кавычек.
	go run ./cmd/scrape_yandex --city=moskva --start=20 --pages=1200 --max-empty-pages=5 --parallel=1 --delay-min=2000ms --delay-max=6000ms --deal=sale --max-items=36 --use-referer=true --cookie="$$(Get-Content $(COOKIE_FILE) -Raw)"

scrape-one:
	go run ./cmd/scrape_yandex --url="$(URL)" --deal=sale --use-referer=true --cookie="$$(Get-Content $(COOKIE_FILE) -Raw)"

norm:
	go run ./cmd/normalize --limit=0 --batch=500 --sleep-ms=0

migrate:
	go run ./cmd/migrate