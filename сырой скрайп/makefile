CITY = Москва

# === ОСНОВНЫЕ КОМАНДЫ СКРАПИНГА ===

# Скрапинг аренды в Москве (resilient режим, рекомендуется)
scrape:
	pwsh -NoProfile -Command "Set-Location -LiteralPath 'c:\Users\grigo\Documents\GitGrisha\fqw-rentAdvisor\сырой скрайп'; go run ./cmd/scrape_yandex --city=moskva --start=1 --pages=30 --max-empty-pages=2 --parallel=2 --delay-min=1200ms --delay-max=2000ms --deal=rent --max-items=500 --use-referer=true --captcha-cooldown=2m --max-retries=3 | Out-String -Width 200"

# Скрапинг продажи в Москве
scrape-sale:
	pwsh -NoProfile -Command "Set-Location -LiteralPath 'c:\Users\grigo\Documents\GitGrisha\fqw-rentAdvisor\сырой скрайп'; go run ./cmd/scrape_yandex --city=moskva --start=1 --pages=30 --max-empty-pages=2 --parallel=2 --delay-min=1200ms --delay-max=2000ms --deal=sale --max-items=500 --use-referer=true --captcha-cooldown=2m --max-retries=3 | Out-String -Width 200"


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


# 	make migrate
pipeline:
	make norm
	make geocode
	make enrich
	make dedupe


# 	make deactivate

