package main

import (
	"bytes"
	"encoding/base64"
	"encoding/csv"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"
)

var (
	scriptMessageChannelMap = map[string]chan string{}
	cachedScriptTemplateMap = map[string]*template.Template{}
)

func scriptAllowedForUser(name string, username string) bool {
	user := config.Users[username]

	for _, script := range user.AllowedScripts {
		if script == name {
			return true
		}
	}

	return false
}

func getScriptTemplate(scriptName string) (*template.Template, error) {
	script := config.Scripts[scriptName]
	var scriptBytes []byte
	var err error

	if strings.HasPrefix(script, "http://") || strings.HasPrefix(script, "https://") {
		scriptResponse, err := http.Get(script)
		if err != nil {
			return nil, err
		}

		defer scriptResponse.Body.Close()

		if scriptResponse.StatusCode != http.StatusOK {
			return nil, nil
		}

		scriptBytes, err = io.ReadAll(scriptResponse.Body)
		if err != nil {
			return nil, err
		}
	} else {
		scriptBytes, err = os.ReadFile(script)
		if err != nil {
			return nil, err
		}
	}

	return template.New(script).Funcs(template.FuncMap{
		"run": func(c ...string) string {
			out, _ := exec.Command(c[0], c[1:]...).Output()
			return strings.TrimSpace(string(out))
		},
		"httpget": func(url string) string {
			response, err := http.Get(url)
			if err != nil {
				return ""
			}

			defer response.Body.Close()

			responseBodyBytes, err := io.ReadAll(response.Body)
			if err != nil {
				return ""
			}

			return strings.TrimSpace(string(responseBodyBytes))
		},
		"readfile": func(path string) string {
			fileBytes, err := os.ReadFile(path)
			if err != nil {
				return ""
			}

			return strings.TrimSpace(string(fileBytes))
		},
		"getsize": func(path string) int {
			fi, err := os.Stat(path)
			if err != nil {
				return -1
			}

			return int(fi.Size())
		},
		"getwd": func() string {
			wd, err := os.Getwd()
			if err != nil {
				return ""
			}

			return wd
		},
		"getenv": os.Getenv,
		"gettime": func() string {
			return strconv.FormatInt(time.Now().Unix(), 10)
		},
		"atoi":   strconv.Atoi,
		"fpabs":  filepath.Abs,
		"fpbase": filepath.Base,
		"fpdir":  filepath.Dir,
		"fpext":  filepath.Ext,
		"fpglob": func(pattern string) []string {
			matches, _ := filepath.Glob(pattern)
			return matches
		},
		"strtolower":   strings.ToLower,
		"strtoupper":   strings.ToUpper,
		"strtrimspace": strings.TrimSpace,
		"regexpfind": func(r string, s string) (string, error) {
			compiled, err := regexp.Compile(r)
			if err != nil {
				return "", err
			}

			return compiled.FindString(s), nil
		},
		"regexpfindall": func(r string, s string, n int) ([]string, error) {
			compiled, err := regexp.Compile(r)
			if err != nil {
				return nil, err
			}

			return compiled.FindAllString(s, n), nil
		},
		"regexpfindallsubmatches": func(r string, s string, n int) ([][]string, error) {
			compiled, err := regexp.Compile(r)
			if err != nil {
				return nil, err
			}

			return compiled.FindAllStringSubmatch(s, n), nil
		},
		"regexpmatch": regexp.MatchString,
		"regexpreplace": func(r string, s string, repl string) (string, error) {
			compiled, err := regexp.Compile(r)
			if err != nil {
				return "", err
			}

			return compiled.ReplaceAllString(s, repl), nil
		},
		"encode": func(encoding string, s string) string {
			switch encoding {
			case "base64":
				return base64.StdEncoding.EncodeToString([]byte(s))
			case "base64url":
				return base64.URLEncoding.EncodeToString([]byte(s))
			case "hex":
				return hex.EncodeToString([]byte(s))
			case "query":
				return url.QueryEscape(s)
			case "path":
				return url.PathEscape(s)
			default:
				return ""
			}
		},
		"decode": func(encoding string, s string) string {
			switch encoding {
			case "base64":
				decoded, err := base64.StdEncoding.DecodeString(s)
				if err != nil {
					return ""
				}
				return string(decoded)
			case "base64url":
				decoded, err := base64.URLEncoding.DecodeString(s)
				if err != nil {
					return ""
				}
				return string(decoded)
			case "hex":
				decoded, err := hex.DecodeString(s)
				if err != nil {
					return ""
				}
				return string(decoded)
			case "url":
				decoded, err := url.QueryUnescape(s)
				if err != nil {
					return ""
				}
				return decoded
			case "path":
				decoded, err := url.PathUnescape(s)
				if err != nil {
					return ""
				}
				return decoded
			default:
				return ""
			}
		},
		"convert": func(fromEncoding string, toEncoding string, s string) string {
			var decoded []byte
			var err error

			switch fromEncoding {
			case "base64":
				decoded, err = base64.StdEncoding.DecodeString(s)
				if err != nil {
					return ""
				}
			case "base64url":
				decoded, err = base64.URLEncoding.DecodeString(s)
				if err != nil {
					return ""
				}
			case "hex":
				decoded, err = hex.DecodeString(s)
				if err != nil {
					return ""
				}
			default:
				return ""
			}

			switch toEncoding {
			case "base64":
				return base64.StdEncoding.EncodeToString(decoded)
			case "base64url":
				return base64.URLEncoding.EncodeToString(decoded)
			case "hex":
				return hex.EncodeToString(decoded)
			default:
				return ""
			}
		},
	}).Parse(string(scriptBytes))
}

