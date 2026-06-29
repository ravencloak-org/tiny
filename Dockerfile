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
ENV TR_PROJECT_DIR=/project
EXPOSE 8000
ENTRYPOINT ["tr"]
CMD ["serve"]
