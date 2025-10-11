CITY = Москва

# === ОСНОВНЫЕ КОМАНДЫ СКРАПИНГА ===

# Скрапинг аренды в Москве (resilient режим, рекомендуется)
scrape:
	go run ./cmd/scrape_yandex --city=moskva --start=1 --pages=12000 --max-empty-pages=5 --parallel=3 --delay-min=2000ms --delay-max=6000ms --deal=rent --max-items=1000 --use-referer=true --captcha-cooldown=15m --max-retries=8

# Скрапинг продажи в Москве
scrape-sale:
	go run ./cmd/scrape_yandex --city=moskva --start=1 --pages=8000 --max-empty-pages=5 --parallel=2 --delay-min=2000ms --delay-max=6000ms --deal=sale --max-items=1000 --use-referer=true --captcha-cooldown=15m

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
