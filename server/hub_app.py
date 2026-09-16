from __future__ import annotations

from fastapi import FastAPI

from .hub.app import create_hub_app
from .hub.config import HubConfig


def create_production_hub_app() -> FastAPI:
    return create_hub_app(HubConfig.from_env())
