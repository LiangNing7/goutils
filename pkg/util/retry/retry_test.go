package retry

import (
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
)

// TestPollImmediateFirstCallSucceeds 断言首次 condition 就满足时立即返回 nil，且只调用一次.
func TestPollImmediateFirstCallSucceeds(t *testing.T) {
	calls := 0
	err := PollImmediate(time.Millisecond, time.Second, func() (bool, error) {
		calls++
		return true, nil
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("condition called %d times, want 1", calls)
	}
}

// TestPollImmediateNoImmediateDoubleCall 断言 PollImmediate 不会在起始处连续调用两次 condition：
// 第一次“立即”检查之后，后续调用应至少间隔 interval.
func TestPollImmediateNoImmediateDoubleCall(t *testing.T) {
	const interval = 100 * time.Millisecond

	var times []time.Time
	condition := func() (bool, error) {
		times = append(times, time.Now())
		return false, nil
	}

	// 超时后返回 DeadlineExceeded，这里只关心调用时序.
	_ = PollImmediate(interval, 300*time.Millisecond, condition)

	if len(times) < 2 {
		t.Fatalf("condition called %d times, want >= 2", len(times))
	}
	if gap := times[1].Sub(times[0]); gap < 50*time.Millisecond {
		t.Fatalf("condition called twice back-to-back at start: gap between call 1 and 2 = %v, want >= ~%v", gap, interval)
	}
}

// TestPollImmediateEventualSuccess 断言未立即满足时会按间隔重试，最终成功返回 nil.
func TestPollImmediateEventualSuccess(t *testing.T) {
	calls := 0
	err := PollImmediate(5*time.Millisecond, time.Second, func() (bool, error) {
		calls++
		return calls == 3, nil
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if calls != 3 {
		t.Fatalf("condition called %d times, want 3", calls)
	}
}

// TestPollEventualSuccess 断言 Poll 会按固定间隔重试直到条件成立.
func TestPollEventualSuccess(t *testing.T) {
	calls := 0
	err := Poll(5*time.Millisecond, time.Second, func() (bool, error) {
		calls++
		return calls == 3, nil
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if calls != 3 {
		t.Fatalf("condition called %d times, want 3", calls)
	}
}

// TestPollTimeout 断言超时会返回错误.
func TestPollTimeout(t *testing.T) {
	err := Poll(5*time.Millisecond, 30*time.Millisecond, func() (bool, error) {
		return false, nil
	})
	if err == nil {
		t.Fatal("expected a timeout error, got nil")
	}
}

var _ wait.ConditionFunc = func() (bool, error) { return true, nil }
