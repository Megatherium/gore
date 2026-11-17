# Build stage
FROM golang:1.27-alpine AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /build/gore ./cmd/gore

# Final stage - minimal distroless image
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /
COPY --from=builder /build/gore /gore

EXPOSE 8080
USER nonroot:nonroot

ENTRYPOINT ["/gore"]
