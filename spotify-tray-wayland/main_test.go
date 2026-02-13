package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- MOCKS ---

type MockWindowManager struct {
	Clients       []HyprlandClient
	MoveCalls     []string
	FocusCalls    []string
	CloseCalls    []string
	LaunchCalled  bool
	GetClientsErr error
}

func (m *MockWindowManager) GetClients() ([]HyprlandClient, error) {
	if m.GetClientsErr != nil {
		return nil, m.GetClientsErr
	}
	return m.Clients, nil
}

func (m *MockWindowManager) FindSpotify() (HyprlandClient, bool) {
	for _, c := range m.Clients {
		if strings.EqualFold(c.Class, "spotify") {
			return c, true
		}
	}
	return HyprlandClient{}, false
}

func (m *MockWindowManager) MoveWindow(addr, workspace string) error {
	m.MoveCalls = append(m.MoveCalls, "move "+addr+" to "+workspace)
	return nil
}

func (m *MockWindowManager) FocusWindow(addr string) error {
	m.FocusCalls = append(m.FocusCalls, "focus "+addr)
	return nil
}

func (m *MockWindowManager) CloseWindow(class string) error {
	m.CloseCalls = append(m.CloseCalls, "close "+class)
	return nil
}

func (m *MockWindowManager) LaunchSpotify() error {
	m.LaunchCalled = true
	return nil
}

type MockMediaPlayer struct {
	Artist      string
	Title       string
	Status      string
	CallHistory []string
}

func (m *MockMediaPlayer) GetMetadata() (artist, title, status string, err error) {
	return m.Artist, m.Title, m.Status, nil
}

func (m *MockMediaPlayer) Call(method string) error {
	m.CallHistory = append(m.CallHistory, method)
	return nil
}

func (m *MockMediaPlayer) Quit() error {
	m.CallHistory = append(m.CallHistory, "Quit")
	return nil
}

func (m *MockMediaPlayer) Close() {}

// --- TOGGLE WINDOW TESTS ---

