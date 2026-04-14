FROM golang:1.22-alpine AS build
WORKDIR /src
RUN apk add --no-cache ca-certificates git
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/webhook-service ./cmd/webhook-service

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /
COPY --from=build /out/webhook-service /webhook-service
USER 65534:65534
EXPOSE 8080
ENTRYPOINT ["/webhook-service"]
CMD ["-config", "/config/config.yaml"]
