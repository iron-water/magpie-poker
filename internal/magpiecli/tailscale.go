package magpiecli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type TailscaleStatus struct {
	Installed bool   `json:"installed"`
	Name      string `json:"name"`
	DNS       string `json:"dns"`
	IP        string `json:"ip"`
	IPv6      string `json:"ipv6"`
	Online    bool   `json:"online"`
	Suffix    string `json:"suffix"`
	Error     string `json:"error,omitempty"`
}

func tailscaleBin() string {
	cands := []string{
		os.Getenv("TAILSCALE_BIN"),
		`C:\Program Files\Tailscale\tailscale.exe`,
		`C:\Program Files (x86)\Tailscale\tailscale.exe`,
		"/usr/bin/tailscale",
		"/usr/local/bin/tailscale",
		"/Applications/Tailscale.app/Contents/MacOS/Tailscale",
	}
	for _, c := range cands {
		if c == "" {
			continue
		}
		if strings.ContainsAny(c, `/\`) {
			if st, err := os.Stat(c); err == nil && !st.IsDir() {
				return c
			}
		}
	}
	if p, err := exec.LookPath("tailscale"); err == nil {
		return p
	}
	return "tailscale"
}

func tsRun(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, tailscaleBin(), args...)
	hide(cmd)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New(msg)
	}
	return out.String(), nil
}

func TailscaleStatusNow() TailscaleStatus {
	out, err := tsRun("status", "--json")
	if err != nil {
		return TailscaleStatus{Error: err.Error()}
	}
	var j struct {
		Self struct {
			HostName     string   `json:"HostName"`
			DNSName      string   `json:"DNSName"`
			TailscaleIPs []string `json:"TailscaleIPs"`
			Online       bool     `json:"Online"`
		} `json:"Self"`
		MagicDNSSuffix string `json:"MagicDNSSuffix"`
	}
	if err := json.Unmarshal([]byte(out), &j); err != nil {
		return TailscaleStatus{Error: "tailscale status 不是 JSON"}
	}
	st := TailscaleStatus{
		Installed: true,
		Name:      j.Self.HostName,
		DNS:       strings.TrimSuffix(j.Self.DNSName, "."),
		Suffix:    j.MagicDNSSuffix,
		Online:    j.Self.Online,
	}
	for _, ip := range j.Self.TailscaleIPs {
		if strings.Contains(ip, ":") && st.IPv6 == "" {
			st.IPv6 = ip
		} else if st.IP == "" {
			st.IP = ip
		}
	}
	return st
}

func ServeStatusNow() (any, error) {
	out, err := tsRun("serve", "status", "--json")
	if err != nil {
		return nil, err
	}
	var v any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		return nil, errors.New("tailscale serve status 不是 JSON")
	}
	return v, nil
}

func EnableServe(targetPort, httpsPort int) (string, error) {
	if httpsPort <= 0 {
		httpsPort = 8443
	}
	if _, err := tsRun("serve", "--bg", fmt.Sprintf("--https=%d", httpsPort), fmt.Sprintf("http://127.0.0.1:%d", targetPort)); err != nil {
		return "", err
	}
	st := TailscaleStatusNow()
	host := st.DNS
	if host == "" {
		host = st.Name
	}
	if host == "" {
		return "", errors.New("没有 Tailscale DNS 名称（tailnet 可能未启用 HTTPS）")
	}
	return fmt.Sprintf("https://%s:%d", host, httpsPort), nil
}

func LocalGatewayPort() int {
	u, err := url.Parse(GatewayURL())
	if err == nil {
		if p := u.Port(); p != "" {
			if n, err := strconv.Atoi(p); err == nil && n > 0 {
				return n
			}
		}
	}
	return 3425
}
