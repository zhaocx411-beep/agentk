FROM registry.corp.kuaishou.com/ksbase/build/golang/golang:1.26.7-alpine AS build

ENV GOPROXY=https://goproxy.corp.kuaishou.com \
    GONOPROXY=none \
    GOSUMDB=off \
    CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -trimpath -ldflags="-s -w" -o /out/storyboard-example-worker .

FROM registry.corp.kuaishou.com/ksbase/os/os/kcsize:1.0.0-alpine3.24.1-r1

WORKDIR /home/web_server/storyboard-example-worker
COPY --from=build /out/storyboard-example-worker /usr/local/bin/storyboard-example-worker

ENV SUPERVISOR_PROGRAM0=storyboard-example-worker \
    SUPERVISOR_COMMAND0=/usr/local/bin/storyboard-example-worker

CMD ["/usr/local/bin/kcsize"]
