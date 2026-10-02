FROM golang:alpine as builder

RUN apk --no-cache add git

# need to be outside of GOPATH for module support
WORKDIR /code

# have this separate for caching purposes
COPY go.mod .
COPY go.sum .
RUN go mod download

COPY . .
RUN mkdir /goback-bin

# static, so the binary also runs when copied into other images
RUN CGO_ENABLED=0 go build -trimpath -o /goback-bin/binary ./cmd/goback

FROM alpine as runner

RUN apk --no-cache add ca-certificates tzdata
WORKDIR /root

COPY --from=builder /goback-bin/binary /root/

ENTRYPOINT ["/root/binary"]
