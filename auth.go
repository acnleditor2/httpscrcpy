package main

import "net/http"

type User struct {
	Password       string   `json:"password"`
	AllowedPorts   []int    `json:"allowedPorts"`
	AllowedScripts []string `json:"allowedScripts"`
	ScriptOnly     bool     `json:"scriptOnly"`
}

func portAllowedForUser(port int, username string) bool {
	user := config.Users[username]

	for _, p := range user.AllowedPorts {
		if p == port {
			return true
		}
	}

	return false
}

func auth(w http.ResponseWriter, req *http.Request) (string, *User) {
	query := req.URL.Query()

	if query.Has("username") && query.Has("password") {
		username := query.Get("username")
		var ok bool

		user, ok := config.Users[username]
		if ok && query.Get("password") == user.Password {
			return username, &user
		}

		w.WriteHeader(http.StatusUnauthorized)
	} else if query.Has("username") || query.Has("password") {
		w.WriteHeader(http.StatusBadRequest)
	} else {
		var username string
		var password string
		var ok bool

		username, password, ok = req.BasicAuth()
		if ok {
			user, ok := config.Users[username]
			if ok && password == user.Password {
				return username, &user
			}
		}

		w.Header().Set("WWW-Authenticate", "Basic")
		w.WriteHeader(http.StatusUnauthorized)
	}

	return "", nil
}
