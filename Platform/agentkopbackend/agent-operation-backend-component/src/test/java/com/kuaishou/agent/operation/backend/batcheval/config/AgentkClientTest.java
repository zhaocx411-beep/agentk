package com.kuaishou.agent.operation.backend.batcheval.config;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import java.util.ArrayList;
import java.util.Arrays;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.function.Function;

import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ArrayNode;
import com.kuaishou.agent.operation.backend.agentk.AgentkOutboundException;
import com.kuaishou.agent.operation.backend.agentk.AgentkOutboundGateway;
import com.kuaishou.agent.operation.backend.batcheval.config.AgentkClient.RunResult;
import com.kuaishou.agent.operation.backend.batcheval.support.LoopUserMessage;

class AgentkClientTest {

    private static final ObjectMapper MAPPER = new ObjectMapper();

    private StubGateway gateway;
    private AgentkClient client;
    private AgentkProperties properties;

    @BeforeEach
    void setUp() {
        gateway = new StubGateway();
        gateway.on("POST", "/sessions", query -> "{\"id\":\"ses_demo\"}");
        gateway.on("POST", "/sessions/ses_demo/messages", query ->
                "{\"sessionId\":\"ses_demo\",\"executionId\":\"exe_demo\",\"events\":["
                        + "{\"seq\":1,\"kind\":\"user.input\",\"data\":{\"text\":\"hello\"}}]}");
        gateway.on("GET", "/sessions/ses_demo/events", query -> {
            assertThat(query).isEqualTo(AgentkClient.ALL_EVENTS_QUERY);
            return "{\"events\":["
                    + "{\"seq\":2,\"kind\":\"round.started\",\"roundId\":\"rnd_demo\","
                    + "\"executionId\":\"exe_demo\",\"data\":{}},"
                    + "{\"seq\":3,\"kind\":\"assistant.text\",\"roundId\":\"rnd_demo\","
                    + "\"executionId\":\"exe_demo\",\"data\":{\"text\":\"hi\"}},"
                    + "{\"seq\":4,\"kind\":\"round.completed\",\"roundId\":\"rnd_demo\","
                    + "\"executionId\":\"exe_demo\","
                    + "\"data\":{\"usage\":{\"inputTokens\":1,\"outputTokens\":2},\"finishReason\":\"stop\"}},"
                    + "{\"seq\":5,\"kind\":\"execution.finished\",\"executionId\":\"exe_demo\","
                    + "\"data\":{\"status\":\"EXECUTION_STATUS_COMPLETED\"}}"
                    + "],\"hasMore\":false}";
        });
        gateway.on("GET", "/capabilities", query -> "{\"capabilities\":[{"
                + "\"id\":\"contrib.x\","
                + "\"kind\":\"activity\","
                + "\"description\":\"x\","
                + "\"inputSchema\":{},"
                + "\"outputSchema\":{},"
                + "\"timeoutSeconds\":30,"
                + "\"toolRef\":\"tool:contrib.x\","
                + "\"tool\":{\"name\":\"x_tool\",\"description\":\"x\",\"mode\":\"blocking\"}"
                + "}]}");

        properties = new AgentkProperties();
        properties.setPollIntervalMs(1);
        properties.setRunTimeoutSeconds(2);
        client = newClient();
    }

    private AgentkClient newClient() {
        return new AgentkClient(gateway, properties, MAPPER, null);
    }

    @Test
    void shouldExecuteRunAndHandOffEventsToSink() {
        RecordingSink sink = new RecordingSink();

        RunResult result = client.execute("demo",
                "{\"defaultModel\":\"demo\",\"system\":\"demo\",\"tools\":[]}", "hello", sink);

        assertThat(result.getSessionId()).isEqualTo("ses_demo");
        assertThat(result.getExecutionId()).isEqualTo("exe_demo");
        assertThat(result.getLastRoundId()).isEqualTo("rnd_demo");
        assertThat(result.getSummary().path("status").asText()).isEqualTo("completed");
        assertThat(result.getSummary().path("executionId").asText()).isEqualTo("exe_demo");
        // 摘要不再带事件本体，事件全部经 sink 交出：user.input 1 条 + 轮询整页 4 条
        assertThat(result.getSummary().has("events")).isFalse();
        assertThat(result.getSummary().path("eventCount").asInt()).isEqualTo(5);
        assertThat(sink.kinds()).containsExactly("user.input", "round.started", "assistant.text",
                "round.completed", "execution.finished");
    }

    @Test
    void shouldPostAttachmentsWithText() {
        RecordingSink sink = new RecordingSink();
        LoopUserMessage message = new LoopUserMessage("看这些附件", Arrays.asList(
                new LoopUserMessage.Attachment("image", "https://img/a.jpg", "image/jpeg"),
                new LoopUserMessage.Attachment("audio", "https://cdn/a.mp3", "audio/mpeg")));

        RunResult result = newClient().runLoop("ses_demo", message, sink);

        assertThat(result.getSummary().path("status").asText()).isEqualTo("completed");
        JsonNode body = gateway.lastBody("POST", "/sessions/ses_demo/messages");
        assertThat(body.path("text").asText()).isEqualTo("看这些附件");
        assertThat(body.path("attachments")).hasSize(2);
        assertThat(body.path("attachments").get(0).path("type").asText()).isEqualTo("image");
        assertThat(body.path("attachments").get(0).path("uri").asText()).isEqualTo("https://img/a.jpg");
        assertThat(body.path("attachments").get(1).path("type").asText()).isEqualTo("audio");
        assertThat(body.path("attachments").get(1).path("mimeType").asText()).isEqualTo("audio/mpeg");
    }

