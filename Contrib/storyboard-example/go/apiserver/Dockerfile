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
RUN go build -trimpath -ldflags="-s -w" -o /out/storyboard-example-apiserver .

FROM registry.corp.kuaishou.com/ksbase/os/os/kcsize:1.0.0-alpine3.24.1-r1

ENV API_HOST=0.0.0.0

WORKDIR /home/web_server/storyboard-example-apiserver
COPY --from=build /out/storyboard-example-apiserver /usr/local/bin/storyboard-example-apiserver
COPY kcsize-start.sh /usr/local/bin/kcsize-start.sh
COPY profile.json ./profile.json
RUN chmod a+x /usr/local/bin/kcsize-start.sh

ENV SUPERVISOR_PROGRAM0=storyboard-example-apiserver \
    SUPERVISOR_COMMAND0=/usr/local/bin/kcsize-start.sh

CMD ["/usr/local/bin/kcsize"]
