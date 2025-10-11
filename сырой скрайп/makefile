CITY = Москва

# === ОСНОВНЫЕ КОМАНДЫ СКРАПИНГА ===

# Скрапинг аренды в Москве (resilient режим, рекомендуется)
scrape:
	go run ./cmd/scrape_yandex --city=moskva --start=1 --pages=12000 --max-empty-pages=5 --parallel=3 --delay-min=2000ms --delay-max=6000ms --deal=rent --max-items=1000 --use-referer=true --captcha-cooldown=15m --max-retries=8

# Скрапинг продажи в Москве
scrape-sale:
	go run ./cmd/scrape_yandex --city=moskva --start=1 --pages=8000 --max-empty-pages=5 --parallel=2 --delay-min=2000ms --delay-max=6000ms --deal=sale --max-items=1000 --use-referer=true --captcha-cooldown=15m

# Скрапинг в СПб (аренда)
scrape-spb:
	go run ./cmd/scrape_yandex --city=peterburg --start=1 --pages=6000 --max-empty-pages=5 --parallel=2 --delay-min=2500ms --delay-max=7000ms --deal=rent --max-items=500 --use-referer=true --captcha-cooldown=15m

# Быстрый тест (100 записей)
scrape-test:
	go run ./cmd/scrape_yandex --city=moskva --deal=rent --max-items=100 --pages=500 --parallel=1 --delay-min=1000ms --delay-max=2000ms

# Максимальная надежность (для сложных случаев)
scrape-safe:
	go run ./cmd/scrape_yandex --city=moskva --deal=rent --max-items=1000 --pages=12000 --parallel=1 --delay-min=3000ms --delay-max=8000ms --captcha-cooldown=25m --max-retries=15 --error-threshold=3

# Скрапинг одной конкретной страницы
scrape-one:
	go run ./cmd/scrape_yandex --url="$(URL)" --deal=sale --use-referer=true --cookie="$$(Get-Content $(COOKIE_FILE) -Raw)"

# === СБОРКА И АВТОМАТИЗАЦИЯ ===

# Сборка исполняемого файла
build:
	go build -o scrape_yandex.exe ./cmd/scrape_yandex

# Автоматический режим с мониторингом (требует .env.resilient)
scrape-auto:
	monitor_scraper.bat

# Быстрый запуск с оптимальными настройками
scrape-quick:
	run_resilient_scraper.bat

# === СПЕЦИАЛЬНЫЕ СЦЕНАРИИ ===

# Большой объем данных (2000+ записей)
scrape-bulk:
	go run ./cmd/scrape_yandex --city=moskva --deal=rent --max-items=2000 --pages=20000 --parallel=2 --delay-min=2500ms --delay-max=7000ms --captcha-cooldown=20m --max-retries=10 --error-threshold=5

# Несколько городов последовательно
scrape-multi-cities:
	go run ./cmd/scrape_yandex --city=moskva --deal=rent --max-items=500 --pages=6000 --parallel=2 --captcha-cooldown=15m
	go run ./cmd/scrape_yandex --city=peterburg --deal=rent --max-items=300 --pages=4000 --parallel=2 --captcha-cooldown=15m
	go run ./cmd/scrape_yandex --city=ekaterinburg --deal=rent --max-items=200 --pages=3000 --parallel=2 --captcha-cooldown=15m

# Отладочный режим (подробные логи)
scrape-debug:
	go run ./cmd/scrape_yandex --city=moskva --deal=rent --max-items=50 --pages=500 --parallel=1 --delay-min=1000ms --delay-max=2000ms --verbose=true --log-level=debug

# Показать справку по параметрам
scrape-help:
	go run ./cmd/scrape_yandex --help

# === КОНФИГУРАЦИЯ И НАСТРОЙКА ===

