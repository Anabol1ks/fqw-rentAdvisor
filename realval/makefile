# 	python -m venv .venv
# 	.venv\Scripts\activate
# $env:PYTHONPATH = ".\src\"

# выгрузка из бд
db_export_rent:
	python -m realval db export \
		--sql-file sql/export_listings_rent.sql \
		--out data/raw/listings.parquet

# предобработка
ingest:
	python -m realval ingest \
		--src data/raw/listings.parquet \
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
		--train-out data/features/train.parquet \
		--valid-out data/features/valid.parquet \
		--time-col first_seen \
		--valid-days 7

train:
	python -m realval train \
		--train-path data/features/train.parquet \
		--valid-path data/features/valid.parquet \
		--model-out models/artefacts/rent_lgbm.joblib \
		--target price_rub \
		--log-target \
		--with-intervals

predict-one:
	python -m realval predict-one \
		--model-path models/artefacts/rent_lgbm.joblib \
		--input-json sample_object.json \
		--out models/reports/predict_sample.json