func runScript(name string, username string, port int) int {
	var message string
	var err error

	select {
	case message = <-scriptMessageChannelMap[name]:
	default:
	}

	t, ok := cachedScriptTemplateMap[name]
	if !ok {
		t, err = getScriptTemplate(name)
		if err != nil {
			return -1
		}
		if t == nil {
			return -1
		}
	}

	var b bytes.Buffer

	err = t.Execute(&b, message)
	if err != nil {
		return -1
	}

	r := csv.NewReader(&b)
	r.FieldsPerRecord = -1

	commands, err := r.ReadAll()
	if err != nil {
		return -1
	}

	var ps *portState
	var result int

	if port > 0 {
		ps = portMap[port]
	}

	for _, command := range commands {
		if len(command) < 1 {
			return -1
		}

		command[0] = strings.ToLower(command[0])

		switch command[0] {
		case "port":
			if len(command) == 2 {
				port := getPort(command[1])
				if port == -1 {
					return -1
				}

				if len(config.Users) > 0 && !portAllowedForUser(port, username) {
					return -1
				}

				ps, ok = portMap[port]
				if !ok {
					return -1
				}

				if !ps.control || ps.controlSocket == nil {
					return -1
				}
			} else {
				return -1
			}
		case "run", "runbase64", "runbase64url", "runhex", "runwait", "runbase64wait", "runbase64urlwait", "runhexwait":
			if len(command) > 1 {
				if strings.HasPrefix(command[0], "runbase64url") {
					for i := range command[1:] {
						decoded, err := base64.URLEncoding.DecodeString(command[i+1])
						if err != nil {
							return -1
						}

						command[i+1] = string(decoded)
					}
				} else if strings.HasPrefix(command[0], "runbase64") {
					for i := range command[1:] {
						decoded, err := base64.StdEncoding.DecodeString(command[i+1])
						if err != nil {
							return -1
						}

						command[i+1] = string(decoded)
					}
				} else if strings.HasPrefix(command[0], "runhex") {
					for i := range command[1:] {
						decoded, err := hex.DecodeString(command[i+1])
						if err != nil {
							return -1
						}

						command[i+1] = string(decoded)
					}
				}

				if strings.HasSuffix(command[0], "wait") {
					cmd := exec.Command(command[1], command[2:]...)
					cmd.Stdout = os.Stdout
					cmd.Stderr = os.Stderr
					cmd.Run()
				} else {
					go func(c []string) {
						cmd := exec.Command(c[1], c[2:]...)
						cmd.Stdout = os.Stdout
						cmd.Stderr = os.Stderr
						cmd.Run()
					}(command)
				}
			} else {
				return -1
			}
		case "runscript":
			if len(command) == 2 {
				if len(config.Users) > 0 && !scriptAllowedForUser(command[1], username) {
					return -1
				}

				newPort := runScript(command[1], username, port)
				if newPort == -1 {
					return -1
				} else if port != newPort {
					port = newPort
					ps = portMap[newPort]
				}
			} else if len(command) == 3 {
				if len(config.Users) > 0 && !scriptAllowedForUser(command[1], username) {
					return -1
				}

				d, err := time.ParseDuration(command[2])
				if err != nil {
					return -1
				}

				time.AfterFunc(d, func() {
					runScript(command[1], username, 0)
				})
			} else {
				return -1
			}
		case "httpget", "httpgetwait":
			if len(command) == 2 {
				if command[0] == "httpget" {
					go func(url string) {
						response, err := http.Get(url)
						if err == nil {
							response.Body.Close()
						}
					}(command[1])
				} else {
					response, err := http.Get(command[1])
					if err != nil {
						return -1
					}

					response.Body.Close()
				}
			} else {
				return -1
			}
		case "netsend", "netsendbase64", "netsendbase64url", "netsendhex", "netsendwait", "netsendbase64wait", "netsendbase64urlwait", "netsendhexwait":
			if len(command) == 4 || len(command) == 6 {
				var data []byte
				var err error

				if strings.HasPrefix(command[0], "netsendbase64url") {
					data, err = base64.URLEncoding.DecodeString(command[3])
					if err != nil {
						return -1
					}
				} else if strings.HasPrefix(command[0], "netsendbase64") {
					data, err = base64.StdEncoding.DecodeString(command[3])
					if err != nil {
						return -1
					}
				} else if strings.HasPrefix(command[0], "netsendhex") {
					data, err = hex.DecodeString(command[3])
					if err != nil {
						return -1
					}
				} else {
					data = []byte(command[3])
				}

				var dialTimeout time.Duration

				if len(command) == 6 {
					dialTimeout, err = time.ParseDuration(command[4])
					if err != nil {
						return -1
					}
				} else {
					dialTimeout = 1 * time.Minute
				}

				var writeTimeout time.Duration

				if len(command) == 6 {
					writeTimeout, err = time.ParseDuration(command[5])
					if err != nil {
						return -1
					}
				} else {
					writeTimeout = 1 * time.Minute
				}

				network := command[1]
				address := command[2]

				if strings.HasSuffix(command[0], "wait") {
					conn, err := net.DialTimeout(network, address, dialTimeout)
					if err != nil {
						return -1
					}
					defer conn.Close()

					err = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
					if err != nil {
						return -1
					}

					n, err := conn.Write(data)
					if err != nil {
						return -1
					}
					if n != len(data) {
						return -1
					}
				} else {
					go func() {
						conn, err := net.DialTimeout(network, address, dialTimeout)
						if err != nil {
							return
						}
						defer conn.Close()

						err = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
						if err != nil {
							return
						}

						conn.Write(data)
					}()
				}
			} else {
				return -1
			}
		case "writefile", "writefilebase64", "writefilebase64url", "writefilehex":
			if len(command) == 3 {
				var data []byte
				var err error

				if command[0] == "writefile" {
					data = []byte(command[2])
				} else if command[0] == "writefilebase64" {
					data, err = base64.StdEncoding.DecodeString(command[2])
					if err != nil {
						return -1
					}
				} else if command[0] == "writefilebase64url" {
					data, err = base64.URLEncoding.DecodeString(command[2])
					if err != nil {
						return -1
					}
				} else if command[0] == "writefilehex" {
					data, err = hex.DecodeString(command[2])
					if err != nil {
						return -1
					}
				}

				err = os.WriteFile(command[1], data, 0644)
				if err != nil {
					return -1
				}
			} else {
				return -1
			}
		case "rename":
			if len(command) == 3 {
				err := os.Rename(command[1], command[2])
				if err != nil {
					return -1
				}
			} else {
				return -1
			}
		case "remove":
			if len(command) == 2 {
				err := os.Remove(command[1])
				if err != nil {
					return -1
				}
			} else {
				return -1
			}
		case "removeall":
			if len(command) == 2 {
				err := os.RemoveAll(command[1])
				if err != nil {
					return -1
				}
			} else {
				return -1
			}
		case "mkdirall":
			if len(command) == 2 {
				err := os.MkdirAll(command[1], 0755)
				if err != nil {
					return -1
				}
			} else {
				return -1
			}
		case "sleep":
			if len(command) == 2 {
				duration, err := time.ParseDuration(command[1])
				if err != nil {
					return -1
				}

				time.Sleep(duration)
			} else {
				return -1
			}
		default:
			if ps == nil {
				port := getPort("")
				if port == -1 {
					return -1
				}

				if len(config.Users) > 0 && !portAllowedForUser(port, username) {
					return -1
				}

				ps = portMap[port]

				if !ps.control || (ps.controlSocket == nil && command[0] != "connect" && command[0] != "disconnect" && command[0] != "scriptmessage") {
					return -1
				}
			}

			result = runCommand(ps, port, command)
			if result != http.StatusNoContent {
				return -1
			}
		}
	}

	return port
}

