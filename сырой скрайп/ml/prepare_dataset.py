import os
import pandas as pd
import sqlalchemy as sa
from dotenv import load_dotenv

# Prefer a single DSN, else assemble from individual vars for convenience
load_dotenv()
PG_DSN = os.environ.get("POSTGRES_DSN_URL")
if not PG_DSN:
    host = os.environ.get("DB_HOST", "localhost")
    port = os.environ.get("DB_PORT", "5432")
    user = os.environ.get("DB_USER", "postgres")
    pwd  = os.environ.get("DB_PASSWORD", "")
    name = os.environ.get("DB_NAME", "postgres")
    ssl  = os.environ.get("DB_SSLMODE", "disable")
    # Map sslmode=disable to '?sslmode=disable' only if provided
    dsn_opts = f"?sslmode={ssl}" if ssl else ""
    PG_DSN = f"postgresql+psycopg2://{user}:{pwd}@{host}:{port}/{name}{dsn_opts}"

q = """
SELECT
  deal_type, city, district,
  price_rub, price_period, price_per_m2,
  rooms, area_total, area_living, area_kitchen,
  floor, floors_total, year_built, house_material, condition,
  lat, lon, dist_to_metro_m, dist_to_center_km, density_500m,
  metro_station, metro_walk_min, first_seen, last_seen
FROM listing_master
WHERE is_active
"""

engine = sa.create_engine(PG_DSN)
df = pd.read_sql(q, engine)

days = int(os.environ.get("DATASET_TEST_DAYS", "14"))

# simple time-based split: last N days to test
if not df.empty:
    df["last_seen"] = pd.to_datetime(df["last_seen"], utc=True, errors="coerce")
    cut = df["last_seen"].max() - pd.Timedelta(days=days)
    train = df[df["last_seen"] <= cut]
    test  = df[df["last_seen"] > cut]
    # Fallback if one side is empty: chronological 80/20 split
    if train.empty or test.empty:
        df = df.sort_values("last_seen")
        k = int(len(df) * 0.8)
        if k == 0:
            train = df.iloc[:0].copy()
            test = df.copy()
        elif k >= len(df):
            train = df.copy()
            test = df.iloc[:0].copy()
        else:
            train = df.iloc[:k].copy()
            test = df.iloc[k:].copy()
else:
    train = df.copy()
    test = df.copy()

os.makedirs("data/splits", exist_ok=True)
train.to_parquet("data/splits/train.parquet", index=False)
test.to_parquet("data/splits/test.parquet", index=False)
print("Exported", len(train), len(test))
