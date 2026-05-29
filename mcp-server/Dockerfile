FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -o montly-mcp .

FROM alpine:3.21
RUN adduser -D -u 1000 montly
WORKDIR /app
COPY --from=build /app/montly-mcp .
USER montly
EXPOSE 8081
CMD ["/app/montly-mcp"]
