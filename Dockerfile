# Conformance gateway: static Go binary on a distroless, non-root base.
FROM golang:1.26 AS build
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X github.com/jjsanda/genai-otel-ingest-conformance/internal/cli.Version=${VERSION}" \
      -o /out/genai-conformance ./cmd/genai-conformance

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/genai-conformance /usr/local/bin/genai-conformance
# 4317 OTLP gRPC · 4318 OTLP HTTP · 8080 report UI/API/metrics/health
EXPOSE 4317 4318 8080
ENTRYPOINT ["/usr/local/bin/genai-conformance"]
CMD ["serve"]
