FROM golang:alpine as builder

RUN apk --no-cache add git build-base

# need to be outside of GOPATH for module support
WORKDIR /code

# have this separate for caching purposes
COPY go.mod .
COPY go.sum .
RUN go mod download

COPY . .
RUN mkdir /goback-bin

RUN go build -o /goback-bin/binary ./cmd/goback

FROM alpine

RUN apk --no-cache add ca-certificates tzdata
WORKDIR /root

COPY --from=builder /goback-bin/binary /root/

ENTRYPOINT ["/root/binary"]
