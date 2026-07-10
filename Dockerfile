FROM docker.m.daocloud.io/library/node:22-alpine AS dashboard_remote_builder

WORKDIR /app
RUN apk add --no-cache bash protobuf protobuf-dev ca-certificates
COPY . /src
RUN set -eux; \
    wa_src=/src; \
    if [ -d /src/wa-app ]; then wa_src=/src/wa-app; fi; \
    mkdir -p /app/wa-app; \
    cp -a "$wa_src/proto" /app/wa-app/proto; \
    cp -a "$wa_src/scripts" /app/wa-app/scripts; \
    cp -a "$wa_src/webui" /app/wa-app/webui; \
    rm -rf /src
WORKDIR /app/wa-app/webui
RUN sed -i 's/\r$//' /app/wa-app/scripts/generate-web-proto.sh \
    && npm ci --prefer-offline --no-audit --fund=false \
    && npm run build

FROM docker.m.daocloud.io/library/golang:1.26-alpine AS builder

WORKDIR /app/wa-app
ENV GOPROXY=https://proxy.golang.org,direct
RUN apk add --no-cache git ca-certificates protobuf-dev

COPY . /src
RUN set -eux; \
    wa_src=/src; \
    if [ -d /src/wa-app ]; then wa_src=/src/wa-app; fi; \
    cp -a "$wa_src/." /app/wa-app/; \
    rm -rf /src; \
    retry() { attempts=0; until "$@"; do attempts=$((attempts + 1)); [ "$attempts" -lt 5 ] || return 1; sleep "$attempts"; done; }; \
    retry go mod download
RUN set -eux; \
    retry() { attempts=0; until "$@"; do attempts=$((attempts + 1)); [ "$attempts" -lt 5 ] || return 1; sleep "$attempts"; done; }; \
    sed -i 's/\r$//' scripts/generate-proto.sh \
    && retry go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11 \
    && retry go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.2 \
    && scripts/generate-proto.sh \
    && CGO_ENABLED=0 GOOS=linux go build -o wa-app-service ./cmd/wa-app-service

FROM scratch

WORKDIR /app
COPY --from=builder /app/wa-app/wa-app-service .
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=dashboard_remote_builder /app/wa-app/webui/dist /app/dashboard/wa
EXPOSE 50091 8080
CMD ["./wa-app-service"]
