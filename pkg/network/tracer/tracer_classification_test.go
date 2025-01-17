// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

//go:build linux_bpf || (windows && npm)
// +build linux_bpf windows,npm

package tracer

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	nethttp "net/http"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DataDog/datadog-agent/pkg/network"
	"github.com/DataDog/datadog-agent/pkg/network/config"
	"github.com/DataDog/datadog-agent/pkg/network/tracer/testutil/grpc"
)

// testContext shares the context of a given test.
// It contains common variable used by all tests, and allows extending the context dynamically by setting more
// attributes to the `extras` map.
type testContext struct {
	// The address of the server to listen on.
	serverAddress string
	serverPort    string
	// The address for the client to communicate with.
	targetAddress string
	// optional - A custom dialer to set the ip/port/socket attributes for the client.
	clientDialer     *net.Dialer
	expectedProtocol network.ProtocolType
	// A channel to mark goroutines (like servers) to halt.
	done chan struct{}
	//nolint:unused
	// A dynamic map that allows extending the context easily between phases of the test.
	extras map[string]interface{}
}

func setupTracer(t *testing.T, cfg *config.Config) *Tracer {
	tr, err := NewTracer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		tr.Stop()
	})

	initTracerState(t, tr)
	require.NoError(t, err)

	// Giving the tracer time to run
	time.Sleep(time.Second)

	return tr
}

func validateProtocolConnection(t *testing.T, ctx testContext, tr *Tracer) {
	waitForConnectionsWithProtocol(t, tr, ctx.targetAddress, ctx.serverAddress, ctx.expectedProtocol)
}

func defaultTeardown(_ *testing.T, ctx testContext) {
	close(ctx.done)
}

//nolint:deadcode,unused
// skipIfNotLinux skips the test if we are not on a linux machine
func skipIfNotLinux(ctx testContext) (bool, string) {
	if runtime.GOOS != "linux" {
		return true, "test is supported on linux machine only"
	}

	return false, ""
}

//nolint:deadcode,unused
// skipIfUsingNAT skips the test if we have a NAT rules applied.
func skipIfUsingNAT(ctx testContext) (bool, string) {
	if ctx.targetAddress != ctx.serverAddress {
		return true, "test is not supported when NAT is applied"
	}

	return false, ""
}

//nolint:deadcode,unused
// composeSkips skips if one of the given filters is matched.
func composeSkips(filters ...func(ctx testContext) (bool, string)) func(ctx testContext) (bool, string) {
	return func(ctx testContext) (bool, string) {
		for _, filter := range filters {
			if skip, err := filter(ctx); skip {
				return skip, err
			}
		}

		return false, ""
	}
}

