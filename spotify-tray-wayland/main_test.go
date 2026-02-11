package main

import (
	"context"
	"runtime"
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

func BenchmarkParseHyprctlClients(b *testing.B) {
	input := []byte(`Window 560770936780 -> Window 1:
	class: class1
	workspace: 1 (workspace1)

Window 560770936781 -> Window 2:
	class: class2
	workspace: 2 (workspace2)

Window 560770936782 -> Window 3:
	class: Spotify
	workspace: -99 (special:spotify)
`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = parseHyprctlClients(input)
	}
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
