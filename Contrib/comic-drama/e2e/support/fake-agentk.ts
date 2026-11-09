import { createServer, type IncomingMessage, type ServerResponse } from "node:http";

type LedgerEvent = {
  seq: number;
  eventId: string;
  executionId: string;
  roundId?: string;
  kind: string;
  data: Record<string, unknown>;
  createdAt: string;
};

type Session = {
  id: string;
  title: string;
  createdAt: string;
  events: LedgerEvent[];
  nextRound: number;
  executionId: string;
};

const host = "127.0.0.1";
const port = Number(process.env.FAKE_AGENTK_PORT || "18081");
const baseURL = `http://${host}:${port}`;
const sessions = new Map<string, Session>();
let nextSession = 1;
let nextExecution = 1;

const rpc = {
  createSession: "/agentk.api.v1.AgentKService/CreateSession",
  listSessions: "/agentk.api.v1.AgentKService/ListSessions",
  startExecution: "/agentk.api.v1.AgentKService/StartExecution",
  listEvents: "/agentk.api.v1.AgentKService/ListEvents",
};

function sendJSON(response: ServerResponse, status: number, value: unknown) {
  response.writeHead(status, { "Content-Type": "application/json; charset=utf-8" });
  response.end(JSON.stringify(value));
}

function sendConnectError(
  response: ServerResponse,
  status: number,
  code: string,
  message: string,
) {
  sendJSON(response, status, { code, message });
}

async function readJSON(request: IncomingMessage): Promise<Record<string, unknown>> {
  const chunks: Buffer[] = [];
  for await (const chunk of request) chunks.push(Buffer.from(chunk));
  const raw = Buffer.concat(chunks).toString("utf8");
  const parsed = JSON.parse(raw || "{}");
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new Error("body must be a JSON object");
  }
  return parsed as Record<string, unknown>;
}

function append(
  session: Session,
  kind: string,
  data: Record<string, unknown>,
  roundId?: string,
) {
  session.events.push({
    seq: session.events.length + 1,
    eventId: `evt_${String(session.events.length + 1).padStart(8, "0")}`,
    executionId: session.executionId,
    roundId,
    kind,
    data,
    createdAt: new Date().toISOString(),
  });
}

const payloadField: Record<string, string> = {
  "user.input": "userInput",
  "round.started": "roundStarted",
  "round.completed": "roundCompleted",
  "assistant.text": "assistantText",
  "assistant.reasoning": "assistantReasoning",
  "assistant.tool_call": "assistantToolCall",
  "tool.exec_started": "toolExecStarted",
  "tool.exec_succeeded": "toolExecSucceeded",
  "tool.exec_failed": "toolExecFailed",
  "ask_user.requested": "askUserRequested",
  "ask_user.responded": "askUserResponded",
  "execution.started": "executionStarted",
  "execution.finished": "executionFinished",
  "invocation.progress": "invocationProgress",
};

function protobufEvent(event: LedgerEvent): Record<string, unknown> {
  const field = payloadField[event.kind];
  if (!field) throw new Error(`unsupported fake event kind ${event.kind}`);
  const data = { ...event.data };
  if (event.kind === "execution.finished" && typeof data.status === "string") {
    data.status = `EXECUTION_STATUS_${data.status.toUpperCase()}`;
  }
  if (event.kind === "invocation.progress" && typeof data.invocationKind === "string") {
    data.invocationKind = `INVOCATION_KIND_${data.invocationKind.toUpperCase()}`;
  }
  return {
    eventId: event.eventId,
    executionId: event.executionId,
    ...(event.roundId ? { roundId: event.roundId } : {}),
    createdAt: event.createdAt,
    [field]: data,
  };
}

function later(delay: number, action: () => void) {
  const timer = setTimeout(action, delay);
  timer.unref();
}

