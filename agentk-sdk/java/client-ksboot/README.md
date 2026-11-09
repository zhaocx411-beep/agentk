# AgentK Java client for KS Boot

`agentk-client-ksboot` is the Java 11 client for applications whose KS Boot root POM fixes
protobuf to 3.16.1 and gRPC to the 1.36.1 line. It uses KESS discovery and native grpc-java
over h2c; it does not use the AP HTTP/1.1 route.

```xml
<dependency>
  <groupId>com.kuaishou.klingai.agentk</groupId>
  <artifactId>agentk-client-ksboot</artifactId>
  <version>0.2.2</version>
</dependency>
```

The KS Boot jar contains its protobuf messages, gRPC stubs, and the same client-facing Java
packages as the modern artifacts. Do not put `agentk-client-ksboot` and any of
`agentk-protocol`, `agentk-client`, or `agentk-client-grpc` on the same classpath; their classes
have the same fully qualified names.

Create the client through `AgentKGrpcClient`. Closing the returned client cancels active calls
and releases its managed channels.

```java
AgentKClient client = AgentKGrpcClient.create(new ClientOptions());
```

The Maven lifecycle rewrites the canonical Edition 2023 sources into a fail-closed proto3 view
under `target/ksboot-proto`, then generates Java messages with protoc 3.16.1 and gRPC stubs with
the official grpc-java 1.36.1 plugin. Generated sources are build outputs and are not committed.

Requests are not colored with a lane: KESS node selection only uses baseline API nodes.
