package main

import (
	"bytes"
	"encoding/binary"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
)

type extensionState struct {
	cmd            *exec.Cmd
	stdin          io.WriteCloser
	stdout         io.ReadCloser
	singleEndpoint bool
	mutex          sync.Mutex
}

var (
	extensionMap         = map[string]*extensionState{}
	endpointExtensionMap = map[string]string{}
)

func extensionRequestHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method == http.MethodOptions {
		w.Header().Set("Allow", "OPTIONS, GET")
		return
	} else if req.Method != http.MethodGet {
		w.Header().Set("Allow", "OPTIONS, GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var username string
	var user *User

	if len(config.Users) > 0 {
		username, user = auth(w, req)
		if user == nil {
			return
		}
		endpoint, ok := endpointMap[req.URL.Path]
		if ok {
			_, ok = endpoint[username]
			if !ok {
				w.WriteHeader(http.StatusForbidden)
				return
			}
		}
	}

	query := req.URL.Query()

	port := getPort(query.Get("port"))

	if port > 0 {
		if len(config.Users) > 0 && !portAllowedForUser(port, username) {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		_, ok := portMap[port]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
	}

	extension := extensionMap[endpointExtensionMap[req.URL.Path]]

	var b bytes.Buffer

	b.WriteByte(0)
	binary.Write(&b, binary.NativeEndian, uint16(port))

	if !extension.singleEndpoint {
		b.WriteByte(byte(len(req.URL.Path)))
		b.WriteString(req.URL.Path)
	}

	binary.Write(&b, binary.NativeEndian, uint32(len(query)))
	for name, values := range query {
		binary.Write(&b, binary.NativeEndian, uint32(len(name)))
		b.WriteString(name)
		binary.Write(&b, binary.NativeEndian, uint32(len(values[0])))
		b.WriteString(values[0])
	}

	binary.Write(&b, binary.NativeEndian, uint32(len(req.Header)))
	for name, values := range req.Header {
		binary.Write(&b, binary.NativeEndian, uint32(len(name)))
		b.WriteString(name)
		binary.Write(&b, binary.NativeEndian, uint32(len(values[0])))
		b.WriteString(values[0])
	}

	extension.mutex.Lock()
	defer extension.mutex.Unlock()

	_, err := b.WriteTo(extension.stdin)
	if err != nil {
		return
	}

	data := make([]byte, 3)

	n, err := io.ReadFull(extension.stdout, data)
	if err != nil {
		return
	}
	if n != 3 {
		return
	}

	status := int(binary.NativeEndian.Uint16(data[:2]))
	headerCount := int(data[2])

	for i := 0; i < headerCount; i++ {
		n, err = io.ReadFull(extension.stdout, data[:1])
		if err != nil {
			return
		}
		if n != 1 || data[0] < 1 {
			return
		}

		data = make([]byte, int(data[0]))

		n, err = io.ReadFull(extension.stdout, data)
		if err != nil {
			return
		}
		if n != len(data) {
			return
		}

		name := string(data)

		n, err = io.ReadFull(extension.stdout, data[:1])
		if err != nil {
			return
		}
		if n != 1 || data[0] < 1 {
			return
		}

		data = make([]byte, int(data[0]))

		n, err = io.ReadFull(extension.stdout, data)
		if err != nil {
			return
		}
		if n != len(data) {
			return
		}

		w.Header().Set(name, string(data))
	}

	w.WriteHeader(status)

	for {
		data = make([]byte, 4)

		n, err = io.ReadFull(extension.stdout, data)
		if err != nil {
			return
		}
		if n != 4 {
			return
		}

		size := int(binary.NativeEndian.Uint32(data))

		if size == 0 {
			break
		}

		data = make([]byte, size)

		n, err = io.ReadFull(extension.stdout, data)
		if err != nil {
			return
		}
		if n != size {
			return
		}

		w.Write(data)
		w.(http.Flusher).Flush()
	}

	data = make([]byte, 1)

	n, err = io.ReadFull(extension.stdout, data)
	if err != nil {
		return
	}
	if n != 1 {
		return
	}

	commands := make([][]string, int(data[0]))

	for i := 0; i < len(commands); i++ {
		n, err = io.ReadFull(extension.stdout, data[:1])
		if err != nil {
			return
		}
		if n != 1 || data[0] < 1 {
			return
		}

		commands[i] = make([]string, int(data[0]))

		for j := 0; j < len(commands[i]); j++ {
			n, err = io.ReadFull(extension.stdout, data[:1])
			if err != nil {
				return
			}
			if n != 1 || data[0] < 0 {
				return
			}

			data = make([]byte, int(data[0]))

			n, err = io.ReadFull(extension.stdout, data)
			if err != nil {
				return
			}
			if n != len(data) {
				return
			}

			if j == 0 {
				commands[i][j] = strings.ToLower(string(data))
			} else {
				commands[i][j] = string(data)
			}
		}
	}

	if len(commands) > 0 {
		go func() {
			ps, ok := portMap[port]

			if !ok {
				return
			}

			if !ps.control {
				return
			}

			for _, c := range commands {
				if ps.controlSocket == nil && c[0] != "connect" && c[0] != "disconnect" && c[0] != "sleep" {
					return
				}

				if runCommand(ps, port, c) != http.StatusNoContent {
					break
				}
			}
		}()
	}
}

func loadExtensions() {
	for _, extension := range config.Extensions {
		cmd := exec.Command(extension[0], extension[1:]...)
		cmd.Stderr = os.Stderr

		stdin, err := cmd.StdinPipe()
		if err != nil {
			panic(err)
		}

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			panic(err)
		}

		err = cmd.Start()
		if err != nil {
			panic(err)
		}

		data := make([]byte, 1)

		n, err := io.ReadFull(stdout, data)
		if err != nil {
			panic(err)
		}
		if n != 1 || len(data) < 1 {
			cmd.Process.Kill()
			cmd.Wait()
			os.Exit(1)
		}

		data = make([]byte, int(data[0]))

		n, err = io.ReadFull(stdout, data)
		if err != nil {
			panic(err)
		}
		if n != len(data) {
			cmd.Process.Kill()
			cmd.Wait()
			os.Exit(1)
		}

		extensionId := string(data)

		{
			_, ok := extensionMap[extensionId]
			if ok {
				cmd.Process.Kill()
				cmd.Wait()
				os.Exit(1)
			}
		}

		data = make([]byte, 1)

		n, err = io.ReadFull(stdout, data)
		if err != nil {
			panic(err)
		}
		if n != 1 {
			cmd.Process.Kill()
			cmd.Wait()
			os.Exit(1)
		}

		endpointCount := int(data[0])

		if endpointCount < 1 {
			cmd.Process.Kill()
			cmd.Wait()
			os.Exit(1)
		}

		for i := 0; i < endpointCount; i++ {
			data = make([]byte, 1)

			n, err = io.ReadFull(stdout, data)
			if err != nil {
				panic(err)
			}
			if n != 1 || data[0] < 2 {
				cmd.Process.Kill()
				cmd.Wait()
				os.Exit(1)
			}

			data = make([]byte, int(data[0]))

			n, err = io.ReadFull(stdout, data)
			if err != nil {
				panic(err)
			}
			if n != len(data) {
				cmd.Process.Kill()
				cmd.Wait()
				os.Exit(1)
			}

			s := strings.TrimSpace(string(data))

			if !strings.HasPrefix(s, "/") || strings.HasSuffix(s, "/") {
				cmd.Process.Kill()
				cmd.Wait()
				os.Exit(1)
			}

			endpoint, ok := endpointMap[s]
			if !ok || (len(config.Users) > 0 && len(endpoint) > 0) {
				http.HandleFunc(s, extensionRequestHandler)
				endpointExtensionMap[s] = extensionId
			}
		}

		extensionMap[extensionId] = &extensionState{
			cmd:            cmd,
			stdin:          stdin,
			stdout:         stdout,
			singleEndpoint: endpointCount == 1,
		}
	}

	for port, ps := range portMap {
		if ps.video && ps.videoExtension != "" {
			_, ok := extensionMap[ps.videoExtension]
			if ok {
				go sendVideoToExtension(port)
			}
		}

		if ps.audio && ps.audioExtension != "" {
			_, ok := extensionMap[ps.audioExtension]
			if ok {
				go sendAudioToExtension(port)
			}
		}

		if ps.control && ps.clipboardStreamExtension != "" {
			_, ok := extensionMap[ps.clipboardStreamExtension]
			if ok {
				go sendClipboardToExtension(port)
			}
		}
	}
}
