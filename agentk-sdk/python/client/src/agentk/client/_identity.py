from __future__ import annotations

import base64
import os
from collections.abc import Callable, Mapping
from types import MappingProxyType

_KWS_SERVICE_NAME = "KWS_SERVICE_NAME"
_KWS_SERVICE_CREDENTIAL = "KWS_SERVICE_CREDENTIAL"


def _encode_varint(value: int) -> bytes:
    encoded = bytearray()
    while value >= 0x80:
        encoded.append((value & 0x7F) | 0x80)
        value >>= 7
    encoded.append(value)
    return bytes(encoded)


def _kws_model(service_name: str) -> bytes:
    value = service_name.encode("utf-8")
    return b"\x0a" + _encode_varint(len(value)) + value


def identity_headers(
    getenv: Callable[[str], str | None] = os.getenv,
) -> Mapping[str, str]:
    headers: dict[str, str] = {}
    service_name = getenv(_KWS_SERVICE_NAME) or ""
    credential = getenv(_KWS_SERVICE_CREDENTIAL) or ""
    if service_name:
        headers["kws-bin"] = (
            base64.b64encode(_kws_model(service_name)).decode("ascii").rstrip("=")
        )
    if credential:
        headers["kwsCredential"] = credential
    return MappingProxyType(headers)
