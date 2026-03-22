FROM golang:1.24-alpine AS build
WORKDIR /app

COPY go.mod ./
RUN go mod download

COPY . .
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -o /sharetab ./cmd/server

FROM alpine:3.20
RUN addgroup -S sharetab && adduser -S sharetab -G sharetab
USER sharetab
WORKDIR /app
COPY --from=build /sharetab /app/sharetab
EXPOSE 3001
ENTRYPOINT ["/app/sharetab"]
