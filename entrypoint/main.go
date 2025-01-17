package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	osuser "os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	jsonpatch "github.com/evanphx/json-patch/v5"
)

// Obtains the current working directory
// Panics on failure
// Used to intiialize package level variables
func requireWd() string {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return wd
}

// Package level variables
var (
	envConfigPatches = "CONFIG_PATCHES"
	envGid           = "GID"
	envModUrls       = "MOD_URLS"
	envUid           = "UID"
	logger           = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{}))
	modName          = "docker-image-helper-mod"
	pathData         = filepath.Join(requireWd(), "data")
	pathSpt          = filepath.Join(requireWd(), "spt")
	userName         = "spt"
)

// CmdOpts are options used to configure [runCmd] behavior
type CmdOpts struct {
	Attach  bool
	Context context.Context
	Cwd     string
	Env     []string
	User    *User
}

// Runs a command using the configured [CmdOpts].
// Returns the stdout of the command
// Returns an error if the command fails with a non-zero exit code
func runCmd(commandSlice []string, opts CmdOpts) (string, error) {
	if opts.User != nil && *opts.User != getCurrentUser() {
		commandSlice = append([]string{"gosu", fmt.Sprintf("%d:%d", opts.User.Uid, opts.User.Gid)}, commandSlice...)
	}

	logger.Info("run cmd", "command", strings.Join(commandSlice, " "))

	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}

	stderrBuffer := strings.Builder{}
	stdoutBuffer := strings.Builder{}
	command := exec.CommandContext(ctx, commandSlice[0], commandSlice[1:]...)
	command.Stderr = &stderrBuffer
	command.Stdout = &stdoutBuffer
	if opts.Attach {
		command.Stderr = os.Stderr
		command.Stdin = os.Stdin
		command.Stdout = os.Stdout
	}
	if opts.Cwd != "" {
		command.Dir = opts.Cwd
	}
	if opts.Env != nil {
		command.Env = opts.Env
	}

	err := command.Run()
	if err != nil && !opts.Attach {
		logger.Error("run cmd failed", "command", strings.Join(commandSlice, " "), "stderr", stderrBuffer.String())
	}

	return stdoutBuffer.String(), err
}

// User holds simple uid/gid metadata for local user accounts
type User struct {
	Uid int
	Gid int
}

// Gets user information associated with [userName].
// Returns an error if any part of the user lookup fails.
func getNonRootUser() (User, error) {
	fail := func(err error) (User, error) {
		return User{}, err
	}

	userLookup, err := osuser.Lookup(userName)
	if err != nil {
		return fail(err)
	}

	gid, err := strconv.Atoi(userLookup.Gid)
	if err != nil {
		return fail(err)
	}
	uid, err := strconv.Atoi(userLookup.Uid)
	if err != nil {
		return fail(err)
	}

	return User{Gid: gid, Uid: uid}, nil
}

// Returns user information as stored in the environment (UID/GID).
// Returns an error if any part of the lookup fails.
func getUserFromEnv() (User, error) {
	fail := func(err error) (User, error) {
		return User{}, err
	}

	getIntFromEnv := func(name string, defaultValue int) (int, error) {
		envValStr := os.Getenv(name)
		if envValStr == "" {
			envValStr = strconv.Itoa(defaultValue)
		}
		return strconv.Atoi(envValStr)
	}

	sptUser, err := getNonRootUser()
	if err != nil {
		return fail(err)
	}

	gid, err := getIntFromEnv(envGid, sptUser.Gid)
	if err != nil {
		return fail(err)
	}
	uid, err := getIntFromEnv(envUid, sptUser.Uid)
	if err != nil {
		return fail(err)
	}

	return User{Gid: gid, Uid: uid}, nil
}

// Gets the current user.
func getCurrentUser() User {
	return User{
		Gid: os.Getgid(),
		Uid: os.Getuid(),
	}
}

