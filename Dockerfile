FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /app/server .

FROM gcr.io/distroless/static:nonroot

COPY --from=builder /app/server /server

EXPOSE 8080

ENTRYPOINT ["/server"]
