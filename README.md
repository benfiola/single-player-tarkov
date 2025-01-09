# escape-from-tarkov

This repo hosts the supporting files and code to provide a [Single-player Tarkov](https://sp-tarkov.com/) server docker image for the game [Escape from Tarkov](https://www.escapefromtarkov.com/?lang=en).

## Environment Variables

Docker containers based off of this image rely upon the environment for configuration. Here are the current settings:

| Name     | Default | Description                                                         |
| -------- | ------- | ------------------------------------------------------------------- |
| GID      | 1000    | The GID to run the server under                                     |
| MOD_URLS |         | Comma-separated list of mod URLs to extract to the server directory |
| UID      | 1000    | The UID to run the server under                                     |

## Entrypoint

The core functionality of this container is controlled by the [entrypoint](./cmd/entrypoint/main.go) and is written in golang.

Prior to launching the server, the entrypoint is responsible for:

- Ensuring that `/server` and `/data` directories have proper ownership if needed
- Relaunching itself as a non-root user if needed
- Installing mods
- Performing an initial launch of the server to generate configuration
- Configuring the server with default values
- Symlinking persistent data into the server directory (e.g., `/data/user/profiles` -> `/server/user/profiles`)
- Launching the server in the foreground

## Persistence

This container uses the `/data` volume for persistent data. If you want to persist data across container runs, you'll want to bind mount a volume to the `/data` folder.

Currently, the only data that's persisted are profiles.

## Running as non-root user

The container is configured to run as a non-root user.

If the container is launched with a UID of 0 (i.e., root), it will change ownership of the `/server` and `/data` directories within the container to the UID and GID defined in the environment, and then relaunch itself under that UID/GID.
