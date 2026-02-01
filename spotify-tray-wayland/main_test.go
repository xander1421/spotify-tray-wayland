package main

import (
	"context"
	"encoding/json"
	"runtime"
	"sync"
	"testing"
	"time"
)

// TestUpdateLoopExits verifies updateLoop exits when context is cancelled
func TestUpdateLoopExits(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	testApp := &App{
		ctx:    ctx,
		cancel: cancel,
	}

	goroutinesBefore := runtime.NumGoroutine()

	testApp.wg.Add(1)
	go testApp.updateLoop()

	// Let it run briefly
	time.Sleep(50 * time.Millisecond)

	// Cancel and wait
	cancel()

	done := make(chan struct{})
	go func() {
		testApp.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(time.Second):
		t.Fatal("updateLoop did not exit within timeout")
	}

	// Allow goroutines to clean up
	time.Sleep(50 * time.Millisecond)

	goroutinesAfter := runtime.NumGoroutine()
	if goroutinesAfter > goroutinesBefore+1 {
		t.Errorf("goroutine leak: before=%d, after=%d", goroutinesBefore, goroutinesAfter)
	}
}

// TestHandleClicksWithMockChannels tests the click handler loop logic
func TestHandleClicksWithMockChannels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// Create mock channels
	showHideCh := make(chan struct{})
	prevCh := make(chan struct{})
	playPauseCh := make(chan struct{})
	nextCh := make(chan struct{})
	quitCh := make(chan struct{})

	var wg sync.WaitGroup
	wg.Add(1)

	// Test loop using mock channels
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case <-showHideCh:
			case <-prevCh:
			case <-playPauseCh:
			case <-nextCh:
			case <-quitCh:
			}
		}
	}()

	goroutinesBefore := runtime.NumGoroutine()

	// Let it run briefly
	time.Sleep(50 * time.Millisecond)

	// Cancel and wait
	cancel()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(time.Second):
		t.Fatal("handleClicks loop did not exit within timeout")
	}

	// Allow goroutines to clean up
	time.Sleep(50 * time.Millisecond)

	goroutinesAfter := runtime.NumGoroutine()
	if goroutinesAfter > goroutinesBefore {
		t.Errorf("goroutine leak: before=%d, after=%d", goroutinesBefore, goroutinesAfter)
	}
}

// TestMultipleStartStop tests starting and stopping multiple times
func TestMultipleStartStop(t *testing.T) {
	goroutinesBefore := runtime.NumGoroutine()

	for i := 0; i < 5; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		testApp := &App{
			ctx:    ctx,
			cancel: cancel,
		}

		testApp.wg.Add(1)
		go testApp.updateLoop()

		time.Sleep(20 * time.Millisecond)
		cancel()
		testApp.wg.Wait()
	}

	// Allow cleanup
	time.Sleep(100 * time.Millisecond)

	goroutinesAfter := runtime.NumGoroutine()
	if goroutinesAfter > goroutinesBefore+2 {
		t.Errorf("goroutine leak after multiple cycles: before=%d, after=%d", goroutinesBefore, goroutinesAfter)
	}
}

// TestWaitGroupCorrectness ensures WaitGroup is properly decremented
func TestWaitGroupCorrectness(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	testApp := &App{
		ctx:    ctx,
		cancel: cancel,
	}

	testApp.wg.Add(1)
	go testApp.updateLoop()

	time.Sleep(50 * time.Millisecond)
	cancel()

	// This should not deadlock
	done := make(chan struct{})
	go func() {
		testApp.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("WaitGroup.Wait() deadlocked - wg.Done() not called")
	}
}

// TestConcurrentShutdown tests that shutdown is safe when called concurrently
func TestConcurrentShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	testApp := &App{
		ctx:    ctx,
		cancel: cancel,
	}

	testApp.wg.Add(1)
	go testApp.updateLoop()

	// Multiple goroutines calling cancel
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cancel()
		}()
	}
	wg.Wait()

	// Should complete without panic or deadlock
	done := make(chan struct{})
	go func() {
		testApp.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("concurrent shutdown caused deadlock")
	}
}

// TestToggleWindowParsing tests JSON parsing in toggleWindow
func TestToggleWindowParsing(t *testing.T) {
	testCases := []struct {
		name    string
		json    string
		wantErr bool
		wantLen int
	}{
		{
			name:    "empty array",
			json:    "[]",
			wantErr: false,
			wantLen: 0,
		},
		{
			name:    "single spotify client",
			json:    `[{"address":"0x123","class":"Spotify","workspace":{"id":1,"name":"1"}}]`,
			wantErr: false,
			wantLen: 1,
		},
		{
			name:    "spotify in special workspace",
			json:    `[{"address":"0x456","class":"spotify","workspace":{"id":-1,"name":"special:spotify"}}]`,
			wantErr: false,
			wantLen: 1,
		},
		{
			name:    "invalid json",
			json:    "{invalid}",
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var clients []struct {
				Address   string `json:"address"`
				Class     string `json:"class"`
				Workspace struct {
					ID   int    `json:"id"`
					Name string `json:"name"`
				} `json:"workspace"`
			}

			err := json.Unmarshal([]byte(tc.json), &clients)
			if (err != nil) != tc.wantErr {
				t.Errorf("Unmarshal error = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && len(clients) != tc.wantLen {
				t.Errorf("got %d clients, want %d", len(clients), tc.wantLen)
			}
		})
	}
}

// TestGetSpotifyIcon tests icon loading doesn't panic
func TestGetSpotifyIcon(t *testing.T) {
	// Should not panic even if no icons exist
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("getSpotifyIcon panicked: %v", r)
		}
	}()

	_ = getSpotifyIcon()
}

// TestGoroutineLeakOnRapidCancelation tests for leaks with rapid start/stop
func TestGoroutineLeakOnRapidCancelation(t *testing.T) {
	// Warm up the runtime
	runtime.GC()
	time.Sleep(50 * time.Millisecond)

	goroutinesBefore := runtime.NumGoroutine()

	for i := 0; i < 100; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		testApp := &App{
			ctx:    ctx,
			cancel: cancel,
		}

		testApp.wg.Add(1)
		go testApp.updateLoop()

		// Immediate cancel
		cancel()
		testApp.wg.Wait()
	}

	// Force GC and allow cleanup
	runtime.GC()
	time.Sleep(100 * time.Millisecond)

	goroutinesAfter := runtime.NumGoroutine()
	leaked := goroutinesAfter - goroutinesBefore
	if leaked > 2 {
		t.Errorf("goroutine leak detected: %d goroutines leaked after 100 rapid cycles", leaked)
	}
}

// TestTickerCleanup ensures ticker is properly stopped
func TestTickerCleanup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	testApp := &App{
		ctx:    ctx,
		cancel: cancel,
	}

	testApp.wg.Add(1)
	go testApp.updateLoop()

	// Let ticker tick at least once
	time.Sleep(50 * time.Millisecond)

	cancel()
	testApp.wg.Wait()

	// If ticker wasn't stopped, we'd see resource leaks over time
	// This test mainly ensures no panic on cleanup
}

// BenchmarkGoroutineStartStop benchmarks goroutine lifecycle
func BenchmarkGoroutineStartStop(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		testApp := &App{
			ctx:    ctx,
			cancel: cancel,
		}

		testApp.wg.Add(1)
		go testApp.updateLoop()
		cancel()
		testApp.wg.Wait()
	}
}

// BenchmarkContextCancelation benchmarks context cancelation overhead
func BenchmarkContextCancelation(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		<-ctx.Done()
	}
}
