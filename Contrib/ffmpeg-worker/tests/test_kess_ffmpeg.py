import base64
import json

import pytest
from temporalio.exceptions import ApplicationError

from abi import FfmpegStep, FfmpegToolInput
from kess_ffmpeg import (
    KessFfmpegClient,
    KessFfmpegConfig,
    _url_to_blob_key,
)
from protos import infer_pb2


class FakeStub:
    def __init__(self, response: infer_pb2.InferResponse) -> None:
        self.response = response
        self.requests: list[infer_pb2.InferRequest] = []

    def Infer(self, request, timeout):  # noqa: N802 (gRPC 方法名)
        self.requests.append(request)
        self.timeout = timeout
        return self.response


def make_client(
    response: infer_pb2.InferResponse,
) -> tuple[KessFfmpegClient, FakeStub]:
    client = KessFfmpegClient(KessFfmpegConfig())
    stub = FakeStub(response)
    client._grpc = stub
    return client, stub


def video_response(key: str = "upload:ylab-stunt:out") -> infer_pb2.InferResponse:
    resp = infer_pb2.InferResponse()
    resp.feature["error_code"].int_val = 0
    resp.feature["video"].str_val = key
    resp.feature["output_size"].int_val = 42
    return resp


def test_url_to_blob_key_known_hosts() -> None:
    assert (
        _url_to_blob_key("http://bs3-hb1.internal/upload-ylab-stunt/some_key")
        == "upload:ylab-stunt:some_key"
    )
    assert (
        _url_to_blob_key("https://bs3.corp.kuaishou.com/upload-ylab-stunt/k")
        == "upload:ylab-stunt:k"
    )
    assert _url_to_blob_key("https://cdn.example.com/upload-x/k") is None
    assert _url_to_blob_key("http://bs3-hb1.internal/nokey") is None


def test_infer_builds_single_op_request() -> None:
    client, stub = make_client(video_response())

    out = client.infer(
        FfmpegToolInput(
            op="cut",
            params={"start": 1.5, "end": 4.0},
            video="db:table:input_key",
        )
    )

    req = stub.requests[0]
    assert req.feature["op"].str_val == "cut"
    assert json.loads(req.feature["params"].str_val) == {
        "start": 1.5,
        "end": 4.0,
    }
    assert req.feature["video"].str_val == "db:table:input_key"
    assert req.feature["db"].str_val == "upload"
    assert req.feature["table"].str_val == "ylab-stunt"
    assert stub.timeout == 600

    assert out.kind == "video"
    assert out.blobKey == "upload:ylab-stunt:out"
    assert out.uri == "http://bs3-hb1.internal/upload-ylab-stunt/out"
    assert out.ssoUri == "https://bs3.corp.kuaishou.com/upload-ylab-stunt/out"
    assert out.outputSize == 42
    assert out.result is None


def test_infer_builds_pipeline_request_and_resolves_slots() -> None:
    client, stub = make_client(video_response())

    client.infer(
        FfmpegToolInput(
            pipeline=[FfmpegStep(op="freeze", params={"duration": 3})],
            image="base64:aGk=",
            videos=[
                "db:table:k1",
                "http://bs3-hb1.internal/upload-ylab-stunt/k2",
            ],
        )
    )

    req = stub.requests[0]
    assert json.loads(req.feature["pipeline"].str_val) == [
        {"op": "freeze", "params": {"duration": 3}}
    ]
    assert req.feature["image"].str_val == "base64:aGk="
    assert json.loads(req.feature["videos"].str_val) == [
        "db:table:k1",
        "upload:ylab-stunt:k2",
    ]


def test_infer_parses_analysis_result() -> None:
    resp = infer_pb2.InferResponse()
    resp.feature["error_code"].int_val = 0
    resp.feature["result"].str_val = json.dumps({"duration": 3.2})
    client, _ = make_client(resp)

    out = client.infer(FfmpegToolInput(op="probe", video="db:table:key"))

    assert out.kind is None
    assert out.result == {"duration": 3.2}


def test_infer_raises_on_error_code() -> None:
    resp = infer_pb2.InferResponse()
    resp.feature["error_code"].int_val = 7
    resp.feature["error_message"].str_val = "boom"
    client, _ = make_client(resp)

    with pytest.raises(ApplicationError) as raised:
        client.infer(FfmpegToolInput(op="probe", video="db:table:key"))

    assert raised.value.type == "FfmpegInferFailed"
    assert raised.value.non_retryable
    assert "boom" in str(raised.value)


def test_infer_rejects_invalid_media_ref() -> None:
    client, _ = make_client(video_response())

    with pytest.raises(ApplicationError) as raised:
        client.infer(FfmpegToolInput(op="probe", video="not-a-ref"))

    assert raised.value.type == "InvalidArgument"
    assert raised.value.non_retryable


def test_download_inlines_small_files(monkeypatch: pytest.MonkeyPatch) -> None:
    client, stub = make_client(video_response())
    payload = b"tiny-video-bytes"

    class FakeHttpResponse:
        content = payload

        def raise_for_status(self) -> None:
            pass

    import requests

    monkeypatch.setattr(
        requests, "get", lambda url, timeout: FakeHttpResponse()
    )

    client.infer(
        FfmpegToolInput(op="probe", video="https://cdn.example.com/a.mp4")
    )

    assert stub.requests[0].feature["video"].str_val == (
        "base64:" + base64.b64encode(payload).decode()
    )


def test_download_rejects_oversized_files(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    config = KessFfmpegConfig(inline_limit=4)
    client = KessFfmpegClient(config)
    client._grpc = FakeStub(video_response())

    class FakeHttpResponse:
        content = b"too-large"

        def raise_for_status(self) -> None:
            pass

    import requests

    monkeypatch.setattr(
        requests, "get", lambda url, timeout: FakeHttpResponse()
    )

    with pytest.raises(ApplicationError) as raised:
        client.infer(
            FfmpegToolInput(op="probe", video="https://cdn.example.com/a.mp4")
        )

    assert raised.value.type == "InvalidArgument"