// Creates the given directories.
// Returns an error if directory creation fails.
func createDirectories(paths ...string) error {
	for _, path := range paths {
		_, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			logger.Info("create directory", "path", path)
			err = os.MkdirAll(path, 0755)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// Extracts a file at path [src] to path [dest].
// Creates [dest] if it does not exist.
// Raises an error if directory creation fails.
// Raises an error if extraction fails.
func extract(src string, dest string) error {
	logger.Info("extract", "src", src, "dest", dest)

	err := createDirectories(dest)
	if err != nil {
		return err
	}

	if strings.HasSuffix(src, ".zip") {
		_, err := runCmd([]string{"unzip", "-o", src, "-d", dest}, CmdOpts{})
		return err
	} else if strings.HasSuffix(src, ".7z") {
		_, err := runCmd([]string{"7z", "x", src, fmt.Sprintf("-o%s", dest)}, CmdOpts{})
		return err
	}
	return fmt.Errorf("unrecongized file type %s", src)
}

// downloadCb is a callback with an argument pointing to the path of a downloaded file.
type downloadCb func(path string) error

// Downloads a file from url to a temporary path, which is then passed to the provided callback so that further action can be taken.
// Raises an error if the download fails.
// Raises an error if the callback returns an error.
func download(url string, cb downloadCb) error {
	tempDir, err := os.MkdirTemp("", "")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	baseName := filepath.Base(url)
	tempFile := filepath.Join(tempDir, baseName)
	handle, err := os.Create(tempFile)
	if err != nil {
		return err
	}
	defer handle.Close()

	logger.Info("download", "url", url, "file", tempFile)
	response, err := http.Get(url)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s sent non-200 status code: %d", url, response.StatusCode)
	}

	chunkSize := 1024 * 1024
	_, err = io.CopyBuffer(handle, response.Body, make([]byte, chunkSize))
	if err != nil {
		return err
	}

	return cb(tempFile)
}

// Installs the given mod urls to the spt path.
// Raises an error if a url download fails.
// Raises an error if mod extraction fails.
func installMods(modUrls ...string) error {
	for _, modUrl := range modUrls {
		logger.Info("install mod", "url", modUrl)
		err := download(modUrl, func(modPath string) error {
			return extract(modPath, pathSpt)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// Obtains a list of mod urls from the environment.
func getModUrlsFromEnv() []string {
	modUrls := []string{}
	modUrlString := os.Getenv(envModUrls)
	if modUrlString == "" {
		return modUrls
	}
	for _, modUrl := range strings.Split(modUrlString, ",") {
		modUrl = strings.TrimSpace(modUrl)
		if modUrl == "" {
			continue
		}
		modUrls = append(modUrls, modUrl)
	}
	return modUrls
}

// Initializes the server.
// Starts the server, waits for it to be connectable, and then shuts it down.
// This allows the server to generate first-launch files for subsequent modification.
// Raises an error if the server fails to start.
// Raises an error if the server is unconnectable after a set timeout.
func initializeServer() error {
	logger.Info("initialize server")

	pathServerBin := filepath.Join(pathSpt, "SPT.Server.exe")

	start := time.Now()
	timeout := 120 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	complete := make(chan bool, 1)
	var err error
	go func() {
		logger.Info("start server")
		_, err = runCmd([]string{pathServerBin}, CmdOpts{Context: ctx, Cwd: pathSpt})
		complete <- true
	}()

	go func() {
		isComplete := func() bool {
			select {
			case <-complete:
				return true
			default:
				return false
			}
		}
		ticker := time.NewTicker(1 * time.Second)
		for range ticker.C {
			if isComplete() {
				break
			}
			response, err := http.Get("http://localhost:6969")
			if err != nil || response.StatusCode != 200 {
				continue
			}
			logger.Info("server initialized")
			cancel()
			break
		}
	}()

	<-complete
	if time.Since(start) > timeout {
		err = fmt.Errorf("command timed out")
	} else {
		select {
		case <-ctx.Done():
			err = nil
		default:
		}
	}

	return err
}

// Starts an spt server and blocks until exit.
// Raises an error if the server exits with a non-zero exit code.
func runServer() error {
	pathServerBin := filepath.Join(pathSpt, "SPT.Server.exe")
	_, err := runCmd([]string{pathServerBin}, CmdOpts{Attach: true, Cwd: pathSpt})
	return err
}

// Symlinks folders from [pathData] into [pathSpt] to persist certain slices of information
func symlinkPersistentData() error {
	serverProfiles := filepath.Join(pathSpt, "user", "profiles")
	persistentProfiles := filepath.Join(pathData, "user", "profiles")
	serverProfilesParent := filepath.Dir(serverProfiles)

	logger.Info("ensure directory", "path", serverProfilesParent)
	err := os.MkdirAll(filepath.Dir(serverProfilesParent), 0755)
	if err != nil {
		return err
	}

	logger.Info("remove directory", "path", serverProfiles)
	err = os.RemoveAll(serverProfiles)
	if err != nil {
		return err
	}

	logger.Info("ensure directory", "path", persistentProfiles)
	err = os.MkdirAll(persistentProfiles, 0755)
	if err != nil {
		return err
	}

	logger.Info("symlink directory", "from", persistentProfiles, "to", serverProfiles)
	err = os.Symlink(persistentProfiles, serverProfiles)
	if err != nil {
		return err
	}

	return nil
}

// A ConfigPatch represents a single JSONPatch object
type ConfigPatch struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value,omitempty"`
}

// ConfigPatches are a map of relative file path -> a list of config patches to apply
type ConfigPatches map[string][]ConfigPatch

// Returns the number of patches in the [ConfigPatches] object
func (cps ConfigPatches) Count() int {
	total := 0
	for _, filePatches := range cps {
		total += len(filePatches)
	}
	return total
}

// Fetches [ConfigPatches] from the environment.
// Returns an error if parsing the JSON fails.
// Returns an empty map if the environment variable is not set.
func getConfigPatchesFromEnv() (ConfigPatches, error) {
	fail := func(err error) (ConfigPatches, error) {
		return nil, err
	}

	configPatches := ConfigPatches{}
	configPatchesString := os.Getenv(envConfigPatches)
	if configPatchesString == "" {
		return configPatches, nil
	}

	err := json.Unmarshal([]byte(configPatchesString), &configPatches)
	if err != nil {
		return fail(err)
	}

	return configPatches, nil
}

// Applies a single [ConfigPatch] to the file located at [pathSpt] + [relPath]
// Returns an error if relPath is not a relative path.
// Returns an error if the resulting absolute path to the file being patched cannot be found, read or written to.
// Returns an error if the patch cannot be applied to the document
func applyConfigPatch(relPath string, patch ConfigPatch) error {
	if strings.HasPrefix(relPath, "/") {
		return fmt.Errorf("patch path %s not relative", relPath)
	}

	path := filepath.Join(pathSpt, relPath)
	_, err := os.Lstat(path)
	if err != nil {
		return err
	}

	doc, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	patchBytes, err := json.Marshal([]ConfigPatch{patch})
	if err != nil {
		return err
	}

	jsonPatch, err := jsonpatch.DecodePatch(patchBytes)
	if err != nil {
		return err
	}

	newDoc, err := jsonPatch.ApplyIndent(doc, "  ")
	if err != nil {
		return err
	}

	err = os.WriteFile(path, newDoc, 0755)
	if err != nil {
		return err
	}

	return nil
}

// Applies config patches to files located in the spt server path
func applyConfigPatches(configPatches ConfigPatches) error {
	logger.Info("apply config patches", "count", configPatches.Count())

	failed := false
	for relPath, patches := range configPatches {
		for _, patch := range patches {
			err := applyConfigPatch(relPath, patch)
			logger.Info("apply config patch", "patch", patch, "error", err)
			if err != nil {
				failed = true
			}
		}
	}

	if failed {
		return fmt.Errorf("failed to apply patches")
	}

	return nil
}

// Merges several [ConfigPatches] objects into a single one.
func mergeConfigPatches(maps ...ConfigPatches) ConfigPatches {
	data := ConfigPatches{}

	for _, currMap := range maps {
		for k, v := range currMap {
			_, ok := data[k]
			if !ok {
				data[k] = []ConfigPatch{}
			}
			data[k] = append(data[k], v...)
		}
	}

	return data
}

// Performs the pre-launch setup of the server.
// This includes mod installation, server and mod configuration, server intialization
// Finally, the server is launched in the foreground and blocks until exit.
// Returns an error if any step of the process fails.
func entrypoint() error {
	err := createDirectories(pathData, pathSpt)
	if err != nil {
		return err
	}

	err = installMods(getModUrlsFromEnv()...)
	if err != nil {
		return err
	}

	err = initializeServer()
	if err != nil {
		return err
	}

	configPatches, err := getConfigPatchesFromEnv()
	if err != nil {
		return err
	}
	configPatches = mergeConfigPatches(
		ConfigPatches{
			"SPT_Data/Server/configs/http.json": []ConfigPatch{
				{Op: "replace", Path: "/ip", Value: "0.0.0.0"},
				{Op: "replace", Path: "/backendIp", Value: "0.0.0.0"},
			},
		},
		configPatches,
	)
	err = applyConfigPatches(configPatches)
	if err != nil {
		return err
	}

	err = symlinkPersistentData()
	if err != nil {
		return err
	}

	return runServer()
}

// Ensures a set of paths exist and are owned by the given [User].
// Returns an error if creating the directories fail.
// Returns an error if attempting to set the directory owner fails.
func setDirectoryOwner(owner User, paths ...string) error {
	for _, path := range paths {
		err := createDirectories(path)
		if err != nil {
			return err
		}

		logger.Info("ensure directory ownership", "gid", owner.Gid, "uid", owner.Uid)
		_, err = runCmd([]string{"chown", "-R", fmt.Sprintf("%d:%d", owner.Uid, owner.Gid), path}, CmdOpts{})
		if err != nil {
			return err
		}
	}

	return nil
}

// Updates the uid and gid of the non-root user within the image (the 'spt' user).
// Returns an error if the non-root user cannot be found.
// Returns an error if setting the non-root user's uid fails.
// Returns an error if setting the non-root user's gid fails.
func updateNonRootUser(user User) error {
	if user.Uid == 0 {
		return fmt.Errorf("refusing to update spt user to uid 0")
	}

	sptUser, err := getNonRootUser()
	if err != nil {
		return err
	}

	if user.Uid != sptUser.Uid {
		logger.Info("change uid", "user", userName, "from", sptUser.Uid, "to", user.Uid)
		_, err := runCmd([]string{"usermod", "-u", strconv.Itoa(user.Uid), userName}, CmdOpts{})
		if err != nil {
			return err
		}
	}

	if user.Gid != sptUser.Gid {
		logger.Info("change gid", "user", userName, "from", sptUser.Gid, "to", user.Gid)
		_, err := runCmd([]string{"groupmod", "-g", strconv.Itoa(user.Gid), userName}, CmdOpts{})
		if err != nil {
			return err
		}
	}

	return nil
}

// Ensures that the docker image is run as a non-root user.
// If the current user is non-root, simply launches the entrypoint.
// If the current user is root, ensures the non-root user has UID/GID (as defined in the environment) - and that necessary directories are owned by the user.  Finally, the entrypoint is launched.
// Returns an error if user modifications fail.
// Returns an error if the entrypoint launch fails.
func preEntrypoint() error {
	logger.Info("pre-entrypoint")

	user := getCurrentUser()

	if getCurrentUser().Uid == 0 {
		var err error
		user, err = getUserFromEnv()
		if err != nil {
			return err
		}

		err = updateNonRootUser(user)
		if err != nil {
			return err
		}

		err = setDirectoryOwner(user, pathData, pathSpt)
		if err != nil {
			return err
		}
	}

	executable, err := os.Executable()
	if err != nil {
		return err
	}

	_, err = runCmd([]string{executable, "entrypoint"}, CmdOpts{Attach: true, Env: os.Environ(), User: &user})
	return err
}

//go:embed version.txt
var versionString string

// Gets the version of the entrypoint
// See: [versionString]
func version() error {
	fmt.Print(strings.TrimSpace(versionString))
	return nil
}

// The main function of the entrypoint.
// Routes arguments to specific commands.
// Raises an error if a command fails.
// Raises an error if a command is unrecognized.
func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		args = append(args, "pre-entrypoint")
	}

	var err error
	switch args[0] {
	case "entrypoint":
		err = entrypoint()
	case "pre-entrypoint":
		err = preEntrypoint()
	case "version":
		err = version()
	default:
		err = fmt.Errorf("unknown command: %s", args[0])
	}

	code := 0
	if err != nil {
		code = 1
		logger.Error(err.Error())
	}
	os.Exit(code)
}
