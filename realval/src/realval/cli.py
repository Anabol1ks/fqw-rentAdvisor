from __future__ import annotations
import json
from pathlib import Path
from typing import Optional, List, Tuple
from .llm_text import generate_text_blocks, LLMConfig


import re
from datetime import datetime, timedelta, timezone

import os
import sqlalchemy as sa
import pyarrow as pa
import pyarrow.parquet as pq

from dotenv import load_dotenv
import time
import math
import requests
from tenacity import retry, wait_fixed, stop_after_attempt
from shapely import wkb as shp_wkb


import typer
import pandas as pd
import numpy as np
from dateutil import parser as dtparser
from sklearn.compose import ColumnTransformer
from sklearn.preprocessing import OneHotEncoder
from sklearn.pipeline import Pipeline
from sklearn.metrics import mean_absolute_error, mean_squared_error
from sklearn.model_selection import train_test_split
from joblib import dump, load
import lightgbm as lgb
import h3


load_dotenv(Path(__file__).resolve().parents[2] / ".env")
app = typer.Typer(help="RealVal — консольный прототип анализа недвижимости")

# ---------- IO helpers ----------

OVERPASS_URL = "https://overpass-api.de/api/interpreter"

@retry(wait=wait_fixed(1), stop=stop_after_attempt(3))
def _overpass_nearby_building(lat: float, lon: float, radius_m: int = 100) -> Optional[dict]:
    """
    Ищем ближайшее здание в радиусе (way/relation с building=*) и возвращаем его tags + geometry center.
    """
    headers = {
        "User-Agent": "realval/0.1 (+grigorogannisyan.12@gmail.com)",
    }
    # Берем несколько кандидатов и выберем по расстоянию
    q = f"""
    [out:json][timeout:25];
    (
      way(around:{radius_m},{lat},{lon})["building"];
      relation(around:{radius_m},{lat},{lon})["building"];
    );
    out tags center 10;
    """
    r = requests.post(OVERPASS_URL, data=q.encode("utf-8"), headers=headers, timeout=30)
    r.raise_for_status()
    js = r.json()
    els = js.get("elements", []) if isinstance(js, dict) else []
    if not els:
        return None
    # выбрать ближайшее по центру:
    def _dkm(e):
        clat = e.get("center", {}).get("lat")
        clon = e.get("center", {}).get("lon")
        if clat is None or clon is None:
            return 1e9
        return _haversine_km(lat, lon, float(clat), float(clon))
    els.sort(key=_dkm)
    best = els[0]
    tags = best.get("tags", {}) or {}
    # нормализация интересующих полей
    floors = None
    for key in ("building:levels", "levels"):
        val = tags.get(key)
        if val is not None:
            try:
                floors = int(float(val))
                break
            except Exception:
                pass

    year_built = None
    for key in ("start_date", "year_of_construction", "building:year_built"):
        val = tags.get(key)
        if val:
            # вытащим 4-значный год
            import re
            m = re.search(r"(18|19|20)\d{2}", str(val))
            if m:
                year_built = int(m.group(0))
                break

    material = tags.get("building:material") or tags.get("material")

    return {
        "floors_total": floors,
        "year_built": year_built,
        "house_material": material,
        "raw_tags": tags,
        "source": "osm_overpass",
    }

def _ensure_building_cache_table(engine) -> None:
    sql = """
    CREATE TABLE IF NOT EXISTS building_cache (
      city          text        NOT NULL,
      address_norm  text        NOT NULL,
      lat           double precision NOT NULL,
      lon           double precision NOT NULL,
      floors_total  integer,
      year_built    integer,
      house_material text,
      source        text,
      raw_tags      jsonb,
      created_at    timestamptz NOT NULL DEFAULT now(),
      PRIMARY KEY (city, address_norm)
    );
    CREATE INDEX IF NOT EXISTS building_cache_created_at_idx ON building_cache (created_at DESC);
    """
    with engine.begin() as conn:
        conn.exec_driver_sql(sql)

def _get_cached_building(engine, city: str, address: str, max_age_days: Optional[int] = 365) -> Optional[dict]:
    key_city = city.strip()
    key_addr = _canon_addr(address)
    sql = """
      SELECT lat, lon, floors_total, year_built, house_material, source, raw_tags, created_at
      FROM building_cache
      WHERE city = %(city)s AND address_norm = %(address)s
      LIMIT 1
    """
    try:
        row = pd.read_sql(sql, engine, params={"city": key_city, "address": key_addr})
        if row.empty:
            return None
        rec = row.iloc[0].to_dict()
        if max_age_days is not None:
            ts = rec.get("created_at")
            if isinstance(ts, pd.Timestamp):
                ts = ts.to_pydatetime()
            from datetime import datetime, timezone, timedelta
            if ts and datetime.now(timezone.utc) - ts.replace(tzinfo=timezone.utc) > timedelta(days=max_age_days):
                return None
        return rec
    except Exception:
        return None

def _put_cached_building(engine, city: str, address: str, lat: float, lon: float, enriched: dict) -> None:
    key_city = city.strip()
    key_addr = _canon_addr(address)
    sql = """
      INSERT INTO building_cache (city, address_norm, lat, lon, floors_total, year_built, house_material, source, raw_tags, created_at)
      VALUES (%(city)s, %(address)s, %(lat)s, %(lon)s, %(floors)s, %(year)s, %(mat)s, %(src)s, %(raw)s, NOW())
      ON CONFLICT (city, address_norm)
      DO UPDATE SET
        lat = EXCLUDED.lat,
        lon = EXCLUDED.lon,
        floors_total = EXCLUDED.floors_total,
        year_built = EXCLUDED.year_built,
        house_material = EXCLUDED.house_material,
        source = EXCLUDED.source,
        raw_tags = EXCLUDED.raw_tags,
        created_at = NOW();
    """
    params = {
        "city": key_city,
        "address": key_addr,
        "lat": lat, "lon": lon,
        "floors": enriched.get("floors_total"),
        "year": enriched.get("year_built"),
        "mat": enriched.get("house_material"),
        "src": enriched.get("source"),
        "raw": json.dumps(enriched.get("raw_tags", {}), ensure_ascii=False),
    }
    with engine.begin() as conn:
        conn.exec_driver_sql(sql, params=params)


SUPPORT_EXT_READ = {".parquet", ".csv", ".json"}
SUPPORT_EXT_WRITE = {".parquet", ".csv"}

def _read_table(path: Path) -> pd.DataFrame:
    ext = path.suffix.lower()
    if ext == ".parquet":
        return pd.read_parquet(path)
    if ext == ".csv":
        return pd.read_csv(path)
    if ext == ".json":
        # пробуем JSON Lines (NDJSON), иначе обычный json
        try:
            return pd.read_json(path, lines=True)
        except ValueError:
            return pd.read_json(path)
    raise typer.BadParameter(f"Unsupported input extension: {ext}")

def _write_table(df: pd.DataFrame, path: Path):
    path.parent.mkdir(parents=True, exist_ok=True)
    ext = path.suffix.lower()
    if ext == ".parquet":
        df.to_parquet(path, index=False)
    elif ext == ".csv":
        df.to_csv(path, index=False)
    else:
        raise typer.BadParameter(f"Unsupported output extension: {ext} (use .parquet or .csv)")

def _coerce_types(df: pd.DataFrame) -> pd.DataFrame:
    # Попытки привести ключевые поля к числам/датам
    num_cols = [
        "price_rub", "price_per_m2", "rooms", "area_total", "area_living", "area_kitchen",
        "floor", "floors_total", "year_built", "lat", "lon",
        "dist_to_center_km", "dist_to_metro_m", "metro_walk_min", "density_500m"
    ]
    for c in num_cols:
        if c in df.columns:
            df[c] = pd.to_numeric(df[c], errors="coerce")
    dt_cols = ["first_seen", "last_seen", "created_at", "updated_at"]
    for c in dt_cols:
        if c in df.columns:
            df[c] = pd.to_datetime(df[c], errors="coerce", utc=True)
    if "is_active" in df.columns:
        if df["is_active"].dtype == object:
            df["is_active"] = df["is_active"].map({"true": True, "false": False}).fillna(df["is_active"])
        df["is_active"] = df["is_active"].astype("boolean")
    return df

