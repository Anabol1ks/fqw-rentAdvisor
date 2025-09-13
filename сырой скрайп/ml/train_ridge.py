import os
from pathlib import Path
import numpy as np
import pandas as pd
import mlflow
import mlflow.sklearn
from mlflow.tracking import MlflowClient
from mlflow.exceptions import MlflowException

from sklearn.compose import ColumnTransformer
from sklearn.preprocessing import OneHotEncoder, StandardScaler
from sklearn.linear_model import Ridge
from sklearn.pipeline import Pipeline
from sklearn.metrics import mean_squared_error, r2_score
from sklearn.impute import SimpleImputer


def main():
    # Resolve paths to splits relative to project root (…/сырой скрайп)
    script_dir = Path(__file__).resolve().parent
    project_root = script_dir.parent
    splits_dir = project_root / "data" / "splits"
    train_path = splits_dir / "train.parquet"
    test_path = splits_dir / "test.parquet"

    if not train_path.exists() or not test_path.exists():
        raise FileNotFoundError(
            f"Parquet splits not found. Expected at: {train_path} and {test_path}.\n"
            "Make sure to run the dataset step first (e.g., 'make dataset')."
        )

    # Load prepared splits
    train = pd.read_parquet(train_path)
    test = pd.read_parquet(test_path)

    target = "price_rub"
    if target not in train.columns:
        raise ValueError(f"Target column '{target}' not found in train dataset")

    # Filter out rows with NaN or non-positive target before log-transform
    def filter_target(df: pd.DataFrame) -> pd.DataFrame:
        m = df[target].notna() & (df[target] > 0)
        return df.loc[m].copy()

    tr_len0, te_len0 = len(train), len(test)
    train = filter_target(train)
    test = filter_target(test)
    dropped_tr = tr_len0 - len(train)
    dropped_te = te_len0 - len(test)

    if len(train) == 0:
        raise ValueError(
            "No training rows after filtering by target (price_rub must be > 0 and not NaN)."
        )

    y_tr = np.log1p(train[target].values)
    y_te = np.log1p(test[target].values) if len(test) > 0 else np.array([])

    num = [
        "area_total",
        "area_kitchen",
        "floor",
        "floors_total",
        "year_built",
        "dist_to_metro_m",
        "dist_to_center_km",
        "density_500m",
    ]
    cat = [
        "deal_type",
        "city",
        "district",
        "house_material",
        "condition",
        "metro_station",
    ]

    # Ensure features are present
    missing_cols = [c for c in (num + cat) if c not in train.columns]
    if missing_cols:
        raise ValueError(f"Missing feature columns in train dataset: {missing_cols}")

    X_tr = train[num + cat].copy()
    X_te = test[num + cat].copy()

    # Preprocessing: impute -> scale for numeric, impute -> one-hot for categoricals
    pre = ColumnTransformer(
        [
            (
                "num",
                Pipeline(
                    steps=[
                        ("imputer", SimpleImputer(strategy="median")),
                        ("scaler", StandardScaler()),
                    ]
                ),
                num,
            ),
            (
                "cat",
                Pipeline(
                    steps=[
                        ("imputer", SimpleImputer(strategy="most_frequent")),
                        (
                            "ohe",
                            OneHotEncoder(handle_unknown="ignore", min_frequency=20),
                        ),
                    ]
                ),
                cat,
            ),
        ]
    )

    alpha = float(os.environ.get("RIDGE_ALPHA", "1.0"))
    pipe = Pipeline([("pre", pre), ("ridge", Ridge(alpha=alpha, random_state=42))])

    # MLflow setup
    tracking_uri = os.environ.get("MLFLOW_TRACKING_URI")
    if tracking_uri:
        mlflow.set_tracking_uri(tracking_uri)

    def ensure_experiment(name: str) -> None:
        try:
            mlflow.set_experiment(name)
        except MlflowException as e:
            # If experiment exists but soft-deleted, restore it
            msg = str(e).lower()
            if "deleted experiment" in msg or "cannot set a deleted experiment" in msg:
                client = MlflowClient()
                exp = client.get_experiment_by_name(name)
                if exp is not None and getattr(exp, "lifecycle_stage", "") == "deleted":
                    client.restore_experiment(exp.experiment_id)
                    mlflow.set_experiment(name)
                else:
                    raise
            else:
                raise

    ensure_experiment("rentadvisor_baseline")

    with mlflow.start_run():
        # Params
        mlflow.log_param("alpha", alpha)
        mlflow.log_param("num_features", len(num))
        mlflow.log_param("cat_features", len(cat))
        mlflow.log_param("dropped_train_rows", int(dropped_tr))
        mlflow.log_param("dropped_test_rows", int(dropped_te))

        # Train
        pipe.fit(X_tr, y_tr)

        # Evaluate (log-space metrics + MAE in RUB) if test is available
        if len(test) > 0:
            pred = pipe.predict(X_te)
            # Backward-compatible RMSE: some older scikit-learn versions don't support 'squared' arg
            try:
                rmse_log = mean_squared_error(y_te, pred, squared=False)
            except TypeError:
                rmse_log = float(np.sqrt(mean_squared_error(y_te, pred)))
            r2 = r2_score(y_te, pred)

            pred_rub = np.expm1(pred)
            y_te_rub = np.expm1(y_te)
            mae_rub = float(np.mean(np.abs(pred_rub - y_te_rub)))

            mlflow.log_metric("rmse_log", rmse_log)
            mlflow.log_metric("r2_log", r2)
            mlflow.log_metric("mae_rub", mae_rub)

            print(f"rmse_log={rmse_log:.4f} r2_log={r2:.4f} mae_rub={mae_rub:.2f}")
        else:
            mlflow.log_param("test_empty", True)
            print("Trained model, but test set is empty after target filtering; skipped evaluation.")

        # Model artifact
        mlflow.sklearn.log_model(pipe, "model")


if __name__ == "__main__":
    main()
