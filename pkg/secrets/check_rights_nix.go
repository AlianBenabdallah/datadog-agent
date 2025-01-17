// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

//go:build secrets && !windows
// +build secrets,!windows

package secrets

import (
	"fmt"
	"os"
	"os/user"
	"strconv"
	"syscall"
)

// checkRights validates that a secret backend has supported permissions
func checkRights(path string, allowGroupExec bool) error {
	var stat syscall.Stat_t
	if err := syscall.Stat(path, &stat); err != nil {
		return fmt.Errorf("invalid executable '%s': can't stat it: %s", path, err)
	}

	// get information about current user
	usr, err := user.Current()
	if err != nil {
		return fmt.Errorf("can't query current user's GIDs: %s", err)
	}

	if !allowGroupExec {
		return checkUserPermission(&stat, usr, path)
	}

	userGroups, err := usr.GroupIds()
	if err != nil {
		return fmt.Errorf("can't query current user's GIDs: %s", err)
	}
	return checkGroupPermission(&stat, usr, userGroups, path)
}

// checkUserPermission check that only the current User can exec and own the file path
func checkUserPermission(stat *syscall.Stat_t, usr *user.User, path string) error {
	if fmt.Sprintf("%d", stat.Uid) != usr.Uid {
		return fmt.Errorf("invalid executable: '%s' isn't owned by this user: username '%s', UID %s. We can't execute it", path, usr.Username, usr.Uid)
	}

	// checking that the owner have exec rights
	if stat.Mode&syscall.S_IXUSR == 0 {
		return fmt.Errorf("invalid executable: '%s' is not executable", path)
	}

	// If *user* executable, user can RWX, and nothing else for anyone.
	if stat.Mode&(syscall.S_IRWXG|syscall.S_IRWXO) != 0 {
		return fmt.Errorf("invalid executable '%s', 'group' or 'others' have rights on it", path)
	}

	return nil
}

// checkGroupPermission check that only the current User or one of his group can exec the path
func checkGroupPermission(stat *syscall.Stat_t, usr *user.User, userGroups []string, path string) error {
	var isUserHavePermission bool
	// checking if the user is the owner and the owner have exec rights
	if (fmt.Sprintf("%d", stat.Uid) == usr.Uid) && (stat.Mode&syscall.S_IXUSR != 0) {
		isUserHavePermission = true
	}

	// If *group* executable, user can RWX, group can RX, and nothing else for anyone.
	if stat.Mode&(syscall.S_IRWXO|syscall.S_IWGRP) != 0 {
		return fmt.Errorf("invalid executable '%s', 'others' have rights on it or 'group' has write permissions on it", path)
	}

	// If the file is not owned by the user, let's check for one of his groups
	if !isUserHavePermission {
		var isGroupFile bool
		for _, userGroup := range userGroups {
			if fmt.Sprintf("%d", stat.Gid) == userGroup {
				isGroupFile = true
				break
			}
		}
		if !isGroupFile {
			return fmt.Errorf("invalid executable: '%s' isn't owned by this user or one of his group: username '%s', UID %s GUI %s. We can't execute it", path, usr.Username, usr.Uid, usr.Gid)
		}

		// Check that *group* can at least exec.
		if stat.Mode&(syscall.S_IXGRP) == 0 {
			return fmt.Errorf("invalid executable: '%s' is not executable by group", path)
		}
	}

	return nil
}

// checkConfigFilePermissions validates that a config file has supported permissions when using secret_backend_command_sha256 hash
var checkConfigFilePermissions = func(path string) error {
	var stat syscall.Stat_t
	if err := syscall.Stat(path, &stat); err != nil {
		return fmt.Errorf("unable to check permissions for '%s': can't stat it: %s", path, err)
	}

	if stat.Mode&syscall.S_IWOTH != 0 {
		return fmt.Errorf("invalid config file permissions for '%s': cannot have o+w permission", path)
	}

	usr, err := user.Current()
	if err != nil {
		return fmt.Errorf("can't query current user: %s", err)
	}

	groups, err := usr.GroupIds()
	if err != nil {
		return fmt.Errorf("can't query user groups: %s", err)
	}

	if strconv.FormatInt(int64(stat.Uid), 10) != usr.Uid {
		return fmt.Errorf("invalid config file permissions for '%s': not owned by %s", path, usr.Uid)
	}

	for _, g := range groups {
		if strconv.FormatInt(int64(stat.Gid), 10) == g {
			return nil
		}
	}

	return fmt.Errorf("invalid config file permissions for '%s': not owned by any groups for user %s", path, usr.Uid)
}

// lockOpenFile opens the file and prevents overwrite and delete by another process
func lockOpenFile(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_CREAT|syscall.O_RDONLY, 0600)
	if err != nil {
		return nil, err
	}

	if err = syscall.Flock(fd, syscall.LOCK_EX); err != nil {
		syscall.Close(fd)
		return nil, err
	}

	return os.NewFile(uintptr(fd), path), nil
}
