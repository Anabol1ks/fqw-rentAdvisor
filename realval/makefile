# 	python -m venv .venv
# 	.venv\Scripts\activate
# $env:PYTHONPATH = ".\src\"

# выгрузка из бд аренды
db_export_rent:
	python -m realval db export \
		--sql-file sql/export_listings_rent.sql \
		--out data/raw/listings_rent.parquet
		
db_export_sale:
	python -m realval db export \
		--sql-file sql/export_listings_sale.sql \
		--out data/raw/listings_sale.parquet

# предобработка
ingest:
	python -m realval ingest \
		--src data/raw/listings_rent.parquet \
		--out data/interim/clean.parquet

# генерация признаков
features:
	python -m realval features \
		--in data/interim/clean.parquet \
		--out data/features/base.parquet \
		--h3-res 7

split:
	python -m realval split \
		--in data/features/base.parquet \
		--train-out data/splits/train.parquet \
		--valid-out data/splits/valid.parquet \
		--time-col first_seen \
		--valid-days 15 \
		--valid-ratio 0.3

train:
	python -m realval train \
		--train-path data/splits/train.parquet \
		--valid-path data/splits/valid.parquet \
		--model-out models/artefacts/rent_lgbm.joblib \
		--target price_rub \
		--log-target \
		--with-intervals \
		--use-local-stats 

predict-one:
	python -m realval predict-one \
		--model-path models/artefacts/rent_lgbm.joblib \
		--input-json sample_object.json \
		--out models/reports/predict_sample.json

predict-address:
	python -m realval predict-address \
		--model-path models/artefacts/rent_lgbm.joblib \
		--params .\sample_object.json \
		--geocode-cache-ttl-days 180 \
		--out models/reports/predict_address.json

predict-address-comps:
	python -m realval predict-address \
		--model-path models/artefacts/rent_lgbm.joblib \
		--params .\sample_object.json \
		--comps-path data/features/base.parquet \
		--comps-k 5 \
		--comps-max-radius-km 3 \
		--with-text \
		--out models/reports/predict_address_with_comps.json

llm_toon_i:
	pip install -U langchain langchain-gigachat
	pip install git+https://github.com/toon-format/toon-python.git

pr_serve:
	uvicorn realval.http_app:app --reload --port 8000
