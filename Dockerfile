FROM golang:latest AS builder
COPY http.go go.mod go.sum /build/
COPY mathgen/ /build/mathgen/
COPY sciarticle.in sciblurb.in scibook.in scirules.in /build
WORKDIR /build
RUN go get -v github.com/joho/godotenv
RUN ls /build
RUN go build -a -o http http.go

FROM texlive/texlive:latest AS deployer
WORKDIR /app
COPY --from=builder /build/http .
EXPOSE 8080/tcp
CMD ["./http"]

