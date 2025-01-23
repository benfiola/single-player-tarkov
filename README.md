# single-player-tarkov

This repo hosts the supporting files and code to provide a [Single-player Tarkov](https://sp-tarkov.com/) server docker image for the game [Escape from Tarkov](https://www.escapefromtarkov.com/?lang=en).

## Usage

Docker images are hosted on the [Docker hub](https://hub.docker.com/r/benfiola/single-player-tarkov). Currently, images are tagged with the entrypoint version.

Use the latest docker image with: `docker.io/benfiola/single-player-tarkov:latest`.

## Environment Variables

Docker containers based off of this image rely upon the environment for configuration. Here are the current settings:

| Name           | Default | Description                                                          |
| -------------- | ------- | -------------------------------------------------------------------- |
| CONFIG_PATCHES |         | A JSON string containing a mapping of files to lists of JSON patches |
| DATA_DIRS      |         | Comma-separated list of additional directories to persist            |
| GID            | 1000    | The GID to run the server under                                      |
| MOD_URLS       |         | Comma-separated list of mod URLs to extract to the server directory  |
| SPT_VERSION    |         | The SPT version that's built on startup and used                     |
| UID            | 1000    | The UID to run the server under                                      |

## Building SPT + Caching

On startup, the docker image will attempt to build the SPT server version defined by the `SPT_VERSION` environmnent variable.

To prevent unnecessary rebuilds, mount a local path to `/cache` to cache SPT server for future container restarts.

## Entrypoint

The core functionality of this container is controlled by the [entrypoint.go](./entrypoint.go) file and is written in golang.

Prior to launching the server, the entrypoint is responsible for:

- Ensuring that directories have proper ownership if needed
- Relaunching itself as a non-root user if needed
- Building and installing the SPT server
- Installing mods
- Performing an initial launch of the server to generate configuration
- Applying configuration patches to default configuration
- Symlinking persistent data into the server directory (e.g., `/data/user/profiles` -> `/server/user/profiles`)
- Launching the server in the foreground

## Configuration

Because SPT and its mods are configured via a large, non-standard, collection of JSON files, there is no straightforward way to systematically handle configuration per-key via the environment.

Instead, configuration modifications are expressed as mapping of _file_ to list of [JSON Patches](https://jsonpatch.com/). Set `CONFIG_PATCHES` to the JSON string representation of this mapping - and your configuration changes will be applied to the default configuration within the docker container.

As an example - to configure SPT to use a different server port, you might use the following `CONFIG_PATCHES` payload:

```json
{
    "SPT_Data/Server/configs/http.json": {
        {"op": "replace", "path": "/port", "value": 12345},
    }
}
```

> [!IMPORTANT]
> The file path _must_ be relative to the SPT folder root. Absolute paths will fail!

## Persistence

This container uses the `/data` volume for persistent data. If you want to persist data across container runs, you'll want to bind mount a volume to the `/data` folder.

Via the `DATA_DIRS` environment variable - and in addition to the `user/profiles` directory - you can specify additional sub-paths of the SPT folder that should be persisted in the `/data` directory. This is particularly useful for mods that write data to mod directory subfolders.

> [!IMPORTANT]
> The file path _must_ be relative to the SPT folder root. Absolute paths will fail!

## Running as non-root user

The container is configured to run as a non-root user.

If the container is launched with a UID of 0 (i.e., root), it will change ownership of the `/server` and `/data` directories within the container to the UID and GID defined in the environment, and then relaunch itself under that UID/GID.
