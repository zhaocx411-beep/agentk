package com.kuaishou.agent.operation.backend.agentk.grpc;

import static org.assertj.core.api.Assertions.assertThat;

import org.junit.jupiter.api.Test;
import org.springframework.http.HttpStatus;

import com.connectrpc.Code;
import com.connectrpc.ConnectException;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKErrorDto;

import io.grpc.Status;
import io.grpc.StatusRuntimeException;

class AgentKConnectErrorsTest {

    @Test
    void invalidArgumentFromGrpcIsNotMappedAsBadGateway() {
        StatusRuntimeException thrown = Status.INVALID_ARGUMENT
                .withDescription("pipeline \"capability_run\": missing referImages")
                .asRuntimeException();

        AgentKConnectErrors.Failure failure = AgentKConnectErrors.describe(thrown);

        assertThat(failure.grpcCode).isEqualTo("INVALID_ARGUMENT");
        assertThat(failure.grpcMessage).contains("referImages");
        assertThat(failure.httpStatus).isEqualTo(HttpStatus.BAD_REQUEST);

        AgentKErrorDto dto = failure.toDto();
        assertThat(dto.getGrpcCode()).isEqualTo("INVALID_ARGUMENT");
        assertThat(dto.getError()).startsWith("INVALID_ARGUMENT:");
        assertThat(dto.getWrappedHttp()).isEqualTo(400);
    }

    @Test
    void unwrapsStatusRuntimeExceptionFromCause() {
        Exception wrapped = new RuntimeException(Status.UNAVAILABLE
                .withDescription("no kess instance")
                .asRuntimeException());

        AgentKConnectErrors.Failure failure = AgentKConnectErrors.describe(wrapped);

        assertThat(failure.grpcCode).isEqualTo("UNAVAILABLE");
        assertThat(failure.grpcMessage).isEqualTo("no kess instance");
        assertThat(failure.httpStatus).isEqualTo(HttpStatus.BAD_GATEWAY);
    }

    @Test
    void connectInvalidArgumentMapsToBadRequest() {
        ConnectException thrown = new ConnectException(Code.INVALID_ARGUMENT, "bad args");

        AgentKConnectErrors.Failure failure = AgentKConnectErrors.describe(thrown);

        assertThat(failure.grpcCode).isEqualTo("INVALID_ARGUMENT");
        assertThat(failure.httpStatus).isEqualTo(HttpStatus.BAD_REQUEST);
        assertThat(failure.grpcMessage).isEqualTo("bad args");
    }
}
