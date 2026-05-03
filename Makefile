BINARY   := setl
VERSION  := 1.0.0
LDFLAGS  := -ldflags "-X main.version=$(VERSION)"

.PHONY: build run tidy clean

build:
	go build $(LDFLAGS) -o $(BINARY) .

run: build
	./$(BINARY) examples/config.yaml

tidy:
	go mod tidy

clean:
	rm -f $(BINARY) setl.log .setl_watermarks.json