    @Test
    void shouldWaitForCurrentExecutionWhenSessionAlreadyHasPriorFinished() {
        gateway.on("POST", "/sessions/ses_demo/messages", query ->
                "{\"sessionId\":\"ses_demo\",\"executionId\":\"exe_turn2\",\"events\":["
                        + "{\"seq\":6,\"kind\":\"user.input\",\"eventId\":\"e6\","
                        + "\"executionId\":\"exe_turn2\",\"data\":{\"text\":\"second\"}}]}");
        AtomicInteger polls = new AtomicInteger();
        gateway.on("GET", "/sessions/ses_demo/events", query -> {
            int n = polls.incrementAndGet();
            if (n == 1) {
                return "{\"events\":["
                        + "{\"seq\":1,\"kind\":\"user.input\",\"eventId\":\"e1\","
                        + "\"executionId\":\"exe_turn1\",\"data\":{\"text\":\"first\"}},"
                        + "{\"seq\":5,\"kind\":\"execution.finished\",\"eventId\":\"e5\","
                        + "\"executionId\":\"exe_turn1\","
                        + "\"data\":{\"status\":\"EXECUTION_STATUS_COMPLETED\"}},"
                        + "{\"seq\":6,\"kind\":\"user.input\",\"eventId\":\"e6\","
                        + "\"executionId\":\"exe_turn2\",\"data\":{\"text\":\"second\"}}"
                        + "],\"hasMore\":false}";
            }
            return "{\"events\":["
                    + "{\"seq\":1,\"kind\":\"user.input\",\"eventId\":\"e1\","
                    + "\"executionId\":\"exe_turn1\",\"data\":{\"text\":\"first\"}},"
                    + "{\"seq\":5,\"kind\":\"execution.finished\",\"eventId\":\"e5\","
                    + "\"executionId\":\"exe_turn1\","
                    + "\"data\":{\"status\":\"EXECUTION_STATUS_COMPLETED\"}},"
                    + "{\"seq\":6,\"kind\":\"user.input\",\"eventId\":\"e6\","
                    + "\"executionId\":\"exe_turn2\",\"data\":{\"text\":\"second\"}},"
                    + "{\"seq\":7,\"kind\":\"assistant.text\",\"eventId\":\"e7\","
                    + "\"executionId\":\"exe_turn2\",\"data\":{\"text\":\"苹果\"}},"
                    + "{\"seq\":8,\"kind\":\"execution.finished\",\"eventId\":\"e8\","
                    + "\"executionId\":\"exe_turn2\","
                    + "\"data\":{\"status\":\"EXECUTION_STATUS_COMPLETED\"}}"
                    + "],\"hasMore\":false}";
        });
        properties.setPollIntervalMs(1);
        properties.setRunTimeoutSeconds(2);

        RecordingSink sink = new RecordingSink();
        RunResult result = newClient().runLoop("ses_demo", "second", sink);

        assertThat(result.getExecutionId()).isEqualTo("exe_turn2");
        assertThat(result.getSummary().path("status").asText()).isEqualTo("completed");
        assertThat(polls.get()).isGreaterThanOrEqualTo(2);
        assertThat(sink.kinds()).contains("assistant.text", "execution.finished");
        assertThat(sink.text()).contains("苹果").doesNotContain("\"exe_turn1\"");
    }

    @Test
    void shouldAggregateMetricsWhileStreaming() {
        RunResult result = client.execute("demo",
                "{\"defaultModel\":\"demo\",\"system\":\"demo\",\"tools\":[]}", "hello");

        JsonNode metrics = result.getSummary().path("metrics");
        assertThat(metrics.path("steps").asInt()).isEqualTo(1);
        assertThat(metrics.path("inputTokens").asInt()).isEqualTo(1);
        assertThat(metrics.path("outputTokens").asInt()).isEqualTo(2);
        assertThat(result.getSummary().path("seqFrom").asInt()).isEqualTo(1);
        assertThat(result.getSummary().path("seqTo").asInt()).isEqualTo(5);
    }

    @Test
    void shouldKeepRunningWhenSinkThrows() {
        RunResult result = client.execute("demo",
                "{\"defaultModel\":\"demo\",\"system\":\"demo\",\"tools\":[]}", "hello",
                events -> {
                    throw new IllegalStateException("db down");
                });

        // 事件账本可以补，重跑一遍视频要几十分钟：落库失败不该把用例判失败
        assertThat(result.getSummary().path("status").asText()).isEqualTo("completed");
    }

    @Test
    void shouldDeliverToolCallPastDefaultListPageToSink() {
        // 列表接口缺省 500；不带 all=true 时适配层会丢掉后面的 tool_call。
        // 客户端必须用与在线 follow-up 相同的 query，并原样把整页交给 sink。
        StringBuilder ledger = new StringBuilder();
        ledger.append("{\"seq\":2,\"kind\":\"round.started\",\"roundId\":\"r\",\"data\":{}}");
        for (int i = 3; i <= 501; i++) {
            ledger.append(",{\"seq\":").append(i)
                    .append(",\"kind\":\"assistant.text\",\"data\":{\"text\":\"p\"}}");
        }
        ledger.append(",{\"seq\":502,\"kind\":\"assistant.tool_call\",\"data\":")
                .append("{\"toolCallId\":\"c1\",\"toolName\":\"generate_image\"}}");
        ledger.append(",{\"seq\":503,\"kind\":\"execution.finished\",\"executionId\":\"exe_demo\",")
                .append("\"data\":{\"status\":\"EXECUTION_STATUS_COMPLETED\"}}");
        String page = "{\"events\":[" + ledger + "],\"hasMore\":false}";
        gateway.on("GET", "/sessions/ses_demo/events", query -> {
            assertThat(query).isEqualTo(AgentkClient.ALL_EVENTS_QUERY);
            return page;
        });

        RecordingSink sink = new RecordingSink();
        RunResult result = newClient().execute("demo",
                "{\"defaultModel\":\"demo\",\"system\":\"demo\",\"tools\":[]}", "hello", sink);

        assertThat(AgentkClient.ALL_EVENTS_QUERY).isEqualTo("after=0&limit=2000&all=true");
        assertThat(result.getSummary().path("status").asText()).isEqualTo("completed");
        assertThat(result.getSummary().path("metrics").path("toolCalls").asInt()).isEqualTo(1);
        assertThat(result.getSummary().path("eventCount").asInt()).isEqualTo(503);
        assertThat(sink.text()).contains("generate_image");
    }