function scheduleLocalResponse(session: Session, userText: string) {
  const round = `rnd_${String(session.nextRound++).padStart(4, "0")}`;
  const imageCall = `${round}_image`;
  const videoCall = `${round}_video`;
  const artifactCall = `${round}_artifacts`;
  const listsArtifacts = userText.includes("历史产物");

  later(100, () => append(session, "round.started", { model: "fake-local" }, round));
  later(160, () => append(session, "assistant.reasoning", {
    text: "正在使用本地 Fake AgentK 生成可重复的测试响应。",
  }, round));
  later(220, () => append(session, "assistant.text", {
    text: userText.includes("Markdown 表格")
      ? "| 工具 | 模型 | 内容 |\n|:---|:---:|---:|\n| generate_image | z-image-turbo | 小鸡吃米 |"
      : `已收到：${userText}`,
  }, round));
  later(280, () => append(session, "assistant.tool_call", {
    toolCallId: imageCall,
    toolName: "generate_image",
    args: { prompt: "local e2e poster" },
  }, round));
  later(340, () => append(session, "tool.exec_started", {
    toolCallId: imageCall,
  }, round));
  later(400, () => append(session, "tool.exec_succeeded", {
    toolCallId: imageCall,
    result: { ok: true },
    artifacts: [{
      type: "image",
      uri: `${baseURL}/assets/poster.svg`,
      description: "本地测试海报",
    }],
  }, round));
  later(460, () => append(session, "assistant.tool_call", {
    toolCallId: videoCall,
    toolName: "compose_video",
    // 引用已生成的海报 URL：驱动血缘画布画出 poster → film 的连线。
    args: { clips: [`${baseURL}/assets/poster.svg`] },
  }, round));
  later(520, () => append(session, "tool.exec_started", {
    toolCallId: videoCall,
  }, round));
  later(580, () => append(session, "tool.exec_succeeded", {
    toolCallId: videoCall,
    result: { videoUrl: `${baseURL}/assets/film.mp4` },
    artifacts: [{
      type: "video",
      uri: `${baseURL}/assets/film.mp4`,
      description: "本地测试成片",
    }],
  }, round));
  if (listsArtifacts) {
    later(640, () => append(session, "assistant.tool_call", {
      toolCallId: artifactCall,
      toolName: "list_artifacts",
      args: {},
    }, round));
    later(700, () => append(session, "tool.exec_started", {
      toolCallId: artifactCall,
    }, round));
    later(760, () => append(session, "tool.exec_succeeded", {
      toolCallId: artifactCall,
      result: {
        artifacts: [{
          type: "image",
          url: `${baseURL}/assets/poster.svg`,
          description: "历史测试海报",
        }],
      },
    }, round));
  }
  const completedAt = listsArtifacts ? 820 : 640;
  later(completedAt, () => append(session, "round.completed", {}, round));

  const finalRound = `${round}_final`;
  later(completedAt + 60, () =>
    append(session, "round.started", { model: "fake-local" }, finalRound));
  later(completedAt + 120, () => append(session, "assistant.text", {
    text: "本地测试任务已完成。",
  }, finalRound));
  later(completedAt + 180, () => append(session, "round.completed", {}, finalRound));
  later(completedAt + 240, () => append(session, "execution.finished", {
    status: "completed",
  }));
}

function scheduleAskResponse(session: Session) {
  const round = `rnd_${String(session.nextRound++).padStart(4, "0")}`;
  const callID = `${round}_ask`;
  const questions = [
    {
      id: "q1",
      label: "画风",
      question: "请选择海报画风",
      options: ["写实", "水墨"],
    },
    {
      id: "q2",
      question: "请选择需要的声音，可多选",
      options: ["环境音", "鼓点", "不要声音"],
      multiSelect: true,
    },
  ];
  later(100, () => append(session, "round.started", { model: "fake-local" }, round));
  later(160, () => append(session, "assistant.tool_call", {
    toolCallId: callID,
    toolName: "ask_user",
    args: { questions },
  }, round));
  later(220, () => append(session, "round.completed", {}, round));
  later(280, () => append(session, "ask_user.requested", {
    toolCallId: callID,
    questions,
  }, round));
  later(340, () => append(session, "execution.finished", { status: "completed" }));
}

function scheduleAnsweredResponse(session: Session) {
  const round = `rnd_${String(session.nextRound++).padStart(4, "0")}`;
  later(80, () => append(session, "round.started", { model: "fake-local" }, round));
  later(140, () => append(session, "assistant.text", {
    text: "已收到你的选择，继续执行。",
  }, round));
  later(200, () => append(session, "round.completed", {}, round));
  later(260, () => append(session, "execution.finished", { status: "completed" }));
}

