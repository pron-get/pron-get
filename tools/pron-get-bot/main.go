// Command pron-get-bot acts on GitHub as the pron-get-bot App, so pushes,
// issues and pull requests are attributed to pron-get-bot[bot] rather than
// to a person's account.
//
//	pron-get-bot token            print an installation token (valid 1 hour)
//	pron-get-bot gh <args...>     run gh with that token
//	pron-get-bot git-credential get
//	                              git credential helper for github.com
//
// Setup (once per clone):
//
//	git config remote.origin.pushurl https://github.com/pron-get/<repo>.git
//	git config credential.https://github.com.helper ''
//	git config --add credential.https://github.com.helper '!pron-get-bot git-credential'
//
// The App's private key is read from ~/.config/pron-get-bot/key.pem. To act
// as a different App, set PRON_GET_BOT_CLIENT_ID and PRON_GET_BOT_KEY (the
// path to its .pem file).
//
// Install with: go install github.com/pron-get/pron-get/tools/pron-get-bot@main
package main

import (
	"bufio"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultClientID = "Iv23liO7uMUEntSsX0cF" // App ID 5685919
	org             = "pron-get"
	api             = "https://api.github.com"
)

func clientID() string {
	if id := os.Getenv("PRON_GET_BOT_CLIENT_ID"); id != "" {
		return id
	}
	return defaultClientID
}

func keyPath() (string, error) {
	if p := os.Getenv("PRON_GET_BOT_KEY"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "pron-get-bot", "key.pem"), nil
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "token":
		tok, err := token()
		check(err)
		fmt.Println(tok)
	case "gh":
		tok, err := token()
		check(err)
		cmd := exec.Command("gh", os.Args[2:]...)
		cmd.Env = append(os.Environ(), "GH_TOKEN="+tok)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				os.Exit(ee.ExitCode())
			}
			check(err)
		}
	case "git-credential":
		if len(os.Args) < 3 || os.Args[2] != "get" {
			return // store/erase: nothing to do
		}
		in := map[string]string{}
		sc := bufio.NewScanner(os.Stdin)
		for sc.Scan() {
			if k, v, ok := strings.Cut(sc.Text(), "="); ok {
				in[k] = v
			}
		}
		if in["host"] != "github.com" {
			return
		}
		tok, err := token()
		check(err)
		fmt.Printf("username=x-access-token\npassword=%s\n", tok)
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: pron-get-bot token | gh <args...> | git-credential get")
	os.Exit(2)
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "pron-get-bot:", err)
		os.Exit(1)
	}
}

// token exchanges a JWT signed with the App key for an installation token.
func token() (string, error) {
	jwt, err := appJWT()
	if err != nil {
		return "", err
	}
	var inst struct {
		ID int64 `json:"id"`
	}
	if err := call("GET", "/orgs/"+org+"/installation", jwt, &inst); err != nil {
		return "", fmt.Errorf("finding the %s installation: %w", org, err)
	}
	var tok struct {
		Token string `json:"token"`
	}
	if err := call("POST", fmt.Sprintf("/app/installations/%d/access_tokens", inst.ID), jwt, &tok); err != nil {
		return "", err
	}
	return tok.Token, nil
}

func call(method, path, jwt string, v any) error {
	req, err := http.NewRequest(method, api+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		var e struct {
			Message string `json:"message"`
		}
		json.NewDecoder(resp.Body).Decode(&e)
		return fmt.Errorf("%s %s: %s %s", method, path, resp.Status, e.Message)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// appJWT returns a 9-minute RS256 JWT identifying the App.
func appJWT() (string, error) {
	path, err := keyPath()
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	blk, _ := pem.Decode(b)
	if blk == nil {
		return "", errors.New("key.pem: no PEM block")
	}
	var key *rsa.PrivateKey
	if key, err = x509.ParsePKCS1PrivateKey(blk.Bytes); err != nil {
		k, err2 := x509.ParsePKCS8PrivateKey(blk.Bytes)
		var ok bool
		if key, ok = k.(*rsa.PrivateKey); err2 != nil || !ok {
			return "", fmt.Errorf("key.pem: %w", err)
		}
	}
	now := time.Now().Unix()
	enc := base64.RawURLEncoding
	header := enc.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, _ := json.Marshal(map[string]any{"iat": now - 60, "exp": now + 540, "iss": clientID()})
	unsigned := header + "." + enc.EncodeToString(claims)
	sum := sha256.Sum256([]byte(unsigned))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return unsigned + "." + enc.EncodeToString(sig), nil
}
