// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

package mongo

import (
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DataDog/datadog-agent/pkg/network/protocols/http/testutil"
)

const (
	User = "root"
	Pass = "password"
)

func RunMongoServer(t *testing.T, serverAddress, serverPort string) {
	t.Helper()

	env := []string{
		"MONGO_ADDR=" + serverAddress,
		"MONGO_PORT=" + serverPort,
		"MONGO_ROOT_PASSWORD=" + Pass,
		"MONGO_REPLICA_SET_KEY=" + Pass,
	}
	dir, _ := testutil.CurDir()
	cmd := exec.Command("docker-compose", "-f", dir+"/testdata/docker-compose.yml", "up", "-d")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stdout
	cmd.Env = append(cmd.Env, env...)
	require.NoErrorf(t, cmd.Run(), "could not start mongo with docker-compose")

	t.Cleanup(func() {
		c := exec.Command("docker-compose", "-f", dir+"/testdata/docker-compose.yml", "down", "--remove-orphans")
		c.Env = append(c.Env, env...)
		_ = c.Run()
	})
}