func testProtocolClassification(t *testing.T, cfg *config.Config, clientHost, targetHost, serverHost string) {
	defaultDialer := &net.Dialer{
		LocalAddr: &net.TCPAddr{
			IP:   net.ParseIP(clientHost),
			Port: 0,
		},
	}

	tests := []struct {
		name            string
		context         testContext
		shouldSkip      func(ctx testContext) (bool, string)
		preTracerSetup  func(t *testing.T, ctx testContext)
		postTracerSetup func(t *testing.T, ctx testContext)
		validation      func(t *testing.T, ctx testContext, tr *Tracer)
		teardown        func(t *testing.T, ctx testContext)
	}{
		{
			name: "tcp client without sending data",
			context: testContext{
				serverPort:       "8080",
				clientDialer:     defaultDialer,
				expectedProtocol: network.ProtocolUnknown,
			},
			postTracerSetup: func(t *testing.T, ctx testContext) {
				server := NewTCPServerOnAddress(ctx.serverAddress, func(c net.Conn) {
					c.Close()
				})
				require.NoError(t, server.Run(ctx.done))
				timedContext, cancel := context.WithTimeout(context.Background(), time.Second)
				c, err := ctx.clientDialer.DialContext(timedContext, "tcp", ctx.targetAddress)
				cancel()
				require.NoError(t, err)
				defer c.Close()
			},
			teardown:   defaultTeardown,
			validation: validateProtocolConnection,
		},
		{
			name: "tcp client with sending random data",
			context: testContext{
				serverPort:       "8080",
				clientDialer:     defaultDialer,
				expectedProtocol: network.ProtocolUnknown,
			},
			postTracerSetup: func(t *testing.T, ctx testContext) {
				server := NewTCPServerOnAddress(ctx.serverAddress, func(c net.Conn) {
					r := bufio.NewReader(c)
					input, err := r.ReadBytes(byte('\n'))
					if err == nil {
						c.Write(input)
					}
					c.Close()
				})
				require.NoError(t, server.Run(ctx.done))

				timedContext, cancel := context.WithTimeout(context.Background(), time.Second)
				c, err := ctx.clientDialer.DialContext(timedContext, "tcp", ctx.targetAddress)
				cancel()
				require.NoError(t, err)
				defer c.Close()
				c.Write([]byte("hello\n"))
				io.ReadAll(c)
			},
			teardown:   defaultTeardown,
			validation: validateProtocolConnection,
		},
		{
			name: "tcp client with sending HTTP request",
			context: testContext{
				serverPort:       "8080",
				clientDialer:     defaultDialer,
				expectedProtocol: network.ProtocolHTTP,
			},
			postTracerSetup: func(t *testing.T, ctx testContext) {
				ln, err := net.Listen("tcp", ctx.serverAddress)
				require.NoError(t, err)

				srv := &nethttp.Server{
					Addr: ln.Addr().String(),
					Handler: nethttp.HandlerFunc(func(w nethttp.ResponseWriter, req *nethttp.Request) {
						io.Copy(io.Discard, req.Body)
						w.WriteHeader(200)
					}),
					ReadTimeout:  time.Second,
					WriteTimeout: time.Second,
				}
				srv.SetKeepAlivesEnabled(false)
				go func() {
					_ = srv.Serve(ln)
				}()
				go func() {
					<-ctx.done
					srv.Shutdown(context.Background())
				}()

				client := nethttp.Client{
					Transport: &nethttp.Transport{
						DialContext: ctx.clientDialer.DialContext,
					},
				}
				resp, err := client.Get("http://" + ctx.targetAddress + "/test")
				require.NoError(t, err)
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			},
			teardown:   defaultTeardown,
			validation: validateProtocolConnection,
		},
		{
			name: "http2 traffic using gRPC - unary call",
			context: testContext{
				serverPort:       "8080",
				clientDialer:     defaultDialer,
				expectedProtocol: network.ProtocolHTTP2,
			},
			postTracerSetup: func(t *testing.T, ctx testContext) {
				server, err := grpc.NewServer(ctx.serverAddress)
				require.NoError(t, err)
				server.Run()
				go func() {
					<-ctx.done
					server.Stop()
				}()

				c, err := grpc.NewClient(ctx.targetAddress, grpc.Options{
					CustomDialer: ctx.clientDialer,
				})
				require.NoError(t, err)
				defer c.Close()
				require.NoError(t, c.HandleUnary(context.Background(), "test"))
			},
			teardown:   defaultTeardown,
			validation: validateProtocolConnection,
		},
		{
			name: "http2 traffic using gRPC - stream call",
			context: testContext{
				serverPort:       "8080",
				clientDialer:     defaultDialer,
				expectedProtocol: network.ProtocolHTTP2,
			},
			postTracerSetup: func(t *testing.T, ctx testContext) {
				server, err := grpc.NewServer(ctx.serverAddress)
				require.NoError(t, err)
				server.Run()
				go func() {
					<-ctx.done
					server.Stop()
				}()

				c, err := grpc.NewClient(ctx.targetAddress, grpc.Options{
					CustomDialer: ctx.clientDialer,
				})
				require.NoError(t, err)
				defer c.Close()
				require.NoError(t, c.HandleStream(context.Background(), 5))
			},
			teardown:   defaultTeardown,
			validation: validateProtocolConnection,
		},
		{
			// A case where we see multiple protocols on the same socket. In that case, we expect to classify the connection
			// with the first protocol we've found.
			name: "mixed protocols",
			context: testContext{
				serverPort:       "8080",
				clientDialer:     defaultDialer,
				expectedProtocol: network.ProtocolHTTP,
			},
			postTracerSetup: func(t *testing.T, ctx testContext) {
				server := NewTCPServerOnAddress(ctx.serverAddress, func(c net.Conn) {
					r := bufio.NewReader(c)
					input, err := r.ReadBytes(byte('\n'))
					if err == nil {
						c.Write(input)
					}
					c.Close()
				})
				require.NoError(t, server.Run(ctx.done))

				timedContext, cancel := context.WithTimeout(context.Background(), time.Second)
				c, err := ctx.clientDialer.DialContext(timedContext, "tcp", ctx.targetAddress)
				cancel()
				require.NoError(t, err)
				defer c.Close()
				c.Write([]byte("GET /200/foobar HTTP/1.1\n"))
				io.ReadAll(c)
				// http2 prefix.
				c.Write([]byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"))
				io.ReadAll(c)
			},
			teardown:   defaultTeardown,
			validation: validateProtocolConnection,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.context.serverAddress = net.JoinHostPort(serverHost, tt.context.serverPort)
			tt.context.targetAddress = net.JoinHostPort(targetHost, tt.context.serverPort)

			if tt.shouldSkip != nil {
				if skip, msg := tt.shouldSkip(tt.context); skip {
					t.Skip(msg)
				}
			}

			tt.context.done = make(chan struct{})
			if tt.teardown != nil {
				t.Cleanup(func() {
					tt.teardown(t, tt.context)
				})
			}
			if tt.preTracerSetup != nil {
				tt.preTracerSetup(t, tt.context)
			}
			tr := setupTracer(t, cfg)
			tt.postTracerSetup(t, tt.context)
			tt.validation(t, tt.context, tr)
		})
	}
}

