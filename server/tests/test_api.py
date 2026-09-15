from fastapi.testclient import TestClient

from server.app import create_app


def test_state_endpoint_uses_canonical_aliases() -> None:
    with TestClient(create_app("healthy.json")) as client:
        response = client.get("/api/v1/state")
    assert response.status_code == 200
    data = response.json()
    assert data["schemaVersion"] == 1
    assert "commandCode" in data
    assert data["overall"]["status"] == "live"


def test_health_is_backend_reachability_plus_source_health() -> None:
    with TestClient(create_app("commandcode_auth_error.json")) as client:
        response = client.get("/api/v1/health")
    assert response.status_code == 200
    assert response.json()["status"] == "ok"
    assert response.json()["sources"] == {"zcode": "ok", "commandCode": "error"}
