from __future__ import annotations

import json
import os
import subprocess
import tempfile
from pathlib import Path
from typing import Optional
import sys
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, Field

app = FastAPI(
    title="RealVal ML API",
    version="1.0.0",
    description="HTTP-обертка над realval CLI для оценки аренды",
)

# Где лежит модель (можно переопределить через ENV)
MODEL_PATH = os.getenv("REALVAL_MODEL_PATH", "models/artefacts/rent_lgbm.joblib")
# DSN для БД читается внутри CLI из POSTGRES_DSN_URL, тут его трогать не надо


class AddressRequest(BaseModel):
    city: str = Field("Москва", description="Город")
    address: str = Field(..., description="Адрес (улица, дом, корпус и т.п.)")
    rooms: int = Field(..., description="Количество комнат (0=студия, 1,2,3...)")
    area_total: float = Field(..., description="Общая площадь, м²")
    area_living: Optional[float] = Field(None, description="Жилая площадь, м²")
    area_kitchen: Optional[float] = Field(None, description="Площадь кухни, м²")
    floor: Optional[int] = Field(None, description="Этаж")
    floors_total: Optional[int] = Field(None, description="Этажность дома")
    year_built: Optional[int] = Field(None, description="Год постройки")
    house_material: Optional[str] = Field(None, description="Материал дома")
    condition: Optional[str] = Field(None, description="Состояние/ремонт")
    with_text: bool = Field(
        False,
        description="Генерировать текстовое объяснение через GigaChat",
    )


@app.post("/v1/predict/address")
def predict_address(req: AddressRequest):
    """
    HTTP-эндпоинт, который под капотом вызывает:
      python -m realval predict-address --model-path ... --params tmp.json [--with-text]
    и возвращает JSON-отчет как есть.
    """
    # 1) пишем тело запроса во временный JSON
    with tempfile.NamedTemporaryFile("w+", suffix=".json", delete=False, encoding="utf-8") as f_in:
        # exclude with_text, он идёт флагом, а не в params
        params = req.model_dump(exclude={"with_text"})
        json.dump(params, f_in, ensure_ascii=False)
        tmp_in = f_in.name
  
    with tempfile.NamedTemporaryFile("w+", suffix=".json", delete=False, encoding="utf-8") as f_out:
        tmp_out = f_out.name
        
    try:
        cmd = [
            sys.executable,
            "-m",
            "realval",
            "predict-address",
            "--model-path",
            MODEL_PATH,
            "--comps-path",
            "data/features/base.parquet",
            "--params",
            tmp_in,
            "--out",
            tmp_out,
        ]
        if req.with_text:
            cmd.append("--with-text")

        # subprocess запускает CLI, который уже знает всё про geocode/DB/LLM
        proc = subprocess.run(
            cmd,
            capture_output=True,
            text=True,
        )

        if proc.returncode != 0:
            err_msg = proc.stderr.strip() or "realval CLI failed"
            raise HTTPException(status_code=500, detail=err_msg)

        # Читаем результат из файла
        try:
            with open(tmp_out, "r", encoding="utf-8") as f:
                data = json.load(f)
        except (FileNotFoundError, json.JSONDecodeError) as e:
            raise HTTPException(
                status_code=500,
                detail=f"Failed to read result: {str(e)}",
            )

        return data

    finally:
        # подчистим временный файл
        try:
            os.unlink(tmp_in)
            os.unlink(tmp_out)
        except OSError:
            pass
