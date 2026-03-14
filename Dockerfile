FROM golang:1.22-alpine AS build
WORKDIR /src
COPY . .
RUN go build -o /out/tlsctl ./cmd/tlsctl

FROM alpine:3.20
COPY --from=build /out/tlsctl /usr/local/bin/tlsctl
ENTRYPOINT ["tlsctl"]

