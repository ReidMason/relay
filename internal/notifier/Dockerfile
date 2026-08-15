FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/notifier ./internal/notifier/cmd

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /out/notifier /usr/local/bin/notifier
EXPOSE 8080
ENTRYPOINT ["notifier"]
