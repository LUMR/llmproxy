FROM golang:1.25-alpine AS builder
ENV GOPROXY=https://goproxy.cn,direct
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o proxy .

FROM alpine:3.21
RUN apk add --no-cache tzdata
WORKDIR /app
COPY --from=builder /build/proxy .
EXPOSE 8080
ENTRYPOINT ["./proxy"]
