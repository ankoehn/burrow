# test-only — never deploy this shape.
# test/integration/full/mockoai/Dockerfile
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/mockoai .

FROM alpine:3.20
RUN adduser -D -u 65532 mockoai
USER 65532
COPY --from=build /out/mockoai /usr/local/bin/mockoai
EXPOSE 8081
ENTRYPOINT ["/usr/local/bin/mockoai", "-addr=:8081"]
