from __future__ import annotations
import os
import sys
from pathlib import Path
import numpy as np
import pandas as pd
import mlflow

# Ensure project root is importable regardless of working directory
PROJECT_ROOT = Path(__file__).resolve().parents[1]
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))

from catboost import CatBoostRegressor, Pool
from sklearn.metrics import mean_squared_error

DATA_DIR = PROJECT_ROOT / "data" / "processed"
TARGET = "price_rub"

# Categorical features (safe, leakage-free); include price_period explicitly
CAT_FEATS = [
    "deal_type",
    "city",
    "district",
    "house_material",
    "condition",
    "metro_station",
    "price_period",
]

# Numeric features including engineered ones
NUM_FEATS = [
    "area_total",
    "area_kitchen",
    "floor",
    "floors_total",
    "year_built",
    "dist_to_metro_m",
    "dist_to_center_km",
    "density_500m",
    "log1p_area_total",
    "log1p_area_kitchen",
    "log1p_dist_to_metro_m",
    "log1p_dist_to_center_km",
    "kitchen_share",
]


def rmse_log(y_true, y_pred):
    y_true = np.clip(y_true.astype(float), 1, None)
    y_pred = np.clip(y_pred.astype(float), 1, None)
    return float(np.sqrt(mean_squared_error(np.log1p(y_true), np.log1p(y_pred))))


def main():
    mlflow.set_experiment("baselines")
    with mlflow.start_run(run_name="catboost_baseline"):
        tr = pd.read_parquet(DATA_DIR / "train_features.parquet")
        te = pd.read_parquet(DATA_DIR / "test_features.parquet")

        # Filter rows with invalid target (CatBoost cannot train on NaN labels; also log-metrics need > 0)
        def _filter_target(df: pd.DataFrame) -> pd.DataFrame:
            s = pd.to_numeric(df[TARGET], errors="coerce")
            return df[s.notna() & (s > 0)].copy()

        tr = _filter_target(tr)
        te = _filter_target(te)

        y_tr = tr[TARGET].astype(float).values
        y_te = te[TARGET].astype(float).values

        # Select controlled feature set to avoid passing unexpected string/datetime columns
        feat_cols = [c for c in (NUM_FEATS + CAT_FEATS) if c in tr.columns]
        X_tr = tr[feat_cols].copy()
        X_te = te[feat_cols].copy()

        # Normalize missing values and dtypes
        X_tr = X_tr.replace({pd.NA: np.nan})
        X_te = X_te.replace({pd.NA: np.nan})
        # Ensure numerics are numeric
        tr_num = [c for c in NUM_FEATS if c in X_tr.columns]
        te_num = [c for c in NUM_FEATS if c in X_te.columns]
        if tr_num:
            X_tr[tr_num] = X_tr[tr_num].apply(pd.to_numeric, errors="coerce")
        if te_num:
            X_te[te_num] = X_te[te_num].apply(pd.to_numeric, errors="coerce")
        # Ensure categoricals are strings with a missing sentinel (CatBoost doesn't accept NaN in cats)
        tr_cat = [c for c in CAT_FEATS if c in X_tr.columns]
        te_cat = [c for c in CAT_FEATS if c in X_te.columns]
        if tr_cat:
            X_tr[tr_cat] = (
                X_tr[tr_cat]
                .astype("string")
                .fillna("__MISSING__")
                .astype(str)
            )
        if te_cat:
            X_te[te_cat] = (
                X_te[te_cat]
                .astype("string")
                .fillna("__MISSING__")
                .astype(str)
            )

        cat_idx = [X_tr.columns.get_loc(c) for c in tr_cat]

        train_pool = Pool(X_tr, y_tr, cat_features=cat_idx)
        test_pool  = Pool(X_te, y_te, cat_features=cat_idx)

        model = CatBoostRegressor(
            depth=int(os.environ.get("CB_DEPTH", 8)),
            learning_rate=float(os.environ.get("CB_LR", 0.06)),
            loss_function="RMSE",
            eval_metric="RMSE",
            random_seed=42,
            iterations=int(os.environ.get("CB_ITERS", 4000)),
            od_type="Iter",
            od_wait=int(os.environ.get("CB_OD_WAIT", 200)),
            verbose=200
        )
        model.fit(train_pool, eval_set=test_pool, use_best_model=True)

        pred = model.predict(test_pool)
        metrics = {
            "rmse_log": rmse_log(y_te, pred),
            "mae_rub": float(np.mean(np.abs(y_te - pred)))
        }
        for k, v in metrics.items():
            mlflow.log_metric(k, v)

        # log model
        try:
            import mlflow.catboost as mlflow_catboost
            mlflow_catboost.log_model(cb_model=model, artifact_path="model")
        except Exception:
            # Fallback: save locally and log as artifact
            tmp_path = PROJECT_ROOT / "models" / "catboost.cbm"
            tmp_path.parent.mkdir(parents=True, exist_ok=True)
            model.save_model(str(tmp_path))
            mlflow.log_artifact(str(tmp_path), artifact_path="model")

        print(metrics)


if __name__ == "__main__":
    main()