def _rooms_bucket(r) -> str:
    try:
        r = int(r)
    except Exception:
        return "other"
    if r <= 0:
        return "studio"
    if r == 1:
        return "r1"
    if r == 2:
        return "r2"
    if r == 3:
        return "r3"
    if r == 4:
        return "r4"
    return "r5p"

def _attach_local_price_stats(
    df: pd.DataFrame,
    stats_df: Optional[pd.DataFrame],
    global_mean: Optional[float],
) -> pd.DataFrame:
    """
    Добавляет в df колонки local_avg_price_rub и local_density_count
    по H3_7 и rooms_bucket на основе stats_df (из трейна).
    """
    df = df.copy()
    if stats_df is None or global_mean is None:
        # на всякий случай — просто не трогаем
        if "local_avg_price_rub" not in df.columns:
            df["local_avg_price_rub"] = global_mean if global_mean is not None else 0.0
        if "local_density_count" not in df.columns:
            df["local_density_count"] = 0.0
        return df

    if "h3_7" not in df.columns:
        df = _ensure_h3(df, res=7)
    if "rooms_bucket" not in df.columns:
        df["rooms_bucket"] = df["rooms"].apply(_rooms_bucket)

    st = stats_df.copy()
    df = df.merge(st, on=["h3_7", "rooms_bucket"], how="left")

    df["local_avg_price_rub"] = np.where(
        df["cnt"].notna(),
        df["sum_price"] / df["cnt"],
        global_mean,
    )
    df["local_density_count"] = df["cnt"].fillna(0)

    df = df.drop(columns=["sum_price", "cnt"], errors="ignore")
    return df

def _find_comparables(
    target: dict,
    comps_df: pd.DataFrame,
    k: int = 5,
    max_radius_km: float = 5.0,
) -> List[dict]:
    """
    Находит K ближайших по месту и типу объявлений как компараблы.
    target: словарь с полями (lat, lon, rooms, area_total, city, deal_type='rent_long' и т.п.)
    """
    if "lat" not in target or "lon" not in target:
        return []

    lat0 = float(target["lat"])
    lon0 = float(target["lon"])
    city = target.get("city")

    df = comps_df.copy()
    df = _coerce_types(df)

    # фильтры: тот же город, аренда long, валидная цена и координаты
    if city and "city" in df.columns:
        df = df[df["city"] == city]
    if "deal_type" in df.columns:
        df = df[df["deal_type"] == "rent_long"]

    df = df.dropna(subset=["lat", "lon", "price_rub", "area_total"])

    if df.empty:
        return []

    # расстояние до target
    df["_dist_km"] = _haversine_km(
        lat0, lon0,
        df["lat"].values,
        df["lon"].values,
    )

    # выкинем очень далеко
    df = df[df["_dist_km"] <= max_radius_km]
    if df.empty:
        return []

    # buckets комнат
    df["_rooms_bucket"] = df["rooms"].apply(_rooms_bucket)
    target_rb = _rooms_bucket(target.get("rooms"))

    # фильтр по bucket (сначала строгий, потом fallback)
    same_rb = df[df["_rooms_bucket"] == target_rb].copy()
    use_df = same_rb if not same_rb.empty else df.copy()

    # похожесть по площади: штраф за отклонение
    tgt_area = float(target.get("area_total") or 0)
    if tgt_area > 0:
        use_df["_area_ratio"] = (use_df["area_total"] / tgt_area).clip(lower=0.2, upper=5.0)
        use_df["_area_penalty"] = (np.log(use_df["_area_ratio"]) ** 2)  # симметричный штраф
    else:
        use_df["_area_penalty"] = 0.0

    # итоговый скор: расстояние + штраф за площадь
    use_df["_score"] = use_df["_dist_km"] + use_df["_area_penalty"]

    use_df = use_df.sort_values("_score").head(k)

    # формируем аккуратный вывод
    cols_basic = [
        "deal_type", "city", "district",
        "price_rub", "price_per_m2",
        "rooms", "area_total", "area_living", "area_kitchen",
        "floor", "floors_total", "year_built", "house_material", "condition",
        "lat", "lon", "dist_to_metro_m", "dist_to_center_km", "density_500m",
        "metro_station", "metro_walk_min", "address_norm", "url",
    ]
    result = []
    for _, r in use_df.iterrows():
        obj = {}
        for c in cols_basic:
            if c in use_df.columns:
                v = r[c]
                # упростим сериализацию дат
                if isinstance(v, (pd.Timestamp, )):
                    v = v.isoformat()
                # Заменяем NaN на None для корректного JSON
                elif pd.isna(v):
                    v = None
                # Конвертируем numpy типы в Python native
                elif hasattr(v, 'item'):
                    v = v.item()
                obj[c] = v
        obj["distance_km"] = float(r["_dist_km"])
        result.append(obj)
    return result


def _ensure_h3(df: pd.DataFrame, res: int = 7) -> pd.DataFrame:
    if "h3_7" in df.columns and res == 7:
        return df
    if {"lat", "lon"}.issubset(df.columns):
        def enc(r):
            try:
                return h3.geo_to_h3(float(r["lat"]), float(r["lon"]), res)
            except Exception:
                return np.nan
        col = f"h3_{res}"
        df[col] = df.apply(enc, axis=1)
    return df


_ADDR_SPACE_RE = re.compile(r"\s+")

def _canon_addr(s: Optional[str]) -> str:
    """Упрощённая нормализация адреса для ключа кэша."""
    if not s:
        return ""
    s = s.replace("ё", "е").strip().lower()
    s = _ADDR_SPACE_RE.sub(" ", s)
    # можно дополнить удалением "г.", "город", запятых и т.п., если нужно
    return s


CITY_CENTERS = {
    "Москва": (55.7558, 37.6173),
}

def _haversine_km(lat1, lon1, lat2, lon2):
    R = 6371.0
    p = np.deg2rad(lat2 - lat1)
    l = np.deg2rad(lon2 - lon1)
    a = np.sin(p/2)**2 + np.cos(np.deg2rad(lat1))*np.cos(np.deg2rad(lat2))*np.sin(l/2)**2
    return 2 * R * np.arcsin(np.sqrt(a))

def _walk_minutes_from_meters(meters: float, speed_m_per_min: float = 80.0) -> int:
    if meters is None or np.isnan(meters):
        return None
    return int(math.ceil(meters / speed_m_per_min))

def _load_metro_points(engine, city: str = "Москва") -> pd.DataFrame:
    # пробуем PostGIS-удобный путь; если не сработает — парсим WKB в Python
    try:
        sql = """
            SELECT name,
                   ST_Y(geom::geometry) AS lat,
                   ST_X(geom::geometry) AS lon
            FROM ref_metro
            WHERE city = %(city)s
        """
        return pd.read_sql(sql, engine, params={"city": city})
    except Exception:
        df = pd.read_sql(
            "SELECT name, geom FROM ref_metro WHERE city = %(city)s",
            engine,
            params={"city": city},
        )
        def _to_xy(wkb_hex: str):
            try:
                g = shp_wkb.loads(bytes.fromhex(wkb_hex), hex=True)
                return g.y, g.x
            except Exception:
                return np.nan, np.nan
        latlon = df["geom"].apply(_to_xy)
        df["lat"] = latlon.apply(lambda t: t[0])
        df["lon"] = latlon.apply(lambda t: t[1])
        return df[["name", "lat", "lon"]]

def _nearest_metro(lat: float, lon: float, metros: pd.DataFrame) -> Tuple[Optional[str], Optional[float], Optional[int]]:
    if metros.empty or any(pd.isna([lat, lon])):
        return None, None, None
    # векторно считаем расстояния
    d_km = _haversine_km(lat, lon, metros["lat"].values, metros["lon"].values)
    idx = int(np.nanargmin(d_km))
    name = metros.iloc[idx]["name"]
    dist_m = float(d_km[idx] * 1000.0)
    walk_min = _walk_minutes_from_meters(dist_m)
    return name, dist_m, walk_min

