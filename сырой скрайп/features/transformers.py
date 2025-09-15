from __future__ import annotations
import json
from pathlib import Path
from typing import Dict, List, Optional

import numpy as np
import pandas as pd
from sklearn.base import BaseEstimator, TransformerMixin


class Winsorizer(BaseEstimator, TransformerMixin):
    """Обрезает значения колонок по порогам из JSON отчёта аудита.

    Источник правил — файл reports/audit/audit_report.json, который
    создаётся скриптом features.audit.
    """

    def __init__(self, rules_json_path: str | Path, cols: Optional[List[str]] = None):
        self.rules_json_path = str(rules_json_path)
        self.cols = cols
        self._bounds: Dict[str, tuple[float, float]] = {}

    def fit(self, X: pd.DataFrame, y=None):  # noqa: N802 (sklearn API)
        rules_path = Path(self.rules_json_path)
        if not rules_path.exists():
            raise FileNotFoundError(
                f"Winsorizer rules file not found: {rules_path}. Run 'python -m features.audit' first."
            )
        with open(rules_path, "r", encoding="utf-8") as f:
            rep = json.load(f)
        rules = rep.get("winsor_rules", [])
        for r in rules:
            if r.get("rule") == "skip":
                continue
            col = r.get("col")
            if not col:
                continue
            if self.cols is not None and col not in self.cols:
                continue
            lo, hi = r.get("lo"), r.get("hi")
            if lo is None or hi is None:
                continue
            self._bounds[col] = (float(lo), float(hi))
        return self

    def transform(self, X: pd.DataFrame):  # noqa: N802 (sklearn API)
        X = X.copy()
        for col, (lo, hi) in self._bounds.items():
            if col in X.columns:
                X[col] = pd.to_numeric(X[col], errors="coerce").clip(lower=lo, upper=hi)
        return X


class DropLeakage(BaseEstimator, TransformerMixin):
    """Удаляет признаки, потенциально зависящие от таргета (например, price_per_m2).

    По умолчанию удаляет 'price_per_m2'. Можно передать список в cols_to_drop.
    """

    def __init__(self, cols_to_drop: Optional[List[str]] = None):
        self.cols_to_drop = cols_to_drop or ["price_per_m2"]

    def fit(self, X, y=None):  # noqa: N802 (sklearn API)
        return self

    def transform(self, X):  # noqa: N802 (sklearn API)
        X = X.copy()
        to_drop = [c for c in self.cols_to_drop if c in X.columns]
        if to_drop:
            X.drop(columns=to_drop, inplace=True)
        return X


class SimpleFeatureMaker(BaseEstimator, TransformerMixin):
    """Простые производные признаки, безопасные с точки зрения leakage.

    - Логарифмы положительных признаков: area_total, area_kitchen, dist_to_metro_m, dist_to_center_km
    - Отношение кухни к общей площади: kitchen_share
    """

    def __init__(self):
        self._created: List[str] = []

    def fit(self, X, y=None):  # noqa: N802 (sklearn API)
        return self

    def transform(self, X: pd.DataFrame):  # noqa: N802 (sklearn API)
        X = X.copy()
        for col in ["area_total", "area_kitchen", "dist_to_metro_m", "dist_to_center_km"]:
            if col in X.columns:
                s = pd.to_numeric(X[col], errors="coerce")
                X[f"log1p_{col}"] = np.log1p(np.clip(s.fillna(np.nan), a_min=0, a_max=None))
                if f"log1p_{col}" not in self._created:
                    self._created.append(f"log1p_{col}")

        if "area_total" in X.columns and "area_kitchen" in X.columns:
            at = pd.to_numeric(X["area_total"], errors="coerce")
            ak = pd.to_numeric(X["area_kitchen"], errors="coerce")
            with np.errstate(divide="ignore", invalid="ignore"):
                share = np.where(at > 0, ak / at, np.nan)
            X["kitchen_share"] = share
            if "kitchen_share" not in self._created:
                self._created.append("kitchen_share")
        return X


class Log1pTargetTransformer(BaseEstimator, TransformerMixin):
    """Помощник для лог-преобразования таргета.

    Не применяется к X; хранит и применяет обратное преобразование для предсказаний.
    """

    def fit(self, X, y=None):  # noqa: N802 (sklearn API)
        return self

    def transform(self, X):  # noqa: N802 (sklearn API)
        return X

    def transform_y(self, y: np.ndarray | pd.Series) -> np.ndarray:
        y = np.asarray(y, dtype=float)
        y = np.where(y > 0, y, np.nan)
        return np.log1p(y)

    def inverse_transform_y(self, y_log: np.ndarray | pd.Series) -> np.ndarray:
        return np.expm1(np.asarray(y_log, dtype=float))
