package main

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"
	"sync"
	"unsafe"
)

// ============================================================================
// VERSION 1: Original (bufio.Scanner + strings)
// ============================================================================

func parseV1Original(data []byte) []HyprlandClient {
	var clients []HyprlandClient
	var current *HyprlandClient

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "Window ") {
			if current != nil {
				clients = append(clients, *current)
			}
			current = &HyprlandClient{}

			parts := strings.SplitN(line, " -> ", 2)
			if len(parts) >= 1 {
				addrPart := strings.TrimPrefix(parts[0], "Window ")
				current.Address = "0x" + strings.TrimSpace(addrPart)
			}
			if len(parts) >= 2 {
				current.Title = strings.TrimSuffix(parts[1], ":")
			}
			continue
		}

		if current == nil {
			continue
		}

		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "class: ") {
			current.Class = strings.TrimPrefix(line, "class: ")
		} else if strings.HasPrefix(line, "workspace: ") {
			ws := strings.TrimPrefix(line, "workspace: ")
			if idx := strings.Index(ws, " ("); idx != -1 {
				idStr := ws[:idx]
				fmt.Sscanf(idStr, "%d", &current.Workspace.ID)
				if endIdx := strings.LastIndex(ws, ")"); endIdx > idx {
					current.Workspace.Name = ws[idx+2 : endIdx]
				}
			}
		}
	}

	if current != nil {
		clients = append(clients, *current)
	}

	return clients
}

// ============================================================================
// VERSION 2: Bytes-based (bufio.Scanner.Bytes, no string allocs)
// ============================================================================

func parseV2Bytes(data []byte) []HyprlandClient {
	var clients []HyprlandClient
	var current *HyprlandClient

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Bytes()

		if len(line) >= 7 && bytes.HasPrefix(line, []byte("Window ")) {
			if current != nil {
				clients = append(clients, *current)
			}
			current = &HyprlandClient{}

			rest := line[7:]
			if idx := bytes.Index(rest, []byte(" -> ")); idx >= 0 {
				current.Address = "0x" + string(rest[:idx])
				title := rest[idx+4:]
				if len(title) > 0 && title[len(title)-1] == ':' {
					title = title[:len(title)-1]
				}
				current.Title = string(title)
			}
			continue
		}

		if current == nil {
			continue
		}

		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		if len(line) >= 7 && bytes.HasPrefix(line, []byte("class: ")) {
			current.Class = string(line[7:])
		} else if len(line) >= 11 && bytes.HasPrefix(line, []byte("workspace: ")) {
			ws := line[11:]
			if idx := bytes.IndexByte(ws, '('); idx > 0 {
				current.Workspace.ID = atoiBytes(ws[:idx-1])
				if endIdx := bytes.LastIndexByte(ws, ')'); endIdx > idx {
					current.Workspace.Name = string(ws[idx+1 : endIdx])
				}
			}
		}
	}

	if current != nil {
		clients = append(clients, *current)
	}

	return clients
}

func atoiBytes(b []byte) int {
	n := 0
	neg := false
	i := 0
	if len(b) > 0 && b[0] == '-' {
		neg = true
		i = 1
	}
	for ; i < len(b); i++ {
		if b[i] >= '0' && b[i] <= '9' {
			n = n*10 + int(b[i]-'0')
		}
	}
	if neg {
		return -n
	}
	return n
}

// ============================================================================
// VERSION 3: Direct scan (no bufio.Scanner)
// ============================================================================

func parseV3Direct(data []byte) []HyprlandClient {
	if len(data) == 0 {
		return nil
	}

	// Count windows for pre-allocation
	count := 0
	for i := 0; i < len(data)-6; i++ {
		if data[i] == 'W' && data[i+1] == 'i' && data[i+2] == 'n' &&
			data[i+3] == 'd' && data[i+4] == 'o' && data[i+5] == 'w' {
			count++
		}
	}

	clients := make([]HyprlandClient, 0, count)
	idx := -1

	lineStart := 0
	for i := 0; i <= len(data); i++ {
		if i < len(data) && data[i] != '\n' {
			continue
		}

		line := data[lineStart:i]
		lineStart = i + 1

		if len(line) == 0 {
			continue
		}

		// Window line
		if len(line) >= 7 && line[0] == 'W' && line[6] == ' ' {
			clients = append(clients, HyprlandClient{})
			idx = len(clients) - 1

			rest := line[7:]
			for j := 0; j < len(rest)-3; j++ {
				if rest[j] == ' ' && rest[j+1] == '-' && rest[j+2] == '>' {
					clients[idx].Address = "0x" + string(rest[:j])
					title := rest[j+4:]
					if len(title) > 0 && title[len(title)-1] == ':' {
						title = title[:len(title)-1]
					}
					clients[idx].Title = string(title)
					break
				}
			}
			continue
		}

		if idx < 0 {
			continue
		}

		// Trim whitespace
		for len(line) > 0 && (line[0] == '\t' || line[0] == ' ') {
			line = line[1:]
		}

		if len(line) >= 7 && line[0] == 'c' && line[5] == ':' {
			clients[idx].Class = string(line[7:])
		} else if len(line) >= 11 && line[0] == 'w' && line[9] == ':' {
			ws := line[11:]
			for j := 0; j < len(ws); j++ {
				if ws[j] == '(' {
					clients[idx].Workspace.ID = atoiBytes(ws[:j-1])
					for k := len(ws) - 1; k > j; k-- {
						if ws[k] == ')' {
							clients[idx].Workspace.Name = string(ws[j+1 : k])
							break
						}
					}
					break
				}
			}
		}
	}

	return clients
}

