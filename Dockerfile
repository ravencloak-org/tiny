# Multi-stage build for the `tr` binary.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=${VERSION}" -o /out/tr ./cmd/tr

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /out/tr /usr/local/bin/tr
# Ship a starter project so a fresh deploy has a working datasource + pipe.
# Replace /project (bind mount or your own COPY) with your real .datasource/.pipe.
COPY examples/quickstart /project
# Bake the dashboards demo so its pipes survive container redeploys (the live
# /use-cases links depend on them). Table + seed data live in the ClickHouse
# volume; the read-only demo token lives in Redis AOF.
COPY examples/dashboards-demo/web_events.datasource \
     examples/dashboards-demo/top_pages.pipe \
     examples/dashboards-demo/views_over_time.pipe \
     /project/
ENV TR_PROJECT_DIR=/project
EXPOSE 8000
ENTRYPOINT ["tr"]
CMD ["serve"]