func TestToggleWindow_Unhide(t *testing.T) {
	mockWM := &MockWindowManager{
		Clients: []HyprlandClient{
			{
				Class:   "Spotify",
				Address: "0x123",
				Workspace: struct {
					ID   int
					Name string
				}{Name: "special:spotify"},
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	testApp := &App{
		wm:     mockWM,
		player: &MockMediaPlayer{},
		ctx:    ctx,
		cancel: cancel,
	}

	testApp.toggleWindow()

	if len(mockWM.MoveCalls) != 1 {
		t.Fatalf("Expected 1 move call, got %d", len(mockWM.MoveCalls))
	}

	expected := "move address:0x123 to e+0"
	if mockWM.MoveCalls[0] != expected {
		t.Errorf("Expected '%s', got '%s'", expected, mockWM.MoveCalls[0])
	}

	if len(mockWM.FocusCalls) != 1 {
		t.Errorf("Expected 1 focus call, got %d", len(mockWM.FocusCalls))
	}
}

func TestToggleWindow_Hide(t *testing.T) {
	mockWM := &MockWindowManager{
		Clients: []HyprlandClient{
			{
				Class:   "Spotify",
				Address: "0xABC",
				Workspace: struct {
					ID   int
					Name string
				}{Name: "1"},
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	testApp := &App{
		wm:     mockWM,
		player: &MockMediaPlayer{},
		ctx:    ctx,
		cancel: cancel,
	}

	testApp.toggleWindow()

	expected := "move address:0xABC to special:spotify"
	if len(mockWM.MoveCalls) == 0 || mockWM.MoveCalls[0] != expected {
		t.Errorf("Expected '%s', got %v", expected, mockWM.MoveCalls)
	}

	// Should not focus when hiding
	if len(mockWM.FocusCalls) != 0 {
		t.Errorf("Expected no focus calls when hiding, got %d", len(mockWM.FocusCalls))
	}
}

func TestToggleWindow_Launch(t *testing.T) {
	mockWM := &MockWindowManager{
		Clients: []HyprlandClient{},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	testApp := &App{
		wm:     mockWM,
		player: &MockMediaPlayer{},
		ctx:    ctx,
		cancel: cancel,
	}

	testApp.toggleWindow()

	if !mockWM.LaunchCalled {
		t.Error("Expected Spotify to be launched, but it wasn't")
	}
}

func TestToggleWindow_CaseInsensitive(t *testing.T) {
	testCases := []string{"spotify", "Spotify", "SPOTIFY", "SpOtIfY"}

	for _, className := range testCases {
		t.Run(className, func(t *testing.T) {
			mockWM := &MockWindowManager{
				Clients: []HyprlandClient{
					{
						Class:   className,
						Address: "0x999",
						Workspace: struct {
							ID   int
							Name string
						}{Name: "2"},
					},
				},
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			testApp := &App{
				wm:     mockWM,
				player: &MockMediaPlayer{},
				ctx:    ctx,
				cancel: cancel,
			}

			testApp.toggleWindow()

			if len(mockWM.MoveCalls) != 1 {
				t.Errorf("Class '%s': Expected 1 move call, got %d", className, len(mockWM.MoveCalls))
			}
		})
	}
}

// --- HYPRLAND PARSER TESTS ---

func TestParseHyprctlClients(t *testing.T) {
	input := `Window 560770936780 -> alex@myhostname:~:
	mapped: 1
	hidden: 0
	at: 2763,405
	size: 2662,1404
	workspace: -98 (special:scratchpad)
	floating: 1
	class: kitty
	title: alex@myhostname:~

Window 560770e3b350 -> Spotify:
	mapped: 1
	hidden: 0
	workspace: 1 (1)
	class: Spotify
	title: Spotify Premium
`

	clients := parseHyprctlClients([]byte(input))

	if len(clients) != 2 {
		t.Fatalf("Expected 2 clients, got %d", len(clients))
	}

	// First client
	if clients[0].Class != "kitty" {
		t.Errorf("Expected class 'kitty', got '%s'", clients[0].Class)
	}
	if clients[0].Address != "0x560770936780" {
		t.Errorf("Expected address '0x560770936780', got '%s'", clients[0].Address)
	}
	if clients[0].Workspace.Name != "special:scratchpad" {
		t.Errorf("Expected workspace 'special:scratchpad', got '%s'", clients[0].Workspace.Name)
	}
	if clients[0].Workspace.ID != -98 {
		t.Errorf("Expected workspace ID -98, got %d", clients[0].Workspace.ID)
	}

	// Second client (Spotify)
	if clients[1].Class != "Spotify" {
		t.Errorf("Expected class 'Spotify', got '%s'", clients[1].Class)
	}
	if clients[1].Workspace.Name != "1" {
		t.Errorf("Expected workspace '1', got '%s'", clients[1].Workspace.Name)
	}
}

func TestParseHyprctlClients_Empty(t *testing.T) {
	clients := parseHyprctlClients([]byte(""))
	if len(clients) != 0 {
		t.Errorf("Expected 0 clients from empty input, got %d", len(clients))
	}
}

func TestParseHyprctlClients_SingleClient(t *testing.T) {
	input := `Window abc123 -> Test Window:
	class: testclass
	workspace: 5 (myworkspace)
`
	clients := parseHyprctlClients([]byte(input))

	if len(clients) != 1 {
		t.Fatalf("Expected 1 client, got %d", len(clients))
	}
	if clients[0].Class != "testclass" {
		t.Errorf("Expected class 'testclass', got '%s'", clients[0].Class)
	}
}

// --- FAST PARSER TESTS ---

func TestParseHyprctlClientsFast(t *testing.T) {
	input := `Window 560770936780 -> alex@myhostname:~:
	mapped: 1
	hidden: 0
	at: 2763,405
	size: 2662,1404
	workspace: -98 (special:scratchpad)
	floating: 1
	class: kitty
	title: alex@myhostname:~

Window 560770e3b350 -> Spotify:
	mapped: 1
	hidden: 0
	workspace: 1 (1)
	class: Spotify
	title: Spotify Premium
`

	clients := parseHyprctlClientsFast([]byte(input))

	if len(clients) != 2 {
		t.Fatalf("Expected 2 clients, got %d", len(clients))
	}

	// First client
	if clients[0].Class != "kitty" {
		t.Errorf("Expected class 'kitty', got '%s'", clients[0].Class)
	}
	if clients[0].Address != "0x560770936780" {
		t.Errorf("Expected address '0x560770936780', got '%s'", clients[0].Address)
	}
	if clients[0].Workspace.Name != "special:scratchpad" {
		t.Errorf("Expected workspace 'special:scratchpad', got '%s'", clients[0].Workspace.Name)
	}
	if clients[0].Workspace.ID != -98 {
		t.Errorf("Expected workspace ID -98, got %d", clients[0].Workspace.ID)
	}

	// Second client (Spotify)
	if clients[1].Class != "Spotify" {
		t.Errorf("Expected class 'Spotify', got '%s'", clients[1].Class)
	}
	if clients[1].Workspace.Name != "1" {
		t.Errorf("Expected workspace '1', got '%s'", clients[1].Workspace.Name)
	}
}

func TestParseHyprctlClientsFast_Empty(t *testing.T) {
	clients := parseHyprctlClientsFast([]byte(""))
	if len(clients) != 0 {
		t.Errorf("Expected 0 clients from empty input, got %d", len(clients))
	}
}

func TestAllVersionsMatch(t *testing.T) {
	type parser struct {
		name string
		fn   func([]byte) []HyprlandClient
	}

	parsers := []parser{
		{"V1_Original", parseV1Original},
		{"V2_Bytes", parseV2Bytes},
		{"V3_Direct", parseV3Direct},
		{"V4_Unsafe", parseV4Unsafe},
		{"V5_Pooled", parseV5Pooled},
		{"V6_Unrolled", parseV6Unrolled},
		{"V7_Minimal", parseV7Minimal},
	}

	for _, n := range []int{1, 3, 10, 50} {
		input := generateHyprctlOutput(n)
		reference := parseV1Original(input)

		for _, p := range parsers[1:] { // Skip V1 (it's the reference)
			result := p.fn(input)

			if len(result) != len(reference) {
				t.Errorf("n=%d, %s: length mismatch: got %d, want %d",
					n, p.name, len(result), len(reference))
				continue
			}

			for i := range reference {
				if result[i].Class != reference[i].Class {
					t.Errorf("n=%d, %s, i=%d: class mismatch: got %q, want %q",
						n, p.name, i, result[i].Class, reference[i].Class)
				}
				if result[i].Address != reference[i].Address {
					t.Errorf("n=%d, %s, i=%d: address mismatch: got %q, want %q",
						n, p.name, i, result[i].Address, reference[i].Address)
				}
				if result[i].Workspace.Name != reference[i].Workspace.Name {
					t.Errorf("n=%d, %s, i=%d: workspace name mismatch: got %q, want %q",
						n, p.name, i, result[i].Workspace.Name, reference[i].Workspace.Name)
				}
				if result[i].Workspace.ID != reference[i].Workspace.ID {
					t.Errorf("n=%d, %s, i=%d: workspace ID mismatch: got %d, want %d",
						n, p.name, i, result[i].Workspace.ID, reference[i].Workspace.ID)
				}
			}
		}
	}
}

func TestParseHyprctlClientsFast_MatchesOriginal(t *testing.T) {
	// Test with various sizes
	for _, n := range []int{1, 3, 10, 50} {
		input := generateHyprctlOutput(n)

		original := parseHyprctlClients(input)
		fast := parseHyprctlClientsFast(input)

		if len(original) != len(fast) {
			t.Fatalf("n=%d: length mismatch: original=%d, fast=%d", n, len(original), len(fast))
		}

		for i := range original {
			if original[i].Class != fast[i].Class {
				t.Errorf("n=%d, i=%d: class mismatch: original=%q, fast=%q",
					n, i, original[i].Class, fast[i].Class)
			}
			if original[i].Address != fast[i].Address {
				t.Errorf("n=%d, i=%d: address mismatch: original=%q, fast=%q",
					n, i, original[i].Address, fast[i].Address)
			}
			if original[i].Workspace.Name != fast[i].Workspace.Name {
				t.Errorf("n=%d, i=%d: workspace name mismatch: original=%q, fast=%q",
					n, i, original[i].Workspace.Name, fast[i].Workspace.Name)
			}
			if original[i].Workspace.ID != fast[i].Workspace.ID {
				t.Errorf("n=%d, i=%d: workspace ID mismatch: original=%d, fast=%d",
					n, i, original[i].Workspace.ID, fast[i].Workspace.ID)
			}
		}
	}
}

// --- LIFECYCLE TESTS ---

func TestUpdateLoopExits(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	testApp := &App{
		ctx:    ctx,
		cancel: cancel,
		player: &MockMediaPlayer{},
	}

	goroutinesBefore := runtime.NumGoroutine()

	testApp.wg.Add(1)
	go testApp.updateLoop()

	time.Sleep(50 * time.Millisecond)
	cancel()

	done := make(chan struct{})
	go func() {
		testApp.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("updateLoop did not exit within timeout")
	}

	time.Sleep(50 * time.Millisecond)
	goroutinesAfter := runtime.NumGoroutine()
	if goroutinesAfter > goroutinesBefore+1 {
		t.Errorf("goroutine leak: before=%d, after=%d", goroutinesBefore, goroutinesAfter)
	}
}

func TestMultipleStartStop(t *testing.T) {
	goroutinesBefore := runtime.NumGoroutine()

	for i := 0; i < 5; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		testApp := &App{
			ctx:    ctx,
			cancel: cancel,
			player: &MockMediaPlayer{},
		}

		testApp.wg.Add(1)
		go testApp.updateLoop()

		time.Sleep(20 * time.Millisecond)
		cancel()
		testApp.wg.Wait()
	}

	time.Sleep(100 * time.Millisecond)
	goroutinesAfter := runtime.NumGoroutine()
	if goroutinesAfter > goroutinesBefore+2 {
		t.Errorf("goroutine leak after multiple cycles: before=%d, after=%d", goroutinesBefore, goroutinesAfter)
	}
}

func TestConcurrentShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	testApp := &App{
		ctx:    ctx,
		cancel: cancel,
		player: &MockMediaPlayer{},
	}

	testApp.wg.Add(1)
	go testApp.updateLoop()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cancel()
		}()
	}
	wg.Wait()

	done := make(chan struct{})
	go func() {
		testApp.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("concurrent shutdown caused deadlock")
	}
}

func TestGoroutineLeakOnRapidCancelation(t *testing.T) {
	runtime.GC()
	time.Sleep(50 * time.Millisecond)

	goroutinesBefore := runtime.NumGoroutine()

	for i := 0; i < 100; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		testApp := &App{
			ctx:    ctx,
			cancel: cancel,
			player: &MockMediaPlayer{},
		}

		testApp.wg.Add(1)
		go testApp.updateLoop()

		cancel()
		testApp.wg.Wait()
	}

	runtime.GC()
	time.Sleep(100 * time.Millisecond)

	goroutinesAfter := runtime.NumGoroutine()
	leaked := goroutinesAfter - goroutinesBefore
	if leaked > 2 {
		t.Errorf("goroutine leak detected: %d goroutines leaked after 100 rapid cycles", leaked)
	}
}

func TestGetSpotifyIcon(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("getSpotifyIcon panicked: %v", r)
		}
	}()

	_ = getSpotifyIcon()
}

// --- BENCHMARKS ---

// generateHyprctlOutput creates realistic hyprctl clients output
func generateHyprctlOutput(numClients int) []byte {
	var buf bytes.Buffer
	classes := []string{"kitty", "firefox", "Spotify", "code", "slack", "discord", "chromium", "nautilus"}
	workspaces := []string{"1", "2", "3", "special:scratchpad", "special:spotify"}

	for i := 0; i < numClients; i++ {
		addr := 0x560770936780 + i
		class := classes[i%len(classes)]
		ws := workspaces[i%len(workspaces)]
		wsID := (i % 5) + 1
		if len(ws) >= 7 && ws[:7] == "special" {
			wsID = -98 - (i % 3)
		}

		buf.WriteString(fmt.Sprintf("Window %x -> %s Window %d:\n", addr, class, i))
		buf.WriteString("\tmapped: 1\n")
		buf.WriteString("\thidden: 0\n")
		buf.WriteString(fmt.Sprintf("\tat: %d,%d\n", 100+i*10, 100+i*5))
		buf.WriteString(fmt.Sprintf("\tsize: %d,%d\n", 800+i, 600+i))
		buf.WriteString(fmt.Sprintf("\tworkspace: %d (%s)\n", wsID, ws))
		buf.WriteString("\tfloating: 0\n")
		buf.WriteString("\tpseudo: 0\n")
		buf.WriteString("\tpinned: 0\n")
		buf.WriteString("\tfullscreen: 0\n")
		buf.WriteString("\tfullscreenMode: 0\n")
		buf.WriteString("\tfakeFullscreen: 0\n")
		buf.WriteString(fmt.Sprintf("\tclass: %s\n", class))
		buf.WriteString(fmt.Sprintf("\ttitle: %s - Instance %d\n", class, i))
		buf.WriteString(fmt.Sprintf("\tinitialClass: %s\n", class))
		buf.WriteString(fmt.Sprintf("\tinitialTitle: %s\n", class))
		buf.WriteString(fmt.Sprintf("\tpid: %d\n", 1000+i))
		buf.WriteString("\txwayland: 0\n")
		buf.WriteString("\n")
	}
	return buf.Bytes()
}

func BenchmarkParseHyprctlClients_1(b *testing.B) {
	input := generateHyprctlOutput(1)
	b.SetBytes(int64(len(input)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = parseHyprctlClients(input)
	}
}

func BenchmarkParseHyprctlClients_3(b *testing.B) {
	input := generateHyprctlOutput(3)
	b.SetBytes(int64(len(input)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = parseHyprctlClients(input)
	}
}

func BenchmarkParseHyprctlClients_10(b *testing.B) {
	input := generateHyprctlOutput(10)
	b.SetBytes(int64(len(input)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = parseHyprctlClients(input)
	}
}

func BenchmarkParseHyprctlClients_50(b *testing.B) {
	input := generateHyprctlOutput(50)
	b.SetBytes(int64(len(input)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = parseHyprctlClients(input)
	}
}

func BenchmarkParseHyprctlClients_100(b *testing.B) {
	input := generateHyprctlOutput(100)
	b.SetBytes(int64(len(input)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = parseHyprctlClients(input)
	}
}

// BenchmarkParseHyprctlClients_Allocs measures memory allocations
func BenchmarkParseHyprctlClients_Allocs(b *testing.B) {
	sizes := []int{1, 10, 50, 100}
	for _, n := range sizes {
		input := generateHyprctlOutput(n)
		b.Run(fmt.Sprintf("clients_%d", n), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(input)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = parseHyprctlClients(input)
			}
		})
	}
}

// === FAST PARSER BENCHMARKS ===

func BenchmarkParseHyprctlClientsFast_1(b *testing.B) {
	input := generateHyprctlOutput(1)
	b.SetBytes(int64(len(input)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = parseHyprctlClientsFast(input)
	}
}

func BenchmarkParseHyprctlClientsFast_3(b *testing.B) {
	input := generateHyprctlOutput(3)
	b.SetBytes(int64(len(input)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = parseHyprctlClientsFast(input)
	}
}

func BenchmarkParseHyprctlClientsFast_10(b *testing.B) {
	input := generateHyprctlOutput(10)
	b.SetBytes(int64(len(input)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = parseHyprctlClientsFast(input)
	}
}

func BenchmarkParseHyprctlClientsFast_50(b *testing.B) {
	input := generateHyprctlOutput(50)
	b.SetBytes(int64(len(input)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = parseHyprctlClientsFast(input)
	}
}

func BenchmarkParseHyprctlClientsFast_100(b *testing.B) {
	input := generateHyprctlOutput(100)
	b.SetBytes(int64(len(input)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = parseHyprctlClientsFast(input)
	}
}

func BenchmarkParseHyprctlClientsFast_Allocs(b *testing.B) {
	sizes := []int{1, 10, 50, 100}
	for _, n := range sizes {
		input := generateHyprctlOutput(n)
		b.Run(fmt.Sprintf("clients_%d", n), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(input)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = parseHyprctlClientsFast(input)
			}
		})
	}
}

// === ALL VERSIONS COMPARISON ===

func BenchmarkAllVersions(b *testing.B) {
	sizes := []int{3, 10, 50, 100}

	type parser struct {
		name string
		fn   func([]byte) []HyprlandClient
	}

	parsers := []parser{
		{"V1_Original", parseV1Original},
		{"V2_Bytes", parseV2Bytes},
		{"V3_Direct", parseV3Direct},
		{"V4_Unsafe", parseV4Unsafe},
		{"V5_Pooled", parseV5Pooled},
		{"V6_Unrolled", parseV6Unrolled},
		{"V7_Minimal", parseV7Minimal},
	}

	for _, n := range sizes {
		input := generateHyprctlOutput(n)

		for _, p := range parsers {
			b.Run(fmt.Sprintf("%s/clients_%d", p.name, n), func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(len(input)))
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					_ = p.fn(input)
				}
			})
		}
	}
}

// BenchmarkVersionsDetailed provides per-operation metrics
func BenchmarkVersionsDetailed(b *testing.B) {
	input := generateHyprctlOutput(10) // Typical desktop: 10 windows

	b.Run("V1_Original_bufio+strings", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(input)))
		for i := 0; i < b.N; i++ {
			_ = parseV1Original(input)
		}
	})

	b.Run("V2_Bytes_bufio+bytes", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(input)))
		for i := 0; i < b.N; i++ {
			_ = parseV2Bytes(input)
		}
	})

	b.Run("V3_Direct_no_bufio", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(input)))
		for i := 0; i < b.N; i++ {
			_ = parseV3Direct(input)
		}
	})

	b.Run("V4_Unsafe_zero_copy", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(input)))
		for i := 0; i < b.N; i++ {
			_ = parseV4Unsafe(input)
		}
	})

	b.Run("V5_Pooled_sync.Pool", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(input)))
		for i := 0; i < b.N; i++ {
			_ = parseV5Pooled(input)
		}
	})

	b.Run("V6_Unrolled_4x_loop", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(input)))
		for i := 0; i < b.N; i++ {
			_ = parseV6Unrolled(input)
		}
	})

	b.Run("V7_Minimal_skip_fields", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(input)))
		for i := 0; i < b.N; i++ {
			_ = parseV7Minimal(input)
		}
	})
}

