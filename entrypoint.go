package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"

	helper "github.com/benfiola/game-server-helper/pkg"
	"golang.org/x/mod/semver"
)

// Installs the given mod urls to the spt path.
// Raises an error if a url download fails.
// Raises an error if mod extraction fails.
func InstallMods(ctx context.Context, modUrls ...string) error {
	for _, modUrl := range modUrls {
		helper.Logger(ctx).Info("install mod", "url", modUrl)
		key := fmt.Sprintf("mod-%s", filepath.Base(modUrl))
		err := helper.CacheFile(ctx, key, helper.Dirs(ctx)["spt"], func(dest string) error {
			return helper.CreateTempDir(ctx, func(tempDir string) error {
				archive := filepath.Join(tempDir, filepath.Base(modUrl))
				err := helper.Download(ctx, modUrl, archive)
				if err != nil {
					return err
				}
				err = helper.Extract(ctx, archive, dest)
				return err
			})
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
func InitializeServer(ctx context.Context) error {
	helper.Logger(ctx).Info("initialize server")
	cb := func(complete func()) error {
		response, err := http.Get("http://localhost:6969")
		if err != nil || response.StatusCode != 200 {
			return nil
		}
		helper.Logger(ctx).Info("server initialized")
		complete()
		return nil
	}
	serverBin := filepath.Join(helper.Dirs(ctx)["spt"], "SPT.Server.exe")
	_, err := helper.Command(ctx, []string{serverBin}, helper.CmdOpts{Cwd: helper.Dirs(ctx)["spt"], Until: cb}).Run()
	return err
}

// Starts an spt server and blocks until exit.
// Raises an error if the server exits with a non-zero exit code.
func RunServer(ctx context.Context) error {
	helper.Logger(ctx).Info("run server")
	pathServerBin := filepath.Join(helper.Dirs(ctx)["spt"], "SPT.Server.exe")
	_, err := helper.Command(ctx, []string{pathServerBin}, helper.CmdOpts{Attach: true, Cwd: helper.Dirs(ctx)["spt"]}).Run()
	return err
}

// ConfigPatches are a map of relative file path -> a list of json patches to apply
type ConfigPatches map[string][]helper.JsonPatch

// Parses a string into a [ConfigPatches] object.
// Used to parse settings from the environment.
func (cps *ConfigPatches) UnmarshalText(data []byte) error {
	parsed := map[string][]helper.JsonPatch{}
	err := json.Unmarshal(data, &parsed)
	*cps = ConfigPatches(parsed)
	return err
}

// Applies config patches to files located in the spt server path
func ApplyConfigPatches(ctx context.Context, configPatches ConfigPatches) error {
	for relPath, patches := range configPatches {
		helper.Logger(ctx).Info("apply config patch", "count", len(patches), "path", relPath)
		path := filepath.Join(helper.Dirs(ctx)["spt"], relPath)
		data := map[string]any{}
		err := helper.UnmarshalFile(ctx, path, &data)
		if err != nil {
			return err
		}
		err = helper.ApplyJsonPatches(ctx, &data, patches...)
		if err != nil {
			return err
		}
		err = helper.MarshalFile(ctx, data, path)
		if err != nil {
			return err
		}
	}

	return nil
}

// Merges several [ConfigPatches] objects into a single one.
func MergeConfigPatches(maps ...ConfigPatches) ConfigPatches {
	data := ConfigPatches{}
	for _, currMap := range maps {
		for k, v := range currMap {
			_, ok := data[k]
			if !ok {
				data[k] = []helper.JsonPatch{}
			}
			data[k] = append(data[k], v...)
		}
	}
	return data
}

// Merges lists of data directories into a single-deduplicated list
func MergeDataDirs(lists ...[]string) []string {
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
func SymlinkDataDirs(ctx context.Context, dataDirs []string) error {
	for _, dataDir := range dataDirs {
		sptPath := filepath.Join(helper.Dirs(ctx)["spt"], dataDir)
		dataPath := filepath.Join(helper.Dirs(ctx)["data"], dataDir)
		err := helper.SymlinkDir(ctx, dataPath, sptPath)
		if err != nil {
			return err
		}
	}
	return nil
}

// Finds all patch files less than or equal to the provided version
// Returns a list of patch files sorted in ascending version order
func FindPatchFiles(ctx context.Context, version string) ([]string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	allPatchFiles, err := filepath.Glob(filepath.Join(wd, "spt-*.patch"))
	if err != nil {
		return nil, err
	}

	versionRegexp, err := regexp.Compile(`spt-(.+)\.patch$`)
	if err != nil {
		return nil, err
	}

	patchFileMap := map[string]string{}
	versions := []string{}
	for _, patchFile := range allPatchFiles {
		match := versionRegexp.FindStringSubmatch(patchFile)
		if match == nil {
			continue
		}
		patchVersion := match[1]
		semVersion := fmt.Sprintf("v%s", version)
		semPatchVersion := fmt.Sprintf("v%s", patchVersion)
		if semver.Compare(semVersion, semPatchVersion) == 1 {
			continue
		}
		patchFileMap[semPatchVersion] = patchFile
		versions = append(versions, semPatchVersion)
	}
	semver.Sort(versions)

	patchFiles := []string{}
	for _, version := range versions {
		patchFiles = append(patchFiles, patchFileMap[version])
	}

	return patchFiles, nil
}

// Command is a helper that holds the command and options passed to helper.Command(...).Run()
type Command struct {
	Args []string
	Opts helper.CmdOpts
}

// Installs spt to the spt directory if spt exists in the cache.  If spt does not exist in the cache, it is checked out, built and copied into the cache.
// Returns an error if any step in this process fails.
func InstallSpt(ctx context.Context, version string) error {
	key := fmt.Sprintf("spt-%s", version)
	return helper.CacheFile(ctx, key, helper.Dirs(ctx)["spt"], func(dest string) error {
		return helper.CreateTempDir(ctx, func(tempDir string) error {
			helper.Logger(ctx).Info("build spt", "version", version)

			patchFiles, err := FindPatchFiles(ctx, version)
			if err != nil {
				return err
			}
			helper.Logger(ctx).Info("found patch files", "count", len(patchFiles))

			projectPath := filepath.Join(tempDir, "project")
			buildPath := filepath.Join(projectPath, "build")
			commands := []Command{
				{Args: []string{"git", "clone", "https://github.com/sp-tarkov/server", tempDir}, Opts: helper.CmdOpts{}},
				{Args: []string{"git", "checkout", version}, Opts: helper.CmdOpts{Cwd: tempDir}},
			}
			for _, patchFile := range patchFiles {
				commands = append(
					commands,
					Command{Args: []string{"git", "apply", patchFile}, Opts: helper.CmdOpts{Cwd: tempDir}},
				)
			}
			commands = append(
				commands,
				Command{Args: []string{"git", "lfs", "pull"}, Opts: helper.CmdOpts{Cwd: tempDir}},
				Command{Args: []string{"npm", "install"}, Opts: helper.CmdOpts{Cwd: projectPath}},
				Command{Args: []string{"npm", "run", "build:release"}, Opts: helper.CmdOpts{Cwd: projectPath}},
				Command{Args: []string{"mv", buildPath, dest}, Opts: helper.CmdOpts{}},
			)
			for _, command := range commands {
				_, err := helper.Command(ctx, command.Args, command.Opts).Run()
				if err != nil {
					return err
				}
			}
			return nil
		})

	})
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
func Entrypoint(ctx context.Context) error {
	config := EntrypointConfig{}
	err := helper.ParseEnv(ctx, &config)
	if err != nil {
		return err
	}
	if config.SptVersion == "" {
		return fmt.Errorf("spt version required")
	}

	err = helper.CreateDirs(ctx, helper.Dirs(ctx).Values()...)
	if err != nil {
		return err
	}

	err = InstallSpt(ctx, config.SptVersion)
	if err != nil {
		return err
	}

	err = InstallMods(ctx, config.ModUrls...)
	if err != nil {
		return err
	}

	err = InitializeServer(ctx)
	if err != nil {
		return err
	}

	err = ApplyConfigPatches(ctx, MergeConfigPatches(
		ConfigPatches{
			"SPT_Data/Server/configs/http.json": []helper.JsonPatch{
				{Op: "replace", Path: "/ip", Value: "0.0.0.0"},
				{Op: "replace", Path: "/backendIp", Value: "0.0.0.0"},
			},
		},
		config.ConfigPatches,
	))
	if err != nil {
		return err
	}

	err = SymlinkDataDirs(ctx, MergeDataDirs(
		[]string{"user/profiles"},
		config.DataDirs,
	))
	if err != nil {
		return err
	}

	return RunServer(ctx)
}

//go:embed version.txt
var Version string

func main() {
	(&helper.Entrypoint{
		Dirs: map[string]string{
			"cache": "./cache",
			"data":  "./data",
			"spt":   "./spt",
		},
		Main:    Entrypoint,
		Version: Version,
	}).Run()
}
