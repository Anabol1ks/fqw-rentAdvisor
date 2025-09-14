CITY = Москва

scrape:
	go run ./cmd/scrape_yandex --city=moskva --start=1 --pages=1200 --max-empty-pages=5 --parallel=1 --delay-min=2000ms --delay-max=6000ms --deal=rent --max-items=1000 --use-referer=true

scrape-one:
	go run ./cmd/scrape_yandex --url="$(URL)" --deal=sale --use-referer=true --cookie="$$(Get-Content $(COOKIE_FILE) -Raw)"

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
