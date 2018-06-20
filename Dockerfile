FROM golang:1.10-alpine as builder
ARG SRC_DIR=/go/src/github.com/charithe/sentiment
RUN apk --no-cache add --update make git
ADD . $SRC_DIR
WORKDIR $SRC_DIR
RUN go get -u github.com/golang/dep/... && dep ensure
RUN make container 

FROM gcr.io/distroless/base
VOLUME ["/credentials"]
ENV GOOGLE_APPLICATION_CREDENTIALS /credentials/service_account.json
COPY --from=builder /go/src/github.com/charithe/sentiment/serve /serve
ENTRYPOINT ["/serve"]