func scriptHandler(w http.ResponseWriter, req *http.Request) {
	origin := req.Header.Get("Origin")

	w.Header().Set("Cache-Control", "no-store")

	switch req.Method {
	case http.MethodOptions:
		if req.Header.Get("Access-Control-Request-Method") == "" {
			w.Header().Set("Allow", "OPTIONS, GET")
		} else if origin != "" {
			requestHeaders := req.Header.Get("Access-Control-Request-Headers")

			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET")

			if requestHeaders != "" {
				w.Header().Set("Access-Control-Allow-Headers", requestHeaders)
			}
		}
	case http.MethodGet:
		if origin != "" {
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		query := req.URL.Query()
		script := query.Get("script")
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
			if !scriptAllowedForUser(script, username) {
				w.WriteHeader(http.StatusForbidden)
				return
			}
		}

		if query.Has("after") {
			d, err := time.ParseDuration(query.Get("after"))
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			time.AfterFunc(d, func() {
				runScript(script, username, 0)
			})
		} else {
			go runScript(script, username, 0)
		}

		w.WriteHeader(http.StatusNoContent)
	default:
		if origin != "" {
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		w.Header().Set("Allow", "OPTIONS, GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func scriptMessageHandler(w http.ResponseWriter, req *http.Request) {
	origin := req.Header.Get("Origin")

	w.Header().Set("Cache-Control", "no-store")

	switch req.Method {
	case http.MethodOptions:
		if req.Header.Get("Access-Control-Request-Method") == "" {
			w.Header().Set("Allow", "OPTIONS, GET")
		} else if origin != "" {
			requestHeaders := req.Header.Get("Access-Control-Request-Headers")

			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET")

			if requestHeaders != "" {
				w.Header().Set("Access-Control-Allow-Headers", requestHeaders)
			}
		}
	case http.MethodGet:
		if origin != "" {
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		query := req.URL.Query()
		script := query.Get("script")

		if len(config.Users) > 0 {
			username, user := auth(w, req)
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
			if !scriptAllowedForUser(script, username) {
				w.WriteHeader(http.StatusForbidden)
				return
			}
		}

		if query.Has("after") {
			after, err := time.ParseDuration(query.Get("after"))
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			time.Sleep(after)
		}

		w.WriteHeader(runCommand(nil, 0, []string{"scriptmessage", script, query.Get("message")}))
	default:
		if origin != "" {
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		w.Header().Set("Allow", "OPTIONS, GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