// === ADVANCED VERSIONS (V8-V11) ===

func BenchmarkAdvancedVersions(b *testing.B) {
	sizes := []int{3, 10, 50, 100}

	for _, n := range sizes {
		input := generateHyprctlOutput(n)

		b.Run(fmt.Sprintf("V5_Pooled/clients_%d", n), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(input)))
			for i := 0; i < b.N; i++ {
				_ = parseV5Pooled(input)
			}
		})

		b.Run(fmt.Sprintf("V7_Minimal/clients_%d", n), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(input)))
			for i := 0; i < b.N; i++ {
				_ = parseV7Minimal(input)
			}
		})

		b.Run(fmt.Sprintf("V8_Stack/clients_%d", n), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(input)))
			for i := 0; i < b.N; i++ {
				_ = parseV8Stack(input)
			}
		})

		b.Run(fmt.Sprintf("V9_Iterator/clients_%d", n), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(input)))
			for i := 0; i < b.N; i++ {
				// Consume all clients to measure full parse
				for client := range parseV9Iterator(input) {
					_ = client
				}
			}
		})

		b.Run(fmt.Sprintf("V10_SWAR/clients_%d", n), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(input)))
			for i := 0; i < b.N; i++ {
				_ = parseV10SWAR(input)
			}
		})
	}
}

