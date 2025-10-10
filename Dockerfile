FROM golang:1.25 AS build

WORKDIR /work

COPY go.mod .
COPY go.sum .

RUN go mod download

COPY . .

RUN go build

FROM golang:1.25

WORKDIR /app

COPY --from=build /work/mcstatus-exporter /app/mcstatus-exporter

EXPOSE 8080
CMD ["/app/mcstatus-exporter"]
