package main

import (
	"iter"
)

// ============================================================================
// V8-V11: Advanced parser implementations for benchmarking
// Production code uses hyprland.go (V12 socket + integrated V11 search)
// ============================================================================

// V8: Stack Allocation
const maxStackClients = 32

type ParseResult struct {
	Clients [maxStackClients]HyprlandClient
	Count   int
}

func parseV8Stack(data []byte) ParseResult {
	var result ParseResult
	if len(data) == 0 {
		return result
	}

	lineStart := 0
	for i := 0; i <= len(data); i++ {
		if i < len(data) && data[i] != '\n' {
			continue
		}
		line := data[lineStart:i]
		lineStart = i + 1

		if len(line) < 7 {
			continue
		}

		switch line[0] {
		case 'W':
			if line[6] == ' ' && result.Count < maxStackClients {
				result.Count++
				idx := result.Count - 1
				rest := line[7:]
				for j := 0; j < len(rest)-3; j++ {
					if rest[j] == ' ' && rest[j+1] == '-' && rest[j+2] == '>' {
						result.Clients[idx].Address = "0x" + string(rest[:j])
						break
					}
				}
			}
		case '\t', ' ':
			if result.Count == 0 {
				continue
			}
			idx := result.Count - 1
			for len(line) > 0 && (line[0] == '\t' || line[0] == ' ') {
				line = line[1:]
			}
			if len(line) < 7 {
				continue
			}
			switch line[0] {
			case 'c':
				if line[5] == ':' {
					result.Clients[idx].Class = string(line[7:])
				}
			case 'w':
				if len(line) >= 11 && line[9] == ':' {
					ws := line[11:]
					for j := 0; j < len(ws); j++ {
						if ws[j] == '(' {
							result.Clients[idx].Workspace.ID = parseIntFast(ws[:j-1])
							for k := len(ws) - 1; k > j; k-- {
								if ws[k] == ')' {
									result.Clients[idx].Workspace.Name = string(ws[j+1 : k])
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
	return result
}

// V9: Iterator
func parseV9Iterator(data []byte) iter.Seq[HyprlandClient] {
	return func(yield func(HyprlandClient) bool) {
		if len(data) == 0 {
			return
		}
		var current HyprlandClient
		hasClient := false
		lineStart := 0

		for i := 0; i <= len(data); i++ {
			if i < len(data) && data[i] != '\n' {
				continue
			}
			line := data[lineStart:i]
			lineStart = i + 1

			if len(line) < 7 {
				continue
			}

			switch line[0] {
			case 'W':
				if line[6] == ' ' {
					if hasClient {
						if !yield(current) {
							return
						}
					}
					current = HyprlandClient{}
					hasClient = true
					rest := line[7:]
					for j := 0; j < len(rest)-3; j++ {
						if rest[j] == ' ' && rest[j+1] == '-' && rest[j+2] == '>' {
							current.Address = "0x" + string(rest[:j])
							break
						}
					}
				}
			case '\t', ' ':
				if !hasClient {
					continue
				}
				for len(line) > 0 && (line[0] == '\t' || line[0] == ' ') {
					line = line[1:]
				}
				if len(line) < 7 {
					continue
				}
				switch line[0] {
				case 'c':
					if line[5] == ':' {
						current.Class = string(line[7:])
					}
				case 'w':
					if len(line) >= 11 && line[9] == ':' {
						ws := line[11:]
						for j := 0; j < len(ws); j++ {
							if ws[j] == '(' {
								current.Workspace.ID = parseIntFast(ws[:j-1])
								for k := len(ws) - 1; k > j; k-- {
									if ws[k] == ')' {
										current.Workspace.Name = string(ws[j+1 : k])
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
		if hasClient {
			yield(current)
		}
	}
}

func FindSpotify(data []byte) (HyprlandClient, bool) {
	for client := range parseV9Iterator(data) {
		if len(client.Class) == 7 &&
			(client.Class[0]|0x20) == 's' &&
			(client.Class[1]|0x20) == 'p' &&
			(client.Class[2]|0x20) == 'o' &&
			(client.Class[3]|0x20) == 't' &&
			(client.Class[4]|0x20) == 'i' &&
			(client.Class[5]|0x20) == 'f' &&
			(client.Class[6]|0x20) == 'y' {
			return client, true
		}
	}
	return HyprlandClient{}, false
}

// V10: SWAR
func parseV10SWAR(data []byte) []HyprlandClient {
	if len(data) == 0 {
		return nil
	}
	// Find newlines
	newlines := make([]int, 0, 64)
	for i := 0; i < len(data); i++ {
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

		if len(line) < 7 {
			continue
		}

		switch line[0] {
		case 'W':
			if line[6] == ' ' {
				clients = append(clients, HyprlandClient{})
				idx = len(clients) - 1
				rest := line[7:]
				for j := 0; j < len(rest)-3; j++ {
					if rest[j] == ' ' && rest[j+1] == '-' && rest[j+2] == '>' {
						clients[idx].Address = "0x" + string(rest[:j])
						break
					}
				}
			}
		case '\t', ' ':
			if idx < 0 {
				continue
			}
			for len(line) > 0 && (line[0] == '\t' || line[0] == ' ') {
				line = line[1:]
			}
			if len(line) < 7 {
				continue
			}
			switch line[0] {
			case 'c':
				if line[5] == ':' {
					clients[idx].Class = string(line[7:])
				}
			case 'w':
				if len(line) >= 11 && line[9] == ':' {
					ws := line[11:]
					for j := 0; j < len(ws); j++ {
						if ws[j] == '(' {
							clients[idx].Workspace.ID = parseIntFast(ws[:j-1])
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
		}
	}
	return clients
}

// V11: Direct Spotify search
func parseV11SpotifyOnly(data []byte) (HyprlandClient, bool) {
	for i := 0; i < len(data)-14; i++ {
		if data[i] == 'c' && data[i+1] == 'l' && data[i+2] == 'a' &&
			data[i+3] == 's' && data[i+4] == 's' && data[i+5] == ':' && data[i+6] == ' ' {
			name := data[i+7:]
			if len(name) >= 7 &&
				(name[0]|0x20) == 's' && (name[1]|0x20) == 'p' && (name[2]|0x20) == 'o' &&
				(name[3]|0x20) == 't' && (name[4]|0x20) == 'i' && (name[5]|0x20) == 'f' && (name[6]|0x20) == 'y' {
				return parseV11SpotifyWindow(data, i)
			}
		}
	}
	return HyprlandClient{}, false
}

func parseV11SpotifyWindow(data []byte, classPos int) (HyprlandClient, bool) {
	var client HyprlandClient
	client.Class = "Spotify"

	for i := classPos; i >= 0; i-- {
		isLineStart := i == 0 || data[i-1] == '\n'
		if isLineStart && i+7 < len(data) && data[i] == 'W' && data[i+6] == ' ' {
			lineEnd := i
			for lineEnd < len(data) && data[lineEnd] != '\n' {
				lineEnd++
			}
			line := data[i:lineEnd]
			if len(line) >= 7 {
				rest := line[7:]
				for j := 0; j < len(rest)-3; j++ {
					if rest[j] == ' ' && rest[j+1] == '-' && rest[j+2] == '>' {
						client.Address = "0x" + string(rest[:j])
						break
					}
				}
			}

			for k := i; k < classPos && k < len(data)-11; k++ {
				if data[k] == 'w' && data[k+1] == 'o' && data[k+9] == ':' && data[k+10] == ' ' {
					ws := data[k+11:]
					lineEnd := 0
					for lineEnd < len(ws) && ws[lineEnd] != '\n' {
						lineEnd++
					}
					ws = ws[:lineEnd]
					for j := 0; j < len(ws); j++ {
						if ws[j] == '(' {
							client.Workspace.ID = parseIntFast(ws[:j-1])
							for m := len(ws) - 1; m > j; m-- {
								if ws[m] == ')' {
									client.Workspace.Name = string(ws[j+1 : m])
									break
								}
							}
							break
						}
					}
					break
				}
			}
			break
		}
	}
	return client, client.Address != ""
}
