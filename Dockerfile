# Multi-stage build for the `tr` binary.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags "-s -w" -o /out/tr ./cmd/tr

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /out/tr /usr/local/bin/tr
EXPOSE 8000
ENTRYPOINT ["tr"]
CMD ["serve"]
