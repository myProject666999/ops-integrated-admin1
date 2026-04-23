FROM node:20-alpine AS frontend-builder

WORKDIR /app/frontend

COPY frontend/package*.json ./
RUN npm install

COPY frontend/ ./
RUN npm run build

FROM golang:1.26-alpine AS backend-builder

WORKDIR /app

RUN apk add --no-cache gcc musl-dev

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ ./

RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o /ops-admin-backend .

FROM alpine:3.19

WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata

COPY --from=backend-builder /ops-admin-backend .
COPY --from=backend-builder /app/data ./data
COPY --from=frontend-builder /app/frontend/dist ./static

RUN mkdir -p /app/db

ENV ADDR=0.0.0.0:8080
ENV TZ=Asia/Shanghai
ENV STATIC_DIR=./static

EXPOSE 8080

CMD ["./ops-admin-backend"]
