FROM golang:1.11

WORKDIR /go/src/app
COPY . .

RUN go get -d -v ./...
RUN go install -v ./...

RUN /bin/bash -c "source ./.env"

CMD ["app"]