// BenchmarkFindSpotify measures the real use case: finding Spotify window
func BenchmarkFindSpotify(b *testing.B) {
	// Generate data where Spotify is at different positions
	positions := []struct {
		name     string
		generate func() []byte
	}{
		{"first", func() []byte {
			// Spotify is first window
			return generateHyprctlOutputWithSpotifyAt(10, 0)
		}},
		{"middle", func() []byte {
			// Spotify is 5th of 10 windows
			return generateHyprctlOutputWithSpotifyAt(10, 4)
		}},
		{"last", func() []byte {
			// Spotify is last of 10 windows
			return generateHyprctlOutputWithSpotifyAt(10, 9)
		}},
		{"not_found", func() []byte {
			// Spotify not present
			return generateHyprctlOutput(10)
		}},
	}

	for _, pos := range positions {
		input := pos.generate()

		b.Run(fmt.Sprintf("V5_Pooled_%s", pos.name), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				clients := parseV5Pooled(input)
				for _, c := range clients {
					if c.Class == "Spotify" {
						break
					}
				}
			}
		})

		b.Run(fmt.Sprintf("V9_Iterator_%s", pos.name), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, _ = FindSpotify(input)
			}
		})

		b.Run(fmt.Sprintf("V11_Direct_%s", pos.name), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, _ = parseV11SpotifyOnly(input)
			}
		})
	}
}