    @Test
    void shouldIdleWhenEventsArriveNewestFirstWithExecutionCompleted() {
        // ListEvents 新事件在前：seq=1 是 execution.finished，seq 更大的才是 round.started。
        // 终态只认 kind=execution.finished，与在线 inferTurnStatus / WireConverter 一致。
        gateway.on("POST", "/sessions/ses_demo/messages", query ->
                "{\"sessionId\":\"ses_demo\",\"executionId\":\"exe_01a0194b-91c3-7426-a891-f100a08c4c11\",\"events\":["
                        + "{\"eventId\":\"evt_user\",\"seq\":9,\"kind\":\"user.input\","
                        + "\"executionId\":\"exe_01a0194b-91c3-7426-a891-f100a08c4c11\","
                        + "\"createdAt\":\"2026-08-19T09:12:40.000Z\",\"data\":{\"text\":\"hello\"}}]}");
        gateway.on("GET", "/sessions/ses_demo/events", query -> {
            assertThat(query).isEqualTo(AgentkClient.ALL_EVENTS_QUERY);
            return "{\"events\":["
                    + "{\"eventId\":\"evt_done\",\"executionId\":\"exe_01a0194b-91c3-7426-a891-f100a08c4c11\","
                    + "\"seq\":1,\"roundId\":\"\",\"kind\":\"execution.finished\","
                    + "\"data\":{\"status\":\"EXECUTION_STATUS_COMPLETED\",\"error\":\"\",\"agent\":{}},"
                    + "\"createdAt\":\"2026-08-19T09:13:15.316Z\"},"
                    + "{\"eventId\":\"evt_round_done\",\"executionId\":\"exe_01a0194b-91c3-7426-a891-f100a08c4c11\","
                    + "\"seq\":2,\"roundId\":\"rnd_3d5a58cc5d8326422b1219c2\",\"kind\":\"round.completed\","
                    + "\"data\":{\"usage\":{\"inputTokens\":6854,\"outputTokens\":93},"
                    + "\"finishReason\":\"stop\"},"
                    + "\"createdAt\":\"2026-08-19T09:13:15.237Z\"},"
                    + "{\"eventId\":\"evt_text\",\"executionId\":\"exe_01a0194b-91c3-7426-a891-f100a08c4c11\","
                    + "\"seq\":3,\"roundId\":\"rnd_3d5a58cc5d8326422b1219c2\",\"kind\":\"assistant.text\","
                    + "\"data\":{\"block\":1,\"text\":\"图片已生成。URL：`https://cdn/a.png`\"},"
                    + "\"createdAt\":\"2026-08-19T09:13:15.237Z\"},"
                    + "{\"eventId\":\"evt_started\",\"executionId\":\"exe_01a0194b-91c3-7426-a891-f100a08c4c11\","
                    + "\"seq\":5,\"roundId\":\"rnd_3d5a58cc5d8326422b1219c2\",\"kind\":\"round.started\","
                    + "\"data\":{\"model\":\"deepseek-v4-pro\"},"
                    + "\"createdAt\":\"2026-08-19T09:12:41.000Z\"},"
                    + "{\"eventId\":\"evt_user\",\"executionId\":\"exe_01a0194b-91c3-7426-a891-f100a08c4c11\","
                    + "\"seq\":9,\"kind\":\"user.input\","
                    + "\"createdAt\":\"2026-08-19T09:12:40.000Z\",\"data\":{\"text\":\"hello\"}}"
                    + "],\"hasMore\":false}";
        });

        RecordingSink sink = new RecordingSink();
        RunResult result = newClient().execute("demo",
                "{\"defaultModel\":\"demo\",\"system\":\"demo\",\"tools\":[]}", "hello", sink);

        assertThat(result.getSummary().path("status").asText()).isEqualTo("completed");
        assertThat(result.getExecutionId()).isEqualTo("exe_01a0194b-91c3-7426-a891-f100a08c4c11");
        assertThat(sink.text()).contains("https://cdn/a.png");
    }

    @Test
    void shouldNotTreatRoundCompletedWithExecutionStatusAsTerminal() {
        // 与在线 inferTurnStatus 一致：kind 必须是 execution.finished，
        // 不能把旧适配伪装的 round.completed + EXECUTION_STATUS_* 当终态。
        gateway.on("POST", "/sessions/ses_demo/messages", query ->
                "{\"sessionId\":\"ses_demo\",\"executionId\":\"exe_disguise\",\"events\":["
                        + "{\"seq\":1,\"kind\":\"user.input\",\"data\":{\"text\":\"hello\"}}]}");
        gateway.on("GET", "/sessions/ses_demo/events", query -> {
            assertThat(query).isEqualTo(AgentkClient.ALL_EVENTS_QUERY);
            return "{\"events\":["
                    + "{\"seq\":2,\"kind\":\"round.started\",\"roundId\":\"rnd_x\",\"data\":{}},"
                    + "{\"seq\":3,\"kind\":\"round.completed\",\"roundId\":\"rnd_x\","
                    + "\"data\":{\"finishReason\":\"stop\"}},"
                    + "{\"seq\":4,\"kind\":\"round.completed\","
                    + "\"data\":{\"status\":\"EXECUTION_STATUS_COMPLETED\",\"agent\":{}}}"
                    + "],\"hasMore\":false}";
        });
        properties.setRunTimeoutSeconds(1);
        properties.setPollIntervalMs(1);

        assertThatThrownBy(() -> newClient().execute("demo",
                "{\"defaultModel\":\"demo\",\"system\":\"demo\",\"tools\":[]}", "hello"))
                .isInstanceOf(AgentkClient.AgentkClientException.class)
                .hasMessageContaining("超时")
                .hasMessageContaining("executionFinished=false");
    }

