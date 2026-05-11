FROM golang:1.26-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS=linux
ARG TARGETARCH=amd64

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags='-s -w' -o /out/api .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags='-s -w' -o /out/build-index ./cmd/build-index

FROM alpine:3.21

WORKDIR /app

RUN adduser -D -H -u 10001 app
RUN mkdir -p /sockets /resources && chown -R app:app /sockets /resources

COPY --from=build /out/api /app/api
COPY --chown=app:app resources/index.bin /resources/index.bin

USER app
EXPOSE 9999

ENTRYPOINT ["/app/api"]
