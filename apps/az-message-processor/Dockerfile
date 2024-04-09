FROM golang:latest

WORKDIR /app

COPY app/ ./

RUN go mod download

RUN CGO_ENABLED=0 GOOS=linux go build -o /az-message-processor

CMD ["/az-message-processor"]
