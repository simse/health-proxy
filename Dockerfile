FROM golang:1.16

WORKDIR /app
COPY . /app/

RUN go build -o health_proxy main.go

EXPOSE 8080

CMD [ "./health_proxy" ]