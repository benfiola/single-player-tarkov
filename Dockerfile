FROM golang:1.23.4 AS entrypoint
WORKDIR /source
ADD cmd cmd
ADD go.mod go.mod
RUN go build -o /entrypoint cmd/entrypoint/main.go
WORKDIR /
RUN rm -rf /source

FROM ubuntu:noble AS server
ARG SPT_SERVER_VERSION
ENV NODE_VERSION="20.11.1"
ENV ASDF_HOME="/asdf"
ENV ASDF_DATA_DIR="${ASDF_HOME}"
ENV ASDF_VERSION="0.15.0"
ENV PATH="${ASDF_DATA_DIR}/installs/nodejs/20.11.1/bin:${ASDF_HOME}/bin:${PATH}"
WORKDIR /
ADD server-build.sh server-build.sh
ADD server-spt.patch server-spt.patch
RUN apt -y update && \
    apt -y install bash curl git git-lfs unzip && \
    git clone https://github.com/asdf-vm/asdf.git "${ASDF_HOME}" --branch "v${ASDF_VERSION}" && \
    asdf plugin add nodejs && \
    asdf install nodejs "${NODE_VERSION}" && \
    /server-build.sh && \
    rm -rf "${ASDF_HOME}" "${ASDF_DATA_DIR}" /build-server.sh


FROM ubuntu:noble AS final
ARG TARGETARCH
WORKDIR /
COPY --from=entrypoint /entrypoint /entrypoint
COPY --from=server /server /server
RUN apt -y update && \
    apt -y install curl gosu p7zip-full unzip && \
    userdel ubuntu && \
    groupadd --gid=1000 eft && \
    useradd --gid=eft --system --uid=1000 --home /data eft && \
    mkdir -p /data && \
    chown -R eft:eft /data /server
EXPOSE 6969/tcp
EXPOSE 25565/udp
VOLUME /data
ENTRYPOINT ["/entrypoint"]