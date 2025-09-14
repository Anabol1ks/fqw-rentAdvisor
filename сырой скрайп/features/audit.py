import os
from pathlib import Path
import json
import numpy as np
import pandas as pd
import mlflow
import matplotlib

# Используем бэкэнд без окна для серверов/CI
matplotlib.use("Agg")
import matplotlib.pyplot as plt

# ---- Настройки путей ----
SCRIPT_DIR = Path(__file__).resolve().parent
PROJECT_ROOT = SCRIPT_DIR.parent
SPLITS_DIR = PROJECT_ROOT / "data" / "splits"
ARTIFACTS_DIR = PROJECT_ROOT / "reports" / "audit"
ARTIFACTS_DIR.mkdir(parents=True, exist_ok=True)

TRAIN_PATH = SPLITS_DIR / "train.parquet"
TEST_PATH = SPLITS_DIR / "test.parquet"
MLFLOW_EXPERIMENT = "eda_audit"

# ---- Базовые колонки ----
TARGET = "price_rub"
NUM_FEATS = [
    "area_total", "area_kitchen", "floor", "floors_total",
    "year_built", "dist_to_metro_m", "dist_to_center_km", "density_500m",
    "price_per_m2"
]
CAT_FEATS = [
    "deal_type", "city", "district", "house_material", "condition", "metro_station"
]
OPTIONAL_COLS = ["is_active", "first_seen", "last_seen", "source", "external_id", "lat", "lon"]

# ---- Параметры аудита ----
# Квантили для отсечения экстрима (winsor)
LOW_Q = 0.005
HIGH_Q = 0.995


def _safe_log1p(x: pd.Series) -> pd.Series:
    x = x.copy()
    x = x.where(x > 0, np.nan)
    return np.log1p(x)


def _read_data() -> pd.DataFrame:
    if not TRAIN_PATH.exists() or not TEST_PATH.exists():
        raise FileNotFoundError(f"Expected splits at {TRAIN_PATH} and {TEST_PATH}")
    tr = pd.read_parquet(TRAIN_PATH)
    te = pd.read_parquet(TEST_PATH)
    df = pd.concat([tr.assign(_split="train"), te.assign(_split="test")], ignore_index=True)
    return df


def _coerce_types(df: pd.DataFrame) -> pd.DataFrame:
    df = df.copy()
    # Приводим числовые
    for col in NUM_FEATS + [TARGET]:
        if col in df.columns:
            df[col] = pd.to_numeric(df[col], errors="coerce")
    # Категории в строки
    for col in CAT_FEATS:
        if col in df.columns:
            df[col] = df[col].astype("string")
    # Даты
    for col in ["first_seen", "last_seen"]:
        if col in df.columns:
            df[col] = pd.to_datetime(df[col], errors="coerce", utc=True)
    # Булевы
    if "is_active" in df.columns and df["is_active"].dtype != bool:
        try:
            df["is_active"] = df["is_active"].astype("boolean")
        except Exception:
            pass
    return df


def _describe_numeric(df: pd.DataFrame, cols: list[str]) -> pd.DataFrame:
    if not cols:
        return pd.DataFrame()
    desc = df[cols].describe(percentiles=[0.01, 0.05, 0.25, 0.5, 0.75, 0.95, 0.99]).T
    desc["missing"] = df[cols].isna().sum()
    return desc


def _plot_hist(series: pd.Series, title: str, fname: Path, log_scale=False, bins=80):
    s = series.dropna()
    if log_scale:
        s = s[s > 0]
        s = np.log1p(s)
    plt.figure()
    plt.hist(s, bins=bins)
    plt.title(title)
    plt.xlabel(("log1p " if log_scale else "") + title)
    plt.ylabel("count")
    plt.tight_layout()
    plt.savefig(fname, dpi=140)
    plt.close()


def _winsor_rules(df: pd.DataFrame, col: str) -> dict:
    s = df[col].dropna()
    if len(s) < 100:
        return {"col": col, "rule": "skip", "reason": "not_enough_data", "n": int(len(s))}
    lo = float(s.quantile(LOW_Q))
    hi = float(s.quantile(HIGH_Q))
    return {"col": col, "low_q": LOW_Q, "high_q": HIGH_Q, "lo": lo, "hi": hi, "n": int(len(s))}


