# single-player-tarkov

This repo hosts the supporting files and code to provide a [Single-player Tarkov](https://sp-tarkov.com/) server docker image for the game [Escape from Tarkov](https://www.escapefromtarkov.com/?lang=en).

## Usage

Docker images are hosted on the [Docker hub](https://hub.docker.com/r/benfiola/single-player-tarkov). Currently, images are tagged with the following format: `<entrypoint version>-spt<spt version>`.

Use the latest docker image with: `docker.io/benfiola/single-player-tarkov:latest-spt3.10.5`.

## Environment Variables

Docker containers based off of this image rely upon the environment for configuration. Here are the current settings:

| Name           | Default | Description                                                          |
| -------------- | ------- | -------------------------------------------------------------------- |
| CONFIG_PATCHES |         | A JSON string containing a mapping of files to lists of JSON patches |
| GID            | 1000    | The GID to run the server under                                      |
| MOD_URLS       |         | Comma-separated list of mod URLs to extract to the server directory  |
| UID            | 1000    | The UID to run the server under                                      |

## Entrypoint

The core functionality of this container is controlled by the [entrypoint](./cmd/entrypoint/main.go) and is written in golang.

Prior to launching the server, the entrypoint is responsible for:

- Ensuring that `/server` and `/data` directories have proper ownership if needed
- Relaunching itself as a non-root user if needed
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
> The file path _must_ be relative to the SPT folder root. Absolute paths will be ignored!

## Persistence

This container uses the `/data` volume for persistent data. If you want to persist data across container runs, you'll want to bind mount a volume to the `/data` folder.

Currently, the only data that's persisted are profiles.

## Running as non-root user

The container is configured to run as a non-root user.

If the container is launched with a UID of 0 (i.e., root), it will change ownership of the `/server` and `/data` directories within the container to the UID and GID defined in the environment, and then relaunch itself under that UID/GID.
