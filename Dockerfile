FROM golang:1.11.1-alpine3.8 as build-env
# All these steps will be cached
RUN apk update && apk add git
RUN mkdir /discord_listener
WORKDIR /discord_listener
COPY go.mod .
COPY go.sum .

# Get dependancies - will also be cached if we won't change mod/sum
RUN go mod download
# COPY the source code as the last step
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -installsuffix cgo -o /go/bin/discord_listener

FROM scratch 
COPY --from=build-env /go/bin/discord_listener /go/bin/discord_listener
ENTRYPOINT ["/go/bin/discord_listener"]
