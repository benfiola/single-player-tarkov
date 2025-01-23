FROM golang:1.23.4 AS entrypoint
WORKDIR /
ADD entrypoint.go entrypoint.go
ADD go.mod go.mod
ADD go.sum go.sum
ADD Makefile Makefile
ADD version.txt version.txt
RUN go build -o /entrypoint entrypoint.go

FROM node:20.11.1-bookworm AS server
WORKDIR /
ARG SPT_SERVER_VERSION
ADD Makefile Makefile
ADD server-spt.patch server-spt.patch
RUN apt -y update && \
    apt -y install git git-lfs make && \
    make spt-build

FROM ubuntu:noble AS final
WORKDIR /
COPY --from=entrypoint /entrypoint /entrypoint
COPY --from=server /spt /spt
RUN apt -y update && \
    apt -y install curl gosu p7zip-full unzip && \
    userdel ubuntu && \
    groupadd --gid=1000 server && \
    useradd --gid=server --system --uid=1000 --home /data server && \
    mkdir -p /data && \
    chown -R server:server /data /spt
EXPOSE 6969/tcp
VOLUME /data
ENTRYPOINT ["/entrypoint"]