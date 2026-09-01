FROM golang:1.27 AS gob

WORKDIR /build

COPY . /build/

RUN go mod download
RUN CGO_ENABLED=0 go build -ldflags="-X main.version=${VERSION}" -a -o prog .

FROM scratch

COPY --from=gob /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=gob /build/prog /prog

ENTRYPOINT [ "/prog" ]