    @Test
    void shouldSerializeOutboundRpcsToMaxInFlight() throws Exception {
        properties.setMaxOutboundInFlight(1);
        AtomicInteger inFlight = new AtomicInteger();
        AtomicInteger peak = new AtomicInteger();
        gateway.beforeExchange = () -> {
            int current = inFlight.incrementAndGet();
            peak.accumulateAndGet(current, Math::max);
            try {
                Thread.sleep(80);
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
                throw new IllegalStateException(e);
            } finally {
                inFlight.decrementAndGet();
            }
        };
        AgentkClient limited = newClient();
        ExecutorService pool = Executors.newFixedThreadPool(2);
        try {
            Future<String> first = pool.submit(() -> limited.createSession("a",
                    "{\"defaultModel\":\"demo\",\"system\":\"demo\",\"tools\":[]}"));
            Future<String> second = pool.submit(() -> limited.createSession("b",
                    "{\"defaultModel\":\"demo\",\"system\":\"demo\",\"tools\":[]}"));
            assertThat(first.get(3, TimeUnit.SECONDS)).isEqualTo("ses_demo");
            assertThat(second.get(3, TimeUnit.SECONDS)).isEqualTo("ses_demo");
        } finally {
            pool.shutdownNow();
        }
        assertThat(peak.get()).isEqualTo(1);
    }

    @Test
    void shouldLeaveExecutionIdEmptyWhenUpstreamOmitsIt() {
        gateway.on("POST", "/sessions/ses_demo/messages", query ->
                "{\"sessionId\":\"ses_demo\",\"events\":["
                        + "{\"seq\":1,\"kind\":\"user.input\",\"data\":{\"text\":\"hello\"}}]}");
        gateway.on("GET", "/sessions/ses_demo/events", query -> {
            assertThat(query).isEqualTo(AgentkClient.ALL_EVENTS_QUERY);
            return "{\"events\":["
                    + "{\"seq\":2,\"kind\":\"round.started\",\"roundId\":\"rnd_only\",\"data\":{}},"
                    + "{\"seq\":3,\"kind\":\"round.completed\",\"roundId\":\"rnd_only\",\"data\":{}},"
                    + "{\"seq\":4,\"kind\":\"execution.finished\","
                    + "\"data\":{\"status\":\"EXECUTION_STATUS_COMPLETED\"}}"
                    + "],\"hasMore\":false}";
        });

        RunResult result = newClient().execute("demo",
                "{\"defaultModel\":\"demo\",\"system\":\"demo\",\"tools\":[]}", "hello");

        // roundId 不能顶替 executionId
        assertThat(result.getExecutionId()).isNull();
        assertThat(result.getLastRoundId()).isEqualTo("rnd_only");
        assertThat(result.getSummary().has("executionId")).isFalse();
    }

    @Test
    void shouldListCapabilities() {
        assertThat(client.listCapabilities().path("capabilities")).hasSize(1);
        assertThat(client.listCapabilities().path("capabilities").get(0).path("tool").path("name").asText())
                .isEqualTo("x_tool");
    }

    @Test
    void shouldRetryGetButNotPostOnRetryableFailure() {
        gateway.fail("GET", "/capabilities", 503, 2);
        properties.setMaxRetries(3);

        assertThat(newClient().listCapabilities().path("capabilities")).hasSize(1);
        assertThat(gateway.calls("GET", "/capabilities")).isEqualTo(3);
    }

    @Test
    void shouldRetryCreateSessionOn502ThenSucceed() {
        gateway.fail("POST", "/sessions", 502, 2);
        properties.setMaxRetries(3);

        assertThat(newClient().createSession("demo",
                "{\"defaultModel\":\"demo\",\"system\":\"demo\",\"tools\":[]}")).isEqualTo("ses_demo");
        assertThat(gateway.calls("POST", "/sessions")).isEqualTo(3);
    }

    @Test
    void shouldNotRetryCreateSessionOnClientError() {
        gateway.fail("POST", "/sessions", 400, 5);
        properties.setMaxRetries(3);

        assertThatThrownBy(() -> newClient().createSession("demo",
                "{\"defaultModel\":\"demo\",\"system\":\"demo\",\"tools\":[]}"))
                .isInstanceOf(AgentkClient.AgentkClientException.class)
                .hasMessageContaining("400");
        assertThat(gateway.calls("POST", "/sessions")).isEqualTo(1);
    }

    @Test
    void shouldNotRetryPostMessageOnRetryableFailure() {
        gateway.fail("POST", "/sessions/ses_demo/messages", 502, 5);
        properties.setMaxRetries(3);

        assertThatThrownBy(() -> newClient().runLoop("ses_demo", "hello"))
                .isInstanceOf(AgentkClient.AgentkClientException.class)
                .hasMessageContaining("502");
        assertThat(gateway.calls("POST", "/sessions/ses_demo/messages")).isEqualTo(1);
    }

    @Test
    void shouldRetryPostMessageWhenPreviousExecutionStillActive() {
        gateway.fail("POST", "/sessions/ses_demo/messages", 400, 2,
                "agent-k 400: FAILED_PRECONDITION: session already has an active execution");
        properties.setPollIntervalMs(1);
        properties.setRunTimeoutSeconds(5);

        RunResult result = newClient().runLoop("ses_demo", "hello");

        assertThat(result.getExecutionId()).isEqualTo("exe_demo");
        assertThat(result.getSummary().path("status").asText()).isEqualTo("completed");
        assertThat(gateway.calls("POST", "/sessions/ses_demo/messages")).isEqualTo(3);
    }

