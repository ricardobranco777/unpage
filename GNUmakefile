BIN	= unpage

all:	$(BIN)

$(BIN): *.go
	@CGO_ENABLED=0 go build

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
