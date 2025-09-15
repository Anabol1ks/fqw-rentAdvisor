from __future__ import annotations
import os
import sys
from pathlib import Path
import numpy as np
import pandas as pd

# Ensure local packages (features/*) are importable when running as a script
THIS_DIR = Path(__file__).resolve().parent
PROJECT_ROOT = THIS_DIR.parent
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))

from features.audit import _coerce_types as audit_coerce  # type: ignore  # reuse typing from audit
from features.transformers import Winsorizer, DropLeakage, SimpleFeatureMaker  # type: ignore

SPLITS_DIR = PROJECT_ROOT / "data" / "splits"
REPORTS_DIR = PROJECT_ROOT / "reports" / "audit"
OUT_DIR = PROJECT_ROOT / "data" / "processed"
OUT_DIR.mkdir(parents=True, exist_ok=True)

TARGET = "price_rub"

# Safe numeric/cat lists for optional type coercion if audit module not available
NUM_FEATS = [
    "area_total","area_kitchen","floor","floors_total",
    "year_built","dist_to_metro_m","dist_to_center_km","density_500m",
]
CAT_FEATS = ["deal_type","city","district","house_material","condition","metro_station"]


def coerce(df: pd.DataFrame) -> pd.DataFrame:
    """Coerce dtypes similar to features.audit._coerce_types.

    Prefer reusing audit._coerce_types when importable; otherwise fallback here.
    """
    try:
        return audit_coerce(df)
    except Exception:
        df = df.copy()
        for c in NUM_FEATS + [TARGET]:
            if c in df.columns:
                df[c] = pd.to_numeric(df[c], errors="coerce")
        for c in CAT_FEATS:
            if c in df.columns:
                df[c] = df[c].astype("string")
        for c in ["first_seen", "last_seen"]:
            if c in df.columns:
                df[c] = pd.to_datetime(df[c], errors="coerce", utc=True)
        return df


def main():
    train_pq = SPLITS_DIR / "train.parquet"
    test_pq = SPLITS_DIR / "test.parquet"
    if not train_pq.exists() or not test_pq.exists():
        raise FileNotFoundError(f"Expected splits at {train_pq} and {test_pq}. Run make dataset.")

    tr = pd.read_parquet(train_pq)
    te = pd.read_parquet(test_pq)
    df = pd.concat([tr.assign(_split="train"), te.assign(_split="test")], ignore_index=True)

    df = coerce(df)

    # Apply transformers
    rules_path = REPORTS_DIR / "audit_report.json"
    win = Winsorizer(str(rules_path), cols=["price_rub","area_total","dist_to_metro_m","dist_to_center_km"])  # do not include target into model later
    df = win.fit_transform(df)

    df = DropLeakage(["price_per_m2"]).fit_transform(df)
    df = SimpleFeatureMaker().fit_transform(df)

    # Optional: spatial grouping via H3 (set H3_LEVEL env var, e.g., 7)
    lvl = os.environ.get("H3_LEVEL")
    if lvl and "lat" in df.columns and "lon" in df.columns:
        try:
            import h3
            level = int(lvl)
            def _to_h3(row):
                try:
                    return h3.geo_to_h3(float(row["lat"]), float(row["lon"]), level)
                except Exception:
                    return None
            df[f"h3_{level}"] = df.apply(_to_h3, axis=1)
        except Exception as e:
            print(f"H3 disabled: {e}")

    # Split back
    trf = df[df["_split"] == "train"].drop(columns=["_split"]) if "_split" in df.columns else df.iloc[:0]
    tef = df[df["_split"] == "test"].drop(columns=["_split"]) if "_split" in df.columns else df.iloc[:0]

    out_tr = OUT_DIR / "train_features.parquet"
    out_te = OUT_DIR / "test_features.parquet"
    trf.to_parquet(out_tr, index=False)
    tef.to_parquet(out_te, index=False)
    print(f"Saved processed features to: {out_tr} and {out_te}")


if __name__ == "__main__":
    main()
