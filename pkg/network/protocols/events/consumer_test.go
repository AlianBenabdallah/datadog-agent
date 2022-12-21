// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

//go:build linux_bpf
// +build linux_bpf

package events

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"github.com/DataDog/datadog-agent/pkg/ebpf/bytecode"
	"github.com/DataDog/datadog-agent/pkg/network/config"
	"github.com/DataDog/datadog-agent/pkg/util/kernel"
	manager "github.com/DataDog/ebpf-manager"
	"github.com/cilium/ebpf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConsumer(t *testing.T) {
	kversion, err := kernel.HostVersion()
	require.NoError(t, err)
	if minVersion := kernel.VersionCode(4, 14, 0); kversion < minVersion {
		t.Skipf("package not supported by kernels < %s", minVersion)
	}

	const numEvents = 100
	c := config.New()
	program, err := newEBPFProgram(c)
	require.NoError(t, err)

	result := make(map[uint64]int)
	callback := func(b []byte) {
		// each event is just a uint64
		n := binary.LittleEndian.Uint64(b)
		result[n] = +1
	}

	consumer, err := NewConsumer("test", program, callback)
	require.NoError(t, err)
	consumer.Start()

	err = program.Start()
	require.NoError(t, err)

	t.Cleanup(func() { program.Stop(manager.CleanAll) })
	t.Cleanup(consumer.Stop)

	// generate test events
	generator := newEventGenerator(program, t)
	for i := 0; i < numEvents; i++ {
		generator.Generate(uint64(i))
	}

	// this ensures that any incomplete batch left in eBPF is fully processed
	consumer.Sync()
	consumer.Stop()

	// ensure that we have received each event exactly once
	for i := 0; i < numEvents; i++ {
		actual := result[uint64(i)]
		assert.Equalf(t, 1, actual, "eventID=%d should have 1 occurrence. got %d", i, actual)
	}
}

type eventGenerator struct {
	// map used for coordinating test with eBPF program space
	testMap *ebpf.Map

	// file used for triggering write(2) syscalls
	testFile *os.File
}

func newEventGenerator(program *manager.Manager, t *testing.T) *eventGenerator {
	m, _, _ := program.GetMap("test")
	require.NotNilf(t, m, "couldn't find test map")

	f, err := os.Create(filepath.Join(t.TempDir(), "foobar"))
	require.NoError(t, err)

	return &eventGenerator{
		testMap:  m,
		testFile: f,
	}
}

func (e *eventGenerator) Generate(eventID uint64) error {
	type testCtx struct {
		fd      uint64
		eventID uint64
	}

	var (
		fd  = uint64(e.testFile.Fd())
		key = uint32(0)
		val = testCtx{fd: fd, eventID: eventID}
	)

	// this is done so the eBPF program will echo back an event that is equal to
	// eventID once the write syscall is triggered below
	err := e.testMap.Put(unsafe.Pointer(&key), unsafe.Pointer(&val))
	if err != nil {
		return err
	}

	e.testFile.Write([]byte("whatever"))
	return nil
}

func newEBPFProgram(c *config.Config) (*manager.Manager, error) {
	bc, err := bytecode.GetReader(c.BPFDir, "events_test-debug.o")
	if err != nil {
		return nil, err
	}
	defer bc.Close()

	m := &manager.Manager{
		Probes: []*manager.Probe{
			{
				ProbeIdentificationPair: manager.ProbeIdentificationPair{
					EBPFSection:  "tracepoint/syscalls/sys_enter_write",
					EBPFFuncName: "tracepoint__syscalls__sys_enter_write",
				},
			},
		},
	}
	options := manager.Options{
		ActivatedProbes: []manager.ProbesSelector{
			&manager.ProbeSelector{
				ProbeIdentificationPair: manager.ProbeIdentificationPair{
					EBPFSection:  "tracepoint/syscalls/sys_enter_write",
					EBPFFuncName: "tracepoint__syscalls__sys_enter_write",
				},
			},
		},
	}

	Configure("test", m, &options)
	err = m.InitWithOptions(bc, options)
	if err != nil {
		return nil, err
	}

	return m, nil
}