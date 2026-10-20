package distlock

import (
	"context"
	"net"
	"testing"
	"time"
)

// TestEtcdTTLRoundsUp asserts the lease TTL is never shorter than the configured
// timeout, given etcd leases are expressed in whole seconds.
func TestEtcdTTLRoundsUp(t *testing.T) {
	for timeout, want := range map[time.Duration]int{
		time.Second:             1,
		1500 * time.Millisecond: 2,
		10 * time.Second:        10,
		100 * time.Millisecond:  1, // defensive; constructors reject this
		-time.Second:            1, // defensive; constructors reject this
	} {
		if got := etcdTTL(timeout); got != want {
			t.Fatalf("etcdTTL(%s) = %d, want %d", timeout, got, want)
		}
	}
}

// TestEtcdLockerIsNotVerifiedAgainstARealCluster is a placeholder that documents
// the coverage gap instead of pretending there is coverage.
//
// EtcdLocker delegates ownership to go.etcd.io/etcd/client/v3/concurrency, whose
// Session/Mutex types wrap a concrete *clientv3.Client. There is no seam to
// inject a fake, and standing up a real etcd (or the embedded etcd server, which
// would add the etcdserver dependency tree) is out of scope for this change.
// Consequently:
//
//   - the constructor validation IS tested (see TestInvalidLockTimeoutIsRejected);
//   - the lock/unlock/renew behaviour is NOT verified at all.
//
// The renaming of this test to something that asserts behaviour is the signal
// that a real cluster test was added.
func TestEtcdLockerIsNotVerifiedAgainstARealCluster(t *testing.T) {
	t.Skip("EtcdLocker has no unit-testable seam; verified by compilation only. See the README coverage table.")
}

// A listening endpoint that never speaks gRPC makes lease creation block until
// the acquisition context is canceled, without requiring an etcd cluster.
func TestEtcdLockRespectsAcquisitionDeadline(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	locker, err := NewEtcdLocker([]string{"http://" + listener.Addr().String()})
	if err != nil {
		t.Fatal(err)
	}
	defer locker.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- locker.Lock(ctx) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected deadline error")
		}
	case <-time.After(2 * time.Second):
		// Closing the client unblocks even the broken implementation.
		_ = locker.cli.Close()
		<-done
		t.Fatal("Lock ignored acquisition deadline")
	}
}
