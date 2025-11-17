from __future__ import annotations

import json
import os
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Dict, List, Optional

from langchain_gigachat.chat_models import GigaChat
from toon_format import encode as toon_encode

from dotenv import load_dotenv
_env_file = Path(__file__).resolve().parents[2] / ".env"
load_dotenv(_env_file, override=True)


@dataclass
class LLMConfig:
    """
    Конфигурация LLM-генерации текстовых блоков.
    Читается из .env, но можно вручную переопределить из кода.
    """
    enabled: bool = True
    model: str = os.getenv("GIGACHAT_MODEL", "GigaChat-Pro")
    scope: str = os.getenv("GIGACHAT_SCOPE", "GIGACHAT_API_PERS")
    credentials: Optional[str] = os.getenv("GIGACHAT_AUTH_KEY")
    # по умолчанию выключаем проверку сертификатов (для локальной разработки)
    verify_ssl_certs: bool = os.getenv("GIGACHAT_VERIFY_SSL", "false").lower() in (
        "1",
        "true",
        "yes",
    )


_llm_instance: Optional[GigaChat] = None


def _get_llm(cfg: Optional[LLMConfig] = None) -> GigaChat:
    """
    Лениво создаёт инстанс GigaChat для LangChain.
    """
    global _llm_instance
    if _llm_instance is not None:
        return _llm_instance

    cfg = cfg or LLMConfig()
    if not cfg.credentials:
        raise RuntimeError("GIGACHAT_AUTH_KEY не задан в окружении (.env)")

    _llm_instance = GigaChat(
        credentials=cfg.credentials,
        scope=cfg.scope,
        model=cfg.model,
        verify_ssl_certs=cfg.verify_ssl_certs,
    )
    return _llm_instance


def _build_toon_payload(report: Dict[str, Any]) -> str:
    """
    Собираем компактный объект для LLM и кодируем его в TOON.
    Берём только то, что нужно для текста, чтобы не тратить лишние токены.
    """
    obj = report.get("object", {}) or {}
    enr = report.get("enriched", {}) or {}
    pricing_block = report.get("pricing", {}) or {}
    expl = report.get("explanation", {}) or {}
    comps: List[Dict[str, Any]] = report.get("comparables") or []

    pricing = {
        "prediction_rub": pricing_block.get("prediction_rub"),
        "interval_low_rub": pricing_block.get("interval_low_rub"),
        "interval_high_rub": pricing_block.get("interval_high_rub"),
        "currency": pricing_block.get("currency", "RUB"),
        "deal_type": pricing_block.get("deal_type", "rent_long"),
    }

    compact_comps = []
    for c in comps[:5]:
        compact_comps.append(
            {
                "price_rub": c.get("price_rub"),
                "rooms": c.get("rooms"),
                "area_total": c.get("area_total"),
                "floor": c.get("floor"),
                "floors_total": c.get("floors_total"),
                "year_built": c.get("year_built"),
                "house_material": c.get("house_material"),
                "dist_km": c.get("distance_km"),
                "metro": c.get("metro_station"),
            }
        )

    payload = {
        "object": {
            "city": obj.get("city"),
            "address": obj.get("address"),
            "rooms": obj.get("rooms"),
            "area_total": obj.get("area_total"),
            "area_living": obj.get("area_living"),
            "area_kitchen": obj.get("area_kitchen"),
            "floor": obj.get("floor"),
            "floors_total": obj.get("floors_total"),
            "year_built": obj.get("year_built"),
            "house_material": obj.get("house_material"),
            "condition": obj.get("condition"),
        },
        "geo": {
            "lat": enr.get("lat"),
            "lon": enr.get("lon"),
            "dist_to_center_km": enr.get("dist_to_center_km"),
            "metro_station": enr.get("metro_station"),
            "dist_to_metro_m": enr.get("dist_to_metro_m"),
            "metro_walk_min": enr.get("metro_walk_min"),
            "density_500m": enr.get("density_500m"),
        },
        "pricing": pricing,
        "explanation_top_features": expl.get("top_features", []),
        "comparables": compact_comps,
    }

    toon_str = toon_encode(
        payload,
        {
            "indent": 2,
            "delimiter": ",",
            "lengthMarker": "#",
        },
    )
    return toon_str



_SYSTEM_PROMPT = (
    "Ты — эксперт по рынку жилой недвижимости Москвы. "
    "Твоя задача — кратко и по делу объяснить оценку рыночной месячной аренды квартиры.\n"
    "Пиши по-русски, деловым, но понятным языком. "
    "Не используй сложных юридических формулировок, избегай канцелярита.\n"
    "Не упоминай, что ты ИИ или языковая модель. Не ссылайся на обучающие данные.\n"
)


