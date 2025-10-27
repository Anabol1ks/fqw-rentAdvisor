from __future__ import annotations
import json
from pathlib import Path
from typing import Optional, List, Tuple

import os
import sqlalchemy as sa
import pyarrow as pa
import pyarrow.parquet as pq

from dotenv import load_dotenv


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

# ---------- Commands ----------

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
):
    """
    Делит по времени: последние valid_days — валидация. (Черновик, spatial CV добавим позже.)
    """
    df = _read_table(inp)
    if time_col not in df.columns:
        raise typer.BadParameter(f"Колонки {time_col} нет в данных")

    ts = pd.to_datetime(df[time_col], errors="coerce", utc=True)
    cutoff = ts.max() - pd.Timedelta(days=valid_days)
    train_df = df[ts <= cutoff].copy()
    valid_df = df[ts > cutoff].copy()

    _write_table(train_df, train_out)
    _write_table(valid_df, valid_out)
    typer.echo(f"[split] cutoff={cutoff.isoformat()} train={len(train_df)} valid={len(valid_df)}")

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


def _build_preprocessor(df: pd.DataFrame, target_col: str) -> Tuple[ColumnTransformer, List[str], List[str]]:
    # Определяем числовые/категориальные фичи
    drop_cols = {
        target_col, "id", "url", "title", "description", "address_norm", "first_seen",
        "last_seen", "created_at", "updated_at", "geom", "contact_phone_hash"
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
    params = dict(
        n_estimators=1200,
        learning_rate=0.03,
        subsample=0.8,
        colsample_bytree=0.8,
        num_leaves=64,
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


@app.command("train")
def train_fit(
    train_path: Path = typer.Option(..., help="train parquet/csv"),
    valid_path: Path = typer.Option(..., help="valid parquet/csv"),
    model_out: Path = typer.Option(..., help="куда сохранить артефакт .joblib"),
    target: str = typer.Option("price_rub", help="Целевая колонка"),
    log_target: bool = typer.Option(True, help="Лог-преобразование целевой"),
    with_intervals: bool = typer.Option(True, help="Обучать квантильные модели 0.1/0.9"),
):
    """
    Обучает LGBM: медианный предиктор (через обычную регрессию) + опционально квантили.
    """
    train_df = _read_table(train_path)
    valid_df = _read_table(valid_path)

    # типы
    train_df = _coerce_types(train_df)
    valid_df = _coerce_types(valid_df)

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
    X_tr = preproc.transform(X_tr_raw)
    X_va = preproc.transform(X_va_raw)

    # основной (медианный) предиктор
    median_pipe = _fit_model(X_tr, y_tr, objective="regression")

    # квантили
    q10_pipe = q90_pipe = None
    if with_intervals:
        q10_pipe = _fit_model(X_tr, y_tr, objective="quantile", alpha=0.1)
        q90_pipe = _fit_model(X_tr, y_tr, objective="quantile", alpha=0.9)

    # валидация
    def _inv(v):
        return np.exp(v) if log_target else v

    pred_va = _inv(median_pipe.predict(X_va))
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

    # гарантируем h3 и приведение типов
    df = _ensure_h3(df, res=7)
    df = _align_frame_to_schema(df, expected_num, expected_cat)
    df = _fill_na_reasonable(df, expected_num, expected_cat)

    # формируем X в том же виде, как на трейне
    X = df.copy()
    if target in X.columns:
        X = X.drop(columns=[target], errors="ignore")
    X = preproc.transform(X)

    def _inv(v):
        return float(np.exp(v)) if log_target else float(v)

    y_hat = _inv(median_pipe.predict(X)[0])
    pi_low = pi_high = None
    if q10_pipe is not None and q90_pipe is not None:
        pi_low = _inv(q10_pipe.predict(X)[0])
        pi_high = _inv(q90_pipe.predict(X)[0])
        # упорядочим интервалы, чтобы pi_low <= y_hat <= pi_high
        
    if pi_low is not None and pi_high is not None:
        low, mid, high = sorted([pi_low, y_hat, pi_high])
        # стараемся сохранить середину как y_hat
        # если y_hat выпал за границы, просто сдвигаем границы
        if not (low <= y_hat <= high):
            if y_hat < low:
                low, high = y_hat, high
            elif y_hat > high:
                low, high = low, y_hat
        pi_low, pi_high = low, high

    result = {
        "prediction": y_hat,
        "pi_low": pi_low,
        "pi_high": pi_high,
        "model_metrics": artefact.get("metrics", {}),
    }

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
