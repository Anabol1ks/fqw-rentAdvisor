CITY = Москва

scrape:
	go run ./cmd/scrape_yandex --city=moskva --start=1 --pages=1200 --max-empty-pages=5 --parallel=1 --delay-min=2000ms --delay-max=6000ms --deal=rent --max-items=1000 --use-referer=true

scrape-cian:
	go run ./cmd/scrape_cian --start=1 --pages=1200 --max-empty-pages=5 --parallel=3 --delay-min=2000ms --delay-max=6000ms --deal=sale --max-items=501 --use-referer=true

scrape-one:
	go run ./cmd/scrape_yandex --url="$(URL)" --deal=sale --use-referer=true --cookie="$$(Get-Content $(COOKIE_FILE) -Raw)"

scrape-cian-one:
	go run ./cmd/scrape_cian --url="$(URL)" --deal=sale --use-referer=true --cookie="$$(Get-Content $(COOKIE_FILE) -Raw)"

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