    @Test
    void shouldWaitForAsyncToolJobAfterRoundCompletesWithToolCalls() {
        // 复现：round.completed(finishReason=tool_calls) + tool.exec_started 后若立刻当空闲，
        // 会丢后续 job.submitted / job.completed。
        gateway.on("POST", "/sessions/ses_demo/messages", query ->
                "{\"sessionId\":\"ses_demo\",\"executionId\":\"exe_async\",\"events\":["
                        + "{\"seq\":1,\"kind\":\"user.input\",\"data\":{\"text\":\"gen video\"}}]}");
        final int[] eventPolls = {0};
        gateway.on("GET", "/sessions/ses_demo/events", query -> {
            assertThat(query).isEqualTo(AgentkClient.ALL_EVENTS_QUERY);
            eventPolls[0]++;
            // 账本按轮询次数变长：先停在 tool.exec_started，再补 job，模拟真实竞态。
            if (eventPolls[0] == 1) {
                return "{\"events\":["
                        + "{\"seq\":2,\"kind\":\"round.started\",\"roundId\":\"rnd_async\",\"data\":{}},"
                        + "{\"seq\":3,\"kind\":\"assistant.tool_call\",\"roundId\":\"rnd_async\","
                        + "\"data\":{\"toolCallId\":\"call_1\",\"toolName\":\"kling_image_to_video\",\"args\":{}}},"
                        + "{\"seq\":4,\"kind\":\"round.completed\",\"roundId\":\"rnd_async\","
                        + "\"data\":{\"finishReason\":\"tool_calls\"}},"
                        + "{\"seq\":5,\"kind\":\"tool.exec_started\",\"roundId\":\"rnd_async\","
                        + "\"data\":{\"toolCallId\":\"call_1\"}}"
                        + "],\"hasMore\":false}";
            }
            if (eventPolls[0] == 2) {
                return "{\"events\":["
                        + "{\"seq\":2,\"kind\":\"round.started\",\"roundId\":\"rnd_async\",\"data\":{}},"
                        + "{\"seq\":3,\"kind\":\"assistant.tool_call\",\"roundId\":\"rnd_async\","
                        + "\"data\":{\"toolCallId\":\"call_1\",\"toolName\":\"kling_image_to_video\",\"args\":{}}},"
                        + "{\"seq\":4,\"kind\":\"round.completed\",\"roundId\":\"rnd_async\","
                        + "\"data\":{\"finishReason\":\"tool_calls\"}},"
                        + "{\"seq\":5,\"kind\":\"tool.exec_started\",\"roundId\":\"rnd_async\","
                        + "\"data\":{\"toolCallId\":\"call_1\"}},"
                        + "{\"seq\":6,\"kind\":\"tool.exec_succeeded\",\"roundId\":\"rnd_async\","
                        + "\"data\":{\"toolCallId\":\"call_1\",\"content\":\"{\\\"jobId\\\":\\\"job_1\\\"}\"}},"
                        + "{\"seq\":7,\"kind\":\"job.submitted\",\"roundId\":\"rnd_async\","
                        + "\"data\":{\"jobId\":\"job_1\"}}"
                        + "],\"hasMore\":false}";
            }
            return "{\"events\":["
                    + "{\"seq\":2,\"kind\":\"round.started\",\"roundId\":\"rnd_async\",\"data\":{}},"
                    + "{\"seq\":3,\"kind\":\"assistant.tool_call\",\"roundId\":\"rnd_async\","
                    + "\"data\":{\"toolCallId\":\"call_1\",\"toolName\":\"kling_image_to_video\",\"args\":{}}},"
                    + "{\"seq\":4,\"kind\":\"round.completed\",\"roundId\":\"rnd_async\","
                    + "\"data\":{\"finishReason\":\"tool_calls\"}},"
                    + "{\"seq\":5,\"kind\":\"tool.exec_started\",\"roundId\":\"rnd_async\","
                    + "\"data\":{\"toolCallId\":\"call_1\"}},"
                    + "{\"seq\":6,\"kind\":\"tool.exec_succeeded\",\"roundId\":\"rnd_async\","
                    + "\"data\":{\"toolCallId\":\"call_1\",\"content\":\"{\\\"jobId\\\":\\\"job_1\\\"}\"}},"
                    + "{\"seq\":7,\"kind\":\"job.submitted\",\"roundId\":\"rnd_async\","
                    + "\"data\":{\"jobId\":\"job_1\"}},"
                    + "{\"seq\":8,\"kind\":\"job.completed\",\"roundId\":\"rnd_async\","
                    + "\"data\":{\"jobId\":\"job_1\",\"artifacts\":[{\"uri\":\"https://cdn/x.mp4\"}]}},"
                    + "{\"seq\":9,\"kind\":\"execution.finished\",\"executionId\":\"exe_async\","
                    + "\"data\":{\"status\":\"EXECUTION_STATUS_COMPLETED\"}}"
                    + "],\"hasMore\":false}";
        });

        properties.setRunTimeoutSeconds(5);
        RecordingSink sink = new RecordingSink();
        RunResult result = newClient().execute("demo",
                "{\"defaultModel\":\"demo\",\"system\":\"demo\",\"tools\":[{\"name\":\"kling_image_to_video\"}]}",
                "gen video", sink);

        assertThat(result.getSummary().path("status").asText()).isEqualTo("completed");
        assertThat(sink.text())
                .contains("job.submitted")
                .contains("job.completed")
                .contains("https://cdn/x.mp4")
                .contains("execution.finished");
    }

