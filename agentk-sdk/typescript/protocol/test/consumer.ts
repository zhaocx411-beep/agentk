import { create } from "@bufbuild/protobuf";
import { AgentKService, EventSchema, ExecutionSchema, SessionSchema, type Event, type Execution, type Session } from "@ks-agentk/protocol/api/v1";
import { ArtifactSchema, type Artifact } from "@ks-agentk/protocol/common/v1";
import { InvocationKind } from "@ks-agentk/protocol/invocation/v1";

const session: Session = create(SessionSchema, { id: "session-1" });
const artifact: Artifact = create(ArtifactSchema, { params: { count: 1 } });
const invocation: InvocationKind = InvocationKind.TOOL;
const event: Event = create(EventSchema, {
  payload: { case: "toolExecSucceeded", value: { artifacts: [artifact] } },
});
const execution: Execution = create(ExecutionSchema, {
  sessionId: session.id, kind: { case: "pipeline", value: { name: "render" } },
});

export function eventText(value: Event): string {
  if (value.payload.case === "assistantText") return value.payload.value.text;
  if (value.payload.case === "toolExecSucceeded") {
    const artifacts: Artifact[] = value.payload.value.artifacts;
    return artifacts.map(item => item.uri).join(",");
  }
  return "";
}

export function pipelineName(value: Execution): string {
  return value.kind.case === "pipeline" ? value.kind.value.name : "";
}

type Assert<T extends true> = T;
type Equal<A, B> = [A, B] extends [B, A] ? true : false;
export type TextBranch = Assert<Equal<Extract<Event["payload"], { case: "assistantText" }>["value"]["text"], string>>;
export type StreamingMethod = Assert<Equal<typeof AgentKService.method.watchEvents.methodKind, "server_streaming">>;
export { session, artifact, invocation, event, execution };

import { SessionSchema as ShortSessionSchema, type Session as ShortSession } from "@ks-agentk/protocol/api/v1/model_pb.js";
import { SessionSchema as LongSessionSchema, type Session as LongSession } from "@ks-agentk/protocol/agentk/api/v1/model_pb.js";
export type SameShortSession = Assert<Equal<Session, ShortSession>>;
export type SameLongSession = Assert<Equal<Session, LongSession>>;
export const shortSession: Session = create(ShortSessionSchema);
export const longSession: Session = create(LongSessionSchema);
