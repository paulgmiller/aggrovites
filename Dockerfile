FROM golang:1.23 as build

WORKDIR /go/src/app
COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY *.go ./

RUN go vet -v
RUN go test -v

RUN go build -o /go/bin/app

FROM gcr.io/distroless/base

COPY --from=build /go/bin/app /
COPY templates/*.tmpl /templates/
COPY assets/ /assets/
CMD ["/app"]
