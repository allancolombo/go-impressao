//go:build windows

package services

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unicode"
)

type MachineIdentity struct {
	Hostname          string `json:"hostname"`
	MotherboardSerial string `json:"motherboard_serial"`
}

var motherboardSerialMu sync.Mutex
var motherboardSerialCached string
var motherboardSerialCachedAt time.Time

func GetMachineIdentity() (MachineIdentity, error) {
	host, _ := os.Hostname()
	serial, err := GetMotherboardSerial()
	if err != nil {
		return MachineIdentity{Hostname: strings.TrimSpace(host)}, err
	}
	return MachineIdentity{
		Hostname:          strings.TrimSpace(host),
		MotherboardSerial: serial,
	}, nil
}

func GetMotherboardSerial() (string, error) {
	motherboardSerialMu.Lock()
	cached := motherboardSerialCached
	cachedAt := motherboardSerialCachedAt
	motherboardSerialMu.Unlock()

	if strings.TrimSpace(cached) != "" && time.Since(cachedAt) < 5*time.Minute {
		return strings.TrimSpace(cached), nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	serial, err := readMotherboardSerial(ctx)
	if err != nil {
		return "", err
	}
	serial = normalizeMotherboardSerial(serial)
	if serial == "" {
		return "", errors.New("serial da placa-mãe não encontrado")
	}

	motherboardSerialMu.Lock()
	motherboardSerialCached = serial
	motherboardSerialCachedAt = time.Now()
	motherboardSerialMu.Unlock()
	return serial, nil
}

func readMotherboardSerial(ctx context.Context) (string, error) {
	candidates := []struct {
		name string
		cmd  *exec.Cmd
	}{
		{
			name: "powershell-cim",
			cmd:  exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", "(Get-CimInstance -ClassName Win32_BaseBoard | Select-Object -ExpandProperty SerialNumber) -join \"`n\""),
		},
		{
			name: "powershell-wmi",
			cmd:  exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", "(Get-WmiObject -Class Win32_BaseBoard | Select-Object -ExpandProperty SerialNumber) -join \"`n\""),
		},
		{
			name: "wmic",
			cmd:  exec.CommandContext(ctx, "wmic", "baseboard", "get", "serialnumber"),
		},
	}

	var lastErr error
	for _, c := range candidates {
		out, err := c.cmd.CombinedOutput()
		if err != nil {
			lastErr = err
			continue
		}
		s := strings.TrimSpace(string(out))
		if s == "" {
			lastErr = errors.New("resposta vazia")
			continue
		}
		serial := pickFirstSerialCandidate(s)
		if normalizeMotherboardSerial(serial) != "" {
			return serial, nil
		}
		lastErr = errors.New("serial vazio")
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", errors.New("serial vazio")
}

func pickFirstSerialCandidate(out string) string {
	out = strings.ReplaceAll(out, "\r\n", "\n")
	out = strings.ReplaceAll(out, "\r", "\n")
	lines := strings.Split(out, "\n")
	for _, line := range lines {
		l := strings.TrimSpace(line)
		l = strings.Trim(l, "\u0000\t ")
		if l == "" {
			continue
		}
		if strings.EqualFold(l, "SerialNumber") {
			continue
		}
		return l
	}
	return ""
}

func normalizeMotherboardSerial(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1
	}, s)
}
