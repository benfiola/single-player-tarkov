SPT_VERSION := 3.10.5

cwd = $(shell pwd)

.PHONY:
default:

.PHONY: clean
clean:
	# clean entrypoint data directory
	rm -rf $(cwd)/entrypoint/data
	# remove entrypoint symlink
	rm -rf $(cwd)/entrypoint/spt
	# clean spt directory
	rm -rf $(cwd)/spt
	# clean generated types
	rm -rf $(cwd)/mod/types

.PHONY: entrypoint-dev
entrypoint-dev: spt-build
	# create data folder
	mkdir -p $(cwd)/entrypoint/data
	# symlink spt build to entrypoint folder
	ln -sf $(cwd)/spt/build $(cwd)/entrypoint/spt

.PHONY: spt-clone
spt-clone:
	# clone spt server (if not exist)
	if [ ! -d $(cwd)/spt/source ]; then git clone https://github.com/sp-tarkov/server.git $(cwd)/spt/source; fi
	# checkout specific spt version
	cd $(cwd)/spt/source && git clean -dxf && git reset --hard && git checkout "$(SPT_VERSION)" && git apply $(cwd)/server-spt.patch && git lfs pull
	# install spt dependencies
	cd $(cwd)/spt/source/project && npm install

.PHONY: spt-build
spt-build: spt-clone
	# build spt server
	cd $(cwd)/spt/source/project && npm run build:release
	# move built files
	mv $(cwd)/spt/source/project/build/ $(cwd)/spt/build

.PHONY: mod-dev
mod-dev: spt-clone mod-gen-types mod-dependencies
	# ensure spt user directory exists
	mkdir -p $(cwd)/spt/source/project/user/mods
	# create new symlink
	ln -sf $(cwd)/mod $(cwd)/spt/source/project/user/mods/docker-image-helper-mod

.PHONY: mod-dependencies
mod-dependencies:
	# install mod dependencies
	cd $(cwd)/mod && npm install

.PHONY: mod-gen-types
mod-gen-types: spt-clone
	# generate spt types
	cd $(cwd)/spt/source/project && npm run gen:types
	# copy types to mod src directory
	mv $(cwd)/spt/source/project/types $(cwd)/mod/types
