# Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /minisms ./cmd/minisms

FROM alpine:3.19
RUN apk --no-cache add ca-certificates tzdata
COPY --from=builder /minisms /usr/local/bin/minisms
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/minisms"]