const server = createServer(async (request, response) => {
  const url = new URL(request.url || "/", baseURL);

  if (request.method === "GET" && url.pathname === "/healthz") {
    response.writeHead(200, { "Content-Type": "text/plain; charset=utf-8" });
    response.end("ok");
    return;
  }

  if (request.method === "GET" && url.pathname === "/assets/poster.svg") {
    response.writeHead(200, { "Content-Type": "image/svg+xml" });
    response.end(
      '<svg xmlns="http://www.w3.org/2000/svg" width="320" height="180">' +
      '<rect width="100%" height="100%" fill="#536fb7"/>' +
      '<text x="50%" y="50%" fill="white" text-anchor="middle">E2E</text></svg>',
    );
    return;
  }

  if (request.method === "GET" && url.pathname === "/assets/film.mp4") {
    response.writeHead(204, { "Content-Type": "video/mp4" });
    response.end();
    return;
  }

  try {
    if (request.method === "PUT" && url.pathname.startsWith("/storage/e2e/")) {
      for await (const _ of request) {
        // Drain the uploaded object to exercise the real streaming code path.
      }
      response.writeHead(204);
      response.end();
      return;
    }

    if (request.method === "POST" && url.pathname === rpc.createSession) {
      const body = await readJSON(request);
      const id = `ses_${String(nextSession++).padStart(4, "0")}`;
      const title = typeof body.title === "string" ? body.title : "";
      const createdAt = new Date().toISOString();
      sessions.set(id, {
        id, title, createdAt, events: [], nextRound: 1, executionId: "",
      });
      sendJSON(response, 200, { session: { id, title, createdAt } });
      return;
    }

    if (request.method === "POST" && url.pathname === rpc.listSessions) {
      await readJSON(request);
      const newestFirst = [...sessions.values()].reverse().map((session) => ({
        id: session.id,
        title: session.title,
        createdAt: session.createdAt,
      }));
      sendJSON(response, 200, { sessions: newestFirst });
      return;
    }

    if (request.method === "POST" && url.pathname === rpc.startExecution) {
      const body = await readJSON(request);
      const sessionId = typeof body.sessionId === "string" ? body.sessionId : "";
      const session = sessions.get(sessionId);
      if (!session) {
        sendConnectError(response, 404, "not_found", "session not found");
        return;
      }
      session.executionId = `exe_${String(nextExecution++).padStart(4, "0")}`;
      const message = body.message as Record<string, unknown> | undefined;
      const answerInput = body.answers as Record<string, unknown> | undefined;
      if (message) {
        const text = typeof message.text === "string" ? message.text.trim() : "";
        if (!text) {
          sendConnectError(response, 400, "invalid_argument", "text is required");
          return;
        }
        append(session, "execution.started", { agent: {} });
        append(session, "user.input", { text });
        if (text.includes("需要选择")) scheduleAskResponse(session);
        else scheduleLocalResponse(session, text);
      } else if (answerInput) {
        const toolCallId = typeof answerInput.toolCallId === "string"
          ? answerInput.toolCallId
          : "";
        const answers = Array.isArray(answerInput.answers) ? answerInput.answers : [];
        if (!toolCallId || !answers.length) {
          sendConnectError(
            response, 400, "invalid_argument", "toolCallId and answers are required",
          );
          return;
        }
        append(session, "execution.started", { agent: {} });
        append(session, "ask_user.responded", { toolCallId, answers });
        scheduleAnsweredResponse(session);
      } else {
        sendConnectError(response, 400, "invalid_argument", "unsupported execution input");
        return;
      }
      sendJSON(response, 200, {
        execution: {
          id: session.executionId,
          sessionId,
          status: "EXECUTION_STATUS_RUNNING",
          agent: {},
          createdAt: new Date().toISOString(),
        },
      });
      return;
    }

    if (request.method === "POST" && url.pathname === rpc.listEvents) {
      const body = await readJSON(request);
      const sessionId = typeof body.sessionId === "string" ? body.sessionId : "";
      const session = sessions.get(sessionId);
      if (!session) {
        sendConnectError(response, 404, "not_found", "session not found");
        return;
      }
      const pageSize = typeof body.pageSize === "number" ? body.pageSize : 20;
      const offset = typeof body.pageToken === "string" && body.pageToken
        ? Number(body.pageToken)
        : 0;
      const newestFirst = [...session.events].reverse();
      const page = newestFirst.slice(offset, offset + pageSize);
      const nextOffset = offset + page.length;
      sendJSON(response, 200, {
        events: page.map(protobufEvent),
        ...(nextOffset < newestFirst.length
          ? { nextPageToken: String(nextOffset) }
          : {}),
      });
      return;
    }
  } catch (error) {
    sendConnectError(
      response,
      400,
      "invalid_argument",
      error instanceof Error ? error.message : "invalid request",
    );
    return;
  }

  sendConnectError(response, 404, "not_found", "not found");
});

server.listen(port, host, () => {
  console.log(`fake AgentK listening on ${baseURL}`);
});

function shutdown() {
  server.close(() => process.exit(0));
}

process.on("SIGINT", shutdown);
process.on("SIGTERM", shutdown);
