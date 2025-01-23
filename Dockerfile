FROM golang:1.23.4 AS entrypoint
WORKDIR /
ADD entrypoint.go entrypoint.go
ADD go.mod go.mod
ADD go.sum go.sum
ADD Makefile Makefile
ADD version.txt version.txt
RUN make build-entrypoint

FROM ubuntu:noble AS final
ARG ASDF_VERSION="0.15.0"
ARG NODEJS_VERSION="20.11.1"
ENV ASDF_HOME="/asdf"
ENV ASDF_DATA_DIR="/asdf"
ENV PATH="/asdf/installs/nodejs/${NODEJS_VERSION}/bin:/asdf/bin:${PATH}"
WORKDIR /
RUN apt -y update && \
    apt -y install curl git git-lfs gosu p7zip-full unzip && \
    git clone https://github.com/asdf-vm/asdf.git "${ASDF_HOME}" --branch "v${ASDF_VERSION}" && \
    asdf plugin add nodejs && \
    asdf install nodejs "${NODEJS_VERSION}" && \
    userdel ubuntu && \
    groupadd --gid=1000 server && \
    useradd --gid=server --system --uid=1000 --create-home server && \
    mkdir -p /cache /data /spt && \
    chown -R server:server /cache /data /spt
COPY --from=entrypoint /entrypoint /usr/local/bin/entrypoint
ADD spt.patch spt.patch
EXPOSE 6969/tcp
VOLUME /cache
VOLUME /data
ENTRYPOINT ["entrypoint"]