    @Test
    void shouldKeepPollingAfterSyncToolSucceededUntilExecutionFinished() {
        // 复现漫剧：round.completed(finishReason=tool_calls) + 同步 generate_image 成功后，
        // 旧逻辑把「round 结束 + pending tools 空」当整场结束，丢掉后续轮次。
        gateway.on("POST", "/sessions/ses_demo/messages", query ->
                "{\"sessionId\":\"ses_demo\",\"executionId\":\"exe_manju\",\"events\":["
                        + "{\"seq\":1,\"kind\":\"user.input\",\"data\":{\"text\":\"gen manju\"}}]}");
        final int[] eventPolls = {0};
        String firstRoundLedger = "{\"seq\":2,\"kind\":\"round.started\",\"roundId\":\"rnd_1\","
                + "\"executionId\":\"exe_manju\",\"data\":{}},"
                + "{\"seq\":3,\"kind\":\"assistant.tool_call\",\"roundId\":\"rnd_1\","
                + "\"data\":{\"toolCallId\":\"call_img\",\"toolName\":\"generate_image\"}},"
                + "{\"seq\":4,\"kind\":\"round.completed\",\"roundId\":\"rnd_1\","
                + "\"data\":{\"finishReason\":\"tool_calls\"}},"
                + "{\"seq\":5,\"kind\":\"tool.exec_succeeded\",\"roundId\":\"rnd_1\","
                + "\"data\":{\"toolCallId\":\"call_img\","
                + "\"content\":\"{\\\"uri\\\":\\\"https://cdn/sheet.png\\\"}\"}}";
        gateway.on("GET", "/sessions/ses_demo/events", query -> {
            assertThat(query).isEqualTo(AgentkClient.ALL_EVENTS_QUERY);
            eventPolls[0]++;
            if (eventPolls[0] < 3) {
                return "{\"events\":[" + firstRoundLedger + "],\"hasMore\":false}";
            }
            return "{\"events\":["
                    + firstRoundLedger + ","
                    + "{\"seq\":6,\"kind\":\"round.started\",\"roundId\":\"rnd_2\","
                    + "\"executionId\":\"exe_manju\",\"data\":{}},"
                    + "{\"seq\":7,\"kind\":\"assistant.text\",\"roundId\":\"rnd_2\","
                    + "\"data\":{\"text\":\"video ready https://cdn/manju.mp4\"}},"
                    + "{\"seq\":8,\"kind\":\"round.completed\",\"roundId\":\"rnd_2\","
                    + "\"data\":{\"finishReason\":\"stop\"}},"
                    + "{\"seq\":9,\"kind\":\"execution.finished\",\"executionId\":\"exe_manju\","
                    + "\"data\":{\"status\":\"EXECUTION_STATUS_COMPLETED\"}}"
                    + "],\"hasMore\":false}";
        });

        properties.setRunTimeoutSeconds(5);
        RecordingSink sink = new RecordingSink();
        RunResult result = newClient().execute("demo",
                "{\"defaultModel\":\"demo\",\"system\":\"demo\",\"tools\":[{\"name\":\"generate_image\"}]}",
                "gen manju", sink);

        assertThat(eventPolls[0]).isGreaterThanOrEqualTo(3);
        assertThat(result.getSummary().path("status").asText()).isEqualTo("completed");
        assertThat(result.getLastRoundId()).isEqualTo("rnd_2");
        assertThat(sink.text())
                .contains("finishReason\":\"tool_calls")
                .contains("https://cdn/sheet.png")
                .contains("https://cdn/manju.mp4")
                .contains("execution.finished");
        // 每轮都把整页交出去，前几轮的事件会重复投递，去重是写入侧的事
        assertThat(sink.pages()).isGreaterThanOrEqualTo(3);
    }

    @Test
    void shouldMarkFailedOnlyWhenExecutionFinishedFailed() {
        gateway.on("POST", "/sessions/ses_demo/messages", query ->
                "{\"sessionId\":\"ses_demo\",\"executionId\":\"exe_fail\",\"events\":["
                        + "{\"seq\":1,\"kind\":\"user.input\",\"data\":{\"text\":\"gen\"}}]}");
        gateway.on("GET", "/sessions/ses_demo/events", query -> {
            assertThat(query).isEqualTo(AgentkClient.ALL_EVENTS_QUERY);
            return "{\"events\":["
                    + "{\"seq\":2,\"kind\":\"round.started\",\"roundId\":\"rnd_fail\",\"data\":{}},"
                    + "{\"seq\":3,\"kind\":\"assistant.tool_call\",\"roundId\":\"rnd_fail\","
                    + "\"data\":{\"toolCallId\":\"call_x\",\"toolName\":\"kling_image_to_video\"}},"
                    + "{\"seq\":4,\"kind\":\"round.completed\",\"roundId\":\"rnd_fail\","
                    + "\"data\":{\"finishReason\":\"tool_calls\"}},"
                    + "{\"seq\":5,\"kind\":\"tool.exec_started\",\"roundId\":\"rnd_fail\","
                    + "\"data\":{\"toolCallId\":\"call_x\"}},"
                    + "{\"seq\":6,\"kind\":\"tool.exec_failed\",\"roundId\":\"rnd_fail\","
                    + "\"data\":{\"toolCallId\":\"call_x\",\"error\":\"Account balance not enough\"}},"
                    + "{\"seq\":7,\"kind\":\"execution.finished\",\"executionId\":\"exe_fail\","
                    + "\"data\":{\"status\":\"EXECUTION_STATUS_FAILED\","
                    + "\"error\":\"Account balance not enough\"}}"
                    + "],\"hasMore\":false}";
        });

        properties.setRunTimeoutSeconds(5);
        RunResult result = newClient().execute("demo",
                "{\"defaultModel\":\"demo\",\"system\":\"demo\",\"tools\":[{\"name\":\"kling_image_to_video\"}]}",
                "gen");

        assertThat(result.getSummary().path("status").asText()).isEqualTo("failed");
        assertThat(result.getSummary().path("error").asText()).contains("Account balance not enough");
    }

