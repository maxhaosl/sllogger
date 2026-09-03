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

package writer

import (
	"sync"
	"testing"
)

// TestConcurrentSafeMarkers 验证两个 writer 都声明自己线程安全。
//
// Config.Build 依赖这个标记来避免给已经线程安全的 sink 再套一层互斥锁
// （双重加锁在压测中约占 11% CPU）。若实现误改为 false，吞吐会回退，
// 因此这里必须断言。
func TestConcurrentSafeMarkers(t *testing.T) {
	cfg, _ := testConfig(t)

	rw, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()
	if !rw.ConcurrentSafe() {
		t.Fatal("RollingWriter.ConcurrentSafe() = false, want true")
	}

	aw := NewAsyncWriter(rw, cfg)
	defer aw.Close()
	if !aw.ConcurrentSafe() {
		t.Fatal("AsyncWriter.ConcurrentSafe() = false, want true")
	}
}

// TestConcurrentWritesAreSerialized 验证并发安全声明与实际行为一致：
// 多线程同时写入不能产生数据竞争或内容错乱（配合 -race 使用）。
func TestConcurrentWritesAreSerialized(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.MaxSize = 0
	cfg.RotationInterval = 0

	rw, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()

	const (
		goroutines = 16
		perRoutine = 100
	)
	// 每个协程写入可识别的定长内容，便于校验没有交叉污染。
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			line := make([]byte, 0, 8)
			for i := 0; i < perRoutine; i++ {
				line = line[:0]
				line = append(line, byte('A'+g%26))
				line = append(line, "------\n"...) // 1 字母 + 6 横杠 + 换行
				if _, err := rw.Write(line); err != nil {
					t.Error(err)
					return
				}
			}
		}(g)
	}
	wg.Wait()

	// 每行的结构是：1 个标识字母 + 6 个 '-' + '\n'。
	// 若写入被并发打断，就会出现错位（如字母出现在行尾）或长度异常。
	data := readAll(t, rw.path)
	const lineLen = 8
	for i := 0; i+lineLen <= len(data); i += lineLen {
		line := data[i : i+lineLen]
		if line[0] < 'A' || line[0] > 'Z' {
			t.Fatalf("行首不是标识字母（写入被交叉打断）: %q at offset %d", line, i)
		}
		for j := 1; j <= 6; j++ {
			if line[j] != '-' {
				t.Fatalf("行内容错位（写入被交叉打断）: %q at offset %d", line, i)
			}
		}
		if line[7] != '\n' {
			t.Fatalf("行未以换行结尾: %q at offset %d", line, i)
		}
	}
	if got := len(data); got != goroutines*perRoutine*lineLen {
		t.Fatalf("总字节 = %d, want %d", got, goroutines*perRoutine*lineLen)
	}
}