def _density_500m(engine, lat: float, lon: float, days_back: int = 90) -> Optional[int]:
    try:
        sql = """
        SELECT COUNT(*) AS cnt
        FROM listing_master
        WHERE is_active
                    AND last_seen >= NOW() - (%(days)s || ' days')::interval
          AND ST_DWithin(
                                ST_SetSRID(ST_MakePoint(%(lon)s, %(lat)s), 4326)::geography,
                geom::geography,
                500
              )
        """
        row = pd.read_sql(sql, engine, params={"lat": lat, "lon": lon, "days": days_back})
        return int(row.iloc[0]["cnt"])
    except Exception:
        return None


# ---------- Commands ----------

@retry(wait=wait_fixed(1), stop=stop_after_attempt(3))
def _nominatim_geocode(address: str, city: str = "Москва") -> Optional[Tuple[float, float]]:
    """
    Геокодирование с приоритизацией нужного города.
    1) Пытаемся структурированным поиском (street+city, RU) с несколькими кандидатами.
    2) Выбираем ближайшего к центру города кандидата (если CITY_CENTERS известен).
    3) Фолбэк: простой q-поиск по строке.
    """
    headers = {
        "User-Agent": "realval/0.1 (+grigorogannisyan.12@gmail.com)",
        "Accept-Language": "ru",
    }
    url = "https://nominatim.openstreetmap.org/search"

    def _pick_best(candidates: list) -> Optional[Tuple[float, float]]:
        if not candidates:
            return None
        # если знаем центр города — берём ближайшего к нему
        center = CITY_CENTERS.get(city)
        if center:
            best = None
            best_d = None
            for it in candidates:
                try:
                    lat = float(it.get("lat"))
                    lon = float(it.get("lon"))
                except Exception:
                    continue
                d = _haversine_km(center[0], center[1], lat, lon)
                if best is None or d < best_d:
                    best = (lat, lon)
                    best_d = d
            return best
        # иначе — первый из списка
        try:
            return float(candidates[0]["lat"]), float(candidates[0]["lon"])
        except Exception:
            return None

    # 1) Структурированный запрос с фильтром по городу
    try:
        params = {
            "street": address,
            "city": city,
            "countrycodes": "ru",
            "format": "jsonv2",
            "addressdetails": 1,
            "limit": 5,
        }
        r = requests.get(url, params=params, headers=headers, timeout=20)
        r.raise_for_status()
        data = r.json() or []
        # Отфильтруем по совпадению города в ответе, если есть addressdetails
        filtered = []
        for it in data:
            addr = it.get("address") or {}
            addr_city = addr.get("city") or addr.get("town") or addr.get("state") or ""
            if city and isinstance(addr_city, str) and city.lower() in addr_city.lower():
                filtered.append(it)
        picked = _pick_best(filtered or data)
        if picked:
            return picked
    except Exception:
        pass

    # 2) Фолбэк: обычный q-поиск, но подмешиваем город, если он ещё не включён
    try:
        addr_str = address or ""
        city_str = city or ""
        q = f"{city_str}, {addr_str}" if city_str and city_str.lower() not in addr_str.lower() else addr_str
        params = {"q": q, "format": "jsonv2", "limit": 5}
        r = requests.get(url, params=params, headers=headers, timeout=20)
        r.raise_for_status()
        data = r.json() or []
        picked = _pick_best(data)
        if picked:
            return picked
    except Exception:
        pass

    return None

def _ensure_geocode_cache_table(engine) -> None:
    sql = """
    CREATE TABLE IF NOT EXISTS geocode_cache (
      city         text        NOT NULL,
      address_norm text        NOT NULL,
      lat          double precision NOT NULL,
      lon          double precision NOT NULL,
      created_at   timestamptz NOT NULL DEFAULT now(),
      PRIMARY KEY (city, address_norm)
    );
    CREATE INDEX IF NOT EXISTS geocode_cache_created_at_idx ON geocode_cache (created_at DESC);
    """
    with engine.begin() as conn:
        conn.exec_driver_sql(sql)

def _get_cached_geocode(engine, city: str, address: str, max_age_days: Optional[int] = 180) -> Optional[Tuple[float, float]]:
    key_city = city.strip()
    key_addr = _canon_addr(address)
    sql = """
      SELECT lat, lon, created_at
      FROM geocode_cache
      WHERE city = %(city)s AND address_norm = %(address)s
      LIMIT 1
    """
    try:
        row = pd.read_sql(sql, engine, params={"city": key_city, "address": key_addr})
        if row.empty:
            return None
        lat, lon, created_at = float(row.iloc[0]["lat"]), float(row.iloc[0]["lon"]), row.iloc[0]["created_at"]
        if max_age_days is not None and isinstance(created_at, (pd.Timestamp, datetime)):
            if isinstance(created_at, pd.Timestamp):
                created_at = created_at.to_pydatetime()
            if created_at.tzinfo is None:
                created_at = created_at.replace(tzinfo=timezone.utc)
            if datetime.now(timezone.utc) - created_at > timedelta(days=max_age_days):
                return None
        return lat, lon
    except Exception:
        return None

def _put_cached_geocode(engine, city: str, address: str, lat: float, lon: float) -> None:
    key_city = city.strip()
    key_addr = _canon_addr(address)
    sql = """
      INSERT INTO geocode_cache (city, address_norm, lat, lon, created_at)
      VALUES (%(city)s, %(address)s, %(lat)s, %(lon)s, NOW())
      ON CONFLICT (city, address_norm)
      DO UPDATE SET lat = EXCLUDED.lat, lon = EXCLUDED.lon, created_at = NOW();
    """
    with engine.begin() as conn:
        conn.exec_driver_sql(sql, params={"city": key_city, "address": key_addr, "lat": lat, "lon": lon})


@app.command()
def ingest(
    src: Path = typer.Option(..., help="Входной файл (.parquet/.csv/.json)"),
    out: Path = typer.Option(..., help="Куда сохранить (.parquet/.csv)"),
):
    """
    Читает сырой файл, приводит типы, сохраняет.
    """
    df = _read_table(src)
    df = _coerce_types(df)
    _write_table(df, out)
    typer.echo(f"[ingest] rows={len(df)} -> {out}")

@app.command("features")
def features_build(
    inp: Path = typer.Option(..., "--in", help="Входной нормализованный файл (.parquet/.csv)"),
    out: Path = typer.Option(..., help="Куда сохранить фичи (.parquet/.csv)"),
    h3_res: int = typer.Option(7, help="H3 resolution для spatial blocking"),
):
    """
    Генерация базовых признаков (age_house, price_per_m2, h3_*).
    """
    df = _read_table(inp)
    df = _coerce_types(df)

    # Геофичи: центр, метро, плотность
    dsn = os.environ.get("POSTGRES_DSN_URL")
    engine = sa.create_engine(dsn) if dsn else None
    
    if engine:
        typer.echo("[features] Обогащаем геофичи (метро, плотность)...")
        # Центр города
        center = CITY_CENTERS.get("Москва", CITY_CENTERS["Москва"])
        
        # Метро
        metros = _load_metro_points(engine, city="Москва")
        
        # Инициализируем колонки
        df["dist_to_center_km"] = None
        df["dist_to_metro_m"] = None
        df["metro_station"] = ""
        df["metro_walk_min"] = None
        df["density_500m"] = None
        
        for idx, row in df.iterrows():
            if pd.notna(row.get("lat")) and pd.notna(row.get("lon")):
                lat, lon = float(row["lat"]), float(row["lon"])
                
                # Расстояние до центра
                df.at[idx, "dist_to_center_km"] = _haversine_km(lat, lon, center[0], center[1])
                
                # Ближайшее метро
                metro_name, dist_m, walk_min = _nearest_metro(lat, lon, metros)
                df.at[idx, "dist_to_metro_m"] = dist_m
                df.at[idx, "metro_station"] = metro_name or ""
                df.at[idx, "metro_walk_min"] = walk_min
                
                # Плотность
                df.at[idx, "density_500m"] = _density_500m(engine, lat, lon, days_back=90)
    else:
        typer.echo("[features] ⚠️  Нет DSN — геофичи не заполнены")
    # ======== КОНЕЦ ВСТАВКИ ========
    
    # Производные фичи
    now_year = pd.Timestamp.utcnow().year
    df["age_house"] = df["year_built"].apply(lambda x: (now_year - x) if pd.notna(x) else None)


    # возраст дома
    if "year_built" in df.columns:
        now_year = pd.Timestamp.utcnow().year
        df["age_house"] = (now_year - df["year_built"]).clip(lower=0)
    else:
        df["age_house"] = np.nan

    # цена за м2 (если доступна площадь)
    if "price_rub" in df.columns and "area_total" in df.columns:
        df["price_per_m2_calc"] = df["price_rub"] / df["area_total"].replace(0, np.nan)

    # H3
    df = _ensure_h3(df, res=h3_res)

    _write_table(df, out)
    typer.echo(f"[features] rows={len(df)} -> {out}")

