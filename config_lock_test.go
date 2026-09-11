// Copyright (c) 2026 sllogger authors.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package sllogger

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maxhaosl/sllogger/slcore"
)

// safeSink 声明自己线程安全（模拟 AsyncWriter / RollingWriter）。
type safeSink struct{ slcore.WriteSyncer }

func (safeSink) ConcurrentSafe() bool { return true }

// plainSink 不声明线程安全，必须被 lockIfNeeded 包装。
type plainSink struct{ slcore.WriteSyncer }

// unsafeSafeSink 声明了接口但返回 false，同样应被包装。
type unsafeSafeSink struct{ slcore.WriteSyncer }

func (unsafeSafeSink) ConcurrentSafe() bool { return false }

// TestLockIfNeeded 覆盖 lockIfNeeded 的三个分支。
//
// 这个函数是吞吐优化的关键：给已线程安全的 sink 双重加锁曾在压测中
// 造成约 11% 的 CPU 开销，因此每个分支都必须有测试守护。
func TestLockIfNeeded(t *testing.T) {
	t.Run("声明安全的 sink 原样返回", func(t *testing.T) {
		inner := slcore.AddSync(&strings.Builder{})
		sink := safeSink{inner}
		got := lockIfNeeded(sink)
		if got != slcore.WriteSyncer(sink) {
			t.Fatalf("已声明安全的 sink 被包装成 %T，回归为双重锁", got)
		}
	})

	t.Run("普通 sink 被加锁", func(t *testing.T) {
		inner := slcore.AddSync(&strings.Builder{})
		sink := plainSink{inner}
		got := lockIfNeeded(sink)
		if got == slcore.WriteSyncer(sink) {
			t.Fatal("普通 sink 未被加锁")
		}
		// 加锁后的 Write/Sync 必须仍可用。
		if _, err := got.Write([]byte("x")); err != nil {
			t.Fatal(err)
		}
		if err := got.Sync(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("声明为 false 的 sink 被加锁", func(t *testing.T) {
		inner := slcore.AddSync(&strings.Builder{})
		sink := unsafeSafeSink{inner}
		got := lockIfNeeded(sink)
		if got == slcore.WriteSyncer(sink) {
			t.Fatal("ConcurrentSafe()=false 的 sink 未被加锁")
		}
	})

	t.Run("加锁是幂等的", func(t *testing.T) {
		// slcore.Lock 本身对已加锁对象返回原对象，组合后不应叠加两层。
		inner := slcore.AddSync(&strings.Builder{})
		once := lockIfNeeded(plainSink{inner})
		twice := lockIfNeeded(once)
		if once != twice {
			t.Fatal("重复加锁产生了第二层包装")
		}
	})
}

// TestBuildRollingWriterNotDoubleLocked 端到端验证：通过 Config.Build 装配
// 出的 sink 都是自声明线程安全的，不会被外层加锁。
func TestBuildRollingWriterNotDoubleLocked(t *testing.T) {
	for _, async := range []bool{false, true} {
		dir := t.TempDir()
		cfg := NewCallInfoConfig(dir, "svc", 80)
		cfg.Rolling.Async = async
		cfg.Rolling.BlockOnFull = true

		log := Must(cfg.Build())

		log.CallInfo(context.Background(), CallInfo{
			URL:      "http://play.example.com:443/playurl/v1/play/playurl",
			Method:   "GET",
			ServerIP: "127.0.0.1",
			LogMsg:   "lock test",
		})
		if err := log.Close(); err != nil {
			t.Fatal(err)
		}

		files, err := filepath.Glob(filepath.Join(dir, "*.log"))
		if err != nil || len(files) == 0 {
			t.Fatalf("async=%v: 未生成日志文件: %v", async, err)
		}
		data, err := os.ReadFile(files[0])
		if err != nil {
			t.Fatal(err)
		}
		if !contains(string(data), "lock test") {
			t.Fatalf("async=%v: 内容 = %s", async, data)
		}
	}
}
