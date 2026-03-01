BINARY_NAME = otel_trace_hook
INSTALL_DIR = $(HOME)/.claude/hooks

.PHONY: build install clean test test-race test-cover

build:
	go build -o $(BINARY_NAME) .

install: build
	mkdir -p $(INSTALL_DIR)
	cp $(BINARY_NAME) $(INSTALL_DIR)/$(BINARY_NAME)
	chmod +x $(INSTALL_DIR)/$(BINARY_NAME)

clean:
	rm -f $(BINARY_NAME)

test:
	go test -v -count=1 ./...

test-race:
	go test -race -v -count=1 ./...

test-cover:
	go test -cover ./...
