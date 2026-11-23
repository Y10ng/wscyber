package main

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/chainreactors/cyber/agent/provider"
	agentsession "github.com/chainreactors/cyber/agent/session"
	"github.com/chainreactors/cyber/core/extension"
	coreproc "github.com/chainreactors/cyber/core/proc"
	"github.com/chainreactors/cyber/core/telemetry"
	"github.com/chainreactors/cyber/pkg/harness"
	"github.com/chainreactors/utils/proc"

	"wscyber/native/internal/testdb"
	"wscyber/native/webapi"
)

// The gate is a test-only Harness extension: no production sleeps or global hooks.
// Stop after base resources exist, but before Harness publishes its capabilities.
func TestModelProfileStopDuringLoad(t *testing.T) {
	for _, tc := range []struct {
		name                string
		product, cancelLoad bool
	}{
		{"product/canceled", true, true},
		{"local/canceled", false, true},
		{"product/completed", true, false},
		{"local/completed", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			capability, err := webapi.NewCapability(testdb.New(t).URL, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer capability.Close(context.Background())
			entered, release := make(chan struct{}), make(chan struct{})
			var listener net.Listener
			var processes *coreproc.Manager
			resourceClosed := false
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			h, err := harness.New(harness.Config{
				Base:    harness.BaseConfig{Directory: t.TempDir(), Logger: telemetry.NopLogger(), Provider: provider.StartupConfig{Mode: provider.StartupDisabled}},
				Session: &agentsession.Config{Logger: telemetry.NopLogger()},
				Extensions: []extension.Extension{extension.Func{
					LoadFunc: func(scope *extension.Scope) error {
						var err error
						listener, err = net.Listen("tcp", "127.0.0.1:0")
						if err != nil {
							return err
						}
						processes, err = extension.Use[*coreproc.Manager](scope)
						if err != nil {
							return err
						}
						// A real local child must be reclaimed even though p.processes is still nil.
						if _, err = processes.Start(context.Background(), proc.Spec{Command: "shutdown-test"}, proc.Pipe(proc.ProcOptions{Binary: "/bin/sleep", Args: []string{"60"}})); err != nil {
							return err
						}
						close(entered)
						select {
						case <-ctx.Done():
							return ctx.Err()
						case <-release:
							return nil
						}
					},
					CloseFunc: func(context.Context) error {
						if listener == nil {
							return nil
						}
						resourceClosed = true
						return listener.Close()
					},
				}},
			})
			if err != nil {
				t.Fatal(err)
			}
			var load func(context.Context) error
			var finish func(*modelNodeReport) error
			if tc.product {
				p := &productModelProfile{Harness: h, capability: capability}
				load = p.Load
				finish = func(r *modelNodeReport) error { return closeProductModelProfiles([]*productModelProfile{p}, r) }
			} else {
				p := &modelNodeProfile{Harness: h, capability: capability}
				load = p.Load
				finish = func(r *modelNodeReport) error { return closeModelNodeProfiles([]*modelNodeProfile{p}, r) }
			}
			loaded := make(chan error, 1)
			go func() { loaded <- load(ctx) }()
			select {
			case <-entered:
			case err := <-loaded:
				t.Fatalf("Load returned before gate: %v", err)
			case <-time.After(10 * time.Second):
				t.Fatal("Load did not reach gate")
			}
			r := &modelNodeReport{}
			finished := make(chan error, 1)
			go func() { finished <- finish(r) }()
			select {
			case err := <-finished:
				t.Fatalf("Close returned before Load ended: %v", err)
			case <-time.After(20 * time.Millisecond):
			}
			var wantLoadErr error
			if tc.cancelLoad {
				wantLoadErr = context.Canceled
				cancel()
			} else {
				close(release)
			}
			select {
			case err := <-loaded:
				if !errors.Is(err, wantLoadErr) {
					t.Fatalf("Load: %v", err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("Load did not cancel")
			}
			select {
			case err := <-finished:
				if err != nil {
					t.Errorf("shutdown after Load: %v", err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("Close did not finish")
			}
			if r.Profiles != 1 || r.ProfilesClosed != 1 || !r.NoActiveProcesses {
				t.Errorf("shutdown report: %+v", r)
			}
			if !resourceClosed || h.Active() {
				t.Fatal("partially loaded resources were not closed")
			}
			for _, entry := range processes.List() {
				if entry.State.Active() {
					t.Errorf("partial Load leaked process: %s", entry.ID)
				}
			}
			if err := load(context.Background()); err == nil {
				t.Error("Load accepted after Close")
			}
			rebound, err := net.Listen("tcp", listener.Addr().String())
			if err != nil {
				t.Fatalf("listener leaked: %v", err)
			}
			rebound.Close()
		})
	}
}

func TestModelProfileStopBeforeLoad(t *testing.T) {
	for _, product := range []bool{true, false} {
		r := &modelNodeReport{}
		var err error
		var load func(context.Context) error
		if product {
			p := &productModelProfile{}
			load = p.Load
			err = closeProductModelProfiles([]*productModelProfile{p}, r)
		} else {
			p := &modelNodeProfile{}
			load = p.Load
			err = closeModelNodeProfiles([]*modelNodeProfile{p}, r)
		}
		if err != nil || r.ProfilesClosed != 1 || !r.NoActiveProcesses {
			t.Fatalf("product=%v report=%+v err=%v", product, r, err)
		}
		if err := load(t.Context()); err == nil {
			t.Fatal("Load accepted after Close")
		}
	}
}

// A closed marker alone must not conceal a live registry entry.
func TestModelProfileShutdownDetectsActiveResources(t *testing.T) {
	for _, product := range []bool{true, false} {
		manager := coreproc.NewManager()
		if _, err := manager.Start(t.Context(), proc.Spec{Command: "active-test"}, proc.Pipe(proc.ProcOptions{Binary: "/bin/sleep", Args: []string{"60"}})); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(manager.Shutdown)
		r := &modelNodeReport{}
		var err error
		if product {
			err = closeProductModelProfiles([]*productModelProfile{{closed: true, processes: manager}}, r)
		} else {
			err = closeModelNodeProfiles([]*modelNodeProfile{{closed: true, processes: manager}}, r)
		}
		if err == nil || !strings.Contains(err.Error(), "原生节点仍有活跃资源") || r.NoActiveProcesses {
			t.Fatalf("product=%v concealed live resource: report=%+v err=%v", product, r, err)
		}
		manager.Shutdown()
	}
}

// A nil Manager does not excuse a failed drain, nor mark the Profile as closed.
func TestModelProfileShutdownPreservesCloseError(t *testing.T) {
	for _, product := range []bool{true, false} {
		closeErr := errors.New("synthetic incomplete drain")
		set, err := extension.New(extension.Func{CloseFunc: func(context.Context) error { return errors.Join(extension.ErrCloseIncomplete, closeErr) }})
		if err != nil {
			t.Fatal(err)
		}
		if err := set.Load(t.Context()); err != nil {
			t.Fatal(err)
		}
		r := &modelNodeReport{}
		if product {
			err = closeProductModelProfiles([]*productModelProfile{{protocols: set}}, r)
		} else {
			err = closeModelNodeProfiles([]*modelNodeProfile{{protocols: set}}, r)
		}
		if !errors.Is(err, closeErr) || r.ProfilesClosed != 0 {
			t.Fatalf("product=%v lost close failure: report=%+v err=%v", product, r, err)
		}
	}
}
