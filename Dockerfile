FROM golang:1.16

WORKDIR /app
COPY . /app/

RUN go build main.go -o health_proxy

EXPOSE 8080

CMD [ "./health_proxy" ]