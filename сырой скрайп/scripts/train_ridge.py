from __future__ import annotations
import os
import sys
from pathlib import Path
import numpy as np
import pandas as pd
import mlflow
import mlflow.sklearn

PROJECT_ROOT = Path(__file__).resolve().parents[1]
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))

from sklearn.compose import ColumnTransformer
from sklearn.impute import SimpleImputer
from sklearn.preprocessing import OneHotEncoder, StandardScaler
from sklearn.pipeline import Pipeline
from sklearn.linear_model import Ridge
from sklearn.metrics import mean_squared_error
from sklearn.model_selection import TimeSeriesSplit, GroupKFold

DATA_DIR = PROJECT_ROOT / "data" / "processed"
TARGET = "price_rub"

CAT_FEATS = ["deal_type","city","district","house_material","condition","metro_station"]
NUM_FEATS = [
    "area_total","area_kitchen","floor","floors_total","year_built",
    "dist_to_metro_m","dist_to_center_km","density_500m",
    "log1p_area_total","log1p_area_kitchen","log1p_dist_to_metro_m","log1p_dist_to_center_km",
    "kitchen_share"
]


def rmse_log(y_true, y_pred):
    y_true = np.clip(y_true.astype(float), 1, None)
    y_pred = np.clip(y_pred.astype(float), 1, None)
    return float(np.sqrt(mean_squared_error(np.log1p(y_true), np.log1p(y_pred))))


