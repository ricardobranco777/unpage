FROM	golang as builder

WORKDIR	/go/src/unpage
COPY	. .

RUN	make

FROM	scratch
COPY	--from=builder /go/src/unpage/unpage /usr/local/bin/unpage

ENTRYPOINT ["/usr/local/bin/unpage"]