def main():
    # MLflow
    mlflow.set_experiment(MLFLOW_EXPERIMENT)
    with mlflow.start_run(run_name="audit"):
        df = _read_data()
        n_all = len(df)

        # Приведение типов
        df = _coerce_types(df)

        # Проверка наличия таргета
        if TARGET not in df.columns:
            raise ValueError(f"Target '{TARGET}' not found")

        # Базовая мета
        meta = {
            "n_rows": n_all,
            "n_train": int((df["_split"] == "train").sum()),
            "n_test": int((df["_split"] == "test").sum()),
            "cols": sorted(df.columns.tolist()),
        }

        # Отчет по пропускам
        nulls = df.isna().mean().sort_values(ascending=False).to_dict()
        meta["null_share_top"] = dict(list(nulls.items())[:20])

        # Диапазон дат (если есть)
        if "first_seen" in df.columns:
            meta["first_seen_min"] = str(df["first_seen"].min())
            meta["first_seen_max"] = str(df["first_seen"].max())
        if "last_seen" in df.columns:
            meta["last_seen_min"] = str(df["last_seen"].min())
            meta["last_seen_max"] = str(df["last_seen"].max())

        # Числовые описания
        num_present = [c for c in NUM_FEATS if c in df.columns]
        num_desc = _describe_numeric(df, num_present + [TARGET])
        num_desc_path = ARTIFACTS_DIR / "numeric_describe.csv"
        if not num_desc.empty:
            num_desc.to_csv(num_desc_path)

        # Гистограммы таргета
        _plot_hist(df[TARGET], "price_rub", ARTIFACTS_DIR / "price_rub_hist.png", log_scale=False)
        _plot_hist(df[TARGET], "price_rub", ARTIFACTS_DIR / "price_rub_log_hist.png", log_scale=True)

        # Гистограммы ключевых числовых
        for col in ["price_per_m2", "area_total", "dist_to_metro_m", "dist_to_center_km", "year_built"]:
            if col in df.columns:
                _plot_hist(df[col], col, ARTIFACTS_DIR / f"{col}_hist.png", log_scale=False)

        # Дубликаты (по source+external_id)
        dup_stats = {}
        if "source" in df.columns and "external_id" in df.columns:
            g = df.groupby(["source", "external_id"]).size().reset_index(name="n")
            dups = g[g["n"] > 1]
            dup_stats = {
                "groups_total": int(len(g)),
                "groups_duplicated": int((g["n"] > 1).sum()),
                "rows_in_dups": int(dups["n"].sum()) if len(dups) else 0,
            }
        meta["duplicates"] = dup_stats

        # Активность объявлений (если есть)
        if "is_active" in df.columns:
            try:
                meta["is_active_share"] = float(df["is_active"].mean(skipna=True))
            except Exception:
                pass

        # Предложение правил отсечения (winsor) для ключевых числовых
        rules = []
        for col in ["price_rub", "price_per_m2", "area_total", "dist_to_metro_m"]:
            if col in df.columns:
                rules.append(_winsor_rules(df, col))

        # Подсчёт потенциально «вне правил»
        preview_impact = {}
        for r in rules:
            if r.get("rule") == "skip":
                continue
            col, lo, hi = r["col"], r["lo"], r["hi"]
            m = df[col].notna() & ((df[col] < lo) | (df[col] > hi))
            preview_impact[col] = int(m.sum())

        # Сохранение JSON-отчёта
        report = {
            "meta": meta,
            "winsor_rules": rules,
            "preview_impact": preview_impact,
            "notes": {
                "log_target_recommended": True,
                "split_warning": "держи тест по времени последних 1–2 месяцев; для spatial CV группируй по H3/районам",
            },
        }
        report_path = ARTIFACTS_DIR / "audit_report.json"
        with open(report_path, "w", encoding="utf-8") as f:
            json.dump(report, f, ensure_ascii=False, indent=2)

        # Логирование в MLflow
        mlflow.log_param("rows_total", n_all)
        mlflow.log_param("cols_total", len(df.columns))
        mlflow.log_param("num_features_present", len(num_present))
        if "is_active_share" in meta:
            mlflow.log_metric("is_active_share", meta.get("is_active_share", np.nan))

        # Сохраняем артефакты
        if 'num_desc_path' in locals() and num_desc_path.exists():
            mlflow.log_artifact(str(num_desc_path), artifact_path="audit")
        mlflow.log_artifact(str(ARTIFACTS_DIR / "price_rub_hist.png"), artifact_path="audit")
        mlflow.log_artifact(str(ARTIFACTS_DIR / "price_rub_log_hist.png"), artifact_path="audit")
        for col in ["price_per_m2", "area_total", "dist_to_metro_m", "dist_to_center_km", "year_built"]:
            p = ARTIFACTS_DIR / f"{col}_hist.png"
            if p.exists():
                mlflow.log_artifact(str(p), artifact_path="audit")
        mlflow.log_artifact(str(report_path), artifact_path="audit")

        print("=== AUDIT DONE ===")
        print(json.dumps(report["meta"], ensure_ascii=False, indent=2))
        print("Winsor rules preview:", json.dumps(preview_impact, ensure_ascii=False))
        print(f"Artifacts saved to: {ARTIFACTS_DIR}")


if __name__ == "__main__":
    main()
