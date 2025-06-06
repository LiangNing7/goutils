package retry

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	backoffSteps    = 10   // 最大重试数
	backoffFactor   = 1.25 // 每次重试的增长因子
	backoffDuration = 5    // 默认的初试重试等待时长
	backoffJitter   = 1.0  // 抖动系数
)

// Retry retries a given function with exponential backoff.
func Retry(fn wait.ConditionFunc, initialBackoffSec int) error {
	if initialBackoffSec <= 0 {
		initialBackoffSec = backoffDuration
	}
	backoffConfig := wait.Backoff{
		Steps:    backoffSteps,
		Factor:   backoffFactor,
		Duration: time.Duration(initialBackoffSec) * time.Second,
		Jitter:   backoffJitter,
	}
	// 调用 wait.ExponentialBackoff，将 backoffConfig 和用户传入的 fn 传递进去
	// ExponentialBackoff 会按照指数退避策略循环调用 fn，直到达到以下三种情况之一：
	// 1. fn 返回 (true, nil)，表示已成功，停止重试并返回 nil
	// 2. fn 返回 (false, err)，表示出现错误，停止重试并返回 err
	// 3. 重试次数达到 backoffConfig.Steps，仍未成功，返回 ErrWaitTimeout
	retryErr := wait.ExponentialBackoff(backoffConfig, fn)
	if retryErr != nil {
		return retryErr
	}
	return nil
}

// Poll 会以固定间隔 interval 调用 condition，直到满足以下三种情况之一：
//  1. condition 返回 (true, nil) —— 条件成立，返回 nil
//  2. condition 返回 (false, err) —— 出现错误，返回 err
//  3. 等待总时长超过 timeout —— 超时后返回 context.DeadlineExceeded
//
// 注意：原先的 wait.Poll 已被弃用，推荐使用 wait.PollUntilContextTimeout。
// 因此，这里我们创建一个带有 timeout 的 context，并调用 PollUntilContextTimeout。
// condition 无法直接接收 context，所以我们将其包裹到一个 wait.ConditionWithContextFunc 中。
func Poll(interval, timeout time.Duration, condition wait.ConditionFunc) error {
	// 使用 context.WithTimeout 包装一个带超时的 context
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// 将原来的 wait.ConditionFunc 包装为 wait.ConditionWithContextFunc，
	// 忽略传入的 Context 值，直接调用无参 condition()
	wrapped := func(ctx context.Context) (bool, error) {
		return condition()
	}

	// PollUntilContextTimeout 会：
	//   1. 每隔 interval 调用一次 wrapped(ctx)
	//   2. 如果 wrapped 返回 true，则返回 nil
	//   3. 如果 wrapped 返回 err，则返回该 err
	//   4. 如果 ctx 超时（DeadlineExceeded），则返回 ctx.Err()
	return wait.PollUntilContextTimeout(ctx, interval, timeout, false, wrapped)
}

// PollImmediate 会“立即”先调用一次 condition，如果 condition 返回 true，则直接返回 nil；
// 如果 condition 返回 (false, nil)，则进入与 Poll 相同的逻辑，以固定间隔调用 condition，直至超时或出错。
// 需要注意：为了与原来 wait.PollImmediate 保持一致，第一次检查之后再开始计时，
// 所以我们先执行一次 “立即” 检查，然后用第二个 context 计算剩余超时时间来调用 PollUntilContextTimeout。
func PollImmediate(interval, timeout time.Duration, condition wait.ConditionFunc) error {
	// 第一次“立即”检查
	done, err := condition()
	if err != nil || done {
		// 如果出错或者首次检查就已满足，直接返回
		return err
	}

	// 如果第一次检查未满足，则剩余的可用时间是 timeout 减去“立即检查”所耗费的极短时间
	// 因为调用 condition() 所耗时间通常很短，可以近似忽略不计，所以这里我们直接使用同样的 timeout。
	// 如果对精确度有更高要求，可以测量 elapsed 并减去，但对于大多数场景，此处忽略即可。

	// 使用 context.WithTimeout 包装一个带超时的 context
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// 复用与 Poll 相同的包装逻辑，将 condition 包装为带 Context 的函数
	wrapped := func(ctx context.Context) (bool, error) {
		return condition()
	}

	// 调用 PollUntilContextTimeout：第一次会在间隔 interval 之后调用 wrapped，
	// 之后每隔 interval 继续调用，直至 timeout 或 condition 返回 true/err。
	return wait.PollUntilContextTimeout(ctx, interval, timeout, true, wrapped)
}
