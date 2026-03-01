FROM golang:latest AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /out/s3gateway .

FROM scratch

COPY --from=builder /out/s3gateway /s3gateway

EXPOSE 8080

ENTRYPOINT ["/s3gateway"]