def main():
    mlflow.set_experiment("baselines")
    with mlflow.start_run(run_name="ridge_baseline"):
        tr = pd.read_parquet(DATA_DIR / "train_features.parquet")
        te = pd.read_parquet(DATA_DIR / "test_features.parquet")

        y_tr = tr[TARGET].astype(float)
        y_te = te[TARGET].astype(float)

        X_tr = tr.drop(columns=[TARGET])
        X_te = te.drop(columns=[TARGET])

        # Keep only existing columns in case some features are missing
        num_cols = [c for c in NUM_FEATS if c in X_tr.columns]
        cat_cols = [c for c in CAT_FEATS if c in X_tr.columns]

        # Normalize missing values so sklearn sees np.nan instead of pd.NA
        X_tr = X_tr.replace({pd.NA: np.nan})
        X_te = X_te.replace({pd.NA: np.nan})

        # Ensure numeric columns are numeric dtype
        if num_cols:
            X_tr[num_cols] = X_tr[num_cols].apply(pd.to_numeric, errors="coerce")
            # Some test columns may be absent; guard with intersection
            te_num_cols = [c for c in num_cols if c in X_te.columns]
            if te_num_cols:
                X_te[te_num_cols] = X_te[te_num_cols].apply(pd.to_numeric, errors="coerce")

        # Ensure categorical columns are plain Python objects and have np.nan for missing
        if cat_cols:
            # Cast to object to avoid pandas StringDtype with pd.NA semantics
            X_tr[cat_cols] = X_tr[cat_cols].astype(object)
            tr_cat = X_tr[cat_cols]
            X_tr[cat_cols] = tr_cat.where(pd.notna(tr_cat), np.nan)

            te_cat_cols = [c for c in cat_cols if c in X_te.columns]
            if te_cat_cols:
                X_te[te_cat_cols] = X_te[te_cat_cols].astype(object)
                te_cat = X_te[te_cat_cols]
                X_te[te_cat_cols] = te_cat.where(pd.notna(te_cat), np.nan)

        # Drop features that are all-NaN in train to avoid imputer "no observed values" case
        num_cols = [c for c in num_cols if X_tr[c].notna().any()]
        cat_cols = [c for c in cat_cols if X_tr[c].notna().any()]

        pre = ColumnTransformer(
            transformers=[
                ("num", Pipeline([
                    ("imp", SimpleImputer(strategy="median")),
                    ("sc", StandardScaler())
                ]), num_cols),
                ("cat", Pipeline([
                    ("imp", SimpleImputer(strategy="most_frequent")),
                    ("oh", OneHotEncoder(handle_unknown="ignore", min_frequency=10))
                ]), cat_cols),
            ],
            remainder="drop"
        )

        model = Pipeline([
            ("pre", pre),
            ("ridge", Ridge(alpha=float(os.environ.get("RIDGE_ALPHA", "1.0")), random_state=42))
        ])

        model.fit(X_tr, y_tr)
        pred = model.predict(X_te)

        metrics = {
            "rmse_log": rmse_log(y_te.values, pred),
            "mae_rub": float(np.mean(np.abs(y_te.values - pred)))
        }
        for k, v in metrics.items():
            mlflow.log_metric(k, v)

        mlflow.sklearn.log_model(model, "model")
        print(metrics)

        # Optional CV
        do_cv = os.environ.get("DO_CV", "0") == "1"
        if do_cv:
            cv_type = os.environ.get("CV_TYPE", "time")  # time|group
            n_splits = int(os.environ.get("CV_FOLDS", "5"))
            if cv_type == "time" and "last_seen" in tr.columns:
                # sort by time and use expanding TimeSeriesSplit
                tr_sorted = tr.sort_values("last_seen")
                y_sorted = tr_sorted[TARGET].astype(float).values
                X_sorted = tr_sorted.drop(columns=[TARGET])
                num_cols_cv = [c for c in NUM_FEATS if c in X_sorted.columns]
                cat_cols_cv = [c for c in CAT_FEATS if c in X_sorted.columns]
                pre_cv = ColumnTransformer(
                    transformers=[
                        ("num", Pipeline([
                            ("imp", SimpleImputer(strategy="median")),
                            ("sc", StandardScaler())
                        ]), num_cols_cv),
                        ("cat", Pipeline([
                            ("imp", SimpleImputer(strategy="most_frequent")),
                            ("oh", OneHotEncoder(handle_unknown="ignore", min_frequency=10))
                        ]), cat_cols_cv),
                    ],
                    remainder="drop"
                )
                model_cv = Pipeline([
                    ("pre", pre_cv),
                    ("ridge", Ridge(alpha=float(os.environ.get("RIDGE_ALPHA", "1.0")), random_state=42))
                ])
                tss = TimeSeriesSplit(n_splits=n_splits)
                scores = []
                for tr_idx, va_idx in tss.split(X_sorted):
                    X_tr_i, X_va_i = X_sorted.iloc[tr_idx], X_sorted.iloc[va_idx]
                    y_tr_i, y_va_i = y_sorted[tr_idx], y_sorted[va_idx]
                    model_cv.fit(X_tr_i, y_tr_i)
                    pred_i = model_cv.predict(X_va_i)
                    scores.append(rmse_log(pd.Series(y_va_i), pd.Series(pred_i)))
                mean_rmse = float(np.mean(scores)) if scores else float("nan")
                mlflow.log_metric("cv_rmse_log_time", mean_rmse)
                print({"cv_rmse_log_time": mean_rmse, "folds": len(scores)})
            elif cv_type == "group":
                group_col = os.environ.get("GROUP_COL", "district")
                if group_col in tr.columns:
                    groups = tr[group_col].astype("string").fillna("?")
                    y_cv = tr[TARGET].astype(float).values
                    X_cv = tr.drop(columns=[TARGET])
                    num_cols_cv = [c for c in NUM_FEATS if c in X_cv.columns]
                    cat_cols_cv = [c for c in CAT_FEATS if c in X_cv.columns]
                    pre_cv = ColumnTransformer(
                        transformers=[
                            ("num", Pipeline([
                                ("imp", SimpleImputer(strategy="median")),
                                ("sc", StandardScaler())
                            ]), num_cols_cv),
                            ("cat", Pipeline([
                                ("imp", SimpleImputer(strategy="most_frequent")),
                                ("oh", OneHotEncoder(handle_unknown="ignore", min_frequency=10))
                            ]), cat_cols_cv),
                        ],
                        remainder="drop"
                    )
                    model_cv = Pipeline([
                        ("pre", pre_cv),
                        ("ridge", Ridge(alpha=float(os.environ.get("RIDGE_ALPHA", "1.0")), random_state=42))
                    ])
                    gkf = GroupKFold(n_splits=n_splits)
                    scores = []
                    for tr_idx, va_idx in gkf.split(X_cv, y_cv, groups=groups):
                        X_tr_i, X_va_i = X_cv.iloc[tr_idx], X_cv.iloc[va_idx]
                        y_tr_i, y_va_i = y_cv[tr_idx], y_cv[va_idx]
                        model_cv.fit(X_tr_i, y_tr_i)
                        pred_i = model_cv.predict(X_va_i)
                        scores.append(rmse_log(pd.Series(y_va_i), pd.Series(pred_i)))
                    mean_rmse = float(np.mean(scores)) if scores else float("nan")
                    mlflow.log_metric("cv_rmse_log_group", mean_rmse)
                    print({"cv_rmse_log_group": mean_rmse, "folds": len(scores), "group_col": group_col})


if __name__ == "__main__":
    main()