@app.command("split")
def split_make(
    inp: Path = typer.Option(..., "--in", help="Файл с фичами (.parquet/.csv)"),
    train_out: Path = typer.Option(..., help="Паркет/CSV для train"),
    valid_out: Path = typer.Option(..., help="Паркет/CSV для valid"),
    time_col: str = typer.Option("first_seen", help="Имя временной колонки для time split"),
    valid_days: int = typer.Option(30, help="Сколько последних дней отдать в валидацию"),
     valid_ratio: Optional[float] = typer.Option(
        None,
        help="Доля объектов (0-1), которую отдать в валидацию по времени. Например 0.2 = последние 20%. Если задано — перекрывает --valid-days.",
        min=0.0,
        max=1.0,
    ),
):
    """
    Делит по времени:
    - если задан --valid-ratio: валидация = последние X% по временной колонке
    - иначе: валидация = последние valid_days.
    """
    df = _read_table(inp)
    if time_col not in df.columns:
        raise typer.BadParameter(f"Колонки {time_col} нет в данных")

    ts = pd.to_datetime(df[time_col], errors="coerce", utc=True)

    if valid_ratio is not None:
        if not (0 < valid_ratio < 1):
            raise typer.BadParameter("--valid-ratio должен быть в диапазоне (0, 1)")
        # Отсечка по квантилю времени: последние valid_ratio попадают в валидацию
        cutoff = ts.quantile(1 - valid_ratio)
        train_df = df[ts <= cutoff].copy()
        valid_df = df[ts > cutoff].copy()
        msg = f"[split] ratio={valid_ratio:.3f} cutoff={cutoff.isoformat()} train={len(train_df)} valid={len(valid_df)}"
    else:
        cutoff = ts.max() - pd.Timedelta(days=valid_days)
        train_df = df[ts <= cutoff].copy()
        valid_df = df[ts > cutoff].copy()
        msg = f"[split] cutoff={cutoff.isoformat()} train={len(train_df)} valid={len(valid_df)}"

    _write_table(train_df, train_out)
    _write_table(valid_df, valid_out)
    typer.echo(msg)
    
def _align_frame_to_schema(
    df: pd.DataFrame,
    expected_numeric: List[str],
    expected_categorical: List[str],
) -> pd.DataFrame:
    """Гарантирует наличие всех ожидаемых колонок и типов."""
    df = df.copy()

    # добавляем отсутствующие колонки
    for c in expected_numeric:
        if c not in df.columns:
            df[c] = np.nan
    for c in expected_categorical:
        if c not in df.columns:
            df[c] = ""

    # порядок колонок не критичен для ColumnTransformer, но
    # приведём базовые типы, чтобы избежать сюрпризов
    for c in expected_numeric:
        df[c] = pd.to_numeric(df[c], errors="coerce")
    for c in expected_categorical:
        if not pd.api.types.is_object_dtype(df[c]):
            df[c] = df[c].astype("string").fillna("")

    return df

def _fill_na_reasonable(df: pd.DataFrame, numeric_cols: List[str], cat_cols: List[str]) -> pd.DataFrame:
    df = df.copy()
    if numeric_cols:
        df[numeric_cols] = df[numeric_cols].fillna(df[numeric_cols].median())
    if cat_cols:
        for c in cat_cols:
            df[c] = df[c].fillna("")
    return df

def _fill_na_from_stats(df, num_cols, cat_cols, stats_num, stats_cat):
    df = df.copy()
    for c in num_cols:
        v = stats_num.get(c)
        if v is not None:
            df[c] = df[c].fillna(v)
    for c in cat_cols:
        v = stats_cat.get(c, "")
        df[c] = df[c].fillna(v if v is not None else "")
    return df



def _build_preprocessor(df: pd.DataFrame, target_col: str) -> Tuple[ColumnTransformer, List[str], List[str]]:
    # Определяем числовые/категориальные фичи
    drop_cols = {
        target_col,
        "id", "url", "title", "description", "address_norm",
        "first_seen", "last_seen", "created_at", "updated_at",
        "geom", "contact_phone_hash",
        # важное: убираем утечку таргета
        "price_per_m2", "price_per_m2_calc",
    } & set(df.columns)
    feat_df = df.drop(columns=list(drop_cols), errors="ignore")
    y = None
    if target_col in df.columns:
        y = df[target_col].astype(float)

    numeric_features = feat_df.select_dtypes(include=[np.number]).columns.tolist()
    categorical_features = feat_df.select_dtypes(include=["object", "category"]).columns.tolist()

    preprocessor = ColumnTransformer(
        transformers=[
            ("num", "passthrough", numeric_features),
            ("cat", OneHotEncoder(handle_unknown="ignore", sparse_output=False), categorical_features),
        ],
        remainder="drop",
        verbose_feature_names_out=False,
    )
    return preprocessor, numeric_features, categorical_features

def _fit_model(
    X: pd.DataFrame, y: pd.Series, objective: str = "regression", alpha: Optional[float] = None
) -> Pipeline:
    # гарантируем, что в LightGBM прилетает numpy-массив, а не DataFrame с именами фич
    X = np.asarray(X)

    params = dict(
        n_estimators=1200,
        learning_rate=0.03,
        subsample=0.9,
        colsample_bytree=0.9,
        num_leaves=63,
        min_child_samples=20,
        min_split_gain=0.0,
        reg_lambda=1.0,
        reg_alpha=0.0,
        n_jobs=-1,
        random_state=42,
    )
    if objective == "quantile":
        model = lgb.LGBMRegressor(objective="quantile", alpha=alpha, **params)
    else:
        model = lgb.LGBMRegressor(objective="regression", **params)
    pipe = Pipeline(steps=[("model", model)])
    pipe.fit(X, y)
    return pipe

def _rmse(y_true, y_pred) -> float:
    """Совместимый RMSE для разных версий sklearn."""
    try:
        from sklearn.metrics import root_mean_squared_error
        return float(root_mean_squared_error(y_true, y_pred))
    except Exception:
        try:
            return float(mean_squared_error(y_true, y_pred, squared=False))
        except TypeError:
            return float(mean_squared_error(y_true, y_pred) ** 0.5)

import uuid
from datetime import datetime, timezone

def _fmt_rub(v: float) -> str:
    try:
        return f"{int(round(v)):,}".replace(",", " ")
    except Exception:
        return str(v)

