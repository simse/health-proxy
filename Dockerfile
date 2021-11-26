FROM golang:1.17

WORKDIR /app
COPY . /app/

RUN go build -o health_proxy main.go

EXPOSE 5000

CMD [ "./health_proxy" ]