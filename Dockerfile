FROM texlive/texlive:latest

COPY sciarticle.in sciblurb.in scibook.in  scirules.in /mathgen/
COPY go/http /mathgen/httpd
WORKDIR /mathgen
CMD ["/mathgen/httpd"]