    @Test
    void shouldKeepPollingAfterToolFailureUntilExecutionCompleted() {
        gateway.on("POST", "/sessions/ses_demo/messages", query ->
                "{\"sessionId\":\"ses_demo\",\"executionId\":\"exe_recover\",\"events\":["
                        + "{\"seq\":1,\"kind\":\"user.input\",\"data\":{\"text\":\"gen\"}}]}");
        final int[] eventPolls = {0};
        String failedLedger = "{\"seq\":2,\"kind\":\"round.started\",\"roundId\":\"rnd_1\","
                + "\"executionId\":\"exe_recover\",\"data\":{}},"
                + "{\"seq\":3,\"kind\":\"assistant.tool_call\",\"roundId\":\"rnd_1\","
                + "\"data\":{\"toolCallId\":\"call_x\",\"toolName\":\"generate_image\"}},"
                + "{\"seq\":4,\"kind\":\"round.completed\",\"roundId\":\"rnd_1\","
                + "\"data\":{\"finishReason\":\"tool_calls\"}},"
                + "{\"seq\":5,\"kind\":\"tool.exec_failed\",\"roundId\":\"rnd_1\","
                + "\"data\":{\"toolCallId\":\"call_x\",\"error\":\"没有工具返回\"}},"
                + "{\"seq\":6,\"kind\":\"error\",\"roundId\":\"rnd_1\","
                + "\"data\":{\"message\":\"没有工具返回\"}}";
        gateway.on("GET", "/sessions/ses_demo/events", query -> {
            assertThat(query).isEqualTo(AgentkClient.ALL_EVENTS_QUERY);
            eventPolls[0]++;
            if (eventPolls[0] < 3) {
                return "{\"events\":[" + failedLedger + "],\"hasMore\":false}";
            }
            return "{\"events\":["
                    + failedLedger + ","
                    + "{\"seq\":7,\"kind\":\"round.started\",\"roundId\":\"rnd_2\","
                    + "\"executionId\":\"exe_recover\",\"data\":{}},"
                    + "{\"seq\":8,\"kind\":\"assistant.tool_call\",\"roundId\":\"rnd_2\","
                    + "\"data\":{\"toolCallId\":\"call_y\",\"toolName\":\"generate_image\"}},"
                    + "{\"seq\":9,\"kind\":\"tool.exec_succeeded\",\"roundId\":\"rnd_2\","
                    + "\"data\":{\"toolCallId\":\"call_y\","
                    + "\"content\":\"{\\\"uri\\\":\\\"https://cdn/ok.png\\\"}\"}},"
                    + "{\"seq\":10,\"kind\":\"round.completed\",\"roundId\":\"rnd_2\","
                    + "\"data\":{\"finishReason\":\"stop\"}},"
                    + "{\"seq\":11,\"kind\":\"execution.finished\",\"executionId\":\"exe_recover\","
                    + "\"data\":{\"status\":\"EXECUTION_STATUS_COMPLETED\"}}"
                    + "],\"hasMore\":false}";
        });

        properties.setRunTimeoutSeconds(5);
        RecordingSink sink = new RecordingSink();
        RunResult result = newClient().execute("demo",
                "{\"defaultModel\":\"demo\",\"system\":\"demo\",\"tools\":[{\"name\":\"generate_image\"}]}",
                "gen", sink);

        assertThat(eventPolls[0]).isGreaterThanOrEqualTo(3);
        assertThat(result.getSummary().path("status").asText()).isEqualTo("completed");
        assertThat(result.getSummary().has("error")).isFalse();
        assertThat(result.getSummary().path("metrics").path("toolErrors").asInt()).isEqualTo(1);
        assertThat(sink.text()).contains("没有工具返回").contains("https://cdn/ok.png")
                .contains("execution.finished");
    }

    @Test
    void runStatsShouldIgnoreProcessErrorsUntilExecutionFinished() {
        ArrayNode events = MAPPER.createArrayNode();
        events.addObject().put("kind", "tool.exec_failed")
                .putObject("data").put("toolCallId", "c1").put("error", "boom");
        events.addObject().put("kind", "error")
                .putObject("data").put("message", "没有工具返回");
        events.addObject().put("kind", "job.failed")
                .putObject("data").put("jobId", "j1").put("error", "job boom");

        AgentkClient.RunStats stats = new AgentkClient.RunStats();
        events.forEach(stats::observe);
        assertThat(stats.resolveFailure(null)).isNull();

        events.addObject().put("kind", "execution.finished")
                .putObject("data").put("status", "EXECUTION_STATUS_COMPLETED");
        stats.observe(events.get(events.size() - 1));
        assertThat(stats.resolveFailure(null)).isNull();
    }

    @Test
    void shouldCreateSessionWithoutRunningLoop() {
        String sessionId = client.createSession("demo",
                "{\"defaultModel\":\"demo\",\"system\":\"demo\",\"tools\":[]}");

        assertThat(sessionId).isEqualTo("ses_demo");
        assertThat(gateway.calls("POST", "/sessions/ses_demo/messages")).isEqualTo(0);
    }

    /** 现行出站契约：工具带裸 capability、不带 tool:* ref，toolModels 一律剥掉。 */
    @Test
    void shouldKeepBareCapabilityAndDropToolModelsBeforeCreateSession() {
        String sessionId = client.createSession("demo",
                "{"
                        + "\"defaultModel\":\"demo\","
                        + "\"system\":\"s\","
                        + "\"tools\":[{"
                        + "\"name\":\"kling_text_to_video\","
                        + "\"capability\":\"agentk.kling_text_to_video\""
                        + "}],"
                        + "\"toolModels\":{\"kling_text_to_video\":\"m1\"}"
                        + "}");

        assertThat(sessionId).isEqualTo("ses_demo");
        JsonNode profile = gateway.lastBody("POST", "/sessions").path("profile");
        assertThat(profile.has("toolModels")).isFalse();
        JsonNode tool = profile.path("tools").get(0);
        assertThat(tool.path("name").asText()).isEqualTo("kling_text_to_video");
        assertThat(tool.path("capability").asText()).isEqualTo("agentk.kling_text_to_video");
        assertThat(tool.has("ref")).isFalse();
    }