// ============================================================================
// VERSION 4: Unsafe (zero-copy strings)
// ============================================================================

func parseV4Unsafe(data []byte) []HyprlandClient {
	if len(data) == 0 {
		return nil
	}

	count := 0
	for i := 0; i < len(data)-6; i++ {
		if data[i] == 'W' && data[i+1] == 'i' && data[i+2] == 'n' &&
			data[i+3] == 'd' && data[i+4] == 'o' && data[i+5] == 'w' {
			count++
		}
	}

	clients := make([]HyprlandClient, 0, count)
	idx := -1

	lineStart := 0
	for i := 0; i <= len(data); i++ {
		if i < len(data) && data[i] != '\n' {
			continue
		}

		line := data[lineStart:i]
		lineStart = i + 1

		if len(line) == 0 {
			continue
		}

		if len(line) >= 7 && line[0] == 'W' && line[6] == ' ' {
			clients = append(clients, HyprlandClient{})
			idx = len(clients) - 1

			rest := line[7:]
			for j := 0; j < len(rest)-3; j++ {
				if rest[j] == ' ' && rest[j+1] == '-' && rest[j+2] == '>' {
					clients[idx].Address = "0x" + unsafeString(rest[:j])
					title := rest[j+4:]
					if len(title) > 0 && title[len(title)-1] == ':' {
						title = title[:len(title)-1]
					}
					clients[idx].Title = unsafeString(title)
					break
				}
			}
			continue
		}

		if idx < 0 {
			continue
		}

		for len(line) > 0 && (line[0] == '\t' || line[0] == ' ') {
			line = line[1:]
		}

		if len(line) >= 7 && line[0] == 'c' && line[5] == ':' {
			clients[idx].Class = unsafeString(line[7:])
		} else if len(line) >= 11 && line[0] == 'w' && line[9] == ':' {
			ws := line[11:]
			for j := 0; j < len(ws); j++ {
				if ws[j] == '(' {
					clients[idx].Workspace.ID = atoiBytes(ws[:j-1])
					for k := len(ws) - 1; k > j; k-- {
						if ws[k] == ')' {
							clients[idx].Workspace.Name = unsafeString(ws[j+1 : k])
							break
						}
					}
					break
				}
			}
		}
	}

	return clients
}

func unsafeString(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return unsafe.String(unsafe.SliceData(b), len(b))
}

// ============================================================================
// VERSION 5: Pooled (sync.Pool for slice reuse)
// ============================================================================

var v5Pool = sync.Pool{
	New: func() any {
		s := make([]HyprlandClient, 0, 16)
		return &s
	},
}

func parseV5Pooled(data []byte) []HyprlandClient {
	if len(data) == 0 {
		return nil
	}

	clientsPtr, ok := v5Pool.Get().(*[]HyprlandClient)
	if !ok || clientsPtr == nil || *clientsPtr == nil {
		s := make([]HyprlandClient, 0, 16)
		clientsPtr = &s
	}
	clients := (*clientsPtr)[:0]

	idx := -1

	lineStart := 0
	for i := 0; i <= len(data); i++ {
		if i < len(data) && data[i] != '\n' {
			continue
		}

		line := data[lineStart:i]
		lineStart = i + 1

		if len(line) == 0 {
			continue
		}

		if len(line) >= 7 && line[0] == 'W' && line[6] == ' ' {
			clients = append(clients, HyprlandClient{})
			idx = len(clients) - 1

			rest := line[7:]
			for j := 0; j < len(rest)-3; j++ {
				if rest[j] == ' ' && rest[j+1] == '-' && rest[j+2] == '>' {
					clients[idx].Address = "0x" + unsafeString(rest[:j])
					title := rest[j+4:]
					if len(title) > 0 && title[len(title)-1] == ':' {
						title = title[:len(title)-1]
					}
					clients[idx].Title = unsafeString(title)
					break
				}
			}
			continue
		}

		if idx < 0 {
			continue
		}

		for len(line) > 0 && (line[0] == '\t' || line[0] == ' ') {
			line = line[1:]
		}

		if len(line) >= 7 && line[0] == 'c' && line[5] == ':' {
			clients[idx].Class = unsafeString(line[7:])
		} else if len(line) >= 11 && line[0] == 'w' && line[9] == ':' {
			ws := line[11:]
			for j := 0; j < len(ws); j++ {
				if ws[j] == '(' {
					clients[idx].Workspace.ID = atoiBytes(ws[:j-1])
					for k := len(ws) - 1; k > j; k-- {
						if ws[k] == ')' {
							clients[idx].Workspace.Name = unsafeString(ws[j+1 : k])
							break
						}
					}
					break
				}
			}
		}
	}

	// Copy result before returning to pool
	result := make([]HyprlandClient, len(clients))
	copy(result, clients)

	*clientsPtr = clients
	v5Pool.Put(clientsPtr)

	return result
}