def _build_text_blocks(report: dict) -> dict:
    """
    Простая шаблонная генерация текста.
    Позже сюда можно подложить LangChain/LLM.
    """
    obj = report.get("object", {})
    pricing = report.get("pricing", {})
    explanation = report.get("explanation") or {}
    comps = report.get("comparables") or []

    addr = obj.get("address") or "объект"
    city = obj.get("city") or ""
    rooms = obj.get("rooms")
    area_total = obj.get("area_total")

    pred = pricing.get("prediction_rub")
    lo = pricing.get("interval_low_rub")
    hi = pricing.get("interval_high_rub")

    # --- краткое резюме ---
    parts = []
    if city:
        parts.append(f"Объект в городе {city}")
    if addr:
        parts.append(f"по адресу «{addr}»")
    header = ", ".join(parts) if parts else "Объект недвижимости"

    summary_short = f"{header}. "

    if pred is not None:
        summary_short += f"Оценочная ставка аренды: ~{_fmt_rub(pred)} ₽/мес"
        if lo is not None and hi is not None:
            summary_short += f" (диапазон { _fmt_rub(lo) }–{ _fmt_rub(hi) } ₽/мес)."
        else:
            summary_short += "."

    # --- факторы ---
    factors_summary: list[str] = []

    top_feats = (explanation or {}).get("top_features") or []
    # немного «русифицируем» самые частые фичи
    for feat in top_feats:
        name = feat.get("feature")
        contrib = float(feat.get("contribution", 0.0))
        if not name:
            continue

        direction = "повышает" if contrib > 0 else "снижает"
        contrib_abs = abs(contrib)

        if name == "area_total":
            factors_summary.append(
                f"Большая общая площадь квартиры (~{area_total} м²) заметно {direction} прогнозируемую стоимость."
            )
        elif name == "dist_to_center_km":
            factors_summary.append(
                f"Удалённость от центра города {direction} ожидаемую цену аренды."
            )
        elif name.startswith("house_material_"):
            mat = name.split("house_material_", 1)[1]
            factors_summary.append(
                f"Материал дома («{mat}») слабо влияет на итоговую оценку и лишь незначительно её {direction}."
            )
        elif name == "floor":
            factors_summary.append(
                f"Этаж расположения квартиры (этаж {obj.get('floor')}) немного {direction} стоимость аренды."
            )
        elif name == "metro_walk_min":
            factors_summary.append(
                f"Время пешей доступности до метро (≈{obj.get('metro_walk_min') or 'N'} минут) слегка {direction} цену."
            )
        else:
            # fallback
            factors_summary.append(f"Признак «{name}» умеренно {direction} оценку объекта (вклад {contrib_abs:.2f} в лог-пространстве).")

        if len(factors_summary) >= 5:
            break

    # --- длинное резюме (для PDF, можно упрощённо) ---
    summary_long = summary_short
    if factors_summary:
        summary_long += " Наибольшее влияние на оценку оказывают следующие факторы: " + " ".join(factors_summary)

    return {
        "summary_short": summary_short,
        "summary_long": summary_long,
        "factors_summary": factors_summary,
    }


def _build_explanation(
    artefact: dict,
    X: np.ndarray,
    top_n: int = 8,
) -> Optional[dict]:
    """
    Строит простое объяснение предсказания на основе LightGBM pred_contrib.

    X — уже преобразованный (preprocessor.transform) вектор признаков формы (1, n_features).
    Возвращает:
      - base_value (базовый уровень модели),
      - prediction_internal (base + сумма вкладов),
      - top_features (список признаков с максимальным вкладом по модулю).
    """
    median_pipe = artefact.get("median")
    preproc = artefact.get("preprocessor")
    if median_pipe is None or preproc is None:
        return None

    # имена фич после препроцессора
    try:
        feature_names = preproc.get_feature_names_out()
    except Exception:
        feature_names = None

    # стараемся получить contrib через Pipeline; если не получится — лезем до самой модели
    try:
        contrib = median_pipe.predict(X, pred_contrib=True)[0]
    except TypeError:
        try:
            model = median_pipe.named_steps["model"]
            contrib = model.predict(X, pred_contrib=True)[0]
        except Exception:
            return None
    except Exception:
        return None

    contrib = np.asarray(contrib, dtype=float)
    if contrib.ndim != 1 or contrib.size < 2:
        return None

    # последний элемент — bias (base value)
    bias = float(contrib[-1])
    contrib_feat = contrib[:-1]

    # подгоняем имена фич
    if feature_names is not None and len(feature_names) == len(contrib_feat):
        feat_names = [str(f) for f in feature_names]
    else:
        feat_names = [f"f_{i}" for i in range(len(contrib_feat))]

    abs_c = np.abs(contrib_feat)
    order = np.argsort(abs_c)[::-1][:top_n]

    top = []
    for idx in order:
        top.append({
            "feature": feat_names[idx],
            "contribution": float(contrib_feat[idx]),
            "abs_contribution": float(abs_c[idx]),
        })

    internal_pred = float(bias + contrib_feat.sum())
    return {
        "is_log_space": bool(artefact.get("log_target", False)),
        "base_value": bias,
        "prediction_internal": internal_pred,
        "top_features": top,
    }


def _conformal_eps_log(y_true_log: np.ndarray, y_pred_log: np.ndarray, q: float = 0.9) -> float:
    """
    Квантиль абсолютной ошибки в лог-пространстве.
    Используем для построения конформального предиктивного интервала.
    """
    resid = np.abs(y_true_log - y_pred_log)
    return float(np.quantile(resid, q))


@app.command("train")
def train_fit(
    train_path: Path = typer.Option(..., help="train parquet/csv"),
    valid_path: Path = typer.Option(..., help="valid parquet/csv"),
    model_out: Path = typer.Option(..., help="куда сохранить артефакт .joblib"),
    target: str = typer.Option("price_rub", help="Целевая колонка"),
    log_target: bool = typer.Option(True, help="Лог-преобразование целевой"),
    with_intervals: bool = typer.Option(True, help="Обучать квантильные модели 0.1/0.9"),
    use_local_stats: bool = typer.Option(
        False,
        help="Добавлять локальные признаки (local_avg_price_rub, local_density_count) по H3+rooms. "
             "На маленьком датасете лучше оставить False.",
    ),
):
    """
    Обучает LGBM: медианный предиктор (через обычную регрессию) + опционально квантили.
    """
    train_df = _read_table(train_path)
    valid_df = _read_table(valid_path)

    # типы
    train_df = _coerce_types(train_df)
    valid_df = _coerce_types(valid_df)
    
    price_min, price_max = 10_000, 600_000  # можно потом подвинуть под твои данные
    train_df = train_df[train_df[target].between(price_min, price_max)]
    valid_df = valid_df[valid_df[target].between(price_min, price_max)]
        # гарантируем H3 и rooms_bucket (это можно делать всегда)
    train_df = _ensure_h3(train_df, res=7)
    valid_df = _ensure_h3(valid_df, res=7)

    train_df["rooms_bucket"] = train_df["rooms"].apply(_rooms_bucket)
    valid_df["rooms_bucket"] = valid_df["rooms"].apply(_rooms_bucket)

    grp = None
    global_mean_price = None

    if use_local_stats:
        # локальные статы по рынку
        grp = (
            train_df
            .groupby(["h3_7", "rooms_bucket"], dropna=True)["price_rub"]
            .agg(["sum", "count"])
            .reset_index()
            .rename(columns={"sum": "sum_price", "count": "cnt"})
        )
        global_mean_price = float(train_df["price_rub"].mean())

        # --- train: leave-one-out ---
        train_df = train_df.merge(grp, on=["h3_7", "rooms_bucket"], how="left")

        train_df["local_cnt"] = train_df["cnt"] - 1
        train_df["local_sum"] = train_df["sum_price"] - train_df["price_rub"]

        train_df["local_avg_price_rub"] = np.where(
            train_df["local_cnt"] > 0,
            train_df["local_sum"] / train_df["local_cnt"],
            global_mean_price,
        )
        train_df["local_density_count"] = train_df["cnt"].fillna(0)

        train_df = train_df.drop(columns=["sum_price", "cnt", "local_sum", "local_cnt"])

        # --- valid: просто по train-группам ---
        valid_df = valid_df.merge(grp, on=["h3_7", "rooms_bucket"], how="left")

        valid_df["local_avg_price_rub"] = np.where(
            valid_df["cnt"].notna(),
            valid_df["sum_price"] / valid_df["cnt"],
            global_mean_price,
        )
        valid_df["local_density_count"] = valid_df["cnt"].fillna(0)

        valid_df = valid_df.drop(columns=["sum_price", "cnt"])


    # подготовка препроцессора
    preproc, num_cols, cat_cols = _build_preprocessor(train_df, target)

    # выделяем X/y
    def _xy(df: pd.DataFrame):
        X = df.drop(columns=[c for c in [target] if c in df.columns], errors="ignore")
        y = df[target].astype(float)
        return X, y

    X_tr_raw, y_tr_raw = _xy(train_df)
    X_va_raw, y_va_raw = _xy(valid_df)

    # лог-цель (устойчивость к выбросам)
    if log_target:
        y_tr = np.log(y_tr_raw.clip(lower=1.0))
        y_va = np.log(y_va_raw.clip(lower=1.0))
    else:
        y_tr, y_va = y_tr_raw, y_va_raw

    # fit preprocesser на train
    preproc.fit(X_tr_raw)
    
    # Импутационные статистики
    try:
        num_cols_list = preproc.transformers_[0][2] if preproc.transformers_ else []
    except Exception:
        num_cols_list = []
    num_stats = {}
    if num_cols_list:
        try:
            num_stats = X_tr_raw[num_cols_list].median(numeric_only=True).to_dict()
        except Exception:
            num_stats = {}
    cat_cols_list = []
    try:
        if len(preproc.transformers_) > 1:
            cat_cols_list = preproc.transformers_[1][2]
    except Exception:
        cat_cols_list = []
    cat_stats = {}
    for c in cat_cols_list:
        try:
            cat_stats[c] = X_tr_raw[c].mode(dropna=False).iloc[0]
        except Exception:
            cat_stats[c] = ""
    
    X_tr = preproc.transform(X_tr_raw)
    X_va = preproc.transform(X_va_raw)

    # основной (медианный) предиктор
    median_pipe = _fit_model(X_tr, y_tr, objective="regression")

    # квантили
    q10_pipe = q90_pipe = None
    if with_intervals:
        q10_pipe = _fit_model(X_tr, y_tr, objective="quantile", alpha=0.1)
        q90_pipe = _fit_model(X_tr, y_tr, objective="quantile", alpha=0.9)

    # валидация + конформальная калибровка
    if log_target:
        # модель училась на логе — работаем в лог-пространстве
        y_va_pred_log = median_pipe.predict(X_va)
        pred_va = np.exp(y_va_pred_log)
        eps_conf = _conformal_eps_log(y_va, y_va_pred_log, q=0.9)
    else:
        y_va_pred = median_pipe.predict(X_va)
        pred_va = y_va_pred
        eps_conf = None  # конформал в таком виде делаем только для лог-таргета

    mae = mean_absolute_error(y_va_raw, pred_va)
    rmse = _rmse(y_va_raw, pred_va)

    artefact = {
        "preprocessor": preproc,
        "median": median_pipe,
        "q10": q10_pipe,
        "q90": q90_pipe,
        "log_target": log_target,
        "target": target,
        "numeric_features": num_cols,
        "categorical_features": cat_cols,
        "metrics": {"valid_mae": float(mae), "valid_rmse": float(rmse)},
        "conformal_eps_log": eps_conf,
        "use_local_stats": use_local_stats,
        "local_stats": grp if use_local_stats else None,
        "local_global_mean": global_mean_price if use_local_stats else None,

    }

    model_out.parent.mkdir(parents=True, exist_ok=True)
    dump(artefact, model_out)
    typer.echo(f"[train] saved {model_out} | valid MAE={mae:.2f}, RMSE={rmse:.2f}")

