FROM golang:1.17

WORKDIR /app
COPY . /app/

RUN go build -o health_proxy main.go

CMD [ "./health_proxy" ]