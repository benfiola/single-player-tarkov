SPT_VERSION := 3.10.5

cwd = $(shell pwd)

.PHONY:
default:

.PHONY: clean
clean:
	# remove cache directory
	rm -rf $(cwd)/cache
	# remove data directory
	rm -rf $(cwd)/data
	# remove entrypoint
	rm -rf $(cwd)/entrypoint
	# remove spt directory
	rm -rf $(cwd)/spt


.PHONY: build-entrypoint
build-entrypoint:
	# build entrypoint
	go build -o entrypoint entrypoint.go
