BIN	= unpage

GO	:= go

# https://github.com/golang/go/issues/64875
arch := $(shell uname -m)
ifeq ($(arch),s390x)
CGO_ENABLED = 0
else
CGO_ENABLED := 1
endif

$(BIN): *.go
	CGO_ENABLED=$(CGO_ENABLED) $(GO) build -trimpath -ldflags="-s -w -buildid=" -buildmode=pie

all:	$(BIN)

.PHONY: test
test:
	staticcheck
	$(GO) vet
	$(GO) test -v

.PHONY: clean
clean:
	$(GO) clean

.PHONY: gen
gen:
	$(RM) go.mod go.sum
	$(GO) mod init $(BIN)
	$(GO) mod tidy
