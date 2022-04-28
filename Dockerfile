FROM golang:1.17

ENV GO111MODULE=on

ENV PKG_NAME=go-whatsapp-web-multidevice
ENV PKG_PATH=$GOPATH/src/$PKG_NAME

RUN apt update -y && apt upgrade -y
RUN apt install git -y
RUN apt install libvips-dev -y

WORKDIR $PKG_PATH/
COPY . $PKG_PATH/

RUN go mod vendor
WORKDIR $PKG_PATH

RUN go build main.go
EXPOSE 3000
CMD ["sh", "-c", "./main"]
