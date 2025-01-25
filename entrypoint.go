package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/benfiola/game-server-helper/pkg/helper"
	"github.com/benfiola/game-server-helper/pkg/helperapi"
)

// Api wraps [helper.Api] and adds SPT specific methods to the struct
type Api struct {
	helper.Api
}

// Defines a callback that accepts a [context.Context] and an [Api]
type Callback func(ctx context.Context, api Api) error

// Converts a [Callback] into an [helper.Callback] for compatibility with [helper.Helper]
func RunCallback(cb Callback) helper.Callback {
	return func(ctx context.Context, parent helper.Api) error {
		api := Api{Api: parent}
		return cb(ctx, api)
	}
}

// Installs the given mod urls to the spt path.
// Raises an error if a url download fails.
// Raises an error if mod extraction fails.
func (api *Api) InstallMods(modUrls ...string) error {
	for _, modUrl := range modUrls {
		api.Logger.Info("install mod", "url", modUrl)
		err := api.Download(modUrl, func(modPath string) error {
			return api.Extract(modPath, api.Directories["spt"])
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// Initializes the server.
// Starts the server, waits for it to be connectable, and then shuts it down.
// This allows the server to generate first-launch files for subsequent modification.
// Raises an error if the server fails to start.
// Raises an error if the server is unconnectable after a set timeout.
func (api *Api) InitializeServer() error {
	api.Logger.Info("initialize server")
	cb := func(complete func()) error {
		response, err := http.Get("http://localhost:6969")
		if err != nil || response.StatusCode != 200 {
			return nil
		}
		api.Logger.Info("server initialized")
		complete()
		return nil
	}
	serverBin := filepath.Join(api.Directories["spt"], "SPT.Server.exe")
	return api.RunCommandUntil([]string{serverBin}, helperapi.CmdUntilOpts{
		CmdOpts:  helperapi.CmdOpts{Cwd: api.Directories["spt"]},
		Callback: cb,
	})
}

// Starts an spt server and blocks until exit.
// Raises an error if the server exits with a non-zero exit code.
func (api *Api) RunServer() error {
	api.Logger.Info("run server")
	pathServerBin := filepath.Join(api.Directories["spt"], "SPT.Server.exe")
	_, err := api.RunCommand([]string{pathServerBin}, helperapi.CmdOpts{Attach: true, Cwd: api.Directories["spt"]})
	return err
}

// ConfigPatches are a map of relative file path -> a list of json patches to apply
type ConfigPatches map[string][]helperapi.JsonPatch

// Parses a string into a [ConfigPatches] object.
// Used to parse settings from the environment.
func (cps *ConfigPatches) UnmarshalText(data []byte) error {
	parsed := map[string][]helperapi.JsonPatch{}
	err := json.Unmarshal(data, &parsed)
	*cps = ConfigPatches(parsed)
	return err
}

// Applies config patches to files located in the spt server path
func (api *Api) ApplyConfigPatches(configPatches ConfigPatches) error {
	for relPath, patches := range configPatches {
		api.Logger.Info("apply config patch", "count", len(patches), "path", relPath)
		path := filepath.Join(api.Directories["spt"], relPath)
		data := map[string]any{}
		err := api.UnmarshalFile(path, &data)
		if err != nil {
			return err
		}
		err = api.ApplyJsonPatches(data, patches...)
		if err != nil {
			return err
		}
		err = api.MarshalFile(data, path)
		if err != nil {
			return err
		}
	}

	return nil
}

// Merges several [ConfigPatches] objects into a single one.
func (api *Api) MergeConfigPatches(maps ...ConfigPatches) ConfigPatches {
	data := ConfigPatches{}
	for _, currMap := range maps {
		for k, v := range currMap {
			_, ok := data[k]
			if !ok {
				data[k] = []helperapi.JsonPatch{}
			}
			data[k] = append(data[k], v...)
		}
	}
	return data
}

// Merges lists of data directories into a single-deduplicated list
func (api *Api) MergeDataDirs(lists ...[]string) []string {
	final := []string{}
	exists := map[string]bool{}
	for _, list := range lists {
		for _, path := range list {
			_, ok := exists[path]
			if ok {
				continue
			}
			final = append(final, path)
			exists[path] = true
		}
	}
	return final
}

// Symlinks folders from a data subpath to a spt subpath to persist certain slices of information
func (api *Api) SymlinkDataDirs(dataDirs []string) error {
	for _, dataDir := range dataDirs {
		sptPath := filepath.Join(api.Directories["spt"], dataDir)
		dataPath := filepath.Join(api.Directories["data"], dataDir)
		err := api.SymlinkDir(dataPath, sptPath)
		if err != nil {
			return err
		}
	}
	return nil
}

// Installs spt to the spt directory if spt exists in the cache.  If spt does not exist in the cache, it is checked out, built and copied into the cache.
// Returns an error if any step in this process fails.
func (api *Api) InstallSpt(version string) error {
	cachePath := filepath.Join(api.Directories["cache"], version)
	_, err := os.Lstat(cachePath)
	exists := true
	if errors.Is(err, os.ErrNotExist) {
		exists = false
		err = nil
	}
	if err != nil {
		return err
	}
	if !exists {
		api.Logger.Info("build spt", "version", version)
		subpaths, err := api.ListDir(api.Directories["cache"])
		if err != nil {
			return err
		}
		err = api.RemovePaths(subpaths...)
		if err != nil {
			return err
		}
		tmpPath := filepath.Join(api.Directories["cache"], ".tmp")
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		patchFile := filepath.Join(wd, "spt.patch")
		_, err = os.Lstat(patchFile)
		if err != nil {
			return err
		}
		projectPath := filepath.Join(tmpPath, "project")
		buildPath := filepath.Join(projectPath, "build")
		commands := [][]any{
			{[]string{"git", "clone", "https://github.com/sp-tarkov/server", tmpPath}, helperapi.CmdOpts{}},
			{[]string{"git", "checkout", version}, helperapi.CmdOpts{Cwd: tmpPath}},
			{[]string{"git", "apply", patchFile}, helperapi.CmdOpts{Cwd: tmpPath}},
			{[]string{"git", "lfs", "pull"}, helperapi.CmdOpts{Cwd: tmpPath}},
			{[]string{"npm", "install"}, helperapi.CmdOpts{Cwd: projectPath}},
			{[]string{"npm", "run", "build:release"}, helperapi.CmdOpts{Cwd: projectPath}},
			{[]string{"mv", buildPath, cachePath}, helperapi.CmdOpts{}},
		}
		for _, command := range commands {
			cmdSlice := command[0].([]string)
			opts := command[1].(helperapi.CmdOpts)
			_, err := api.RunCommand(cmdSlice, opts)
			if err != nil {
				return err
			}
		}

		api.Logger.Info("sleeping")
		time.Sleep(10 * time.Minute)

		err = api.RemovePaths(tmpPath)
		if err != nil {
			return err
		}
	}
	api.Logger.Info("copy spt from cache")
	_, err = api.RunCommand([]string{"cp", "-R", fmt.Sprintf("%s/.", cachePath), api.Directories["spt"]}, helperapi.CmdOpts{})
	if err != nil {
		return err
	}
	return nil
}

// EntrypointConfig is loaded from the environment and is used during [Entrypoint]
type EntrypointConfig struct {
	ConfigPatches ConfigPatches `env:"CONFIG_PATCHES"`
	DataDirs      []string      `env:"DATA_DIRS"`
	ModUrls       []string      `env:"MOD_URLS"`
	SptVersion    string        `env:"SPT_VERSION"`
}

// Performs the pre-launch setup of the server.
// This includes mod installation, server and mod configuration, server intialization
// Finally, the server is launched in the foreground and blocks until exit.
// Returns an error if any step of the process fails.
func Entrypoint(ctx context.Context, api Api) error {
	config := EntrypointConfig{}
	err := api.ParseEnv(&config)
	if err != nil {
		return err
	}
	if config.SptVersion == "" {
		return fmt.Errorf("spt version required")
	}

	err = api.CreateDirs(api.Directories.Values()...)
	if err != nil {
		return err
	}

	err = api.InstallSpt(config.SptVersion)
	if err != nil {
		return err
	}

	err = api.InstallMods(config.ModUrls...)
	if err != nil {
		return err
	}

	err = api.InitializeServer()
	if err != nil {
		return err
	}

	err = api.ApplyConfigPatches(api.MergeConfigPatches(
		ConfigPatches{
			"SPT_Data/Server/configs/http.json": []helperapi.JsonPatch{
				{Op: "replace", Path: "/ip", Value: "0.0.0.0"},
				{Op: "replace", Path: "/backendIp", Value: "0.0.0.0"},
			},
		},
		config.ConfigPatches,
	))
	if err != nil {
		return err
	}

	err = api.SymlinkDataDirs(api.MergeDataDirs(
		[]string{"user/profiles"},
		config.DataDirs,
	))
	if err != nil {
		return err
	}

	return api.RunServer()
}

//go:embed version.txt
var Version string

func main() {
	wd, _ := os.Getwd()
	(&helper.Helper{
		Directories: map[string]string{
			"cache": filepath.Join(wd, "cache"),
			"data":  filepath.Join(wd, "data"),
			"spt":   filepath.Join(wd, "spt"),
		},
		Entrypoint: RunCallback(Entrypoint),
		Version:    Version,
	}).Run()
}
