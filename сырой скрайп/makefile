scrape:
	go run ./cmd/scrape_yandex --city=moskva --start=1 --pages=1200 --max-empty-pages=5 --parallel=1 --delay-min=2000ms --delay-max=6000ms --deal=sale --max-items=1000 --use-referer=true

scrape-one:
	go run ./cmd/scrape_yandex --url="$(URL)" --deal=sale --use-referer=true --cookie="$$(Get-Content $(COOKIE_FILE) -Raw)"

norm:
	go run ./cmd/normalize --limit=0 --batch=500 --sleep-ms=0

migrate:
	go run ./cmd/migrate