@app.command("predict-one")
def predict_one(
    model_path: Path = typer.Option(..., help="путь к .joblib"),
    input_json: Path = typer.Option(..., help="JSON с одним объектом (dict)"),
    out: Optional[Path] = typer.Option(None, help="Куда сохранить результат (.json)"),
):
    """
    Прогноз для одного объекта: точка + (если обучены) интервалы по квантилям.
    """
    artefact = load(model_path)
    preproc = artefact["preprocessor"]
    median_pipe = artefact["median"]
    q10_pipe = artefact.get("q10")
    q90_pipe = artefact.get("q90")
    log_target = artefact["log_target"]
    target = artefact["target"]

    obj = json.loads(Path(input_json).read_text(encoding="utf-8"))
    df = pd.DataFrame([obj])
    df = _coerce_types(df)
    df = _ensure_h3(df, res=7)

    expected_num = artefact.get("numeric_features", [])
    expected_cat = artefact.get("categorical_features", [])
    
     # локальный якорь рынка
    if artefact.get("use_local_stats"):
        df["rooms_bucket"] = df["rooms"].apply(_rooms_bucket)
        df = _attach_local_price_stats(
            df,
            artefact.get("local_stats"),
            artefact.get("local_global_mean"),
        )

    # гарантируем h3 и приведение типов
    df = _ensure_h3(df, res=7)
    df = _align_frame_to_schema(df, expected_num, expected_cat)
    df = _fill_na_from_stats(df, expected_num, expected_cat,
                             artefact.get("impute_num", {}),
                             artefact.get("impute_cat", {}))
    # Фолбэк на случай отсутствующих статистик/новых колонок
    df = _fill_na_reasonable(df, expected_num, expected_cat)

    # формируем X в том же виде, как на трейне
    X = df.copy()
    if target in X.columns:
        X = X.drop(columns=[target], errors="ignore")
    X = preproc.transform(X)

    def _inv(v):
        return float(np.exp(v)) if log_target else float(v)

    raw_pred = float(median_pipe.predict(X)[0])
    y_hat = _inv(median_pipe.predict(X)[0])

    pi_low = pi_high = None
    eps_conf = artefact.get("conformal_eps_log")

    if eps_conf is not None and log_target:
        # конформальный интервал в лог-пространстве
        pi_low = float(np.exp(raw_pred - eps_conf))
        pi_high = float(np.exp(raw_pred + eps_conf))
    elif q10_pipe is not None and q90_pipe is not None:
        # fallback на квантильные модели
        pi_low = _inv(q10_pipe.predict(X)[0])
        pi_high = _inv(q90_pipe.predict(X)[0])

    # упорядочим интервалы, чтобы pi_low <= y_hat <= pi_high
    if pi_low is not None and pi_high is not None:
        low, mid, high = sorted([pi_low, y_hat, pi_high])
        if not (low <= y_hat <= high):
            if y_hat < low:
                low, high = y_hat, high
            elif y_hat > high:
                low, high = low, y_hat
        pi_low, pi_high = low, high


    explanation = _build_explanation(artefact, X, top_n=8)

    result = {
        "prediction": y_hat,
        "pi_low": pi_low,
        "pi_high": pi_high,
        "model_metrics": artefact.get("metrics", {}),
    }
    if explanation is not None:
        result["explanation"] = explanation

    if out:
        out.parent.mkdir(parents=True, exist_ok=True)
        out.write_text(json.dumps(result, ensure_ascii=False, indent=2), encoding="utf-8")
        typer.echo(f"[predict-one] -> {out}")
    else:
        typer.echo(json.dumps(result, ensure_ascii=False, indent=2))


db_app = typer.Typer(help="DB утилиты: выгрузка данных из PostgreSQL")
app.add_typer(db_app, name="db")

