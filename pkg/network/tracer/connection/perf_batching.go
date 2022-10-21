// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

//go:build linux_bpf
// +build linux_bpf

package connection

import (
	"fmt"
	"time"
	"unsafe"

	"github.com/cilium/ebpf"

	"github.com/DataDog/datadog-agent/pkg/network"
	netebpf "github.com/DataDog/datadog-agent/pkg/network/ebpf"
	"github.com/DataDog/datadog-agent/pkg/network/ebpf/probes"
	manager "github.com/DataDog/ebpf-manager"
)

const defaultExpiredStateInterval = 60 * time.Second

// PerfBatchManager is reponsbile for two things:
//
// * Keeping track of the state of each batch object we read off the perf ring;
// * Detecting idle batches (this might happen in hosts with a low connection churn);
//
// The motivation is to impose an upper limit on how long a TCP close connection
// event remains stored in the eBPF map before being processed by the NetworkAgent.
type PerfBatchManager struct {
	// eBPF
	batchMap *ebpf.Map

	// stateByCPU contains the state of each batch.
	// The slice is indexed by the CPU core number.
	stateByCPU []percpuState

	expiredStateInterval time.Duration
}

// NewPerfBatchManager returns a new `PerfBatchManager` and initializes the
// eBPF map that holds the tcp_close batch objects.
func NewPerfBatchManager(batchMap *ebpf.Map, numCPUs int) (*PerfBatchManager, error) {
	if batchMap == nil {
		return nil, fmt.Errorf("batchMap is nil")
	}

	state := make([]percpuState, numCPUs)
	for cpu := 0; cpu < numCPUs; cpu++ {
		b := new(netebpf.Batch)
		if err := batchMap.Put(unsafe.Pointer(&cpu), unsafe.Pointer(b)); err != nil {
			return nil, fmt.Errorf("error initializing perf batch manager maps: %w", err)
		}
		state[cpu] = percpuState{
			processed: make(map[uint64]batchState),
		}
	}

	return &PerfBatchManager{
		batchMap:             batchMap,
		stateByCPU:           state,
		expiredStateInterval: defaultExpiredStateInterval,
	}, nil
}

// ExtractBatchInto extracts from the given batch all connections that haven't been processed yet.
func (p *PerfBatchManager) ExtractBatchInto(buffer *network.ConnectionBuffer, b *netebpf.Batch, cpu int) {
	if cpu >= len(p.stateByCPU) {
		return
	}

	batchID := b.Id
	cpuState := &p.stateByCPU[cpu]
	start := uint16(0)
	if bState, ok := cpuState.processed[batchID]; ok {
		start = bState.offset
	}

	p.extractBatchInto(buffer, b, start, netebpf.BatchSize)
	delete(cpuState.processed, batchID)
}

// GetPendingConns return all connections that are in batches that are not yet full.
// It tracks which connections have been processed by this call, by batch id.
// This prevents double-processing of connections between GetPendingConns and Extract.
func (p *PerfBatchManager) GetPendingConns(buffer *network.ConnectionBuffer) {
	b := new(netebpf.Batch)
	for cpu := 0; cpu < len(p.stateByCPU); cpu++ {
		cpuState := &p.stateByCPU[cpu]

		err := p.batchMap.Lookup(unsafe.Pointer(&cpu), unsafe.Pointer(b))
		if err != nil {
			continue
		}

		batchLen := b.Len
		if batchLen == 0 {
			continue
		}

		// have we already processed these messages?
		start := uint16(0)
		batchID := b.Id
		if bState, ok := cpuState.processed[batchID]; ok {
			start = bState.offset
		}

		p.extractBatchInto(buffer, b, start, batchLen)
		// update timestamp regardless since this partial batch still exists
		cpuState.processed[batchID] = batchState{offset: batchLen, updated: time.Now()}
	}

	p.cleanupExpiredState(time.Now())
}

type percpuState struct {
	// map of batch id -> offset of conns already processed by GetPendingConns
	processed map[uint64]batchState
}

type batchState struct {
	offset  uint16
	updated time.Time
}