def _build_prompt(report: Dict[str, Any]) -> str:
    toon_block = _build_toon_payload(report)

    pricing = report.get("pricing", {}) or {}
    pred = pricing.get("prediction_rub")
    lo = pricing.get("interval_low_rub")
    hi = pricing.get("interval_high_rub")

    # аккуратно округлим до тысяч, чтобы текст был красивее
    def _round_thousand(x):
        if x is None:
            return None
        return int(round(x / 1000.0) * 1000)

    pred_r = _round_thousand(pred)
    lo_r = _round_thousand(lo)
    hi_r = _round_thousand(hi)

    # сделаем текстовый факт-блок, который модель должна уважать
    price_facts = []
    if pred_r is not None:
        price_facts.append(f"точечная оценка модели: {pred_r} ₽ в месяц")
    if lo_r is not None and hi_r is not None:
        price_facts.append(
            f"интервал значений: от {lo_r} ₽ до {hi_r} ₽ в месяц"
        )

    price_facts_str = "; ".join(price_facts) if price_facts else "нет доступных числовых оценок."

    prompt = f"""{_SYSTEM_PROMPT}

Даны данные об объекте и расчёте в формате TOON:

{toon_block}

Ключевые числовые значения ОКОНЧАТЕЛЬНОЙ оценки аренды (раздел pricing):
- {price_facts_str}

ОЧЕНЬ ВАЖНО:
- Все числовые оценки стоимости аренды (конкретная цена и диапазон) ты обязан брать ТОЛЬКО из раздела pricing.
- НЕЛЬЗЯ использовать цены сопоставимых объектов (comparables) как диапазон итоговой оценки.
- НЕЛЬЗЯ придумывать свои новые минимумы/максимумы цены.
- Допускается лёгкое округление до тысяч (например, 165 600 → 166 000), но границы и масштаб должны соответствовать указанным значениям.

Сформируй JSON с тремя полями:
- "summary_short" — 1–2 предложения для краткого блока на сайте. В этом блоке обязательно упомяни конечную оценку и (если есть) диапазон, используя только числа из pricing.
- "summary_long" — 2–4 абзаца подробного объяснения, можно ссылаться на те же числа, а также описывать факторы (район, площадь, метро и т.п.).
- "factors_summary" — массив из 3–7 коротких маркеров (строк), каждый — один важный фактор, влияющий на цену (например: "Большая площадь квартиры", "Близость к метро", "Современный дом" и т.п.).

Ответ верни ТОЛЬКО в виде JSON-объекта без дополнительных комментариев и Markdown.
"""
    return prompt



def generate_text_blocks(report: Dict[str, Any], cfg: Optional[LLMConfig] = None) -> Dict[str, Any]:
    cfg = cfg or LLMConfig()
    pricing = report.get("pricing", {}) or {}
    obj = report.get("object", {}) or {}

    pred = pricing.get("prediction_rub")
    lo = pricing.get("interval_low_rub")
    hi = pricing.get("interval_high_rub")

    def _fmt(x):
        if x is None:
            return None
        return f"{int(round(x)):,}".replace(",", " ")

    # детерминированный summary_short
    parts = []
    city = obj.get("city")
    addr = obj.get("address")
    if city:
        parts.append(f"в городе {city}")
    if addr:
        parts.append(f"по адресу «{addr}»")

    header = "Квартира"
    if parts:
        header += " " + ", ".join(parts)

    if pred is not None and lo is not None and hi is not None:
        summary_short = (
            f"{header}. Оценочная ставка долгосрочной аренды: около {_fmt(pred)} ₽ в месяц "
            f"(ожидаемый диапазон { _fmt(lo) }–{ _fmt(hi) } ₽)."
        )
    elif pred is not None:
        summary_short = (
            f"{header}. Оценочная ставка долгосрочной аренды: около {_fmt(pred)} ₽ в месяц."
        )
    else:
        summary_short = f"{header}. Оценочная ставка не может быть рассчитана."

    # если LLM выключен — вернём только короткое резюме
    if not cfg.enabled:
        return {
            "summary_short": summary_short,
            "summary_long": summary_short,
            "factors_summary": [],
        }

    # иначе просим LLM сгенерировать только long + факторы
    llm = _get_llm(cfg)
    prompt = _build_prompt(report)  # обновлённый, как выше

    resp = llm.invoke(prompt)
    text = getattr(resp, "content", resp)
    if not isinstance(text, str):
        text = str(text)

    try:
        data = json.loads(text)
    except json.JSONDecodeError:
        return {
            "summary_short": summary_short,
            "summary_long": text.strip(),
            "factors_summary": [],
        }

    long_txt = data.get("summary_long", "") or ""
    factors = data.get("factors_summary") or []

    if isinstance(factors, str):
        factors_list = [factors]
    elif isinstance(factors, list):
        factors_list = [str(x) for x in factors]
    else:
        factors_list = [str(factors)]

    return {
        "summary_short": summary_short,
        "summary_long": long_txt.strip() or summary_short,
        "factors_summary": [f.strip() for f in factors_list if str(f).strip()],
    }