// generateHyprctlOutputWithSpotifyAt creates output with Spotify at specific position
// Use spotifyPos = -1 to generate output without Spotify
func generateHyprctlOutputWithSpotifyAt(numClients, spotifyPos int) []byte {
	var buf bytes.Buffer
	classes := []string{"kitty", "firefox", "code", "slack", "discord", "chromium", "nautilus", "vlc"}
	workspaces := []string{"1", "2", "3", "special:scratchpad"}

	for i := 0; i < numClients; i++ {
		addr := 0x560770936780 + i
		class := classes[i%len(classes)]
		if spotifyPos >= 0 && i == spotifyPos {
			class = "Spotify"
		}
		ws := workspaces[i%len(workspaces)]
		wsID := (i % 4) + 1
		if len(ws) >= 7 && ws[:7] == "special" {
			wsID = -98
		}

		buf.WriteString(fmt.Sprintf("Window %x -> %s Window %d:\n", addr, class, i))
		buf.WriteString("\tmapped: 1\n")
		buf.WriteString("\thidden: 0\n")
		buf.WriteString(fmt.Sprintf("\tat: %d,%d\n", 100+i*10, 100+i*5))
		buf.WriteString(fmt.Sprintf("\tsize: %d,%d\n", 800+i, 600+i))
		buf.WriteString(fmt.Sprintf("\tworkspace: %d (%s)\n", wsID, ws))
		buf.WriteString("\tfloating: 0\n")
		buf.WriteString(fmt.Sprintf("\tclass: %s\n", class))
		buf.WriteString(fmt.Sprintf("\ttitle: %s - Instance %d\n", class, i))
		buf.WriteString(fmt.Sprintf("\tpid: %d\n", 1000+i))
		buf.WriteString("\n")
	}
	return buf.Bytes()
}

