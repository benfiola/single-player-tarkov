SPT_VERSION := 3.10.5

cwd = $(shell pwd)

.PHONY:
default:

.PHONY: clean
clean:
	# clean tmp directory
	rm -rf $(cwd)/.tmp
	# clean data directory
	rm -rf $(cwd)/data
	# clean spt directory
	rm -rf $(cwd)/spt

.PHONY: entrypoint-dev
entrypoint-dev: spt-build
	# create data folder
	mkdir -p $(cwd)/data

.PHONY: spt-clone
spt-clone: | tmp-dir
	# clone spt server (if not exist)
	if [ ! -d $(cwd)/.tmp/spt-source ]; then git clone https://github.com/sp-tarkov/server.git $(cwd)/.tmp/spt-source; fi
	# checkout specific spt version
	cd $(cwd)/.tmp/spt-source && git clean -dxf && git reset --hard && git checkout "$(SPT_VERSION)" && git apply $(cwd)/server-spt.patch && git lfs pull
	# install spt dependencies
	cd $(cwd)/.tmp/spt-source/project && npm install

.PHONY: spt-build
spt-build: spt-clone
	# build spt server
	cd $(cwd)/.tmp/spt-source/project && npm run build:release
	# move built files
	mv $(cwd)/.tmp/spt-source/project/build/ $(cwd)/spt

.PHONY: tmp-dir
tmp-dir:
	# make tmp dir
	mkdir -p $(cwd)/.tmp

