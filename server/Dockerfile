# Stage 1: Build Go server binary
FROM golang:1.26-alpine AS builder

WORKDIR /build

# Cache dependency downloads.
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build.
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /abwriter-server ./server/

# Stage 2: Minimal runtime image
FROM alpine:3.21

RUN apk add --no-cache ca-certificates

COPY --from=builder /abwriter-server /app/abwriter-server

COPY /server/config.example.yaml /app/conf/config.yaml

WORKDIR /app
CMD ["/app/abwriter-server","-f","/app/conf/config.yaml"]