    @Test
    void shouldPollPipelineRunUntilCompleted() {
        gateway.on("POST", "/sessions/ses_demo/pipeline-runs", query ->
                "{\"id\":\"pex_1\",\"status\":\"running\",\"pipeline\":\"generate_brand_ad\"}");
        final int[] polls = {0};
        gateway.on("GET", "/sessions/ses_demo/pipeline-runs/pex_1", query -> {
            polls[0]++;
            if (polls[0] == 1) {
                return "{\"id\":\"pex_1\",\"status\":\"running\",\"pipeline\":\"generate_brand_ad\"}";
            }
            return "{\"id\":\"pex_1\",\"status\":\"completed\",\"pipeline\":\"generate_brand_ad\","
                    + "\"output\":{\"artifacts\":[{\"type\":\"video\",\"uri\":\"https://cdn/ad.mp4\"}]}}";
        });
        properties.setPipelinePollIntervalMs(1);
        properties.setRunTimeoutSeconds(5);

        AgentkClient.PipelineRunResult result = newClient().runPipeline("ses_demo", "generate_brand_ad",
                MAPPER.createObjectNode().put("title", "t"));

        assertThat(result.isSucceeded()).isTrue();
        assertThat(result.getExecId()).isEqualTo("pex_1");
        assertThat(result.getOutput().path("artifacts").get(0).path("uri").asText())
                .isEqualTo("https://cdn/ad.mp4");
    }

    @Test
    void shouldReturnFailedPipelineRunWithoutThrowing() {
        gateway.on("POST", "/sessions/ses_demo/pipeline-runs", query ->
                "{\"id\":\"pex_fail\",\"status\":\"failed\",\"pipeline\":\"generate_brand_ad\","
                        + "\"error\":\"schema mismatch\"}");
        properties.setPipelinePollIntervalMs(1);

        AgentkClient.PipelineRunResult result = newClient().runPipeline("ses_demo", "generate_brand_ad",
                MAPPER.createObjectNode());

        assertThat(result.isSucceeded()).isFalse();
        assertThat(result.getError()).contains("schema mismatch");
    }

    /** 记下每轮交出来的整页事件，替代过去对 trace.events 的断言。 */
    private static final class RecordingSink implements AgentkClient.EventSink {

        private final List<JsonNode> pages = new ArrayList<>();

        @Override
        public void accept(JsonNode events) {
            pages.add(events.deepCopy());
        }

        int pages() {
            return pages.size();
        }

        /** 按投递顺序展平后的 kind 列表，重复投递会重复出现。 */
        List<String> kinds() {
            List<String> kinds = new ArrayList<>();
            for (JsonNode page : pages) {
                for (JsonNode event : page) {
                    kinds.add(event.path("kind").asText());
                }
            }
            return kinds;
        }

        String text() {
            return pages.toString();
        }
    }

    /** 按 {@code METHOD path} 编排响应的出站桩，替代内嵌 HTTP server。 */
    private static final class StubGateway implements AgentkOutboundGateway {

        private final Map<String, Function<String, String>> handlers = new LinkedHashMap<>();
        private final Map<String, Integer> callCounts = new LinkedHashMap<>();
        private final Map<String, List<JsonNode>> bodies = new LinkedHashMap<>();
        private final Map<String, int[]> failures = new LinkedHashMap<>();
        private final Map<String, String> failureMessages = new LinkedHashMap<>();
        private Runnable beforeExchange;

        void on(String method, String path, Function<String, String> responder) {
            handlers.put(key(method, path), responder);
        }

        /** 前 {@code times} 次调用按给定状态码失败，之后恢复正常。 */
        void fail(String method, String path, int status, int times) {
            fail(method, path, status, times, "stub failure " + status);
        }

        void fail(String method, String path, int status, int times, String message) {
            failures.put(key(method, path), new int[] {status, times});
            failureMessages.put(key(method, path), message);
        }

        int calls(String method, String path) {
            return callCounts.getOrDefault(key(method, path), 0);
        }

        JsonNode lastBody(String method, String path) {
            List<JsonNode> recorded = bodies.getOrDefault(key(method, path), new ArrayList<>());
            return recorded.isEmpty() ? MAPPER.createObjectNode() : recorded.get(recorded.size() - 1);
        }

        @Override
        public JsonNode exchange(String method, String path, String query, JsonNode body) {
            if (beforeExchange != null) {
                beforeExchange.run();
            }
            String key = key(method, path);
            callCounts.merge(key, 1, Integer::sum);
            bodies.computeIfAbsent(key, k -> new ArrayList<>()).add(body == null ? MAPPER.createObjectNode() : body);
            int[] failure = failures.get(key);
            if (failure != null && failure[1] > 0) {
                failure[1]--;
                throw new AgentkOutboundException(failure[0],
                        failureMessages.getOrDefault(key, "stub failure " + failure[0]));
            }
            Function<String, String> responder = handlers.get(key);
            if (responder == null) {
                throw new AgentkOutboundException(404, "no stub for " + key);
            }
            try {
                return MAPPER.readTree(responder.apply(query));
            } catch (Exception e) {
                throw new IllegalStateException("桩响应不是合法 JSON: " + key, e);
            }
        }

        private static String key(String method, String path) {
            return method + " " + path;
        }
    }
}
