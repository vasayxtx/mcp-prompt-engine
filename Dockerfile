FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o mcp-prompt-engine .

FROM alpine:latest
RUN apk --no-cache add ca-certificates
RUN addgroup -g 1001 -S mcpuser && \
    adduser -S -D -H -u 1001 -h /app -s /sbin/nologin -G mcpuser mcpuser
WORKDIR /app
COPY --from=builder /app/mcp-prompt-engine .
RUN mkdir -p /app/prompts /app/logs && chown -R mcpuser:mcpuser /app
USER mcpuser
VOLUME ["/app/prompts", "/app/logs"]
ENV MCP_PROMPTS_DIR=/app/prompts
CMD ["./mcp-prompt-engine", "serve", "--quiet", "--log-file", "/app/logs/mcp-prompt-engine.log"]
