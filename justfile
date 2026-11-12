# List the recipes
[private]
default:
  @just --list --unsorted

# Run the tests
test *args:
  go test {{args}} ./...

# Lint the code
lint *args:
  go vet {{args}} ./...

# Format the code
format:
  gofmt -w .

# Build sshwarm into .output/build
build *args:
  go build -o .output/build/ {{args}} .