// ExtractBatchInto extract network.ConnectionStats objects from the given `batch` into the supplied `buffer`.
// The `start` (inclusive) and `end` (exclusive) arguments represent the offsets of the connections we're interested in.
func (p *PerfBatchManager) extractBatchInto(buffer *network.ConnectionBuffer, b *netebpf.Batch, start, end uint16) {
	if start >= end || end > netebpf.BatchSize {
		return
	}

	var ct netebpf.Conn
	for i := start; i < end; i++ {
		switch i {
		case 0:
			ct = b.C0
			break
		case 1:
			ct = b.C1
			break
		case 2:
			ct = b.C2
			break
		case 3:
			ct = b.C3
			break
		default:
			panic("batch size is out of sync")
		}

		conn := buffer.Next()
		PopulateConnStats(conn, &ct.Tup, &ct.Conn_stats)
		UpdateTCPStats(conn, ct.Conn_stats.Cookie, &ct.Tcp_stats)
	}
}

func (p *PerfBatchManager) cleanupExpiredState(now time.Time) {
	for cpu := 0; cpu < len(p.stateByCPU); cpu++ {
		cpuState := &p.stateByCPU[cpu]
		for id, s := range cpuState.processed {
			if now.Sub(s.updated) >= p.expiredStateInterval {
				delete(cpuState.processed, id)
			}
		}
	}
}

func PopulateConnStats(stats *network.ConnectionStats, t *netebpf.ConnTuple, s *netebpf.ConnStats) {
	*stats = network.ConnectionStats{
		Pid:              t.Pid,
		NetNS:            t.Netns,
		Source:           t.SourceAddress(),
		Dest:             t.DestAddress(),
		SPort:            t.Sport,
		DPort:            t.Dport,
		SPortIsEphemeral: network.IsPortInEphemeralRange(t.Sport),
		Monotonic:        make(network.StatCountersByCookie, 0, 3),
		LastUpdateEpoch:  s.Timestamp,
		IsAssured:        s.IsAssured(),
	}

	stats.Monotonic.Put(s.Cookie, network.StatCounters{
		SentBytes:   s.Sent_bytes,
		RecvBytes:   s.Recv_bytes,
		SentPackets: s.Sent_packets,
		RecvPackets: s.Recv_packets,
	})

	if t.Type() == netebpf.TCP {
		stats.Type = network.TCP
	} else {
		stats.Type = network.UDP
	}

	switch t.Family() {
	case netebpf.IPv4:
		stats.Family = network.AFINET
	case netebpf.IPv6:
		stats.Family = network.AFINET6
	}

	switch s.ConnectionDirection() {
	case netebpf.Incoming:
		stats.Direction = network.INCOMING
	case netebpf.Outgoing:
		stats.Direction = network.OUTGOING
	default:
		stats.Direction = network.OUTGOING
	}
}

func UpdateTCPStats(conn *network.ConnectionStats, cookie uint32, tcpStats *netebpf.TCPStats) {
	if conn.Type != network.TCP {
		return
	}

	m, _ := conn.Monotonic.Get(cookie)
	m.Retransmits = tcpStats.Retransmits
	m.TCPEstablished = uint32(tcpStats.State_transitions >> netebpf.Established & 1)
	m.TCPClosed = uint32(tcpStats.State_transitions >> netebpf.Close & 1)
	conn.Monotonic.Put(cookie, m)
	conn.RTT = tcpStats.Rtt
	conn.RTTVar = tcpStats.Rtt_var
}

func NewConnBatchManager(mgr *manager.Manager) (*PerfBatchManager, error) {
	connCloseEventMap, _, err := mgr.GetMap(string(probes.ConnCloseEventMap))
	if err != nil {
		return nil, err
	}

	connCloseMap, _, err := mgr.GetMap(string(probes.ConnCloseBatchMap))
	if err != nil {
		return nil, err
	}

	numCPUs := int(connCloseEventMap.MaxEntries())
	batchMgr, err := NewPerfBatchManager(connCloseMap, numCPUs)
	if err != nil {
		return nil, err
	}

	return batchMgr, nil
}