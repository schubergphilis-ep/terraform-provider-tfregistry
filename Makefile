default: build

build:
	go build -o terraform-provider-tfregistry

install: build
	mkdir -p ~/.terraform.d/plugins/registry.terraform.io/schubergphilis-ep/tfregistry/0.0.1/$$(go env GOOS)_$$(go env GOARCH)
	cp terraform-provider-tfregistry ~/.terraform.d/plugins/registry.terraform.io/schubergphilis-ep/tfregistry/0.0.1/$$(go env GOOS)_$$(go env GOARCH)/

test:
	go test ./... -timeout 30s -parallel 4

testacc:
	TF_ACC=1 go test ./... -v -timeout 15m

fmt:
	go fmt ./...

vet:
	go vet ./...

.PHONY: default build install test testacc fmt vet
