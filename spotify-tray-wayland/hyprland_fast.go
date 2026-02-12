package main

import (
	"sync"
	"unsafe"
)

// Pool for reusing client slices
var clientPool = sync.Pool{
	New: func() any {
		clients := make([]HyprlandClient, 0, 16)
		return &clients
	},
}

// GetClients returns a pooled slice - call PutClients when done
func GetClients() *[]HyprlandClient {
	clients := clientPool.Get().(*[]HyprlandClient)
	*clients = (*clients)[:0]
	return clients
}

// PutClients returns the slice to the pool
func PutClients(clients *[]HyprlandClient) {
	clientPool.Put(clients)
}

// bytesToString zero-copy conversion (Go 1.20+)
// WARNING: result is only valid while data slice is valid
func bytesToString(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return unsafe.String(unsafe.SliceData(b), len(b))
}

// parseHyprctlClientsFast parses hyprctl clients output with minimal allocations
// Returns slices into the original data - do not modify data while using result
func parseHyprctlClientsFast(data []byte) []HyprlandClient {
	if len(data) == 0 {
		return nil
	}

	// Pre-count windows for slice capacity (avoid reallocations)
	windowCount := 0
	for i := 0; i < len(data)-6; i++ {
		if data[i] == 'W' && data[i+1] == 'i' && data[i+2] == 'n' &&
			data[i+3] == 'd' && data[i+4] == 'o' && data[i+5] == 'w' && data[i+6] == ' ' {
			windowCount++
		}
	}

	clients := make([]HyprlandClient, 0, windowCount)
	clientIdx := -1

	// Direct byte scanning - no bufio.Scanner overhead
	lineStart := 0
	for i := 0; i <= len(data); i++ {
		// Find line end
		if i < len(data) && data[i] != '\n' {
			continue
		}

		// Process line [lineStart:i]
		line := data[lineStart:i]
		lineStart = i + 1

		if len(line) == 0 {
			continue
		}

		// Check for "Window " prefix (7 chars)
		if len(line) >= 7 && line[0] == 'W' && line[1] == 'i' && line[2] == 'n' &&
			line[3] == 'd' && line[4] == 'o' && line[5] == 'w' && line[6] == ' ' {

			// Append new client and track index
			clients = append(clients, HyprlandClient{})
			clientIdx = len(clients) - 1

			// Parse: "Window 560770936780 -> title:"
			rest := line[7:] // Skip "Window "

			// Find " -> " separator
			arrowIdx := -1
			for j := 0; j < len(rest)-3; j++ {
				if rest[j] == ' ' && rest[j+1] == '-' && rest[j+2] == '>' && rest[j+3] == ' ' {
					arrowIdx = j
					break
				}
			}

			if arrowIdx >= 0 {
				// Address is before " -> ", add "0x" prefix
				clients[clientIdx].Address = "0x" + bytesToString(rest[:arrowIdx])
				// Title is after " -> ", trim trailing ":"
				title := rest[arrowIdx+4:]
				if len(title) > 0 && title[len(title)-1] == ':' {
					title = title[:len(title)-1]
				}
				clients[clientIdx].Title = bytesToString(title)
			}
			continue
		}

		if clientIdx < 0 {
			continue
		}

		// Skip leading whitespace (tab or space)
		trimmed := line
		for len(trimmed) > 0 && (trimmed[0] == '\t' || trimmed[0] == ' ') {
			trimmed = trimmed[1:]
		}

		// Fast prefix matching using first character + length check
		if len(trimmed) >= 7 && trimmed[0] == 'c' && trimmed[1] == 'l' &&
			trimmed[2] == 'a' && trimmed[3] == 's' && trimmed[4] == 's' && trimmed[5] == ':' && trimmed[6] == ' ' {
			// "class: "
			clients[clientIdx].Class = bytesToString(trimmed[7:])
		} else if len(trimmed) >= 11 && trimmed[0] == 'w' && trimmed[1] == 'o' &&
			trimmed[2] == 'r' && trimmed[3] == 'k' && trimmed[4] == 's' && trimmed[5] == 'p' &&
			trimmed[6] == 'a' && trimmed[7] == 'c' && trimmed[8] == 'e' && trimmed[9] == ':' && trimmed[10] == ' ' {
			// "workspace: "
			ws := trimmed[11:]
			// Parse: "-98 (special:scratchpad)" or "1 (1)"
			parenIdx := -1
			for j := 0; j < len(ws); j++ {
				if ws[j] == '(' {
					parenIdx = j
					break
				}
			}
			if parenIdx > 0 {
				// Parse ID - everything before the space before paren
				clients[clientIdx].Workspace.ID = atoi(ws[:parenIdx-1])

				// Extract name from parentheses
				if parenIdx+1 < len(ws) {
					closeIdx := len(ws) - 1
					for closeIdx > parenIdx && ws[closeIdx] != ')' {
						closeIdx--
					}
					if closeIdx > parenIdx+1 {
						clients[clientIdx].Workspace.Name = bytesToString(ws[parenIdx+1 : closeIdx])
					}
				}
			}
		}
	}

	return clients
}

// atoi parses integer from bytes without allocation
func atoi(b []byte) int {
	if len(b) == 0 {
		return 0
	}

	neg := false
	i := 0

	if b[0] == '-' {
		neg = true
		i = 1
	} else if b[0] == '+' {
		i = 1
	}

	n := 0
	for ; i < len(b); i++ {
		if b[i] < '0' || b[i] > '9' {
			break
		}
		n = n*10 + int(b[i]-'0')
	}

	if neg {
		return -n
	}
	return n
}
