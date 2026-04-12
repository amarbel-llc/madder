set dotenv-load

default: build

build:
  nix build --show-trace

test:
  cd go && go test -v ./...

fmt:
  cd go && goimports -w .
  cd go && gofumpt -w .

tidy:
  cd go && go mod tidy

gomod2nix:
  cd go && gomod2nix
