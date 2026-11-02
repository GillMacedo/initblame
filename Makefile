.PHONY: test lint demo traces

RUN = go run ./plugin/scripts

test:
	test -z "$$(gofmt -l plugin tests examples)"
	go vet ./...
	go test -count=1 ./...

lint:
	claude plugin validate ./plugin --strict
	claude plugin validate . --strict

demo:
	$(RUN) --dir examples/slowstart --pkg ./cmd/slowstart examples/slowstart/trace.txt
	$(RUN) --dir examples/slowstart --pkg ./cmd/slowstart --base examples/slowstart/trace-lazy.txt examples/slowstart/trace.txt
	$(RUN) examples/quickstart/trace.txt

# Records new traces of the example programs on this machine. The numbers
# will differ from the saved ones, so update the README after running it.
traces:
	cd examples/slowstart && go build -o ../../.bin/slowstart ./cmd/slowstart && go build -tags lazy -o ../../.bin/slowstart-lazy ./cmd/slowstart
	cd examples/quickstart && go build -o ../../.bin/quickstart .
	$(RUN) run --save examples/slowstart/trace.txt -- .bin/slowstart --version > /dev/null
	$(RUN) run --save examples/slowstart/trace-lazy.txt -- .bin/slowstart-lazy --version > /dev/null
	$(RUN) run --save examples/quickstart/trace.txt -- .bin/quickstart --version > /dev/null
