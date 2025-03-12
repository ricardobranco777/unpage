BIN	= unpage

GO	:= go

# https://github.com/golang/go/issues/64875
arch := $(shell uname -m)
ifeq ($(arch),s390x)
CGO_ENABLED ?= 1
else
CGO_ENABLED := 0
endif

$(BIN): *.go
	CGO_ENABLED=$(CGO_ENABLED) $(GO) build -trimpath -ldflags="-s -w -buildid=" -buildmode=pie

all:	$(BIN)

.PHONY: test
test:
	$(GO) test -v
	staticcheck
	gofmt -s -l .
	$(GO) vet

.PHONY: lint
lint:
	golangci-lint run ./...

.PHONY: clean
clean:
	$(GO) clean

.PHONY: gen
gen:
	$(RM) go.mod go.sum
	$(GO) mod init $(BIN)
	$(GO) mod tidy