func waitForConnectionsWithProtocol(t *testing.T, tr *Tracer, targetAddr, serverAddr string, expectedProtocol network.ProtocolType) {
	var incomingConns, outgoingConns []network.ConnectionStats

	foundIncomingWithProtocol := false
	foundOutgoingWithProtocol := false

	for start := time.Now(); time.Since(start) < 5*time.Second; {
		conns := getConnections(t, tr)
		newOutgoingConns := searchConnections(conns, func(cs network.ConnectionStats) bool {
			return cs.Type == network.TCP && fmt.Sprintf("%s:%d", cs.Dest, cs.DPort) == targetAddr
		})
		newIncomingConns := searchConnections(conns, func(cs network.ConnectionStats) bool {
			return cs.Type == network.TCP && fmt.Sprintf("%s:%d", cs.Source, cs.SPort) == serverAddr
		})

		outgoingConns = append(outgoingConns, newOutgoingConns...)
		incomingConns = append(incomingConns, newIncomingConns...)

		for _, conn := range newOutgoingConns {
			t.Logf("Found outgoing connection %v", conn)
			if conn.Protocol == expectedProtocol {
				foundOutgoingWithProtocol = true
				break
			}
		}
		for _, conn := range newIncomingConns {
			t.Logf("Found incoming connection %v", conn)
			if conn.Protocol == expectedProtocol {
				foundIncomingWithProtocol = true
				break
			}
		}

		if foundOutgoingWithProtocol && foundIncomingWithProtocol {
			return
		}

		time.Sleep(200 * time.Millisecond)
	}

	// If we didn't find both -> fail
	if foundOutgoingWithProtocol || foundIncomingWithProtocol {
		// We have found at least one.
		// Checking if the reason for not finding the other is flakiness of npm
		if !foundIncomingWithProtocol && len(incomingConns) == 0 {
			t.Log("npm didn't find the incoming connections, not failing the test")
			return
		}

		if !foundOutgoingWithProtocol && len(outgoingConns) == 0 {
			t.Log("npm didn't find the outgoing connections, not failing the test")
			return
		}

	}

	t.Errorf("couldn't find incoming and outgoing connections with protocol %d for "+
		"server address %s and target address %s.\nIncoming: %v\nOutgoing: %v\nfound incoming with protocol: "+
		"%v\nfound outgoing with protocol: %v", expectedProtocol, serverAddr, targetAddr, incomingConns, outgoingConns, foundIncomingWithProtocol, foundOutgoingWithProtocol)
}