// === V12: Socket vs Exec Benchmarks (requires running Hyprland) ===

func BenchmarkExecVsSocket(b *testing.B) {
	// Skip if not running under Hyprland
	if os.Getenv("HYPRLAND_INSTANCE_SIGNATURE") == "" {
		b.Skip("Not running under Hyprland")
	}

	b.Run("Exec_hyprctl", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			out, err := exec.Command("hyprctl", "clients").Output()
			if err != nil {
				b.Fatal(err)
			}
			_ = parseV7Minimal(out)
		}
	})

	// V12: Socket-based HyprlandManager
	wm := NewHyprlandManager()

	b.Run("Socket_direct", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, err := wm.GetClients()
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("Socket_FindSpotify", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _ = wm.FindSpotify()
		}
	})
}

// BenchmarkRealWorldToggle measures the actual toggle operation
func BenchmarkRealWorldToggle(b *testing.B) {
	if os.Getenv("HYPRLAND_INSTANCE_SIGNATURE") == "" {
		b.Skip("Not running under Hyprland")
	}

	// Old way: exec.Command
	b.Run("Exec_GetClients", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			out, _ := exec.Command("hyprctl", "clients").Output()
			clients := parseV7Minimal(out)
			for _, c := range clients {
				if c.Class == "Spotify" || c.Class == "spotify" {
					_ = c.Address
					break
				}
			}
		}
	})

	// New way: V12 socket + V11 search
	wm := NewHyprlandManager()

	b.Run("Socket_FindSpotify", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			client, found := wm.FindSpotify()
			if found {
				_ = client.Address
			}
		}
	})
}

func BenchmarkToggleWindow(b *testing.B) {
	mockWM := &MockWindowManager{
		Clients: []HyprlandClient{
			{Class: "Spotify", Address: "0x123", Workspace: struct {
				ID   int
				Name string
			}{Name: "1"}},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	testApp := &App{
		wm:     mockWM,
		player: &MockMediaPlayer{},
		ctx:    ctx,
		cancel: cancel,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mockWM.MoveCalls = nil
		testApp.toggleWindow()
	}
}
