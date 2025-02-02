BIN	= unpage

all:	$(BIN)

$(BIN): *.go
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid=" -buildmode=pie

.PHONY: test
test:
	@go vet
	@staticcheck
	@go test -v

.PHONY: clean
clean:
	@go clean

.PHONY: gen
gen:
	@rm -f go.mod go.sum
	@go mod init $(BIN)
	@go mod tidy
