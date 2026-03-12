BINARY    := lazyproc
CMD       := ./cmd/lazyproc
BUILD_DIR := ./bin

LDFLAGS := -s -w

.DEFAULT_GOAL := build

.PHONY: build install run clean

build:
	@mkdir -p $(BUILD_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) $(CMD)

install:
	go install -ldflags "$(LDFLAGS)" $(CMD)

run: build
	$(BUILD_DIR)/$(BINARY)

clean:
	@rm -rf $(BUILD_DIR)
