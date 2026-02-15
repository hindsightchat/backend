FROM alpine:3.19

WORKDIR /app

# Set environment variables
ENV CGO_ENABLED=1
ENV GO_VERSION=1.24.0
ENV GOOS=linux
ENV GOARCH=amd64
ENV IS_PROD=true

# gcc & g++ are required for cgo
RUN apk add --no-cache  \
    build-base \
    gcc \
    g++ \
    make \
    git \
    wget \
    curl \
    && rm -rf /var/cache/apk/* 

# install go lang
RUN wget https://dl.google.com/go/go${GO_VERSION}.linux-amd64.tar.gz && \
    tar -C /usr/local -xzf go${GO_VERSION}.linux-amd64.tar.gz && \
    rm go${GO_VERSION}.linux-amd64.tar.gz

ENV PATH="/usr/local/go/bin:${PATH}"

COPY go.mod go.sum ./

RUN go mod download

# force cache busting for go build
ARG CACHEBUST=1 
RUN echo "Cache bust: ${CACHEBUST}"

COPY . .


RUN go build -o hindsightchat-backend

EXPOSE 3000

CMD ["./hindsightchat-backend"]