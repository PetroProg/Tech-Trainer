# Stage 1: Build Frontend
FROM node:20-alpine AS frontend-builder
WORKDIR /app/frontend

COPY frontend/package.json ./
RUN npm install

COPY frontend/ ./
RUN npm run build

# Stage 2: Build Go Backend
FROM golang:1.23-alpine AS backend-builder
WORKDIR /app

# SQLite без CGO (modernc.org/sqlite)
ENV CGO_ENABLED=0 \
    GOOS=linux

COPY go.mod go.sum* ./
RUN go mod download || true

COPY cmd/ cmd/
COPY internal/ internal/

RUN go build -ldflags="-s -w" -o /app/tech-trainer ./cmd/server

# Stage 3: Minimal Runtime
FROM alpine:3.20
WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata \
    && mkdir -p /data /app/frontend/dist

COPY --from=frontend-builder /app/frontend/dist /app/frontend/dist
COPY --from=backend-builder /app/tech-trainer /app/tech-trainer

ENV PORT=8080 \
    DB_PATH=/data/trainer.db \
    OLLAMA_URL=http://100.114.251.81:11434 \
    OLLAMA_MODEL=qwen2.5:7b \
    CHECK_INTERVAL_HOURS=4 \
    MIN_CATEGORY_BUFFER=15 \
    BATCH_SIZE=50

EXPOSE 8080
VOLUME ["/data"]

CMD ["/app/tech-trainer"]