// ============================================================================
// VERSION 6: Unrolled (4x loop unrolling for line finding)
// ============================================================================

func parseV6Unrolled(data []byte) []HyprlandClient {
	if len(data) == 0 {
		return nil
	}

	// Find all newline positions with 4x unrolling
	newlines := make([]int, 0, 64)
	i := 0
	for ; i+3 < len(data); i += 4 {
		if data[i] == '\n' {
			newlines = append(newlines, i)
		}
		if data[i+1] == '\n' {
			newlines = append(newlines, i+1)
		}
		if data[i+2] == '\n' {
			newlines = append(newlines, i+2)
		}
		if data[i+3] == '\n' {
			newlines = append(newlines, i+3)
		}
	}
	for ; i < len(data); i++ {
		if data[i] == '\n' {
			newlines = append(newlines, i)
		}
	}
	newlines = append(newlines, len(data))

	clients := make([]HyprlandClient, 0, 8)
	idx := -1
	lineStart := 0

	for _, lineEnd := range newlines {
		line := data[lineStart:lineEnd]
		lineStart = lineEnd + 1

		if len(line) == 0 {
			continue
		}

		if len(line) >= 7 && line[0] == 'W' && line[6] == ' ' {
			clients = append(clients, HyprlandClient{})
			idx = len(clients) - 1

			rest := line[7:]
			for j := 0; j < len(rest)-3; j++ {
				if rest[j] == ' ' && rest[j+1] == '-' && rest[j+2] == '>' {
					clients[idx].Address = "0x" + unsafeString(rest[:j])
					title := rest[j+4:]
					if len(title) > 0 && title[len(title)-1] == ':' {
						title = title[:len(title)-1]
					}
					clients[idx].Title = unsafeString(title)
					break
				}
			}
			continue
		}

		if idx < 0 {
			continue
		}

		for len(line) > 0 && (line[0] == '\t' || line[0] == ' ') {
			line = line[1:]
		}

		if len(line) >= 7 && line[0] == 'c' && line[5] == ':' {
			clients[idx].Class = unsafeString(line[7:])
		} else if len(line) >= 11 && line[0] == 'w' && line[9] == ':' {
			ws := line[11:]
			for j := 0; j < len(ws); j++ {
				if ws[j] == '(' {
					clients[idx].Workspace.ID = atoiBytes(ws[:j-1])
					for k := len(ws) - 1; k > j; k-- {
						if ws[k] == ')' {
							clients[idx].Workspace.Name = unsafeString(ws[j+1 : k])
							break
						}
					}
					break
				}
			}
		}
	}

	return clients
}

// ============================================================================
// VERSION 7: Minimal (only extract what we need, skip everything else)
// ============================================================================

func parseV7Minimal(data []byte) []HyprlandClient {
	if len(data) == 0 {
		return nil
	}

	clients := make([]HyprlandClient, 0, 8)
	idx := -1

	i := 0
	for i < len(data) {
		// Find next line
		lineEnd := i
		for lineEnd < len(data) && data[lineEnd] != '\n' {
			lineEnd++
		}
		line := data[i:lineEnd]
		i = lineEnd + 1

		if len(line) < 7 {
			continue
		}

		// Check first byte to decide what to do
		switch line[0] {
		case 'W': // Window
			if line[6] == ' ' {
				clients = append(clients, HyprlandClient{})
				idx = len(clients) - 1
				// Quick parse address only (we need it for hyprctl commands)
				rest := line[7:]
				for j := 0; j < len(rest)-3; j++ {
					if rest[j] == ' ' && rest[j+1] == '-' {
						clients[idx].Address = "0x" + unsafeString(rest[:j])
						break
					}
				}
			}
		case '\t', ' ': // Indented field
			if idx < 0 {
				continue
			}
			// Skip whitespace
			for len(line) > 0 && (line[0] == '\t' || line[0] == ' ') {
				line = line[1:]
			}
			if len(line) < 7 {
				continue
			}
			switch line[0] {
			case 'c': // class
				if line[5] == ':' {
					clients[idx].Class = unsafeString(line[7:])
				}
			case 'w': // workspace
				if len(line) >= 11 && line[9] == ':' {
					ws := line[11:]
					for j := 0; j < len(ws); j++ {
						if ws[j] == '(' {
							clients[idx].Workspace.ID = atoiBytes(ws[:j-1])
							for k := len(ws) - 1; k > j; k-- {
								if ws[k] == ')' {
									clients[idx].Workspace.Name = unsafeString(ws[j+1 : k])
									break
								}
							}
							break
						}
					}
				}
			}
		}
	}

	return clients
}
