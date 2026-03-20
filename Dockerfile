FROM golang:1.22-alpine AS build
WORKDIR /src
COPY . .
RUN go build -o /out/volclog ./cmd/volclog

FROM alpine:3.20
COPY --from=build /out/volclog /usr/local/bin/volclog
ENTRYPOINT ["volclog"]
