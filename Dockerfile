# Stage 1: Build frontend
FROM node:22-alpine AS frontend
WORKDIR /app
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ .
RUN npm run build

# Stage 2: Build backend (embeds the frontend dist)
FROM golang:1.25-alpine AS backend
WORKDIR /app
# Copy dependency manifests first so module downloads are cached independently of source changes
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ .
COPY --from=frontend /app/dist ./dist
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o montly .

# Stage 3: Minimal runtime image
FROM alpine:3.21
RUN adduser -D -u 1000 montly && mkdir -p /data && chown montly:montly /data
WORKDIR /app
COPY --from=backend /app/montly .
USER montly
ENV PORT=8080
ENV DATA_DIR=/data
EXPOSE 8080
CMD ["/app/montly"]