@db_app.command("export")
def db_export(
    out: Path = typer.Option(..., help="Выходной файл (.parquet или .csv)"),
    sql_file: Optional[Path] = typer.Option(None, help="Путь к .sql с запросом. Если не задан, используется дефолтный SELECT."),
    dsn: Optional[str] = typer.Option(None, help="DSN для PostgreSQL. По умолчанию читается из переменной окружения POSTGRES_DSN_URL."),
    chunksize: int = typer.Option(0, help="Размер чанка при выгрузке. 0 = читать целиком в память."),
):
    """
    Выполняет SELECT к PostgreSQL и сохраняет результат в .parquet или .csv.
    Пример DSN: postgresql+psycopg2://user:pass@host:5432/dbname
    """
    # 1) DSN
    dsn = dsn or os.environ.get("POSTGRES_DSN_URL")
    if not dsn:
        raise typer.BadParameter("Не задан DSN. Укажи --dsn или переменную окружения POSTGRES_DSN_URL.")

    # 2) SQL
    if sql_file is not None:
        query = Path(sql_file).read_text(encoding="utf-8")
    else:
        # дефолтный запрос (можно править под себя)
        query = """
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

    out = Path(out)
    out.parent.mkdir(parents=True, exist_ok=True)
    ext = out.suffix.lower()
    if ext not in {".parquet", ".csv"}:
        raise typer.BadParameter("Поддерживаются только .parquet или .csv")

    engine = sa.create_engine(dsn)

    # 3) Чтение: целиком или чанками
    total = 0
    if chunksize and chunksize > 0:
        if ext == ".parquet":
            writer = None
            try:
                for chunk in pd.read_sql(query, engine, chunksize=chunksize):
                    # Привод типов и дат
                    chunk = _coerce_types(chunk)
                    # Явно в UTC
                    for c in ("first_seen", "last_seen"):
                        if c in chunk.columns:
                            chunk[c] = pd.to_datetime(chunk[c], errors="coerce", utc=True)

                    table = pa.Table.from_pandas(chunk, preserve_index=False)
                    if writer is None:
                        writer = pq.ParquetWriter(out, table.schema)
                    writer.write_table(table)
                    total += len(chunk)
                if writer is not None:
                    writer.close()
            finally:
                if writer is not None:
                    writer.close()
        else:
            # CSV: сначала с заголовком, потом без
            first = True
            for chunk in pd.read_sql(query, engine, chunksize=chunksize):
                chunk = _coerce_types(chunk)
                for c in ("first_seen", "last_seen"):
                    if c in chunk.columns:
                        chunk[c] = pd.to_datetime(chunk[c], errors="coerce", utc=True)
                chunk.to_csv(out, index=False, mode=("w" if first else "a"), header=first)
                total += len(chunk)
                first = False
    else:
        df = pd.read_sql(query, engine)
        df = _coerce_types(df)
        for c in ("first_seen", "last_seen"):
            if c in df.columns:
                df[c] = pd.to_datetime(df[c], errors="coerce", utc=True)
        if ext == ".parquet":
            df.to_parquet(out, index=False)
        else:
            df.to_csv(out, index=False)
        total = len(df)

    typer.echo(f"[db export] saved {out} rows={total}")


@app.command("predict-address")
def predict_address(
    model_path: Path = typer.Option(..., help="путь к .joblib"),
    # Можно передать параметры квартиры через флаги или целиком файлом JSON
    params: Optional[Path] = typer.Option(None, "--params", "--input-json", help="JSON с параметрами квартиры (address, city, rooms, area_total, ...). Если указан, перекрывает соответствующие флаги."),
    address: Optional[str] = typer.Option(None, help="Адрес (улица, дом, квартира не обязательно)"),
    city: str = typer.Option("Москва"),
    rooms: Optional[int] = typer.Option(None, help="0=студия, 1, 2, ..."),
    area_total: Optional[float] = typer.Option(None, help="Общая площадь, м²"),
    area_living: Optional[float] = typer.Option(None, help="Жилая площадь, м²"),
    area_kitchen: Optional[float] = typer.Option(None, help="Площадь кухни, м²"),
    floor: Optional[int] = typer.Option(None),
    floors_total: Optional[int] = typer.Option(None),
    year_built: Optional[int] = typer.Option(None),
    house_material: Optional[str] = typer.Option(None),
    condition: Optional[str] = typer.Option(None),
    dsn: Optional[str] = typer.Option(None, help="DSN PostgreSQL для ref_metro/плотности (иначе возьмем из POSTGRES_DSN_URL)"),
    out: Optional[Path] = typer.Option(None, help="Куда сохранить результат (.json)"),
    geocode_cache_ttl_days: int = typer.Option(365, help="Через сколько дней запись кэша считать устаревшей"),
    no_geocode_cache: bool = typer.Option(False, help="Не использовать кэш геокодера вообще"),
    building_cache_ttl_days: int = typer.Option(365, help="TTL записи building_cache"),
    no_building_cache: bool = typer.Option(False, help="Не использовать кэш building_cache"),
    overpass_radius_m: int = typer.Option(120, help="Поисковый радиус для Overpass"),
    comps_path: Optional[Path] = typer.Option(
        None,
        help="Паркет/CSV с фичами объявлений для поиска компараблов (обычно train/features)."
    ),
    comps_k: int = typer.Option(5, help="Сколько компараблов возвращать"),
    comps_max_radius_km: float = typer.Option(5.0, help="Максимальный радиус поиска компараблов, км"),
    with_text: bool = typer.Option(
        False,
        "--with-text",
        help="Сгенерировать текстовое объяснение через GigaChat",
    ),

):
    """
    Сценарий для пользователя: вводит адрес и базовые параметры — мы сами геокодим,
    достраиваем геофичи (метро/центр/плотность), формируем фичи и считаем прогноз.

    Можно передать параметры либо флагами, либо одним JSON-файлом через --params/--input-json
    со структурой вида:
    {
      "address": "улица Пушкина, дом Колотушкина",
      "city": "Москва",
      "rooms": 2,
      "area_total": 60,
      "area_living": 35,
      "area_kitchen": 10,
      "floor": 5,
      "floors_total": 12,
      "year_built": 2008,
      "house_material": "монолит",
      "condition": "хорошее",
    }
    """
    artefact = load(model_path)
    preproc = artefact["preprocessor"]
    expected_num = artefact.get("numeric_features", [])
    expected_cat = artefact.get("categorical_features", [])
    target = artefact["target"]
    log_target = artefact["log_target"]

    # 0) Параметры квартиры из файла (если указан)
    lat: Optional[float] = None
    lon: Optional[float] = None
    if params is not None:
        try:
            obj_in = json.loads(Path(params).read_text(encoding="utf-8"))
            if not isinstance(obj_in, dict):
                raise ValueError("Ожидается JSON-объект (dict) с полями квартиры")
        except Exception as e:
            raise typer.BadParameter(f"Не удалось прочитать --params: {e}")

        address = obj_in.get("address", address)
        city = obj_in.get("city", city)
        rooms = obj_in.get("rooms", rooms)
        area_total = obj_in.get("area_total", area_total)
        area_living = obj_in.get("area_living", area_living)
        area_kitchen = obj_in.get("area_kitchen", area_kitchen)
        floor = obj_in.get("floor", floor)
        floors_total = obj_in.get("floors_total", floors_total)
        year_built = obj_in.get("year_built", year_built)
        house_material = obj_in.get("house_material", house_material)
        condition = obj_in.get("condition", condition)
        lat = obj_in.get("lat")
        lon = obj_in.get("lon")

    # Валидация ключевых полей
    if rooms is None:
        raise typer.BadParameter("Не задано количество комнат (rooms). Укажи флагом или в --params")
    if area_total is None:
        raise typer.BadParameter("Не задана общая площадь (area_total). Укажи флагом или в --params")

    # 1) Геокод (если lat/lon не заданы)
    if lat is None or lon is None:
        if not address:
            raise typer.BadParameter("Не задан адрес. Укажи --address или параметр 'address' в --params")

        # Инициализируем подключение (если есть DSN) и таблицу кэша
        dsn = dsn or os.environ.get("POSTGRES_DSN_URL")
        engine = sa.create_engine(dsn) if dsn else None
        if engine and not no_geocode_cache:
            try:
                _ensure_geocode_cache_table(engine)
            except Exception:
                pass

        # 1.1 Пробуем кэш
        if engine and not no_geocode_cache:
            cached = _get_cached_geocode(engine, city=city, address=address, max_age_days=geocode_cache_ttl_days)
            if cached:
                lat, lon = cached

        # 1.2 Если промах кэша — Nominatim
        if lat is None or lon is None:
            coords = _nominatim_geocode(address, city=city)
            if not coords:
                raise typer.BadParameter("Адрес не найден геокодером. Попробуй уточнить формулировку.")
            lat, lon = coords
            # 1.3 Запишем в кэш
            if engine and not no_geocode_cache:
                try:
                    _put_cached_geocode(engine, city=city, address=address, lat=lat, lon=lon)
                except Exception:
                    pass

    # 2) Геофичи
    center = CITY_CENTERS.get(city, CITY_CENTERS["Москва"])
    dist_to_center_km = _haversine_km(lat, lon, center[0], center[1])

    # 3) Метро и (опционально) плотность
    dsn = dsn or os.environ.get("POSTGRES_DSN_URL")
    metro_name = None
    dist_to_metro_m = None
    metro_walk_min = None
    density_500m = None

    if dsn:
        engine = sa.create_engine(dsn)
        metros = _load_metro_points(engine, city=city)
        metro_name, dist_to_metro_m, metro_walk_min = _nearest_metro(lat, lon, metros)
        density_500m = _density_500m(engine, lat, lon, days_back=90)

    engine = sa.create_engine(dsn) if dsn else None
    if engine and not no_building_cache:
        try:
            _ensure_building_cache_table(engine)
        except Exception:
            pass

    cache_bld = None
    if engine and not no_building_cache:
        cache_bld = _get_cached_building(engine, city=city, address=address, max_age_days=building_cache_ttl_days)

    floors_total_final = floors_total
    year_built_final = year_built
    material_final = house_material

    if cache_bld:
        floors_total_final = floors_total_final or cache_bld.get("floors_total")
        year_built_final   = year_built_final   or cache_bld.get("year_built")
        material_final     = material_final     or cache_bld.get("house_material")

    # если всё ещё не хватает — спросим Overpass
    need_overpass = (floors_total_final is None) or (year_built_final is None) or (not material_final)
    if need_overpass:
        enrich = _overpass_nearby_building(lat, lon, radius_m=overpass_radius_m)
        if enrich:
            floors_total_final = floors_total_final or enrich.get("floors_total")
            year_built_final   = year_built_final   or enrich.get("year_built")
            material_final     = material_final     or enrich.get("house_material")
            if engine and not no_building_cache:
                try:
                    _put_cached_building(engine, city=city, address=address, lat=lat, lon=lon, enriched=enrich)
                except Exception:
                    pass

    # обновим объект перед фичами
    floors_total = floors_total_final
    year_built = year_built_final
    house_material = material_final
    
    # 4) Производные фичи
    now_year = pd.Timestamp.utcnow().year
    age_house = (now_year - year_built) if year_built else None

    # 5) Собираем «объект»
    obj = {
        "deal_type": "rent_long",
        "city": city,
        "address_norm": address,
        "rooms": rooms,
        "area_total": area_total,
        "area_living": area_living,
        "area_kitchen": area_kitchen,
        "floor": floor,
        "floors_total": floors_total,
        "year_built": year_built,
        "house_material": house_material or "",
        "condition": condition or "",
        "lat": lat,
        "lon": lon,
        "dist_to_center_km": dist_to_center_km,
        "dist_to_metro_m": dist_to_metro_m,
        "metro_station": metro_name or "",
        "metro_walk_min": metro_walk_min,
        "density_500m": density_500m,
        "first_seen": pd.Timestamp.utcnow(),   # не влияет на предикт, но полезно для унификации
        "last_seen": pd.Timestamp.utcnow(),
        "age_house": age_house,
    }

    df = pd.DataFrame([obj])
    df = _coerce_types(df)
    df = _ensure_h3(df, res=7)
    
    if artefact.get("use_local_stats"):
        df["rooms_bucket"] = df["rooms"].apply(_rooms_bucket)
        df = _attach_local_price_stats(
            df,
            artefact.get("local_stats"),
            artefact.get("local_global_mean"),
        )


    # 6) Выравнивание под схему фич и заполнение пропусков
    df = _align_frame_to_schema(df, expected_num, expected_cat)
    df = _fill_na_from_stats(df, expected_num, expected_cat,
                             artefact.get("impute_num", {}),
                             artefact.get("impute_cat", {}))
    # Фолбэк на случай отсутствующих статистик/новых колонок
    df = _fill_na_reasonable(df, expected_num, expected_cat)

    X = df.copy()
    if target in X.columns:
        X = X.drop(columns=[target], errors="ignore")
    X = preproc.transform(X)

    median_pipe = artefact["median"]
    q10_pipe = artefact.get("q10")
    q90_pipe = artefact.get("q90")

    def _inv(v): return float(np.exp(v)) if log_target else float(v)

    raw_pred = float(median_pipe.predict(X)[0])
    y_hat = _inv(raw_pred)

    pi_low = pi_high = None
    eps_conf = artefact.get("conformal_eps_log")

    if eps_conf is not None and log_target:
        pi_low = float(np.exp(raw_pred - eps_conf))
        pi_high = float(np.exp(raw_pred + eps_conf))
    elif q10_pipe is not None and q90_pipe is not None:
        pi_low = _inv(q10_pipe.predict(X)[0])
        pi_high = _inv(q90_pipe.predict(X)[0])

    if pi_low is not None and pi_high is not None:
        low, mid, high = sorted([pi_low, y_hat, pi_high])
        if not (low <= y_hat <= high):
            if y_hat < low:
                low, high = y_hat, high
            elif y_hat > high:
                low, high = low, y_hat
        pi_low, pi_high = low, high

        # --- Компараблы, если задан comps_path ---
    comparables: List[dict] = []
    if comps_path is not None:
        comps_path = Path(comps_path)
        if comps_path.exists():
            comps_df = _read_table(comps_path)
            comparables = _find_comparables(
                target=obj,
                comps_df=comps_df,
                k=comps_k,
                max_radius_km=comps_max_radius_km,
            )

    explanation = _build_explanation(artefact, X, top_n=8)
    
    report_id = str(uuid.uuid4())
    generated_at = datetime.now(timezone.utc).isoformat()

    obj_block = {
        "address": address,
        "city": city,
        "rooms": rooms,
        "area_total": area_total,
        "area_living": area_living,
        "area_kitchen": area_kitchen,
        "floor": floor,
        "floors_total": floors_total,
        "year_built": year_built,
        "house_material": house_material,
        "condition": condition,
    }

    enriched_block = {
        "lat": lat,
        "lon": lon,
        "dist_to_center_km": dist_to_center_km,
        "metro_station": metro_name,
        "dist_to_metro_m": dist_to_metro_m,
        "metro_walk_min": metro_walk_min,
        "density_500m": density_500m,
    }

    pricing_block = {
        "prediction_rub": y_hat,
        "interval_low_rub": pi_low,
        "interval_high_rub": pi_high,
        "currency": "RUB",
        "deal_type": "rent_long",
    }

    model_metrics = artefact.get("metrics", {})
    model_info = {
        "model_name": "rent_lgbm",
        "target": artefact.get("target"),
        "log_target": artefact.get("log_target", False),
        "valid_mae": model_metrics.get("valid_mae"),
        "valid_rmse": model_metrics.get("valid_rmse"),
    }

    report = {
        "report_id": report_id,
        "generated_at": generated_at,
        "version": "realval_report_v1",
        "object": obj_block,
        "enriched": enriched_block,
        "pricing": pricing_block,
        "comparables": comparables,
        "explanation": explanation,
        "model_info": model_info,
    }

    # текстовые блоки (пока шаблоны, потом можно переключить на LLM)
    if with_text:
        try:
            text_blocks = generate_text_blocks(report)
            report["text"] = text_blocks
        except Exception as e:
            typer.echo(f"[llm warning] не удалось сгенерировать текст: {e}", err=True)
    else: 
        report["text_blocks"] = _build_text_blocks(report)
        typer.echo(f"используется шаблонный текст")
            


    # финальный вывод для CLI
    if out:
        out.parent.mkdir(parents=True, exist_ok=True)
        out.write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
        typer.echo(f"[predict-address] -> {out}")
    else:
        typer.echo(json.dumps(report, ensure_ascii=False, indent=2))
        
    # result = {
    #     "input": {
    #         "address": address, "city": city, "rooms": rooms, "area_total": area_total,
    #         "area_living": area_living, "area_kitchen": area_kitchen,
    #         "floor": floor, "floors_total": floors_total, "year_built": year_built,
    #         "house_material": house_material, "condition": condition
    #     },
    #     "enriched": {
    #         "lat": lat, "lon": lon, "dist_to_center_km": dist_to_center_km,
    #         "metro_station": metro_name, "dist_to_metro_m": dist_to_metro_m,
    #         "metro_walk_min": metro_walk_min, "density_500m": density_500m
    #     },
    #     "prediction": y_hat,
    #     "pi_low": pi_low,
    #     "pi_high": pi_high,
    #     "comparables": comparables,
    # }
    
    # if explanation is not None:
    #     result["explanation"] = explanation

    # if out:
    #     out.parent.mkdir(parents=True, exist_ok=True)
    #     out.write_text(json.dumps(result, ensure_ascii=False, indent=2), encoding="utf-8")
    #     typer.echo(f"[predict-address] -> {out}")
    # else:
    #     typer.echo(json.dumps(result, ensure_ascii=False, indent=2))
