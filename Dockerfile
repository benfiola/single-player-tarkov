FROM golang:1.23.4 AS entrypoint
WORKDIR /
ADD entrypoint src
RUN go build -o entrypoint src/main.go

FROM node:20.11.1-bookworm AS server
WORKDIR /
ARG SPT_SERVER_VERSION
ADD Makefile Makefile
ADD server-spt.patch server-spt.patch
RUN apt -y update && \
    apt -y install git git-lfs make && \
    make spt-build

FROM ubuntu:noble AS mod
WORKDIR /
ADD Makefile Makefile
ADD mod/config mod/config
ADD mod/src mod/src
ADD mod/package.json mod/package.json
RUN apt -y update && \
    apt -y install make && \
    make mod-vendored

FROM ubuntu:noble AS final
WORKDIR /
COPY --from=entrypoint /entrypoint /entrypoint
COPY --from=server /spt/build /spt
COPY --from=mod /mod /spt/user/mods/docker-image-helper-mod
RUN apt -y update && \
    apt -y install curl gosu p7zip-full unzip && \
    userdel ubuntu && \
    groupadd --gid=1000 spt && \
    useradd --gid=spt --system --uid=1000 --home /data spt && \
    mkdir -p /data && \
    chown -R spt:spt /data /spt
EXPOSE 6969/tcp
VOLUME /data
ENTRYPOINT ["/entrypoint"]