# Создать конфигурационный файл с оптимальными настройками
setup-config:
	@echo "# Resilient Scraper Configuration" > .env.resilient
	@echo "CITY=moskva" >> .env.resilient
	@echo "MAX_ITEMS=1000" >> .env.resilient
	@echo "PAGES=12000" >> .env.resilient
	@echo "PARALLELISM=3" >> .env.resilient
	@echo "DEAL_TYPE=rent" >> .env.resilient
	@echo "DELAY_MIN=2000ms" >> .env.resilient
	@echo "DELAY_MAX=6000ms" >> .env.resilient
	@echo "CAPTCHA_COOLDOWN=15m" >> .env.resilient
	@echo "MAX_RETRIES=8" >> .env.resilient
	@echo "ERROR_THRESHOLD=8" >> .env.resilient
	@echo "USE_REFERER=true" >> .env.resilient
	@echo "USER_AGENT_ROTATION=true" >> .env.resilient
	@echo "Configuration created in .env.resilient"

# Показать текущую конфигурацию
show-config:
	@if exist .env.resilient (type .env.resilient) else (echo No .env.resilient file found)

norm:
	go run ./cmd/normalize --limit=0 --batch=500 --sleep-ms=0

migrate:
	go run ./cmd/migrate

geocode:
	go run ./cmd/geocode --workers=2 --sleep=800ms --city="$(CITY)" --limit=0

dedupe:
	go run ./cmd/dedupe

enrich:
	go run ./cmd/enrich --city="$(CITY)"

deactivate:
	go run ./cmd/deactivate

fix-seq:
	go run ./cmd/fix_sequences

import-metro:
	go run ./cmd/import_metro --file="./metro.ru.csv" --city="$(CITY)" --sep=,

dataset:
	python ml/prepare_dataset.py

train-ridge:
	python ml/train_ridge.py

mlflow-ui:
	mlflow ui --port 5000

# Аудит фичей и таргета (EDA)
audit:
	python -m features.audit

transformers:
	python -m features.transformers

# Материализация фичей из splits в processed
make-features:
	python scripts/make_features.py

# Обучение бейзлайнов на processed-фичах
train-ridge-processed:
	python scripts/train_ridge.py

train-catboost:
	python scripts/train_catboost.py

# Установка ML-зависимостей (pandas, sklearn, mlflow, catboost, h3 и т.д.)
ml-deps:
	pip install -r ml/requirements-ml.txt

# --- Полезные конвейеры ---
# Только ML-часть после подготовки listing_master: dataset -> audit -> make-features -> train-ridge-processed
ml-pipeline:
	make dataset
	make audit
	make make-features
	make train-ridge-processed

# Полный конвейер после скрейпинга: pipeline (ETL) -> dataset -> audit -> make-features -> train-ridge-processed
full-pipeline:
	make pipeline
	make dataset
	make audit
	make make-features
	make train-ridge-processed

# Удобные обёртки для кросс-валидации Ridge (Windows cmd: set VAR=... && ...)
# По времени (нужна колонка last_seen в train_features.parquet)
train-ridge-cv-time:
	cmd /C "set DO_CV=1 && set CV_TYPE=time && python scripts/train_ridge.py"

# По группам (районы по умолчанию; можно переопределить GROUP_COL=h3_7 при наличии)
train-ridge-cv-group:
	cmd /C "set DO_CV=1 && set CV_TYPE=group && set GROUP_COL=district && python scripts/train_ridge.py"


# --- raw table transfer helpers ---
# Usage examples (PowerShell):
#  make export-raw FILE=raw.bin
#  make import-raw FILE=raw.bin
#  make copy-raw SRC_DSN="postgres://..." DST_DSN="postgres://..."


FILE=listing_raw.bin

export-raw:
	go run ./cmd/xfer_listing_raw --mode=export --schema=public --table=listing_raw --file="$(FILE)"

import-raw:
	go run ./cmd/xfer_listing_raw --mode=import --schema=public --table=listing_raw --file="$(FILE)"

copy-raw:
	go run ./cmd/xfer_listing_raw --mode=copy --schema=public --table=listing_raw --src="$(SRC_DSN)" --dst="$(DST_DSN)"


pipeline:
	make migrate
	make norm
	make geocode
	make enrich
	make dedupe
	make deactivate
