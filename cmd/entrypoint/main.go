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
)

var (
	envGid     = "GID"
	envModUrls = "MOD_URLS"
	envUid     = "UID"
	logger     = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{}))
	pathData   = "/data"
	pathServer = "/server"
	userName   = "eft"
)

type CmdOpts struct {
	Attach  bool
	Context context.Context
	Cwd     string
	Env     []string
	User    *User
}

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

type User struct {
	Uid int
	Gid int
}

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

	eftUser, err := getNonRootUser()
	if err != nil {
		return fail(err)
	}

	gid, err := getIntFromEnv(envGid, eftUser.Gid)
	if err != nil {
		return fail(err)
	}
	uid, err := getIntFromEnv(envUid, eftUser.Uid)
	if err != nil {
		return fail(err)
	}

	return User{Gid: gid, Uid: uid}, nil
}

func getCurrentUser() User {
	return User{
		Gid: os.Getgid(),
		Uid: os.Getuid(),
	}
}

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

type downloadCb func(path string) error

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

func installMods(modUrls ...string) error {
	for _, modUrl := range modUrls {
		logger.Info("install mod", "url", modUrl)
		err := download(modUrl, func(modPath string) error {
			return extract(modPath, pathServer)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

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

func initializeServer() error {
	logger.Info("initialize server")

	pathServerBin := filepath.Join(pathServer, "SPT.Server.exe")

	start := time.Now()
	timeout := 60 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	complete := make(chan bool, 1)
	var err error
	go func() {
		logger.Info("start server")
		_, err = runCmd([]string{pathServerBin}, CmdOpts{Context: ctx, Cwd: pathServer})
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

func configureSptServer() error {
	pathHttpConfig := filepath.Join(pathServer, "SPT_Data", "Server", "configs", "http.json")
	logger.Info("configuring", "path", pathHttpConfig)
	dataBytes, err := os.ReadFile(pathHttpConfig)
	if err != nil {
		return err
	}
	data := map[string]any{}
	err = json.Unmarshal(dataBytes, &data)
	if err != nil {
		return err
	}
	data["ip"] = "0.0.0.0"
	data["backendIp"] = "0.0.0.0"
	dataBytes, err = json.Marshal(&data)
	if err != nil {
		return err
	}
	err = os.WriteFile(pathHttpConfig, dataBytes, 0755)
	if err != nil {
		return err
	}
	return nil
}

func configureServer() error {
	err := configureSptServer()
	if err != nil {
		return err
	}

	return nil
}

func runServer() error {
	pathServerBin := filepath.Join(pathServer, "SPT.Server.exe")
	_, err := runCmd([]string{pathServerBin}, CmdOpts{Attach: true, Cwd: pathServer})
	return err
}

func symlinkPersistentData() error {
	serverProfiles := filepath.Join(pathServer, "user", "profiles")
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

func entrypoint() error {
	err := createDirectories(pathData, pathServer)
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

	err = configureServer()
	if err != nil {
		return err
	}

	err = symlinkPersistentData()
	if err != nil {
		return err
	}

	return runServer()
}

type DirOpts struct {
	Owner *User
}

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

func updateNonRootUser(user User) error {
	if user.Uid == 0 {
		return fmt.Errorf("refusing to update eft user to uid 0")
	}

	eftUser, err := getNonRootUser()
	if err != nil {
		return err
	}

	if user.Uid != eftUser.Uid {
		logger.Info("change uid", "user", userName, "from", eftUser.Uid, "to", user.Uid)
		_, err := runCmd([]string{"usermod", "-u", strconv.Itoa(user.Uid), userName}, CmdOpts{})
		if err != nil {
			return err
		}
	}

	if user.Gid != eftUser.Gid {
		logger.Info("change gid", "user", userName, "from", eftUser.Gid, "to", user.Gid)
		_, err := runCmd([]string{"groupmod", "-g", strconv.Itoa(user.Gid), userName}, CmdOpts{})
		if err != nil {
			return err
		}
	}

	return nil
}

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

		err = setDirectoryOwner(user, pathData, pathServer)
		if err != nil {
			return err
		}
	}

	executable, err := os.Executable()
	if err != nil {
		return err
	}

	_, err = runCmd([]string{executable, "entrypoint"}, CmdOpts{Attach: true, User: &user})
	return err
}

//go:embed version.txt
var versionString string

func version() error {
	fmt.Print(strings.TrimSpace(versionString))
	return nil
}

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
